package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	kkaiPublicUpstreamUnavailable = "upstream unavailable"
	kkaiPublicUpstreamError       = "upstream error"
	kkaiUpstreamUnavailableCode   = "upstream_unavailable"
)

func kkaiPublicOpenAIError(c *gin.Context, apiErr *types.NewAPIError) (int, types.OpenAIError) {
	if apiErr == nil {
		return http.StatusInternalServerError, kkaiGenericUpstreamError()
	}
	if apiErr.GetErrorCode() == types.ErrorCodeSensitiveWordsDetected || apiErr.GetErrorCode() == types.ErrorCodePromptBlocked {
		return http.StatusBadRequest, types.OpenAIError{
			Message: service.KKAIPolicyMessageForKeyword(),
			Type:    string(types.ErrorTypeNewAPIError),
			Code:    types.ErrorCodePromptBlocked,
		}
	}
	classification := service.ClassifyKKAIUpstreamPolicyError(apiErr)
	causality := classification.Causality
	if causality == "" && c != nil {
		causality = c.GetString(service.KKAIPolicyCausalityContextKey)
	}
	if classification.Detected || service.ShouldSkipRetryAfterKKAIPolicy(c) {
		return kkaiPublicPolicyOpenAIError(c, causality)
	}

	status := apiErr.StatusCode
	if status == 0 {
		status = http.StatusInternalServerError
	}
	openAIError := apiErr.ToOpenAIError()
	if kkaiIsPublicUpstreamError(apiErr) && kkaiPublicPayloadUnsafe(c, openAIError.Message, openAIError.Param, string(openAIError.Metadata)) {
		return status, kkaiGenericUpstreamError()
	}
	openAIError.Message = kkaiScrubPublicText(c, openAIError.Message)
	openAIError.Param = kkaiScrubPublicText(c, openAIError.Param)
	if kkaiPublicTextUnsafe(c, string(openAIError.Metadata)) {
		openAIError.Metadata = nil
	}
	return status, openAIError
}

func kkaiPublicClaudeError(c *gin.Context, apiErr *types.NewAPIError) (int, types.ClaudeError) {
	status, openAIError := kkaiPublicOpenAIError(c, apiErr)
	return status, types.ClaudeError{
		Type:    publicErrorCode(openAIError),
		Message: openAIError.Message,
	}
}

func kkaiPublicTaskError(c *gin.Context, taskErr *dto.TaskError) *dto.TaskError {
	if taskErr == nil {
		return nil
	}
	publicErr := *taskErr
	if taskErr.Code == string(types.ErrorCodeSensitiveWordsDetected) || taskErr.Code == string(types.ErrorCodePromptBlocked) {
		publicErr.StatusCode = http.StatusBadRequest
		publicErr.Code = string(types.ErrorCodePromptBlocked)
		publicErr.Message = service.KKAIPolicyMessageForKeyword()
		publicErr.Data = nil
		publicErr.Error = nil
		return &publicErr
	}
	classification := service.ClassifyKKAITaskPolicyError(taskErr)
	causality := classification.Causality
	if causality == "" && c != nil {
		causality = c.GetString(service.KKAIPolicyCausalityContextKey)
	}
	if classification.Detected || service.ShouldSkipRetryAfterKKAIPolicy(c) {
		publicErr.Data = kkaiPolicyCaseData(c)
		publicErr.Error = nil
		if causality == service.KKAIPolicyCausalityClientToken {
			publicErr.StatusCode = http.StatusForbidden
			publicErr.Code = string(types.ErrorCodeConversationPolicyViolation)
			publicErr.Message = kkaiPolicyMessage(c)
			setKKAIPolicyRetryAfter(c)
			return &publicErr
		}
		publicErr.StatusCode = http.StatusServiceUnavailable
		publicErr.Code = kkaiUpstreamUnavailableCode
		publicErr.Message = kkaiPublicUpstreamUnavailable
		return &publicErr
	}

	if !taskErr.LocalError && kkaiPublicPayloadUnsafe(c, taskErr.Message, taskErrorText(taskErr)) {
		publicErr.Code = string(types.ErrorTypeUpstreamError)
		publicErr.Message = kkaiPublicUpstreamError
		publicErr.Data = nil
		publicErr.Error = nil
		return &publicErr
	}
	if !taskErr.LocalError && kkaiPublicTaskDataUnsafe(c, taskErr.Data) {
		publicErr.Data = nil
	}
	publicErr.Message = kkaiScrubPublicText(c, publicErr.Message)
	publicErr.Error = nil
	return &publicErr
}

