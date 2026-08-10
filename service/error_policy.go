package service

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
)

func kkaiOpenAIErrorFromGeneralError(errResponse dto.GeneralErrorResponse) *types.OpenAIError {
	if common.GetJsonType(errResponse.Error) != "object" {
		return nil
	}
	var openAIError types.OpenAIError
	if err := common.Unmarshal(errResponse.Error, &openAIError); err != nil {
		return nil
	}
	return &openAIError
}

func kkaiStructuredErrorCode(openAIError *types.OpenAIError) string {
	if openAIError == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(openAIError.Code))
}

func kkaiStructuredPolicyEvidence(openAIError *types.OpenAIError) string {
	if openAIError == nil {
		return ""
	}
	return kkaiPolicyMarkerEvidence(kkaiStructuredErrorCode(openAIError) + " " + openAIError.Message)
}

// NewKKAIStructuredRelayError converts an in-band provider error object into a
// relay error. Only policy-bearing or local fail-closed errors suppress retry;
// unrelated response fields and raw response text are never trusted evidence.
func NewKKAIStructuredRelayError(openAIError *types.OpenAIError) *types.NewAPIError {
	if openAIError == nil {
		return nil
	}
	code := kkaiStructuredErrorCode(openAIError)
	if code == "" && strings.TrimSpace(openAIError.Message) == "" && strings.TrimSpace(openAIError.Type) == "" {
		return nil
	}

	statusCode := http.StatusBadGateway
	var options []types.NewAPIErrorOptions
	if localCode := KKAILocalPolicyCode(code); localCode != "" {
		statusCode = KKAILocalPolicyStatus(localCode)
		options = append(options,
			types.ErrOptionWithSkipRetry(),
			types.ErrOptionWithOriginalStatusCode(statusCode),
			types.ErrOptionWithOriginalErrorCode(localCode),
			types.ErrOptionWithPolicyEvidence(string(localCode)),
		)
	} else if evidence := kkaiStructuredPolicyEvidence(openAIError); evidence != "" {
		// In-band policy frames use the same canonical status as an HTTP 403.
		// The exact structured code still determines whether user action is allowed.
		statusCode = http.StatusForbidden
		options = append(options,
			types.ErrOptionWithSkipRetry(),
			types.ErrOptionWithOriginalStatusCode(http.StatusForbidden),
			types.ErrOptionWithOriginalErrorCode(types.ErrorCode(code)),
			types.ErrOptionWithPolicyEvidence(evidence),
		)
	}
	return types.WithOpenAIError(*openAIError, statusCode, options...)
}
