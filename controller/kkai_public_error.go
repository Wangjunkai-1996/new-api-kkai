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
	kkaiPublicUpstreamUnavailable = "服务暂时不可用，请稍后重试。"
	kkaiPublicUpstreamError       = "upstream error"
	kkaiUpstreamUnavailableCode   = "upstream_unavailable"
)

func kkaiPublicOpenAIError(c *gin.Context, apiErr *types.NewAPIError) (int, types.OpenAIError) {
	if apiErr == nil {
		return http.StatusInternalServerError, kkaiGenericUpstreamError()
	}
	if localCode := service.KKAILocalPolicyCode(string(apiErr.GetErrorCode()), string(apiErr.GetOriginalErrorCode())); localCode != "" {
		return kkaiPublicLocalPolicyOpenAIError(localCode)
	}
	classification := service.ClassifyKKAIUpstreamPolicyError(apiErr)
	causality := classification.Causality
	if causality == "" && c != nil {
		causality = c.GetString(service.KKAIPolicyCausalityContextKey)
	}
	if classification.Detected || service.ShouldSkipRetryAfterKKAIPolicy(c) {
		return kkaiPublicPolicyOpenAIError(c, causality)
	}
	if apiErr.GetErrorCode() == types.ErrorCodeSensitiveWordsDetected {
		return http.StatusBadRequest, types.OpenAIError{
			Message: service.KKAIPolicyMessageForKeyword(),
			Type:    string(types.ErrorTypeNewAPIError),
			Code:    types.ErrorCodePromptBlocked,
		}
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
	if localCode := service.KKAILocalPolicyCode(taskErr.Code); localCode != "" {
		status, openAIError := kkaiPublicLocalPolicyOpenAIError(localCode)
		publicErr.StatusCode = status
		publicErr.Code = string(localCode)
		publicErr.Message = openAIError.Message
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
			publicErr.Code = string(types.ErrorCodeRequestPolicyWarning)
			publicErr.Message = service.KKAIPolicyMessageForCyber()
			return &publicErr
		}
		publicErr.StatusCode = http.StatusServiceUnavailable
		publicErr.Code = kkaiUpstreamUnavailableCode
		publicErr.Message = kkaiPublicUpstreamUnavailable
		return &publicErr
	}
	if taskErr.Code == string(types.ErrorCodeSensitiveWordsDetected) {
		publicErr.StatusCode = http.StatusBadRequest
		publicErr.Code = string(types.ErrorCodePromptBlocked)
		publicErr.Message = service.KKAIPolicyMessageForKeyword()
		publicErr.Data = nil
		publicErr.Error = nil
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

func kkaiPublicLocalPolicyOpenAIError(code types.ErrorCode) (int, types.OpenAIError) {
	status := service.KKAILocalPolicyStatus(code)
	return status, types.OpenAIError{
		Message: service.KKAIPolicyMessageForLocalCode(code),
		Type:    string(types.ErrorTypeNewAPIError),
		Code:    code,
	}
}

func kkaiPublicPolicyOpenAIError(c *gin.Context, causality string) (int, types.OpenAIError) {
	if causality == service.KKAIPolicyCausalityClientToken {
		return http.StatusForbidden, types.OpenAIError{
			Message:  service.KKAIPolicyMessageForCyber(),
			Type:     string(types.ErrorTypeNewAPIError),
			Code:     types.ErrorCodeRequestPolicyWarning,
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

func kkaiPolicyCaseData(c *gin.Context) map[string]string {
	if c == nil || c.GetString(service.KKAIPolicyCaseContextKey) == "" {
		return nil
	}
	return map[string]string{"case_id": c.GetString(service.KKAIPolicyCaseContextKey)}
}

func publicErrorCode(openAIError types.OpenAIError) string {
	switch code := openAIError.Code.(type) {
	case string:
		if code != "" {
			return code
		}
	case types.ErrorCode:
		if code != "" {
			return string(code)
		}
	}
	return openAIError.Type
}

func taskErrorText(taskErr *dto.TaskError) string {
	if taskErr == nil || taskErr.Error == nil {
		return ""
	}
	return taskErr.Error.Error()
}
