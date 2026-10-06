package controller

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func relayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		err = relay.ImageHelper(c, info)
	case relayconstant.RelayModeAudioSpeech:
		fallthrough
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err = relay.AudioHelper(c, info)
	case relayconstant.RelayModeRerank:
		err = relay.RerankHelper(c, info)
	case relayconstant.RelayModeEmbeddings:
		err = relay.EmbeddingHelper(c, info)
	case relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
		err = relay.ResponsesHelper(c, info)
	case relayconstant.RelayModeAlphaSearch:
		err = relay.AlphaSearchHelper(c, info)
	default:
		err = relay.TextHelper(c, info)
	}
	return err
}

func geminiRelayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	if strings.Contains(c.Request.URL.Path, "embed") {
		err = relay.GeminiEmbeddingHandler(c, info)
	} else {
		err = relay.GeminiHelper(c, info)
	}
	return err
}

func Relay(c *gin.Context, relayFormat types.RelayFormat) {

	requestId := c.GetString(common.RequestIdKey)
	//group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	//originalModel := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)

	var (
		newAPIError *types.NewAPIError
		ws          *websocket.Conn
	)

	if relayFormat == types.RelayFormatOpenAIRealtime {
		var err error
		ws, err = upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		defer ws.Close()
	}

	defer func() {
		if newAPIError != nil {
			service.RecordRequestPolicyTermination(c, newAPIError)
			if !c.Writer.Written() && newAPIError.RetryAfter != "" {
				c.Header("Retry-After", newAPIError.RetryAfter)
			}
			logger.LogError(c, fmt.Sprintf("relay error: %s", common.LocalLogPreview(newAPIError.Error())))
			newAPIError.SetMessage(common.MessageWithRequestId(newAPIError.Error(), requestId))
			switch relayFormat {
			case types.RelayFormatOpenAIRealtime:
				_, publicError := kkaiPublicOpenAIError(c, newAPIError)
				helper.WssError(c, ws, publicError)
			case types.RelayFormatClaude:
				status, publicError := kkaiPublicClaudeError(c, newAPIError)
				_ = writeRelayHTTPError(c, relayFormat, status, gin.H{
					"type":  "error",
					"error": publicError,
				})
			default:
				status, publicError := kkaiPublicOpenAIError(c, newAPIError)
				_ = writeRelayHTTPError(c, relayFormat, status, gin.H{
					"error": publicError,
				})
			}
		}
	}()

	request, err := helper.GetAndValidateRequest(c, relayFormat)
	if err != nil {
		// Map "request body too large" to 413 so clients can handle it correctly
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			newAPIError = types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithStatusCode(http.StatusBadRequest), types.ErrOptionWithSkipRetry())
		}
		return
	}

	relayInfo, err := relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}
	defer func() {
		recovered := recover()
		resultErr := newAPIError
		if recovered != nil {
			resultErr = types.NewError(fmt.Errorf("relay panic: %v", recovered), types.ErrorCodeBadResponse)
		}
		if relayFormat != types.RelayFormatOpenAIRealtime {
			perfmetrics.RecordRelayResult(c.Request.Context(), relayInfo, resultErr)
		}
		if recovered != nil {
			panic(recovered)
		}
	}()

	if newAPIError = relay.PrepareRequestBilling(c, relayInfo, func() error { return runImageStudioPreRelayHook(c) }); newAPIError != nil {
		return
	}

	defer func() { newAPIError = relay.RefundFailedRequestBilling(c, relayInfo, newAPIError) }()

	retryParam := &service.RetryParam{
		Ctx:         c,
		TokenGroup:  relayInfo.TokenGroup,
		ModelName:   relayInfo.OriginModelName,
		RequestPath: c.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		relayInfo.StreamStatus = nil
		relayInfo.PerformanceBusinessRejection = false
		relayInfo.PerformanceOutputTokens = 0
		relayInfo.RetryIndex = retryParam.GetRetry()
		relayInfo.ResetAttemptTiming()
		channel, channelErr := getChannel(c, relayInfo, retryParam)
		if channelErr != nil {
			logger.LogError(c, channelErr.Error())
			newAPIError = channelErr
			break
		}

		service.AppendUsedChannel(c, channel.Id)
		if billingErr := service.PrepareTieredBillingForSelectedGroup(c, relayInfo); billingErr != nil {
			newAPIError = billingErr
			break
		}
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			// Ensure consistent 413 for oversized bodies even when error occurs later (e.g., retry path)
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
			} else {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		switch relayFormat {
		case types.RelayFormatOpenAIRealtime:
			newAPIError = relay.WssHelper(c, relayInfo)
		case types.RelayFormatClaude:
			newAPIError = relay.ClaudeHelper(c, relayInfo)
		case types.RelayFormatGemini:
			newAPIError = geminiRelayHandler(c, relayInfo)
		default:
			newAPIError = relayHandler(c, relayInfo)
		}

		if newAPIError == nil {
			service.MarkRequestPolicySuccess(c, relayInfo.StreamStatus)
			relayInfo.LastError = nil
			return
		}

		newAPIError = service.NormalizeViolationFeeError(newAPIError)
		relayInfo.LastError = newAPIError
		if service.IsUpstreamPoolExhausted(newAPIError) {
			retryParam.ExcludedChannelIDs = append(retryParam.ExcludedChannelIDs, channel.Id)
		} else {
			retryParam.PriorityRetry++
		}

		channelError := *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan())
		policyDetected := service.ProcessKKAIPolicyAPIError(c, channelError, newAPIError)
		decision := decideRelayRetry(c, relayInfo, newAPIError, common.RetryTimes-retryParam.GetRetry())
		service.RecordPolicyFailure(c, channel.Id, newAPIError, decision)
		service.ProcessChannelErrorAfterPolicy(c, channelError, newAPIError, relayInfo, policyDetected)

		if decision.Action != "retry" {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}
}

