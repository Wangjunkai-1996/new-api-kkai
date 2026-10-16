package relay

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
)

// An explicit balance rejection without an accepted task ID is safe to refund,
// even when a provider puts its business error inside HTTP 200 or 500.
func taskSubmissionBalanceRejected(responseBody []byte) bool {
	var envelope struct {
		ID     string `json:"id"`
		TaskID string `json:"task_id"`
		Data   any    `json:"data"`
		Error  *struct {
			Code    any    `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if common.Unmarshal(responseBody, &envelope) != nil || envelope.Error == nil ||
		envelope.ID != "" || envelope.TaskID != "" || envelope.Data != nil {
		return false
	}
	for _, code := range []string{fmt.Sprint(envelope.Error.Code), envelope.Error.Type} {
		switch strings.ToLower(strings.TrimSpace(code)) {
		case "insufficient_balance", "insufficient_quota", "insufficient_funds", "balance_not_enough", "upstream_insufficient_balance":
			return true
		}
	}
	switch strings.ToLower(strings.TrimSpace(envelope.Error.Message)) {
	case "用户余额不足", "账户余额不足", "余额不足", "insufficient balance", "insufficient quota", "insufficient funds":
		return true
	}
	return false
}

func embeddedTaskPolicyError(statusCode int, responseBody []byte) *dto.TaskError {
	var envelope relaydto.GeneralErrorResponse
	if err := common.Unmarshal(responseBody, &envelope); err != nil || common.GetJsonType(envelope.Error) != "object" {
		return nil
	}

	apiErr := service.NewKKAIStructuredRelayErrorFromField(envelope.Error)
	if apiErr == nil {
		return nil
	}
	localCode := service.KKAILocalPolicyCode(string(apiErr.GetErrorCode()), string(apiErr.GetOriginalErrorCode()))
	if localCode == "" && !service.ClassifyKKAIUpstreamPolicyError(apiErr).Detected {
		return nil
	}

	taskErr := service.TaskErrorWrapperUpstream(errors.New(string(responseBody)), "fail_to_fetch_task", statusCode)
	taskErr.StatusCode = apiErr.StatusCode
	taskErr.UpstreamErrorCode = string(apiErr.GetOriginalErrorCode())
	taskErr.PolicyEvidence = apiErr.GetPolicyEvidence()
	if localCode != "" {
		taskErr.Code = string(localCode)
	}
	return taskErr
}

func rejectTaskHTTPResponse(task *model.Task, result *TaskSubmitResult, statusCode int, responseBody []byte) *dto.TaskError {
	upstreamErr := service.TaskErrorWrapperUpstream(
		fmt.Errorf("%s", string(responseBody)),
		"fail_to_fetch_task",
		statusCode,
	)
	policyDetected := service.ClassifyKKAITaskPolicyError(upstreamErr).Detected
	balanceRejected := taskSubmissionBalanceRejected(responseBody)
	if balanceRejected {
		upstreamErr.Code = "upstream_insufficient_balance"
		upstreamErr.StatusCode = http.StatusPaymentRequired
	}

	if service.IsKKAILocalPolicyCode(upstreamErr.Code) || balanceRejected {
		result.Outcome = TaskSubmitRejected
		if resetErr := resetTaskSubmissionClaim(task); resetErr != nil {
			result.Outcome = TaskSubmitUnknown
			task.PrivateData.TargetQuota = quotaPointer(result.Quota)
			_ = markTaskSubmissionUnknown(task, fmt.Errorf("reset submission claim after policy rejection: %w", resetErr))
		}
		return upstreamErr
	}
	if taskHTTPStatusIsAmbiguous(statusCode) {
		result.Outcome = TaskSubmitUnknown
		task.PrivateData.TargetQuota = quotaPointer(result.Quota)
		unknownErr := markTaskSubmissionUnknown(task, fmt.Errorf("upstream returned ambiguous status %d: %s", statusCode, responseBody))
		if policyDetected {
			return upstreamErr
		}
		return unknownErr
	}

	result.Outcome = TaskSubmitRejected
	if resetErr := resetTaskSubmissionClaim(task); resetErr != nil {
		result.Outcome = TaskSubmitUnknown
		task.PrivateData.TargetQuota = quotaPointer(result.Quota)
		unknownErr := markTaskSubmissionUnknown(task, fmt.Errorf("reset submission claim after upstream rejection: %w", resetErr))
		if policyDetected {
			return upstreamErr
		}
		return unknownErr
	}
	return upstreamErr
}

// rejectParsedTaskError handles the new plugin parser contract. A parser
// returns the host DTO directly; the host owns the durable claim reset after
// the response has been classified as a rejected submission.
func rejectParsedTaskError(task *model.Task, result *TaskSubmitResult, taskErr *dto.TaskError) *dto.TaskError {
	if taskErr == nil {
		taskErr = service.TaskErrorWrapperLocal(errors.New("upstream rejected task submission"), "task_rejected", http.StatusBadRequest)
	}
	policyErr := service.IsKKAILocalPolicyCode(taskErr.Code) || service.ClassifyKKAITaskPolicyError(taskErr).Detected
	result.Outcome = TaskSubmitRejected
	if resetErr := resetTaskSubmissionClaim(task); resetErr != nil {
		result.Outcome = TaskSubmitUnknown
		task.PrivateData.TargetQuota = quotaPointer(result.Quota)
		unknownErr := markTaskSubmissionUnknown(task, fmt.Errorf("reset submission claim after adaptor rejection: %w", resetErr))
		if policyErr {
			return taskErr
		}
		return unknownErr
	}
	return taskErr
}
