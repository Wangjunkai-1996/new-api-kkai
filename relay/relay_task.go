package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type TaskSubmitOutcome int

const (
	TaskSubmitNotSent TaskSubmitOutcome = iota
	TaskSubmitRejected
	TaskSubmitUnknown
	TaskSubmitAccepted
)

type TaskSubmitResult struct {
	Response *channel.TaskSubmitResponse
	Task     *model.Task
	Platform constant.TaskPlatform
	Quota    int
	Outcome  TaskSubmitOutcome
}

type TaskQuoteResult struct {
	Platform    constant.TaskPlatform
	Model       string
	Quota       int
	OtherRatios map[string]float64
}

type preparedTaskSubmission struct {
	adaptor  channel.TaskAdaptor
	platform constant.TaskPlatform
}

func (r *TaskSubmitResult) CanRefund() bool {
	return r == nil || r.Outcome == TaskSubmitNotSent || r.Outcome == TaskSubmitRejected
}

func (r *TaskSubmitResult) CanRetry() bool {
	return r == nil || r.Outcome == TaskSubmitNotSent || r.Outcome == TaskSubmitRejected
}

type TaskProvisionalPersistHook func(tx *gorm.DB, task *model.Task) error

const (
	taskProvisionalPersistHookKey = "task_provisional_persist_hook"
	taskMaxQuotaContextKey        = "task_max_quota"
)

func SetTaskProvisionalPersistHook(c *gin.Context, hook TaskProvisionalPersistHook) {
	if c == nil || hook == nil {
		return
	}
	c.Set(taskProvisionalPersistHookKey, hook)
}

// SetTaskMaxQuota rejects a submission if recalculated pricing exceeds the quoted ceiling.
func SetTaskMaxQuota(c *gin.Context, maxQuota int) {
	if c == nil || maxQuota < 0 {
		return
	}
	c.Set(taskMaxQuotaContextKey, maxQuota)
}

func enforceTaskMaxQuota(c *gin.Context, currentQuota int) *dto.TaskError {
	if c == nil {
		return nil
	}
	value, exists := c.Get(taskMaxQuotaContextKey)
	if !exists {
		return nil
	}
	maxQuota, ok := value.(int)
	if !ok || maxQuota < 0 || currentQuota <= maxQuota {
		return nil
	}

	taskErr := service.TaskErrorWrapperLocal(
		fmt.Errorf("current quota %d exceeds quoted maximum %d", currentQuota, maxQuota),
		"quote_stale",
		http.StatusConflict,
	)
	taskErr.Data = map[string]any{"current_quota": currentQuota}
	return taskErr
}

// ResolveOriginTask 处理基于已有任务的提交（remix / continuation）：
// 查找原始任务、从中提取模型名称、将渠道锁定到原始任务的渠道
// （通过 info.LockedChannel，重试时复用同一渠道并轮换 key），
// 以及提取 OtherRatios（时长、分辨率）。
// 该函数在控制器的重试循环之前调用一次，其结果通过 info 字段和上下文持久化。
func ResolveOriginTask(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	// 检测 remix action
	path := c.Request.URL.Path
	if strings.Contains(path, "/v1/videos/") && strings.HasSuffix(path, "/remix") {
		info.Action = constant.TaskActionRemix
	}

	// 提取 remix 任务的 video_id
	if info.Action == constant.TaskActionRemix {
		videoID := c.Param("video_id")
		if strings.TrimSpace(videoID) == "" {
			return service.TaskErrorWrapperLocal(fmt.Errorf("video_id is required"), "invalid_request", http.StatusBadRequest)
		}
		info.OriginTaskID = videoID
	}

	if info.OriginTaskID == "" {
		return nil
	}

	// 查找原始任务
	originTask, exist, err := model.GetByTaskId(info.UserId, info.OriginTaskID)
	if err != nil {
		return service.TaskErrorWrapper(err, "get_origin_task_failed", http.StatusInternalServerError)
	}
	if !exist {
		return service.TaskErrorWrapperLocal(errors.New("task_origin_not_exist"), "task_not_exist", http.StatusBadRequest)
	}

	// 从原始任务推导模型名称
	if info.OriginModelName == "" {
		if originTask.Properties.OriginModelName != "" {
			info.OriginModelName = originTask.Properties.OriginModelName
		} else if originTask.Properties.UpstreamModelName != "" {
			info.OriginModelName = originTask.Properties.UpstreamModelName
		} else {
			var taskData map[string]interface{}
			_ = common.Unmarshal(originTask.Data, &taskData)
			if m, ok := taskData["model"].(string); ok && m != "" {
				info.OriginModelName = m
			}
		}
	}

	// 锁定到原始任务的渠道（重试时复用同一渠道，轮换 key）
	ch, err := model.GetChannelById(originTask.ChannelId, true)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "channel_not_found", http.StatusBadRequest)
	}
	if ch.Status != common.ChannelStatusEnabled {
		return service.TaskErrorWrapperLocal(errors.New("the channel of the origin task is disabled"), "task_channel_disable", http.StatusBadRequest)
	}
	info.LockedChannel = ch

	if originTask.ChannelId != info.ChannelId {
		key, _, newAPIError := ch.GetNextEnabledKey()
		if newAPIError != nil {
			return service.TaskErrorWrapper(newAPIError, "channel_no_available_key", newAPIError.StatusCode)
		}
		common.SetContextKey(c, constant.ContextKeyChannelKey, key)
		common.SetContextKey(c, constant.ContextKeyChannelType, ch.Type)
		common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, ch.GetBaseURL())
		common.SetContextKey(c, constant.ContextKeyChannelId, originTask.ChannelId)

		info.ChannelBaseUrl = ch.GetBaseURL()
		info.ChannelId = originTask.ChannelId
		info.ChannelType = ch.Type
		info.ApiKey = key
	}

	// 提取 remix 参数（时长、分辨率 → OtherRatios）
	if info.Action == constant.TaskActionRemix {
		if originTask.PrivateData.BillingContext != nil {
			// 新的 remix 逻辑：直接从原始任务的 BillingContext 中提取 OtherRatios（如果存在）
			for s, f := range originTask.PrivateData.BillingContext.OtherRatios {
				info.PriceData.AddOtherRatio(s, f)
			}
		} else {
			// 旧的 remix 逻辑：直接从 task data 解析 seconds 和 size（如果存在）
			var taskData map[string]interface{}
			_ = common.Unmarshal(originTask.Data, &taskData)
			secondsStr, _ := taskData["seconds"].(string)
			seconds, _ := strconv.Atoi(secondsStr)
			if seconds <= 0 {
				seconds = 4
			}
			// 历史任务数据可能包含未经校验的时长，作为计费乘数前必须钳制
			if seconds > relaycommon.MaxTaskDurationSeconds {
				seconds = relaycommon.MaxTaskDurationSeconds
			}
			sizeStr, _ := taskData["size"].(string)
			info.PriceData.AddOtherRatio("seconds", float64(seconds))
			info.PriceData.AddOtherRatio("size", 1)
			if sizeStr == "1792x1024" || sizeStr == "1024x1792" {
				info.PriceData.AddOtherRatio("size", 1.666667)
			}
		}
	}

	return nil
}

