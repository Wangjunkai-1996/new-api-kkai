package relay

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/service"
)

func embeddedTaskPolicyError(statusCode int, responseBody []byte) *dto.TaskError {
	var envelope dto.GeneralErrorResponse
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

	if service.IsKKAILocalPolicyCode(upstreamErr.Code) {
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

func rejectParsedTaskResponse(task *model.Task, result *TaskSubmitResult, responseErr *channel.TaskResponseError) *dto.TaskError {
	policyErr := responseErr.TaskError != nil &&
		(service.IsKKAILocalPolicyCode(responseErr.TaskError.Code) ||
			service.ClassifyKKAITaskPolicyError(responseErr.TaskError).Detected)
	result.Outcome = TaskSubmitRejected
	if resetErr := resetTaskSubmissionClaim(task); resetErr != nil {
		result.Outcome = TaskSubmitUnknown
		task.PrivateData.TargetQuota = quotaPointer(result.Quota)
		unknownErr := markTaskSubmissionUnknown(task, fmt.Errorf("reset submission claim after adaptor rejection: %w", resetErr))
		if policyErr {
			return responseErr.TaskError
		}
		return unknownErr
	}
	if responseErr.TaskError != nil {
		return responseErr.TaskError
	}
	return service.TaskErrorWrapperLocal(errors.New("upstream rejected task submission"), "task_rejected", http.StatusBadRequest)
}