// CountClaudeTokens implements Anthropic's token-counting utility endpoint.
// It deliberately skips upstream generation and billing; callers use this
// endpoint to size prompts before creating a Message.
func CountClaudeTokens(c *gin.Context) {
	request, err := helper.GetAndValidateClaudeRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "invalid_request_error",
				"message": common.MessageWithRequestId(err.Error(), c.GetString(common.RequestIdKey)),
			},
		})
		return
	}

	info := relaycommon.GenRelayInfoClaude(c, request)
	inputTokens, err := service.CountRequestToken(c, request.GetTokenCountMeta(), info)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "api_error",
				"message": common.MessageWithRequestId(err.Error(), c.GetString(common.RequestIdKey)),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"input_tokens": inputTokens})
}

var upgrader = websocket.Upgrader{
	Subprotocols: []string{"realtime", "responses"}, // WS 握手支持的协议，如果有使用 Sec-WebSocket-Protocol，则必须在此声明对应的 Protocol TODO add other protocol
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
}

func getChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) (*model.Channel, *types.NewAPIError) {
	common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "")
	if info.ChannelMeta == nil {
		autoBan := c.GetBool("auto_ban")
		autoBanInt := 1
		if !autoBan {
			autoBanInt = 0
		}
		channel := &model.Channel{
			Id:      c.GetInt("channel_id"),
			Type:    c.GetInt("channel_type"),
			Name:    c.GetString("channel_name"),
			AutoBan: &autoBanInt,
		}
		service.RequestPolicy(c).BeginAttempt(channel, info.UsingGroup)
		return channel, nil
	}
	channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam)

	groupRatioInfo := helper.HandleGroupRatio(c, info)
	if info.ImagePricingSnapshot == nil {
		info.PriceData.GroupRatioInfo = groupRatioInfo
	}

	if err != nil {
		return nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		if len(retryParam.ExcludedChannelIDs) > 0 && service.IsUpstreamPoolExhausted(info.LastError) {
			return nil, info.LastError
		}
		return nil, types.NewError(fmt.Errorf("分组 %s 下模型 %s 的可用渠道不存在（retry）", selectGroup, info.OriginModelName), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	service.RequestPolicy(c).BeginAttempt(channel, selectGroup)
	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName)
	if newAPIError != nil {
		return nil, newAPIError
	}
	return channel, nil
}

func shouldRetry(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError, retryTimes int) bool {
	return decideRelayRetry(c, info, apiErr, retryTimes).Action == "retry"
}