// ApplyOriginTaskAffinity carries resolved origin-task facts into plugin
// submissions and locks retries to the pinned origin channel when requested.
func ApplyOriginTaskAffinity(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if info == nil {
		return nil
	}
	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	}
	if tasks, ok := common.GetContextKeyType[[]*model.Task](c, constant.ContextKeyOriginTasks); ok {
		refs := make([]relaycommon.OriginTaskRef, 0, len(tasks))
		for _, task := range tasks {
			if task == nil {
				continue
			}
			refs = append(refs, relaycommon.OriginTaskRef{TaskID: task.TaskID, UpstreamTaskID: task.GetUpstreamTaskID(), Action: task.Action, Status: string(task.Status), Data: append([]byte(nil), task.Data...)})
		}
		info.OriginTasks = refs
	}
	pin, found, _ := service.GetChannelConstraints(c).ResolvedPin()
	if !found || pin.RetryMode != dto.PinRetrySameChannel {
		return nil
	}
	ch, err := model.CacheGetChannel(pin.ChannelId)
	if err != nil || ch == nil || ch.Status != common.ChannelStatusEnabled {
		if err == nil {
			err = errors.New("the channel of the origin task is disabled")
		}
		return service.TaskErrorWrapperLocal(err, "origin_task_channel_disabled", http.StatusBadRequest)
	}
	info.LockedChannel = ch
	return nil
}

// RelayTaskSubmit 完成 task 提交的全部流程（每次尝试调用一次）：
// 刷新渠道元数据 → 确定 platform/adaptor → 验证请求 →
// 估算计费(EstimateBilling) → 计算价格 → 预扣费（仅首次）→
// 构建/发送/解析上游请求 → 提交后计费调整(AdjustBillingOnSubmit)。
// 控制器负责 defer Refund 和成功后 Settle。
func RelayTaskSubmit(c *gin.Context, info *relaycommon.RelayInfo, existingTask *model.Task) (*TaskSubmitResult, *dto.TaskError) {
	prepared, taskErr := prepareTaskSubmission(c, info)
	if taskErr != nil {
		return nil, taskErr
	}
	if taskErr := enforceTaskMaxQuota(c, info.PriceData.Quota); taskErr != nil {
		return nil, taskErr
	}

	if info.PublicTaskID == "" {
		info.PublicTaskID = model.GenerateTaskID()
	}

	return submitPreparedTask(c, info, prepared.adaptor, prepared.platform, existingTask)
}

func QuoteTaskSubmission(c *gin.Context, info *relaycommon.RelayInfo) (*TaskQuoteResult, *dto.TaskError) {
	prepared, taskErr := prepareTaskSubmission(c, info)
	if taskErr != nil {
		return nil, taskErr
	}
	otherRatios := info.PriceData.OtherRatios()
	if otherRatios == nil {
		otherRatios = map[string]float64{}
	}
	return &TaskQuoteResult{
		Platform:    prepared.platform,
		Model:       info.OriginModelName,
		Quota:       info.PriceData.Quota,
		OtherRatios: otherRatios,
	}, nil
}

