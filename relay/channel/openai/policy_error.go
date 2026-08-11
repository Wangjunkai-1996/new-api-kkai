package openai

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
)

func structuredStreamError(data string) *types.NewAPIError {
	var envelope struct {
		Error any `json:"error"`
	}
	if err := common.UnmarshalJsonStr(data, &envelope); err != nil {
		return nil
	}
	return service.NewKKAIStructuredRelayErrorFromField(envelope.Error)
}

func structuredPolicyStreamError(data string) *types.NewAPIError {
	var envelope struct {
		Error any `json:"error"`
	}
	if err := common.UnmarshalJsonStr(data, &envelope); err != nil {
		return nil
	}
	apiErr := service.NewKKAIStructuredRelayErrorFromField(envelope.Error)
	if apiErr == nil {
		return nil
	}
	if service.IsKKAILocalPolicyCode(string(apiErr.GetErrorCode()), string(apiErr.GetOriginalErrorCode())) ||
		apiErr.GetPolicyEvidence() != "" {
		return apiErr
	}
	return nil
}

func structuredPolicyError(openAIError *types.OpenAIError) *types.NewAPIError {
	apiErr := service.NewKKAIStructuredRelayError(openAIError)
	if apiErr == nil {
		return nil
	}
	if service.IsKKAILocalPolicyCode(string(apiErr.GetErrorCode()), string(apiErr.GetOriginalErrorCode())) ||
		apiErr.GetPolicyEvidence() != "" {
		return apiErr
	}
	return nil
}

func responsesStreamError(response *dto.ResponsesStreamResponse) *types.NewAPIError {
	if response == nil {
		return nil
	}
	apiErr := service.NewKKAIStructuredRelayError(response.GetOpenAIError())
	if apiErr == nil {
		return nil
	}
	// A Responses error object is a terminal stream event. Keep the established
	// no-retry behavior even for ordinary provider failures; policy attribution
	// still depends exclusively on the structured code/evidence.
	return types.NewError(apiErr, apiErr.GetErrorCode(), types.ErrOptionWithSkipRetry())
}
