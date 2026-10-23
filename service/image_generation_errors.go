package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// ClassifyImageGenerationError uses structured diagnostics only. Provider text
// can contain credentials, advertisements, or misleading account instructions.
func ClassifyImageGenerationError(err error) string {
	var apiErr *types.NewAPIError
	hasAPIError := errors.As(err, &apiErr)
	if errors.Is(err, ErrInvalidImageRelayResponse) || errors.Is(err, ErrImageRelayResponseTooLarge) || errors.Is(err, ErrImageStudioResponseTooLarge) ||
		hasAPIError && apiErr.GetErrorType() == types.ErrorTypeNewAPIError && apiErr.GetOriginalStatusCode() == 0 && apiErr.GetOriginalErrorCode() == "archive_failed" {
		return "image_result_unavailable"
	}
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
		return "image_request_timeout"
	}
	if errors.Is(err, ErrImageStudioTokenRequired) || errors.Is(err, ErrImageStudioTokenInvalid) ||
		errors.Is(err, ErrImageStudioTokenGroupUnavailable) || errors.Is(err, ErrImageStudioTokenIPForbidden) ||
		errors.Is(err, ErrImageStudioTokenModelForbidden) {
		return "image_service_unavailable"
	}
	if !hasAPIError {
		return ""
	}
	if IsDefinitiveImageProviderError(err) {
		return ImageExecutionFailureProviderError
	}
	local := apiErr.GetErrorType() == types.ErrorTypeNewAPIError && apiErr.GetOriginalStatusCode() == 0
	code := strings.ToLower(strings.TrimSpace(string(apiErr.GetOriginalErrorCode())))
	if code == "insufficient_user_quota" {
		if local {
			return "insufficient_user_quota"
		}
		return "image_service_unavailable"
	}
	status := apiErr.GetOriginalStatusCode()
	if status == 0 {
		status = apiErr.StatusCode
	}
	publicCode := imageGenerationErrorCode(code)
	if publicCode == ImageExecutionFailureContentPolicy {
		if status == http.StatusOK || status >= 400 && status < 500 {
			return ImageExecutionFailureContentPolicy
		}
		return "image_service_unavailable"
	}
	// Broad input errors must not blame the user's settings for a gateway or
	// internal failure. Specific policy/result codes retain their diagnostics.
	if publicCode != "" && publicCode != "image_invalid_request" && publicCode != "image_reference_invalid" {
		return publicCode
	}
	switch status {
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return "image_request_timeout"
	case http.StatusTooManyRequests:
		return "image_rate_limited"
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusPaymentRequired, http.StatusNotFound, 499:
		return "image_service_unavailable"
	}
	if status >= 500 {
		return "image_service_unavailable"
	}
	if publicCode != "" && code != "invalid_request_error" {
		return publicCode
	}
	errorType := strings.ToLower(strings.TrimSpace(apiErr.ToOpenAIError().Type))
	if errorType == "insufficient_user_quota" {
		return "image_service_unavailable"
	}
	if typeCode := imageGenerationErrorCode(errorType); typeCode != "" {
		if typeCode == ImageExecutionFailureContentPolicy && status != http.StatusOK && (status < 400 || status >= 500) {
			return "image_service_unavailable"
		}
		return typeCode
	}
	if publicCode != "" {
		return publicCode
	}
	switch status {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return "image_invalid_request"
	}
	return ""
}

// IsDefinitiveImageProviderError identifies the gateway envelope used when an
// upstream image provider returned a structured error body. A transport error
// or an unstructured upstream failure remains unknown because the provider may
// have accepted the request before the connection failed.
func IsDefinitiveImageProviderError(err error) bool {
	var apiErr *types.NewAPIError
	if !errors.As(err, &apiErr) || apiErr.GetErrorType() != types.ErrorTypeOpenAIError {
		return false
	}
	status := apiErr.GetOriginalStatusCode()
	if status == 0 {
		status = apiErr.StatusCode
	}
	if status != http.StatusOK && status != http.StatusBadGateway {
		return false
	}
	oaiErr := apiErr.ToOpenAIError()
	code, ok := oaiErr.Code.(string)
	return ok && strings.EqualFold(strings.TrimSpace(code), ImageExecutionFailureProviderError) && strings.TrimSpace(oaiErr.Type) != ""
}