func prepareTaskSubmission(c *gin.Context, info *relaycommon.RelayInfo) (*preparedTaskSubmission, *dto.TaskError) {
	info.InitChannelMeta(c)

	// 1. 确定 platform → 创建适配器 → 验证请求
	platform := constant.TaskPlatform(c.GetString("platform"))
	if platform == "" {
		platform = GetTaskPlatform(c)
	}
	platform, adaptor := getTaskAdaptorForRequest(c, platform)
	if adaptor == nil {
		code, message := TaskPlatformUnavailableError(platform)
		return nil, service.TaskErrorWrapperLocal(errors.New(message), code, http.StatusBadRequest)
	}
	// buildSubmitRequest runs during validation and the unreleased plugin
	// contract exposes this host-generated id to that hook.
	if info.PublicTaskID == "" {
		info.PublicTaskID = model.GenerateTaskID()
	}
	adaptor.Init(info)
	// Plugin submit hooks run during ValidateRequestAndSetAction and cache the
	// upstream body. OriginModelName is already seeded on that line (protocol
	// resolved_task_model, legacy submit, or GenRelayInfo original_model), so
	// map before validation. The empty-name CoverTaskActionToModelName
	// synthesis happens after validate and cannot move; skip the late block
	// when early mapping ran so a chain is never applied twice.
	mappedBeforeValidate := info.OriginModelName != ""
	if mappedBeforeValidate {
		info.UpstreamModelName = info.OriginModelName
		if err := helper.ModelMappedHelper(c, info, nil); err != nil {
			return nil, service.TaskErrorWrapperLocal(err, "model_mapping_failed", http.StatusBadRequest)
		}
	}
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
		return nil, taskErr
	}

	// 2. 确定模型名称
	modelName := info.OriginModelName
	if modelName == "" {
		modelName = service.CoverTaskActionToModelName(platform, info.Action)
	}

	if !mappedBeforeValidate {
		info.OriginModelName = modelName
		info.UpstreamModelName = modelName
		if err := helper.ModelMappedHelper(c, info, nil); err != nil {
			return nil, service.TaskErrorWrapperLocal(err, "model_mapping_failed", http.StatusBadRequest)
		}
	}

	// 4. 价格计算：基础模型价格
	info.OriginModelName = modelName
	var priceData types.PriceData
	var err error
	pluginKey := c.GetString("task_plugin_key")
	pinnedValue, _ := c.Get(jsplugin.ContextKeyPinnedPlugin)
	pinnedPlugin, _ := pinnedValue.(jsplugin.PinnedPlugin)
	if pinnedPlugin.Plugin != nil {
		pluginKey = pinnedPlugin.Plugin.Meta.Key
	}
	exprStr, exists := billing_setting.ResolveTaskBillingExpr(pluginKey, modelName, info.UpstreamModelName)
	useTiered := exists || billing_setting.GetBillingMode(modelName) == billing_setting.BillingModeTieredExpr
	if useTiered {
		if billingexpr.UsesFixedPricing(exprStr) {
			return nil, service.TaskErrorWrapperLocal(errors.New("fixed pricing is not supported for task usage expressions"), "model_price_error", http.StatusBadRequest)
		}
		provider, supported := adaptor.(channel.TaskUsageFactsProvider)
		if !exists || !supported {
			return nil, service.TaskErrorWrapper(fmt.Errorf("task model %s has no usage expression or meter", modelName), "model_price_error", http.StatusBadRequest)
		}
		sharedModel := pinnedPlugin.Generation.SharedModel(modelName) || pinnedPlugin.Generation.SharedModel(info.UpstreamModelName)
		if sharedModel && pinnedPlugin.Plugin != nil {
			schema, _ := pinnedPlugin.Plugin.Meta.UsageForModels(info.UpstreamModelName, modelName)
			if !billing_setting.TaskExprCompatible(exprStr, schema) {
				return nil, service.TaskErrorWrapper(fmt.Errorf("task model %s pricing is not configured for plugin %s", modelName, pluginKey), "model_price_error", http.StatusBadRequest)
			}
		}
		var facts map[string]any
		if validatedProvider, ok := adaptor.(channel.TaskValidatedUsageFactsProvider); ok {
			facts, err = validatedProvider.ExtractUsageFactsValidated(c, info)
			if err != nil {
				return nil, service.TaskErrorWrapperLocal(err, "plugin_usage_invalid", http.StatusBadRequest)
			}
		} else {
			facts = provider.ExtractUsageFacts(c, info)
		}
		cost, trace, runErr := billingexpr.RunExprWithRequest(exprStr, billingexpr.TokenParams{}, billingexpr.RequestInput{Usage: facts})
		if runErr != nil || cost < 0 {
			if runErr == nil {
				runErr = fmt.Errorf("negative task expression result")
			}
			return nil, service.TaskErrorWrapper(runErr, "model_price_error", http.StatusBadRequest)
		}
		groupRatioInfo := helper.HandleGroupRatio(c, info)
		quota, clamp := common.QuotaRoundChecked(cost * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
		noteTaskQuotaClamp(info, clamp)
		priceData = types.PriceData{Quota: quota, QuotaToPreConsume: quota, GroupRatioInfo: groupRatioInfo}
		info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{BillingMode: billing_setting.BillingModeTieredExpr, ModelName: modelName, ExprString: exprStr, ExprHash: billingexpr.ExprHashString(exprStr), GroupRatio: groupRatioInfo.GroupRatio, EstimatedQuotaBeforeGroup: cost * common.QuotaPerUnit, EstimatedQuotaAfterGroup: quota, EstimatedTier: trace.MatchedTier, QuotaPerUnit: common.QuotaPerUnit, ExprVersion: billingexpr.ExprVersion(exprStr), TaskUsageBilling: true, UsageFacts: facts}
	} else {
		priceData, err = helper.ModelPriceHelperPerCall(c, info)
		if err != nil {
			return nil, service.TaskErrorWrapper(err, "model_price_error", http.StatusBadRequest)
		}
	}
	info.PriceData = priceData

	// 5. 计费估算：让适配器根据用户请求提供 OtherRatios（时长、分辨率等）
	//    必须在 ModelPriceHelperPerCall 之后调用（它会重建 PriceData）。
	//    ResolveOriginTask 可能已在 remix 路径中预设了 OtherRatios，此处合并。
	if info.TieredBillingSnapshot == nil {
		var estimatedRatios map[string]float64
		if validatedProvider, ok := adaptor.(channel.TaskValidatedBillingProvider); ok {
			estimatedRatios, err = validatedProvider.EstimateBillingValidated(c, info)
			if err != nil {
				return nil, service.TaskErrorWrapperLocal(err, "plugin_usage_invalid", http.StatusBadRequest)
			}
		} else {
			estimatedRatios = adaptor.EstimateBilling(c, info)
		}
		if len(estimatedRatios) > 0 {
			for k, v := range estimatedRatios {
				info.PriceData.AddOtherRatio(k, v)
			}
		}
	}

	// 6. 将 OtherRatios 应用到基础额度（饱和转换，防止溢出成负数）
	if info.TieredBillingSnapshot == nil && !common.StringsContains(constant.TaskPricePatches, modelName) {
		quotaWithRatios := info.PriceData.ApplyOtherRatiosToFloat(float64(info.PriceData.Quota))
		quota, clamp := common.QuotaFromFloatChecked(quotaWithRatios)
		info.PriceData.Quota = quota
		noteTaskQuotaClamp(info, clamp)
	}

	return &preparedTaskSubmission{adaptor: adaptor, platform: platform}, nil
}

