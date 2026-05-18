package controller

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func publicOpenAIError(c *gin.Context, apiErr *types.NewAPIError) (int, types.OpenAIError) {
	if apiErr == nil {
		return http.StatusInternalServerError, types.OpenAIError{
			Message: types.PublicMessageUpstreamError,
			Type:    string(types.ErrorTypeUpstreamError),
			Code:    types.ErrorCodeBadResponse,
		}
	}

	classification := service.ClassifyPolicyIncident(apiErr)
	if classification.Detected {
		caseID := policyIncidentCaseID(c)
		if classification.ClientTokenActionAllowed {
			return http.StatusForbidden, withPolicyCaseID(types.OpenAIError{
				Message: types.PublicMessageRequestBlockedByPolicy,
				Type:    string(types.ErrorTypeNewAPIError),
				Code:    types.ErrorCodePolicyBlocked,
			}, caseID)
		}
		return http.StatusServiceUnavailable, withPolicyCaseID(upstreamUnavailableOpenAIError(), caseID)
	}

	if isPolicyIncidentContext(c) {
		caseID := policyIncidentCaseID(c)
		return http.StatusForbidden, withPolicyCaseID(types.OpenAIError{
			Message: types.PublicMessageRequestBlockedByPolicy,
			Type:    string(types.ErrorTypeNewAPIError),
			Code:    types.ErrorCodePolicyBlocked,
		}, caseID)
	}

	if types.IsUpstreamUnavailableError(apiErr) {
		return http.StatusServiceUnavailable, upstreamUnavailableOpenAIError()
	}

	openAIError := apiErr.ToOpenAIError()
	if shouldSanitizeNoisyUpstreamError(apiErr, openAIError.Message) {
		openAIError.Message = types.PublicMessageUpstreamError
		openAIError.Type = string(types.ErrorTypeUpstreamError)
		openAIError.Code = types.ErrorTypeUpstreamError
		openAIError.Param = ""
		openAIError.Metadata = nil
		openAIError.CaseID = ""
	}
	statusCode := apiErr.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusInternalServerError
	}
	return statusCode, openAIError
}

func publicClaudeError(c *gin.Context, apiErr *types.NewAPIError) (int, types.ClaudeError) {
	statusCode, openAIError := publicOpenAIError(c, apiErr)
	return statusCode, types.ClaudeError{
		Message: openAIError.Message,
		Type:    fmt.Sprintf("%v", openAIError.Code),
		CaseID:  openAIError.CaseID,
	}
}

func publicTaskError(c *gin.Context, taskErr *dto.TaskError) *dto.TaskError {
	if taskErr == nil {
		return nil
	}
	publicErr := *taskErr

	classification := taskPolicyIncidentClassification(taskErr)
	if classification.Detected {
		publicErr.CaseID = policyIncidentCaseID(c)
		publicErr.Code = string(types.ErrorCodePolicyBlocked)
		publicErr.Message = types.PublicMessageRequestBlockedByPolicy
		publicErr.StatusCode = http.StatusForbidden
		if !classification.ClientTokenActionAllowed {
			publicErr.Code = string(types.ErrorCodeUpstreamUnavailable)
			publicErr.Message = types.PublicMessageUpstreamUnavailable
			publicErr.StatusCode = http.StatusServiceUnavailable
		}
		return &publicErr
	}

	if isTaskUpstreamUnavailableError(taskErr) {
		publicErr.Code = string(types.ErrorCodeUpstreamUnavailable)
		publicErr.Message = types.PublicMessageUpstreamUnavailable
		publicErr.StatusCode = http.StatusServiceUnavailable
		return &publicErr
	}

	if taskErr.StatusCode == http.StatusServiceUnavailable && types.LooksLikeNoisyUpstreamMessage(taskErr.Message) {
		publicErr.Code = string(types.ErrorCodeUpstreamUnavailable)
		publicErr.Message = types.PublicMessageUpstreamUnavailable
		return &publicErr
	}
	if !taskErr.LocalError && types.LooksLikeNoisyUpstreamMessage(taskErr.Message) {
		publicErr.Code = string(types.ErrorTypeUpstreamError)
		publicErr.Message = types.PublicMessageUpstreamError
	}
	return &publicErr
}