func imageGenerationErrorCode(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "upstream_content_policy", "content_policy", "content_policy_violation", "content_filter", "safety", "prompt_blocked", "sensitive_words_detected":
		return ImageExecutionFailureContentPolicy
	case "image_provider_error", "image_service_unavailable", "insufficient_balance", "insufficient_quota", "insufficient_funds", "balance_not_enough", "upstream_insufficient_balance", "billing_hard_limit_reached", "billing_not_active", "quota_exceeded", "pre_consume_token_quota_failed", "invalid_api_key", "api_key_invalid", "api_key_expired", "authentication_error", "invalid_authentication", "permission_denied", "permission_error", "access_denied", "channel:no_available_key", "channel:invalid_key", "get_channel_failed", "model_not_found", "do_request_failed", "server_error", "internal_error", "service_unavailable", "overloaded_error", "policy_audit_unavailable", "request_policy_blocked", "session_blocked_by_cyber_policy", "cyber_policy", "key_cooldown", "account_pool_exhausted", "ali_error", "minimax_image_error", "jimeng_error", "zhipu_image_error":
		return "image_service_unavailable"
	case "image_rate_limited", "rate_limit_exceeded", "rate_limit_error", "resource_exhausted", "egress_capacity_exhausted", "account_pool_rate_limited":
		return "image_rate_limited"
	case "image_request_timeout", "timeout", "request_timeout", "gateway_timeout", "channel:response_time_exceeded", "image_queue_expired":
		return "image_request_timeout"
	case "image_invalid_request", "invalid_request", "invalid_request_error", "invalid_argument", "bad_request_body", "policy_context_incomplete":
		return "image_invalid_request"
	case "image_reference_invalid", "invalid_image", "invalid_image_format", "invalid_image_url", "image_parse_error", "image_too_large", "unsupported_image":
		return "image_reference_invalid"
	case "image_result_unavailable", "response_too_large", "invalid_provider_response", "archive_failed", "incomplete_accounting_intent", "read_response_body_failed", "bad_response", "bad_response_body", "empty_response":
		return "image_result_unavailable"
	case "upstream_outcome_unknown", "submission_interrupted":
		return "upstream_outcome_unknown"
	case "image_prepare_failed", "invalid_execution_payload":
		return "image_prepare_failed"
	case "insufficient_user_quota":
		return "insufficient_user_quota"
	case "image_partial_result", "partial_archive":
		return "image_partial_result"
	case "image_generation_failed", "relay_failed", "billing_not_settled", "provider_usage_exceeded_quote":
		return "image_generation_failed"
	default:
		return ""
	}
}

func imageGenerationPublicError(status, storedCode string) (string, string) {
	switch status {
	case model.ImageGenerationStatusSucceeded, model.ImageGenerationStatusSubmitting, model.ImageGenerationStatusRecovering:
		return "", ""
	case model.ImageGenerationStatusPartial:
		return "image_partial_result", "Only some requested images are available. You can view the completed images."
	}
	code := imageGenerationErrorCode(storedCode)
	if code == "" {
		switch status {
		case model.ImageGenerationStatusUnknown:
			code = "upstream_outcome_unknown"
		case model.ImageGenerationStatusArchiveFailed:
			code = "image_result_unavailable"
		default:
			code = "image_generation_failed"
		}
	}
	if status == model.ImageGenerationStatusUnknown {
		return code, "The generation result could not be confirmed. Please contact support before submitting again."
	}
	switch code {
	case "upstream_content_policy":
		return code, "This generation was blocked by a content safety check. The check may apply to the prompt, reference image, or generated result."
	case "image_service_unavailable":
		return code, "Image generation is temporarily unavailable. Try another model or try again later."
	case "image_rate_limited":
		return code, "The image service is busy. Please try again later."
	case "image_request_timeout":
		return code, "Image generation timed out. Please try again later."
	case "image_invalid_request":
		return code, "The image settings are not supported. Adjust the settings and try again."
	case "image_reference_invalid":
		return code, "The reference image could not be processed. Replace it and try again."
	case "image_result_unavailable":
		return code, "The image results could not be delivered. Please contact support."
	case "upstream_outcome_unknown":
		return code, "The generation result could not be confirmed. Please contact support before submitting again."
	case "insufficient_user_quota":
		return code, "Your account quota is insufficient. Top up and try again."
	case "image_partial_result":
		return code, "Only some requested images are available. You can view the completed images."
	default:
		return code, "Image generation could not be completed. Please try again later or contact support."
	}
}