func submitPreparedTask(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.TaskAdaptor, platform constant.TaskPlatform, existingTask *model.Task) (*TaskSubmitResult, *dto.TaskError) {
	result := &TaskSubmitResult{
		Task:     existingTask,
		Platform: platform,
		Quota:    info.PriceData.Quota,
		Outcome:  TaskSubmitNotSent,
	}

	requestBody, err := adaptor.BuildRequestBody(c, info)
	if err != nil {
		return result, service.TaskErrorWrapper(err, "build_request_failed", http.StatusInternalServerError)
	}

	task := buildProvisionalTask(c, platform, info, existingTask)
	result.Task = task
	if err := persistProvisionalTask(c, task); err != nil {
		return result, service.TaskErrorWrapperLocal(err, "persist_task_failed", http.StatusInternalServerError)
	}
	if info.Billing == nil && !info.PriceData.FreeModel {
		info.ForcePreConsume = true
		if apiErr := service.PreConsumeTaskBilling(c, task, info.PriceData.Quota, info); apiErr != nil {
			return result, service.TaskErrorFromAPIError(apiErr)
		}
	}

	claimedTask, claimed, err := model.ClaimTaskSubmission(c, task.ID, &model.TaskSubmissionAttempt{
		Platform:          platform,
		ChannelID:         info.ChannelId,
		Action:            info.Action,
		OriginModelName:   info.OriginModelName,
		UpstreamModelName: info.UpstreamModelName,
		Execution:         task.PrivateData.Execution,
		TieredSnapshot:    info.TieredBillingSnapshot,
	})
	if err != nil {
		return result, service.TaskErrorWrapperLocal(err, "claim_task_submission_failed", http.StatusInternalServerError)
	}
	if claimedTask != nil {
		task = claimedTask
		result.Task = task
	}
	if !claimed {
		result.Outcome = TaskSubmitUnknown
		return result, taskSubmissionUnknownError(task.TaskID)
	}

	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		var requestErr *channel.TaskRequestError
		if errors.As(err, &requestErr) && !requestErr.SubmissionPossible() {
			if resetErr := resetTaskSubmissionClaim(task); resetErr != nil {
				result.Outcome = TaskSubmitUnknown
				task.PrivateData.TargetQuota = quotaPointer(result.Quota)
				return result, markTaskSubmissionUnknown(task, fmt.Errorf("reset submission claim after pre-send failure: %w", resetErr))
			}
			return result, service.TaskErrorWrapper(err, "do_request_failed", http.StatusInternalServerError)
		}
		result.Outcome = TaskSubmitUnknown
		task.PrivateData.TargetQuota = quotaPointer(result.Quota)
		return result, markTaskSubmissionUnknown(task, err)
	}
	if resp == nil {
		result.Outcome = TaskSubmitUnknown
		task.PrivateData.TargetQuota = quotaPointer(result.Quota)
		return result, markTaskSubmissionUnknown(task, errors.New("upstream response is nil"))
	}
	if resp.Body == nil {
		resp.Body = http.NoBody
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer resp.Body.Close()
		responseBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			responseBody = []byte(readErr.Error())
		}
		return result, rejectTaskHTTPResponse(task, result, resp.StatusCode, responseBody)
	}

	responseBody, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		result.Outcome = TaskSubmitUnknown
		task.PrivateData.TargetQuota = quotaPointer(result.Quota)
		return result, markTaskSubmissionUnknown(task, fmt.Errorf("read successful upstream response: %w", readErr))
	}
	resp.Body = io.NopCloser(bytes.NewReader(responseBody))
	if policyErr := embeddedTaskPolicyError(resp.StatusCode, responseBody); policyErr != nil {
		return result, rejectParsedTaskError(task, result, policyErr)
	}

	response, responseErr := adaptor.ParseResponse(c, resp, info)
	if responseErr != nil {
		if responseErr.NoRetry || strings.HasPrefix(responseErr.Code, "plugin_submit_response_") {
			result.Outcome = TaskSubmitUnknown
			task.PrivateData.TargetQuota = quotaPointer(result.Quota)
			return result, markTaskSubmissionUnknown(task, responseErr.Error)
		}
		return result, rejectParsedTaskError(task, result, responseErr)
	}
	if response == nil || response.UpstreamTaskID == "" {
		result.Outcome = TaskSubmitUnknown
		task.PrivateData.TargetQuota = quotaPointer(result.Quota)
		return result, markTaskSubmissionUnknown(task, errors.New("upstream task id is empty"))
	}

	finalQuota := info.PriceData.Quota
	if response.Immediate != nil && response.Immediate.Status == model.TaskStatusFailure {
		finalQuota = 0
	} else if snap := info.TieredBillingSnapshot; snap != nil {
		if response.Immediate != nil && response.Immediate.Status == model.TaskStatusSuccess && len(response.Immediate.UsageFacts) > 0 {
			settlement, facts, err := service.EvaluateTaskCompletionUsage(snap, response.Immediate.UsageFacts)
			if err != nil {
				logger.LogWarn(c, fmt.Sprintf("task immediate usage settlement failed; retaining reserved quota: %v", err))
			} else {
				finalQuota = settlement.ActualQuotaAfterGroup
				snap.UsageFacts = facts
				snap.EstimatedTier = settlement.MatchedTier
				noteTaskQuotaClamp(info, settlement.Clamp)
			}
		}
	} else {
		if adjustedRatios := adaptor.AdjustBillingOnSubmit(info, response.TaskData); len(adjustedRatios) > 0 {
			if adjustedQuota, ok := recalcQuotaFromRatios(info, adjustedRatios); ok {
				// 基于调整后的 ratios 重新计算 quota
				finalQuota = adjustedQuota
				info.PriceData.ReplaceOtherRatios(adjustedRatios)
				info.PriceData.Quota = finalQuota
			}
		}
	}

	finalQuota = authorizedTaskQuota(task, finalQuota)
	info.PriceData.Quota = finalQuota

	acceptance := model.TaskSubmissionAcceptance{
		UpstreamTaskID: response.UpstreamTaskID,
		TaskData:       response.TaskData,
		ChannelID:      info.ChannelId,
		Status:         model.TaskStatusSubmitted,
		Progress:       taskcommon.ProgressSubmitted,
		OtherRatios:    info.PriceData.OtherRatios(),
		TargetQuota:    finalQuota,
		PluginState:    response.PluginState,
		TieredSnapshot: info.TieredBillingSnapshot,
	}
	if immediate := response.Immediate; immediate != nil {
		acceptance.Status = model.TaskStatus(immediate.Status)
		acceptance.Progress = immediate.Progress
		acceptance.ResultURL = immediate.Url
		if acceptance.Status == model.TaskStatusSuccess || acceptance.Status == model.TaskStatusFailure {
			acceptance.FinishTime = time.Now().Unix()
			if acceptance.Progress == "" {
				acceptance.Progress = taskcommon.ProgressComplete
			}
		}
		if acceptance.Status == model.TaskStatusFailure {
			acceptance.FailReason = immediate.Reason
		}
		if acceptance.Status == model.TaskStatusSuccess && acceptance.ResultURL == "" {
			acceptance.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		}
		if task.IsAssetHostedResult() {
			acceptance.ArchiveSource = immediate.Url
			if acceptance.ArchiveSource == "" {
				acceptance.ArchiveSource = immediate.RemoteUrl
			}
		}
	}

	immediateTerminal := acceptance.Status == model.TaskStatusSuccess || acceptance.Status == model.TaskStatusFailure
	if immediateTerminal {
		if value, ok := c.Get(jsplugin.ContextKeyPinnedRoute); ok {
			if pinned, ok := value.(jsplugin.PinnedRoute); ok && pinned.Route.RetainResult != nil && !*pinned.Route.RetainResult {
				acceptance.ResultDiscarded = true
			}
		}
		if value, ok := c.Get(jsplugin.ContextKeyPinnedEndpoint); ok {
			if pinned, ok := value.(jsplugin.PinnedEndpoint); ok && pinned.Protocol == jsplugin.ProtocolOpenAIImage {
				acceptance.ResultDiscarded = true
			}
		}
	}

	if acceptance.Status == model.TaskStatusFailure {
		service.RequestPolicy(c).AddEvent(service.PolicyEvent{ChannelID: info.ChannelId, Decision: service.PolicyDecision{Action: "stop", Reason: "task_failed", Source: "upstream"}})
	} else {
		service.MarkRequestPolicySuccess(c, nil)
	}
	policyEvents, policyErr := common.Marshal(service.RequestPolicy(c).Events())
	if policyErr != nil {
		// An accepted upstream task must still cross the durable acceptance boundary.
		logger.LogWarn(c, "failed to encode request policy audit: "+policyErr.Error())
	}
	acceptance.RequestPolicy = policyEvents

	acceptedTask, accepted, err := model.PersistTaskSubmissionAcceptance(c, task.ID, acceptance)
	if acceptedTask != nil {
		task = acceptedTask
		result.Task = task
	}
	if err != nil {
		result.Response = response
		result.Quota = finalQuota
		receiptStored := persistAcceptedTaskRecoveryReceipt(task, acceptance)
		if !recoverAcceptedTaskPersistence(task, acceptance, fmt.Errorf("persist accepted task: %w", err)) {
			if !receiptStored {
				common.SysError(fmt.Sprintf("accepted task %s lost both task persistence and recovery receipt; manual reconciliation required for upstream task %s", task.TaskID, acceptance.UpstreamTaskID))
			}
			result.Outcome = TaskSubmitUnknown
			task.PrivateData.TargetQuota = quotaPointer(finalQuota)
			return result, markTaskSubmissionUnknown(task, errors.New("accepted task persistence retries exhausted"))
		}
		accepted = true
	}
	if !accepted {
		result.Response = response
		result.Quota = finalQuota
		result.Outcome = TaskSubmitUnknown
		return result, taskSubmissionUnknownError(task.TaskID)
	}
	if info.PriceData.FreeModel && finalQuota == 0 {
		mutation, settleErr := model.AdjustTaskBilling(c, task.ID, 0)
		if settleErr != nil {
			common.SysError(fmt.Sprintf("settle accepted free task %s failed: %v", task.TaskID, settleErr))
		} else if mutation != nil && mutation.Task != nil {
			task = mutation.Task
			result.Task = task
		}
	}

	if acceptance.ResultDiscarded {
		task.Data = append(json.RawMessage(nil), response.TaskData...)
		task.PrivateData.PluginState = append(json.RawMessage(nil), response.PluginState...)
	}
	result.Response = response
	result.Quota = finalQuota
	result.Outcome = TaskSubmitAccepted
	return result, nil
}