func taskPolicyIncidentClassification(taskErr *dto.TaskError) service.PolicyIncidentClassification {
	if taskErr == nil {
		return service.PolicyIncidentClassification{}
	}
	apiErr := types.NewErrorWithStatusCode(errors.New(taskErr.Message), types.ErrorCode(taskErr.Code), taskErr.StatusCode)
	classification := service.ClassifyPolicyIncident(apiErr)
	if classification.Detected || taskErr.Error == nil {
		return classification
	}
	apiErr = types.NewErrorWithStatusCode(taskErr.Error, types.ErrorCode(taskErr.Code), taskErr.StatusCode)
	return service.ClassifyPolicyIncident(apiErr)
}

func isTaskUpstreamUnavailableError(taskErr *dto.TaskError) bool {
	if taskErr == nil {
		return false
	}
	switch taskErr.Code {
	case "policy_breaker_open", "channel_no_available_key":
		return true
	}
	apiErr := types.NewErrorWithStatusCode(errors.New(taskErr.Message), types.ErrorCode(taskErr.Code), taskErr.StatusCode)
	if types.IsUpstreamUnavailableError(apiErr) {
		return true
	}
	if taskErr.Error == nil {
		return false
	}
	apiErr = types.NewErrorWithStatusCode(taskErr.Error, types.ErrorCode(taskErr.Code), taskErr.StatusCode)
	return types.IsUpstreamUnavailableError(apiErr)
}

func upstreamUnavailableOpenAIError() types.OpenAIError {
	return types.OpenAIError{
		Message: types.PublicMessageUpstreamUnavailable,
		Type:    string(types.ErrorTypeUpstreamError),
		Code:    types.ErrorCodeUpstreamUnavailable,
	}
}

func withPolicyCaseID(openAIError types.OpenAIError, caseID string) types.OpenAIError {
	if caseID == "" {
		return openAIError
	}
	openAIError.CaseID = caseID
	metadata, err := common.Marshal(map[string]string{"case_id": caseID})
	if err == nil {
		openAIError.Metadata = metadata
	}
	return openAIError
}

func isPolicyIncidentContext(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return common.GetContextKeyBool(c, constant.ContextKeyPolicyIncidentDetected) ||
		service.ShouldSkipRetryAfterPolicyIncident(c)
}

func shouldSanitizeNoisyUpstreamError(apiErr *types.NewAPIError, message string) bool {
	if apiErr == nil {
		return false
	}
	if apiErr.GetErrorType() != types.ErrorTypeOpenAIError && apiErr.GetErrorCode() != types.ErrorCodeBadResponseStatusCode {
		return false
	}
	return types.LooksLikeNoisyUpstreamMessage(message)
}

func policyIncidentCaseID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if caseID := c.GetString("policy_incident_case_id"); caseID != "" {
		return caseID
	}
	requestID := c.GetString(common.RequestIdKey)
	if requestID == "" || model.DB == nil {
		return ""
	}
	var event model.PolicyIncidentEvent
	if err := model.DB.Where("request_id = ?", requestID).Order("id DESC").First(&event).Error; err != nil {
		return ""
	}
	return policyIncidentCaseIDFromMetadata(event.Metadata)
}

func policyIncidentCaseIDFromMetadata(metadata model.JSONValue) string {
	if len(metadata) == 0 {
		return ""
	}
	var decoded map[string]any
	if err := common.Unmarshal([]byte(metadata), &decoded); err != nil {
		return ""
	}
	caseID, _ := decoded["case_id"].(string)
	return caseID
}