func kkaiPublicPolicyOpenAIError(c *gin.Context, causality string) (int, types.OpenAIError) {
	if state, ok := service.KKAIPolicyCooldownStateFromContext(c); ok && state.Blocked {
		setKKAIPolicyRetryAfter(c)
		if state.Reason == service.KKAIPolicyCooldownReasonWords {
			scope, _ := service.KKAIPolicyConversationScopeFromContext(c)
			return http.StatusForbidden, types.OpenAIError{
				Message:  service.KKAIPolicyMessageForKeywordCooldown(state.RetryAfter, scope.Stable),
				Type:     string(types.ErrorTypeNewAPIError),
				Code:     types.ErrorCodeConversationPolicyViolation,
				Metadata: kkaiPolicyCaseMetadata(c),
			}
		}
		scope, _ := service.KKAIPolicyConversationScopeFromContext(c)
		return http.StatusForbidden, types.OpenAIError{
			Message:  service.KKAIPolicyMessageForCyber(state.RetryAfter, scope.Stable),
			Type:     string(types.ErrorTypeNewAPIError),
			Code:     types.ErrorCodeConversationPolicyViolation,
			Metadata: kkaiPolicyCaseMetadata(c),
		}
	}
	if causality == service.KKAIPolicyCausalityClientToken {
		return http.StatusForbidden, types.OpenAIError{
			Message:  service.KKAIPolicyMessageForCyber(0, true),
			Type:     string(types.ErrorTypeNewAPIError),
			Code:     types.ErrorCodeConversationPolicyViolation,
			Metadata: kkaiPolicyCaseMetadata(c),
		}
	}
	return http.StatusServiceUnavailable, types.OpenAIError{
		Message:  kkaiPublicUpstreamUnavailable,
		Type:     string(types.ErrorTypeUpstreamError),
		Code:     kkaiUpstreamUnavailableCode,
		Metadata: kkaiPolicyCaseMetadata(c),
	}
}

func kkaiGenericUpstreamError() types.OpenAIError {
	return types.OpenAIError{
		Message: kkaiPublicUpstreamError,
		Type:    string(types.ErrorTypeUpstreamError),
		Code:    types.ErrorTypeUpstreamError,
	}
}

func kkaiPolicyCaseMetadata(c *gin.Context) []byte {
	caseData := kkaiPolicyCaseData(c)
	if caseData == nil {
		return nil
	}
	data, _ := common.Marshal(caseData)
	return data
}

func kkaiPolicyCaseData(c *gin.Context) map[string]any {
	if c == nil {
		return nil
	}
	data := make(map[string]any)
	if caseID := c.GetString(service.KKAIPolicyCaseContextKey); caseID != "" {
		data["case_id"] = caseID
	}
	if scope, ok := service.KKAIPolicyConversationScopeFromContext(c); ok {
		data["scope"] = scope.PublicScope()
	}
	if state, ok := service.KKAIPolicyCooldownStateFromContext(c); ok {
		if state.RetryAfter > 0 {
			data["retry_after_seconds"] = state.RetryAfter
		}
		if state.Strike > 0 {
			data["cooldown_level"] = state.Strike
		}
	}
	if len(data) == 0 {
		return nil
	}
	return data
}

func kkaiPolicyMessage(c *gin.Context) string {
	state, hasState := service.KKAIPolicyCooldownStateFromContext(c)
	scope, _ := service.KKAIPolicyConversationScopeFromContext(c)
	if hasState && state.Reason == service.KKAIPolicyCooldownReasonWords {
		return service.KKAIPolicyMessageForKeywordCooldown(state.RetryAfter, scope.Stable)
	}
	if hasState {
		return service.KKAIPolicyMessageForCyber(state.RetryAfter, scope.Stable)
	}
	return service.KKAIPolicyMessageForCyber(0, scope.Stable)
}

func setKKAIPolicyRetryAfter(c *gin.Context) {
	state, ok := service.KKAIPolicyCooldownStateFromContext(c)
	if !ok || state.RetryAfter <= 0 || c == nil {
		return
	}
	c.Header("Retry-After", fmt.Sprintf("%d", state.RetryAfter))
}

func publicErrorCode(openAIError types.OpenAIError) string {
	if code, ok := openAIError.Code.(string); ok && code != "" {
		return code
	}
	return openAIError.Type
}

func taskErrorText(taskErr *dto.TaskError) string {
	if taskErr == nil || taskErr.Error == nil {
		return ""
	}
	return taskErr.Error.Error()
}