func taskHTTPStatusIsAmbiguous(statusCode int) bool {
	return statusCode == http.StatusRequestTimeout || statusCode >= http.StatusInternalServerError
}

func buildProvisionalTask(c *gin.Context, platform constant.TaskPlatform, info *relaycommon.RelayInfo, existingTask *model.Task) *model.Task {
	var task *model.Task
	if existingTask != nil {
		copy := *existingTask
		task = &copy
	} else {
		task = model.InitTask(platform, info)
		task.Status = model.TaskStatusNotStart
		task.Progress = taskcommon.ProgressComplete
		task.Quota = 0
		task.PrivateData.BillingState = model.TaskBillingStatePending
		if info.PriceData.FreeModel {
			task.PrivateData.BillingState = model.TaskBillingStateReserved
		}
	}
	if task.PrivateData.BillingState == "" {
		task.PrivateData.BillingState = model.TaskBillingStatePending
		if info.PriceData.FreeModel {
			task.PrivateData.BillingState = model.TaskBillingStateReserved
		}
	}
	task.PrivateData.Execution = service.TaskExecutionSnapshotFromContext(c)
	task.Platform = platform
	task.ChannelId = info.ChannelId
	task.Action = info.Action
	task.Properties.OriginModelName = info.OriginModelName
	task.Properties.UpstreamModelName = info.UpstreamModelName
	if task.PrivateData.NodeName == "" {
		task.PrivateData.NodeName = common.NodeName
	}
	if task.PrivateData.TokenId == 0 {
		task.PrivateData.TokenId = info.TokenId
	}
	if task.PrivateData.RecoveryAt == 0 {
		task.PrivateData.RecoveryAt = time.Now().Add(service.TaskBillingRecoveryDelay).Unix()
	}
	if task.PrivateData.BillingContext == nil {
		task.PrivateData.BillingContext = &model.TaskBillingContext{
			ModelPrice:      info.PriceData.ModelPrice,
			GroupRatio:      info.PriceData.GroupRatioInfo.GroupRatio,
			ModelRatio:      info.PriceData.ModelRatio,
			OtherRatios:     info.PriceData.OtherRatios(),
			OriginModelName: info.OriginModelName,
			PerCallBilling:  common.StringsContains(constant.TaskPricePatches, info.OriginModelName) || info.PriceData.UsePrice,
			MaxQuota:        taskMaxQuota(c),
			TieredSnapshot:  info.TieredBillingSnapshot,
		}
	} else if task.PrivateData.BillingContext.MaxQuota == nil {
		task.PrivateData.BillingContext.MaxQuota = taskMaxQuota(c)
	}
	if task.PrivateData.AccountingState == "" {
		task.PrivateData.AccountingState = model.TaskAccountingStatePending
		task.PrivateData.AccountingQuota = info.PriceData.Quota
	}
	if task.PrivateData.AccountingContext == nil {
		requestPath := ""
		username := ""
		tokenName := ""
		if c != nil && c.Request != nil && c.Request.URL != nil {
			requestPath = c.Request.URL.Path
		}
		if c != nil {
			username = c.GetString("username")
			tokenName = c.GetString("token_name")
		}
		var quotaClamp *common.QuotaClamp
		if info.QuotaClamp != nil {
			copy := *info.QuotaClamp
			quotaClamp = &copy
		}
		task.PrivateData.AccountingContext = &model.TaskAccountingContext{
			RequestPath:       requestPath,
			Username:          username,
			TokenName:         tokenName,
			HasUserGroupRatio: info.PriceData.GroupRatioInfo.HasSpecialRatio,
			UserGroupRatio:    info.PriceData.GroupRatioInfo.GroupSpecialRatio,
			QuotaClamp:        quotaClamp,
		}
	}
	return task
}