// Decide once, then use the same decision for routing and the admin log.
func decideRelayRetry(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError, retryTimes int) service.PolicyDecision {
	stop := service.PolicyDecision{Action: "stop", Source: "system"}
	if apiErr == nil {
		stop.Reason = "request_completed"
		return stop
	}
	if c.Request != nil && c.Request.Context().Err() != nil {
		stop.Reason = "request_cancelled"
		return stop
	}
	if c.Writer.Written() && (info == nil || info.RelayFormat != types.RelayFormatOpenAIRealtime || info.TargetWs != nil || apiErr.GetErrorCode() != types.ErrorCodeDoRequestFailed) {
		stop.Reason = "response_started"
		return stop
	}
	if service.ShouldSkipRetryAfterKKAIPolicy(c) {
		stop.Reason = "local_rejection"
		return stop
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		stop.Reason, stop.Source = "strict_session", service.RequestPolicy(c).SessionModeSource
		if stop.Source == "" {
			stop.Source = "session_rule"
		}
		return stop
	}
	if service.GetChannelConstraints(c).SuppressesRetry() {
		stop.Reason, stop.Source = "pinned_channel", "channel_constraint"
		return stop
	}
	if _, pinned := c.Get("specific_channel_id"); pinned {
		stop.Reason, stop.Source = "pinned_channel", "channel_constraint"
		return stop
	}
	if types.IsSkipRetryError(apiErr) {
		stop.Reason = "non_retryable_error"
		return stop
	}
	if retryTimes <= 0 {
		stop.Reason, stop.Source = "attempt_budget_exhausted", "global"
		return stop
	}
	if types.IsChannelError(apiErr) {
		return service.PolicyDecision{Action: "retry", Reason: "channel_error", Source: "system"}
	}
	code := apiErr.StatusCode
	if code >= 200 && code < 300 || operation_setting.IsAlwaysSkipRetryCode(apiErr.GetErrorCode()) || operation_setting.IsAlwaysSkipRetryStatusCode(code) {
		stop.Reason = "system_retry_exclusion"
		return stop
	}
	if code < 100 || code > 599 {
		return service.PolicyDecision{Action: "retry", Reason: "unrecognized_status", Source: "system"}
	}
	if operation_setting.ShouldRetryByStatusCode(code) {
		return service.PolicyDecision{Action: "retry", Reason: "retry_status_matched", Source: "global"}
	}
	stop.Reason, stop.Source = "status_not_retryable", "global"
	return stop
}

func RelayMidjourney(c *gin.Context) {
	policy := service.RequestPolicy(c)
	defer func() {
		if policy.Attempts > 0 && !policy.Successful {
			service.RecordRequestPolicyTermination(c, types.NewErrorWithStatusCode(errors.New("Midjourney submission failed"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway, types.ErrOptionWithSkipRetry()))
		}
	}()
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatMjProxy, nil, nil)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"description": fmt.Sprintf("failed to generate relay info: %s", err.Error()),
			"type":        "upstream_error",
			"code":        4,
		})
		return
	}

	var mjErr *dto.MidjourneyResponse
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeMidjourneyNotify:
		mjErr = relay.RelayMidjourneyNotify(c)
	case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		mjErr = relay.RelayMidjourneyTask(c, relayInfo.RelayMode)
	case relayconstant.RelayModeMidjourneyTaskImageSeed:
		mjErr = relay.RelayMidjourneyTaskImageSeed(c)
	case relayconstant.RelayModeSwapFace:
		mjErr = relay.RelaySwapFace(c, relayInfo)
	default:
		mjErr = relay.RelayMidjourneySubmit(c, relayInfo)
	}
	//err = relayMidjourneySubmit(c, relayMode)
	log.Println(mjErr)
	if mjErr != nil {
		policy.Successful = false
		statusCode := http.StatusBadRequest
		if mjErr.Code == 30 {
			mjErr.Result = "当前分组负载已饱和，请稍后再试，或升级账户以提升服务质量。"
			statusCode = http.StatusTooManyRequests
		}
		c.JSON(statusCode, gin.H{
			"description": fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result),
			"type":        "upstream_error",
			"code":        mjErr.Code,
		})
		channelId := c.GetInt("channel_id")
		logger.LogError(c, fmt.Sprintf("relay error (channel #%d, status code %d): %s", channelId, statusCode, fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result)))
	}
}

