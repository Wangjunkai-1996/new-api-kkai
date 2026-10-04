package controller

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// taskSubmissionOutcome is the durable boundary shared by the legacy task
// endpoint and the plugin protocol presenters.
type taskSubmissionOutcome struct {
	Result    *relay.TaskSubmitResult
	Task      *model.Task
	RelayInfo *relaycommon.RelayInfo
}

func presentTaskSubmission(c *gin.Context, outcome *taskSubmissionOutcome) {
	if outcome == nil || outcome.Task == nil || outcome.RelayInfo == nil {
		respondTaskError(c, service.TaskErrorWrapperLocal(errors.New("task submission returned no result"), "task_submit_failed", http.StatusInternalServerError))
		return
	}
	if otherRatios := outcome.RelayInfo.PriceData.OtherRatios(); otherRatios != nil {
		if encoded, err := common.Marshal(otherRatios); err == nil {
			c.Header("X-New-Api-Other-Ratios", string(encoded))
		}
	}
	if value, ok := c.Get(pluginruntime.ContextKeyPinnedRoute); ok {
		if pinned, ok := value.(pluginruntime.PinnedRoute); ok && pinned.Plugin != nil && pinned.Route.Render != "" {
			if view, err := service.BuildTaskPluginView(outcome.Task); err == nil {
				if encoded, err := taskPluginProtocolJSONValue(view); err == nil {
					if requestValue, exists := c.Get(pluginruntime.ContextKeyRouteRequest); exists {
						if requestContext, ok := requestValue.(pluginruntime.RouteRequestContext); ok {
							if body, err := pinned.Plugin.Engine.CallPath(c.Request.Context(), "native", []string{pinned.Route.Render}, requestContext.JSValue(), encoded); err == nil {
								c.JSON(http.StatusOK, body)
								return
							}
						}
					}
				}
			}
		}
	}
	if value, ok := c.Get(pluginruntime.ContextKeyPinnedEndpoint); ok {
		if pinned, ok := value.(pluginruntime.PinnedEndpoint); ok && pinned.Protocol == "openai_video" && pinned.Operation.Name == "create" {
			c.JSON(http.StatusOK, outcome.Task.ToOpenAIVideo())
			return
		}
	}
	if outcome.Result != nil && outcome.Result.Response != nil && outcome.Result.Response.ClientResponse != nil {
		c.JSON(http.StatusOK, outcome.Result.Response.ClientResponse)
		return
	}
	createdAt := outcome.Task.CreatedAt
	if createdAt == 0 {
		createdAt = outcome.Task.SubmitTime
	}
	c.JSON(http.StatusOK, map[string]any{
		"id": outcome.Task.TaskID, "task_id": outcome.Task.TaskID,
		"status": "queued", "model": outcome.RelayInfo.OriginModelName, "created_at": createdAt,
	})
}

func respondTaskSubmissionError(c *gin.Context, taskErr *dto.TaskError) {
	if taskErr == nil {
		taskErr = service.TaskErrorWrapperLocal(errors.New("task submission failed"), "task_submit_failed", http.StatusInternalServerError)
	}
	if middleware.RespondTaskPluginError(c, taskErr) {
		return
	}
	respondTaskError(c, taskErr)
}

// executeTaskSubmission performs channel selection, retry, persistence and
// billing without writing an HTTP response. Protocol bridges can therefore
// observe the same durable task outcome as the native task endpoint.
func executeTaskSubmission(c *gin.Context, relayInfo *relaycommon.RelayInfo) (*taskSubmissionOutcome, *dto.TaskError) {
	if relayInfo == nil {
		return nil, service.TaskErrorWrapperLocal(errors.New("relay info is nil"), "invalid_relay_info", http.StatusInternalServerError)
	}
	var result *relay.TaskSubmitResult
	var task *model.Task
	var taskErr *dto.TaskError
	durable := false
	defer func() {
		if durable || (result != nil && !result.CanRefund()) {
			return
		}
		if relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
		if task != nil && task.ID > 0 {
			if failed, _, err := model.FailTaskBeforeSubmission(c, task.ID, "task submission failed"); err == nil && failed != nil {
				*task = *failed
			}
		}
	}()

	retryParam := &service.RetryParam{Ctx: c, TokenGroup: relayInfo.TokenGroup, ModelName: relayInfo.OriginModelName, RequestPath: c.Request.URL.Path, Retry: common.GetPointer(0)}
	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		var channel *model.Channel
		if locked, ok := relayInfo.LockedChannel.(*model.Channel); ok && locked != nil {
			channel = locked
			if retryParam.GetRetry() > 0 {
				if setupErr := middlewareSetupTaskChannel(c, channel, relayInfo.OriginModelName); setupErr != nil {
					taskErr = setupErr
					break
				}
			}
		} else {
			var channelErr *types.NewAPIError
			channel, channelErr = getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				taskErr = service.TaskErrorWrapperLocal(channelErr.Err, "get_channel_failed", channelErr.StatusCode)
				break
			}
		}
		if channel == nil {
			taskErr = service.TaskErrorWrapperLocal(errors.New("channel unavailable"), "get_channel_failed", http.StatusServiceUnavailable)
			break
		}
		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			status := http.StatusBadRequest
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				status = http.StatusRequestEntityTooLarge
			}
			taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", status)
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)
		result, taskErr = relay.RelayTaskSubmit(c, relayInfo, task)
		if result != nil {
			task = result.Task
		}
		if taskErr == nil {
			break
		}
		if !taskErr.LocalError {
			channelError := *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan())
			policyDetected := processKKAIPolicyTaskError(c, channelError, taskErr)
			processChannelErrorAfterKKAIPolicy(c, channelError, kkaiTaskAPIError(taskErr), policyDetected)
		}
		if result != nil && !result.CanRetry() || !shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}
	if len(c.GetStringSlice("use_channel")) > 1 {
		logger.LogInfo(c, fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(c.GetStringSlice("use_channel"))), "->"), "[]")))
	}
	if taskErr != nil {
		return nil, taskErr
	}
	if result == nil || task == nil {
		return nil, service.TaskErrorWrapperLocal(errors.New("task submission returned no result"), "task_submit_failed", http.StatusInternalServerError)
	}
	if settleErr := service.SettleBilling(c, relayInfo, result.Quota); settleErr != nil {
		return nil, service.TaskErrorWrapperLocal(settleErr, "settle_task_billing_failed", http.StatusInternalServerError)
	}
	task.Quota = result.Quota
	durable = true
	return &taskSubmissionOutcome{Result: result, Task: task, RelayInfo: relayInfo}, nil
}

func middlewareSetupTaskChannel(c *gin.Context, channel *model.Channel, modelName string) *dto.TaskError {
	if err := middleware.SetupContextForSelectedChannel(c, channel, modelName); err != nil {
		return service.TaskErrorWrapperLocal(err.Err, "setup_locked_channel_failed", http.StatusInternalServerError)
	}
	return nil
}

func taskSubmissionAPIError(taskErr *dto.TaskError) *types.NewAPIError {
	if taskErr == nil {
		return types.NewOpenAIError(errors.New("task submission failed"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)
	}
	err := taskErr.Error
	if err == nil {
		err = errors.New(taskErr.Message)
	}
	return types.NewOpenAIError(err, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode)
}