func taskMaxQuota(c *gin.Context) *int {
	if c == nil {
		return nil
	}
	value, exists := c.Get(taskMaxQuotaContextKey)
	if !exists {
		return nil
	}
	maxQuota, ok := value.(int)
	if !ok || maxQuota < 0 {
		return nil
	}
	return quotaPointer(maxQuota)
}

func persistProvisionalTask(c *gin.Context, task *model.Task) error {
	created := task.ID == 0
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		if created {
			if err := tx.Create(task).Error; err != nil {
				return err
			}
			hookValue, ok := c.Get(taskProvisionalPersistHookKey)
			if ok {
				hook, ok := hookValue.(TaskProvisionalPersistHook)
				if !ok || hook == nil {
					return errors.New("invalid task provisional persist hook")
				}
				if err := hook(tx, task); err != nil {
					return err
				}
			}
		}
		if err := service.EnqueueTaskBillingRecovery(c, tx, task); err != nil {
			return err
		}
		return nil
	})
	if err != nil && created {
		task.ID = 0
		task.CreatedAt = 0
		task.UpdatedAt = 0
	}
	return err
}

func markTaskSubmissionUnknown(task *model.Task, cause error) *dto.TaskError {
	if cause == nil {
		cause = errors.New("task submission outcome is unknown")
	}
	common.SysError(fmt.Sprintf("task %s submission outcome unknown: %v", task.TaskID, cause))
	markedTask, marked, err := model.MarkTaskSubmissionAmbiguous(context.Background(), task.ID, model.TaskSubmissionAmbiguity{
		Reason:      "task submission outcome is unknown",
		TargetQuota: task.PrivateData.TargetQuota,
	})
	if markedTask != nil {
		*task = *markedTask
	}
	if err != nil {
		common.SysError(fmt.Sprintf("persist unknown task %s failed: %v", task.TaskID, err))
	} else if !marked {
		common.SysError(fmt.Sprintf("task %s changed before unknown submission state could be persisted", task.TaskID))
	}
	return taskSubmissionUnknownError(task.TaskID)
}

func recoverAcceptedTaskPersistence(task *model.Task, acceptance model.TaskSubmissionAcceptance, cause error) bool {
	if cause == nil {
		cause = errors.New("accepted task persistence outcome is unknown")
	}
	common.SysError(fmt.Sprintf("accepted task %s persistence outcome unknown: %v", task.TaskID, cause))
	const maxAttempts = 4
	retryCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := time.Duration(25*(1<<(attempt-1))) * time.Millisecond
			timer := time.NewTimer(delay)
			select {
			case <-retryCtx.Done():
				timer.Stop()
				common.SysError(fmt.Sprintf("persist recoverable accepted task %s timed out: %v", task.TaskID, retryCtx.Err()))
				return false
			case <-timer.C:
			}
		}
		persisted, accepted, err := model.PersistTaskSubmissionAcceptance(retryCtx, task.ID, acceptance)
		if persisted != nil {
			*task = *persisted
		}
		if err == nil && accepted {
			return true
		}
		if err == nil {
			common.SysError(fmt.Sprintf("persist recoverable accepted task %s skipped because task state changed", task.TaskID))
			return false
		}
		common.SysError(fmt.Sprintf("persist recoverable accepted task %s attempt %d/%d failed: %v", task.TaskID, attempt+1, maxAttempts, err))
	}
	return false
}

func persistAcceptedTaskRecoveryReceipt(task *model.Task, acceptance model.TaskSubmissionAcceptance) bool {
	if task == nil || task.ID <= 0 {
		return false
	}
	if acceptance.ResultDiscarded {
		acceptance.TaskData = nil
		acceptance.PluginState = nil
		acceptance.ResultURL = ""
	}
	receipt := service.TaskBillingRecoveryAcceptanceReceipt{
		PluginState:     acceptance.PluginState,
		ResultURL:       acceptance.ResultURL,
		ArchiveSource:   acceptance.ArchiveSource,
		ResultDiscarded: acceptance.ResultDiscarded,
		TieredSnapshot:  acceptance.TieredSnapshot,
		UpstreamTaskID:  acceptance.UpstreamTaskID,
		RawResponse:     append(json.RawMessage(nil), acceptance.TaskData...),
		ChannelID:       acceptance.ChannelID,
		Status:          acceptance.Status,
		Progress:        acceptance.Progress,
		FailReason:      acceptance.FailReason,
		FinishTime:      acceptance.FinishTime,
		OtherRatios:     acceptance.OtherRatios,
		TargetQuota:     acceptance.TargetQuota,
	}

	const maxAttempts = 4
	retryCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := time.Duration(25*(1<<(attempt-1))) * time.Millisecond
			timer := time.NewTimer(delay)
			select {
			case <-retryCtx.Done():
				timer.Stop()
				common.SysError(fmt.Sprintf("persist accepted task recovery receipt %s timed out: %v", task.TaskID, retryCtx.Err()))
				return false
			case <-timer.C:
			}
		}
		if err := service.StoreTaskBillingRecoveryAcceptanceReceipt(retryCtx, task.ID, receipt); err == nil {
			return true
		} else {
			common.SysError(fmt.Sprintf("persist accepted task recovery receipt %s attempt %d/%d failed: %v", task.TaskID, attempt+1, maxAttempts, err))
		}
	}
	return false
}