func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &dto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if taskErr := relay.RelayTaskFetch(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func PreparePlaygroundTaskContext(c *gin.Context) *dto.TaskError {
	if c.GetBool("use_access_token") {
		return service.TaskErrorWrapperLocal(errors.New("playground does not support access tokens"), "access_denied", http.StatusForbidden)
	}

	userID := c.GetInt("id")
	userCache, err := model.GetUserCache(userID)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "query_user_failed", http.StatusInternalServerError)
	}
	userCache.WriteContext(c)
	if c.GetInt("token_id") > 0 {
		return nil
	}

	usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	tempToken := &model.Token{
		UserId: userID,
		Name:   fmt.Sprintf("playground-video-%s", usingGroup),
		Group:  usingGroup,
	}
	if err := middleware.SetupContextForToken(c, tempToken); err != nil {
		return service.TaskErrorWrapperLocal(err, "setup_playground_token_failed", http.StatusInternalServerError)
	}
	return nil
}

func PlaygroundTask(c *gin.Context) {
	if taskErr := PreparePlaygroundTaskContext(c); taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}
	RelayTask(c)
}

func RelayTask(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		respondTaskSubmissionError(c, &dto.TaskError{Code: "gen_relay_info_failed", Message: err.Error(), StatusCode: http.StatusInternalServerError})
		return
	}
	if action := c.GetString("task_action"); action != "" {
		relayInfo.Action = action
	}
	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskSubmissionError(c, taskErr)
		return
	}
	if taskErr := relay.ApplyOriginTaskAffinity(c, relayInfo); taskErr != nil {
		respondTaskSubmissionError(c, taskErr)
		return
	}
	outcome, taskErr := executeTaskSubmission(c, relayInfo)
	if taskErr != nil {
		respondTaskSubmissionError(c, taskErr)
		return
	}
	presentTaskSubmission(c, outcome)
}

// respondTaskError 统一输出 Task 错误响应（含 429 限流提示改写）
func respondTaskError(c *gin.Context, taskErr *dto.TaskError) {
	taskErr = kkaiPublicTaskError(c, taskErr)
	if taskErr == nil {
		return
	}
	if taskErr.StatusCode == http.StatusTooManyRequests &&
		taskErr.Code != string(types.ErrorCodeRequestPolicyWarning) {
		taskErr.Message = "当前分组上游负载已饱和，请稍后再试"
	}
	c.JSON(taskErr.StatusCode, taskErr)
}

func shouldRetryTaskRelay(c *gin.Context, channelId int, taskErr *dto.TaskError, retryTimes int) bool {
	return decideTaskRetry(c, taskErr, retryTimes).Action == "retry"
}

func decideTaskRetry(c *gin.Context, taskErr *dto.TaskError, retryTimes int) service.PolicyDecision {
	stop := service.PolicyDecision{Action: "stop", Source: "system"}
	retry := service.PolicyDecision{Action: "retry", Reason: "retry_status_matched", Source: "system"}
	switch {
	case taskErr == nil:
		stop.Reason = "request_completed"
	case taskErr.NoRetry:
		stop.Reason = "task_accepted"
	case c.Request != nil && c.Request.Context().Err() != nil:
		stop.Reason = "request_cancelled"
	case service.ShouldSkipRetryAfterKKAIPolicy(c):
		stop.Reason = "local_rejection"
	case service.ShouldSkipRetryAfterChannelAffinityFailure(c):
		stop.Reason, stop.Source = "strict_session", service.RequestPolicy(c).SessionModeSource
		if stop.Source == "" {
			stop.Source = "session_rule"
		}
	case retryTimes <= 0:
		stop.Reason, stop.Source = "attempt_budget_exhausted", "global"
	case service.GetChannelConstraints(c).SuppressesRetry():
		stop.Reason, stop.Source = "pinned_channel", "channel_constraint"
	default:
		if _, pinned := c.Get("specific_channel_id"); pinned {
			stop.Reason, stop.Source = "pinned_channel", "channel_constraint"
			return stop
		}
		switch {
		case taskErr.StatusCode == http.StatusTooManyRequests || taskErr.StatusCode == http.StatusTemporaryRedirect:
			return retry
		case taskErr.StatusCode/100 == 5:
			if !operation_setting.IsAlwaysSkipRetryStatusCode(taskErr.StatusCode) {
				return retry
			}
			stop.Reason = "system_retry_exclusion"
		case taskErr.StatusCode == http.StatusBadRequest || taskErr.StatusCode == http.StatusRequestTimeout:
			stop.Reason = "status_not_retryable"
		case taskErr.LocalError:
			stop.Reason = "local_rejection"
		case taskErr.StatusCode/100 == 2:
			stop.Reason = "system_retry_exclusion"
		default:
			return retry
		}
	}
	return stop
}