func resetTaskSubmissionClaim(task *model.Task) error {
	if task == nil || task.ID <= 0 {
		return model.ErrTaskBillingInvalidRequest
	}
	if err := model.ResetTaskSubmissionClaim(context.Background(), task.ID); err != nil {
		common.SysError(fmt.Sprintf("reset task submission claim %s failed: %v", task.TaskID, err))
		return err
	}
	task.PrivateData.BillingState = model.TaskBillingStateReserved
	return nil
}

func authorizedTaskQuota(task *model.Task, quota int) int {
	if task == nil || task.PrivateData.BillingContext == nil || task.PrivateData.BillingContext.MaxQuota == nil {
		return quota
	}
	if quota > *task.PrivateData.BillingContext.MaxQuota {
		return *task.PrivateData.BillingContext.MaxQuota
	}
	return quota
}

func quotaPointer(quota int) *int {
	value := quota
	return &value
}

func taskSubmissionUnknownError(taskID string) *dto.TaskError {
	taskErr := service.TaskErrorWrapperLocal(errors.New("task submission outcome is unknown"), "task_submission_unknown", http.StatusBadGateway)
	taskErr.Data = taskID
	return taskErr
}

// recalcQuotaFromRatios 根据 adjustedRatios 重新计算 quota。
// 公式: baseQuota × ∏(ratio) — 其中 baseQuota 是不含 OtherRatios 的基础额度。
func recalcQuotaFromRatios(info *relaycommon.RelayInfo, ratios map[string]float64) (int, bool) {
	// 从 PriceData 获取不含 OtherRatios 的基础价格
	baseQuota := info.PriceData.RemoveOtherRatiosFromFloat(float64(info.PriceData.Quota))
	priceData := info.PriceData
	if !priceData.ReplaceOtherRatios(ratios) {
		return 0, false
	}
	// 应用新的 ratios
	result := priceData.ApplyOtherRatiosToFloat(baseQuota)
	quota, clamp := common.QuotaFromFloatChecked(result)
	noteTaskQuotaClamp(info, clamp)
	return quota, true
}

// noteTaskQuotaClamp records the first quota saturation event onto the task's
// RelayInfo so LogTaskConsumption can surface it on the submit log's
// admin_info. First non-nil clamp wins.
func noteTaskQuotaClamp(info *relaycommon.RelayInfo, clamp *common.QuotaClamp) {
	if clamp == nil || info == nil {
		return
	}
	if info.QuotaClamp == nil {
		info.QuotaClamp = clamp
	}
}

var fetchRespBuilders = map[int]func(c *gin.Context) (respBody []byte, taskResp *dto.TaskError){
	relayconstant.RelayModeSunoFetchByID:  sunoFetchByIDRespBodyBuilder,
	relayconstant.RelayModeSunoFetch:      sunoFetchRespBodyBuilder,
	relayconstant.RelayModeVideoFetchByID: videoFetchByIDRespBodyBuilder,
}

func RelayTaskFetch(c *gin.Context, relayMode int) (taskResp *dto.TaskError) {
	respBuilder, ok := fetchRespBuilders[relayMode]
	if !ok {
		taskResp = service.TaskErrorWrapperLocal(errors.New("invalid_relay_mode"), "invalid_relay_mode", http.StatusBadRequest)
	}

	respBody, taskErr := respBuilder(c)
	if taskErr != nil {
		return taskErr
	}
	if len(respBody) == 0 {
		respBody = []byte("{\"code\":\"success\",\"data\":null}")
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	_, err := io.Copy(c.Writer, bytes.NewBuffer(respBody))
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "copy_response_body_failed", http.StatusInternalServerError)
		return
	}
	return
}

func sunoFetchRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	userId := c.GetInt("id")
	var condition = struct {
		IDs    []any  `json:"ids"`
		Action string `json:"action"`
	}{}
	err := c.BindJSON(&condition)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "invalid_request", http.StatusBadRequest)
		return
	}
	var tasks []any
	if len(condition.IDs) > 0 {
		taskModels, err := model.GetByTaskIds(userId, condition.IDs)
		if err != nil {
			taskResp = service.TaskErrorWrapper(err, "get_tasks_failed", http.StatusInternalServerError)
			return
		}
		for _, task := range taskModels {
			tasks = append(tasks, TaskModel2Dto(task))
		}
	} else {
		tasks = make([]any, 0)
	}
	respBody, err = common.Marshal(dto.TaskResponse[[]any]{
		Code: "success",
		Data: tasks,
	})
	return
}

func sunoFetchByIDRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("id")
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest)
		return
	}

	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: TaskModel2Dto(originTask),
	})
	return
}

func videoFetchByIDRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("task_id")
	if taskId == "" {
		taskId = c.GetString("task_id")
	}
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest)
		return
	}

	isOpenAIVideoAPI := strings.HasPrefix(c.Request.RequestURI, "/v1/videos/")

	// Gemini/Vertex support an immediate status refresh. Asset-hosted tasks may
	// persist the provider result for the archive worker, but the response below
	// remains redacted.
	if realtimeResp := tryRealtimeFetch(originTask, isOpenAIVideoAPI); len(realtimeResp) > 0 {
		respBody = realtimeResp
		return
	}
	if originTask.IsAssetHostedResult() {
		if isOpenAIVideoAPI {
			respBody, err = common.Marshal(originTask.ToOpenAIVideo())
		} else {
			respBody, err = common.Marshal(dto.TaskResponse[any]{
				Code: "success",
				Data: TaskModel2Dto(originTask),
			})
		}
		if err != nil {
			taskResp = service.TaskErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError)
		}
		return
	}

	// OpenAI Video API 格式: 走各 adaptor 的 ConvertToOpenAIVideo
	if isOpenAIVideoAPI {
		adaptor := GetTaskAdaptor(originTask.Platform)
		if adaptor == nil {
			taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("invalid channel id: %d", originTask.ChannelId), "invalid_channel_id", http.StatusBadRequest)
			return
		}
		if converter, ok := adaptor.(channel.OpenAIVideoConverter); ok {
			openAIVideoData, err := converter.ConvertToOpenAIVideo(originTask)
			if err != nil {
				taskResp = service.TaskErrorWrapper(err, "convert_to_openai_video_failed", http.StatusInternalServerError)
				return
			}
			respBody = openAIVideoData
			return
		}
		taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("not_implemented:%s", originTask.Platform), "not_implemented", http.StatusNotImplemented)
		return
	}

	// 通用 TaskDto 格式
	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: TaskModel2Dto(originTask),
	})
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError)
	}
	return
}

// tryRealtimeFetch 尝试从上游实时拉取 Gemini/Vertex 任务状态。
// 仅当渠道类型为 Gemini 或 Vertex 时触发；其他渠道或出错时返回 nil。
// 当非 OpenAI Video API 时，还会构建自定义格式的响应体。
func tryRealtimeFetch(task *model.Task, isOpenAIVideoAPI bool) []byte {
	if task == nil {
		return nil
	}
	if task.IsAssetHostedResult() && (task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure) {
		return nil
	}
	channelModel, err := model.GetChannelById(task.ChannelId, true)
	if err != nil {
		return nil
	}
	if channelModel.Type != constant.ChannelTypeVertexAi && channelModel.Type != constant.ChannelTypeGemini {
		return nil
	}

	baseURL := constant.GetChannelBaseURL(channelModel.Type)
	if channelModel.GetBaseURL() != "" {
		baseURL = channelModel.GetBaseURL()
	}
	proxy := channelModel.GetSetting().Proxy
	adaptor := GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(channelModel.Type)))
	if adaptor == nil {
		return nil
	}

	resp, err := adaptor.FetchTask(baseURL, channelModel.Key, task, proxy)
	if err != nil || resp == nil {
		return nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	ti, err := adaptor.ParseTaskResult(task, resp, body)
	if err != nil || ti == nil {
		return nil
	}

	snap := task.Snapshot()

	// 将上游最新状态更新到 task
	if ti.Status != "" {
		task.Status = model.TaskStatus(ti.Status)
	}
	if ti.Progress != "" {
		task.Progress = ti.Progress
	}
	if task.IsAssetHostedResult() {
		archiveSource := strings.TrimSpace(ti.Url)
		if archiveSource == "" {
			archiveSource = strings.TrimSpace(ti.RemoteUrl)
		}
		if archiveSource != "" {
			task.PrivateData.ArchiveSource = archiveSource
		}
	}
	if strings.HasPrefix(ti.Url, "data:") {
		// data: URI — kept in Data, not ResultURL
	} else if ti.Url != "" {
		task.PrivateData.ResultURL = ti.Url
	} else if task.Status == model.TaskStatusSuccess {
		// No URL from adaptor — construct proxy URL using public task ID
		task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
	}

	if !snap.Equal(task.Snapshot()) {
		_, _ = task.UpdateWithStatusPreservingBilling(snap)
	}
	if task.IsAssetHostedResult() {
		return nil
	}

	// OpenAI Video API 由调用者的 ConvertToOpenAIVideo 分支处理
	if isOpenAIVideoAPI {
		return nil
	}

	// 非 OpenAI Video API: 构建自定义格式响应
	format := detectVideoFormat(body)
	out := map[string]any{
		"error":    nil,
		"format":   format,
		"metadata": nil,
		"status":   mapTaskStatusToSimple(task.Status),
		"task_id":  task.TaskID,
		"url":      task.GetResultURL(),
	}
	respBody, _ := common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: out,
	})
	return respBody
}

// detectVideoFormat 从 Gemini/Vertex 原始响应中探测视频格式
func detectVideoFormat(rawBody []byte) string {
	var raw map[string]any
	if err := common.Unmarshal(rawBody, &raw); err != nil {
		return "mp4"
	}
	respObj, ok := raw["response"].(map[string]any)
	if !ok {
		return "mp4"
	}
	vids, ok := respObj["videos"].([]any)
	if !ok || len(vids) == 0 {
		return "mp4"
	}
	v0, ok := vids[0].(map[string]any)
	if !ok {
		return "mp4"
	}
	mt, ok := v0["mimeType"].(string)
	if !ok || mt == "" || strings.Contains(mt, "mp4") {
		return "mp4"
	}
	return mt
}

// mapTaskStatusToSimple 将内部 TaskStatus 映射为简化状态字符串
func mapTaskStatusToSimple(status model.TaskStatus) string {
	switch status {
	case model.TaskStatusSuccess:
		return "succeeded"
	case model.TaskStatusFailure:
		return "failed"
	case model.TaskStatusQueued, model.TaskStatusSubmitted:
		return "queued"
	default:
		return "processing"
	}
}

func TaskModel2Dto(task *model.Task) *dto.TaskDto {
	quota, err := common.HistoricalCreditQuota("tasks", task.ID, task.Quota)
	if err != nil {
		common.SysError("task historical quota unavailable: " + err.Error())
	}
	return &dto.TaskDto{
		ID:         task.ID,
		CreatedAt:  task.CreatedAt,
		UpdatedAt:  task.UpdatedAt,
		TaskID:     task.TaskID,
		Platform:   string(task.Platform),
		UserId:     task.UserId,
		Group:      task.Group,
		ChannelId:  task.ChannelId,
		Quota:      quota,
		Action:     task.Action,
		Status:     string(task.Status),
		FailReason: task.PublicFailReason(),
		ResultURL:  task.PublicResultURL(),
		SubmitTime: task.SubmitTime,
		StartTime:  task.StartTime,
		FinishTime: task.FinishTime,
		Progress:   task.Progress,
		Properties: task.Properties,
		Username:   task.Username,
		Data:       task.PublicData(),
	}
}
