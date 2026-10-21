package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyImageGenerationErrorUsesStructuredDiagnostics(t *testing.T) {
	providerError := func(code, errorType string, status int, opts ...types.NewAPIErrorOptions) error {
		return types.WithOpenAIError(types.OpenAIError{
			Code: code, Type: errorType, Message: "insufficient_quota content_policy https://advert.example secret-key",
		}, status, opts...)
	}
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"provider_quota_before_broad_type", providerError("insufficient_quota", "invalid_request_error", 400), "image_service_unavailable"},
		{"provider_credentials_before_broad_type", providerError("invalid_api_key", "invalid_request_error", 401), "image_service_unavailable"},
		{"provider_impersonates_user_quota_code", providerError("insufficient_user_quota", "invalid_request_error", 403), "image_service_unavailable"},
		{"provider_impersonates_user_quota_type", providerError("unrecognized", "insufficient_user_quota", 403), "image_service_unavailable"},
		{"wrapped_provider_quota", types.NewErrorWithStatusCode(errors.New("private"), types.ErrorCodeInsufficientUserQuota, 403, types.ErrOptionWithOriginalStatusCode(403)), "image_service_unavailable"},
		{"local_account_quota", types.NewErrorWithStatusCode(errors.New("private"), types.ErrorCodeInsufficientUserQuota, 403), "insufficient_user_quota"},
		{"local_token_preconsume_failure", types.NewErrorWithStatusCode(errors.New("private"), types.ErrorCodePreConsumeTokenQuotaFailed, 403), "image_service_unavailable"},
		{"content_policy", providerError("content_policy_violation", "invalid_request_error", 400), "upstream_content_policy"},
		{"in_band_policy_mapped_502", providerError("content_policy", "upstream_error", 502, types.ErrOptionWithOriginalStatusCode(200)), "upstream_content_policy"},
		{"gateway_policy_mapped_400", providerError("content_policy", "server_error", 400, types.ErrOptionWithOriginalStatusCode(502)), "image_service_unavailable"},
		{"gateway_policy_alias_mapped_400", providerError("prompt_blocked", "invalid_request_error", 400, types.ErrOptionWithOriginalStatusCode(502)), "image_service_unavailable"},
		{"rate_limit", providerError("rate_limit_exceeded", "invalid_request_error", 429), "image_rate_limited"},
		{"reference_image", providerError("invalid_image_format", "invalid_request_error", 400), "image_reference_invalid"},
		{"unsupported_parameters", providerError("invalid_argument", "invalid_request_error", 400), "image_invalid_request"},
		{"parameters_before_adapter_family", providerError("invalid_argument", "ali_error", 200), "image_invalid_request"},
		{"reference_before_adapter_family", providerError("invalid_image_format", "ali_error", 400), "image_reference_invalid"},
		{"internal_invalid_request_is_not_user_error", types.NewError(errors.New("billing state conflict"), types.ErrorCodeInvalidRequest), "image_service_unavailable"},
		{"gateway_invalid_argument_is_not_user_error", providerError("invalid_argument", "invalid_request_error", 503), "image_service_unavailable"},
		{"reference_credentials_is_not_user_error", providerError("invalid_image_format", "invalid_request_error", 401), "image_service_unavailable"},
		{"ali_in_band_unknown_code", providerError("unrecognized", "ali_error", 200), "image_service_unavailable"},
		{"minimax_in_band_unknown_code", providerError("1000", "minimax_image_error", 200), "image_service_unavailable"},
		{"jimeng_in_band_unknown_code", providerError("1000", "jimeng_error", 200), "image_service_unavailable"},
		{"zhipu_in_band_unknown_code", providerError("1000", "zhipu_image_error", 200), "image_service_unavailable"},
		{"provider_pool_unavailable", providerError("account_pool_exhausted", "api_error", 503), "image_service_unavailable"},
		{"provider_pool_rate_limited", providerError("account_pool_rate_limited", "rate_limit_error", 429), "image_rate_limited"},
		{"provider_egress_rate_limited", providerError("egress_capacity_exhausted", "rate_limit_error", 429), "image_rate_limited"},
		{"provider_connection_interrupted", providerError("unrecognized", "upstream_error", 499), "image_service_unavailable"},
		{"provider_type_fallback", providerError("unrecognized", "rate_limit_error", 400), "image_rate_limited"},
		{"status_timeout", providerError("unrecognized", "upstream_error", 504), "image_request_timeout"},
		{"status_service_unavailable", providerError("unrecognized", "upstream_error", 503), "image_service_unavailable"},
		{"credentials_override_broad_request_type", providerError("unrecognized", "invalid_request_error", 401), "image_service_unavailable"},
		{"gateway_overrides_broad_request_type", providerError("unrecognized", "invalid_request_error", 503), "image_service_unavailable"},
		{"credentials_override_broad_request_code", providerError("invalid_request_error", "invalid_request_error", 401), "image_service_unavailable"},
		{"gateway_overrides_broad_request_code", providerError("invalid_request_error", "invalid_request_error", 503), "image_service_unavailable"},
		{"timeout_overrides_broad_request_type", providerError("unrecognized", "invalid_request_error", 504), "image_request_timeout"},
		{"deadline", fmt.Errorf("request: %w", context.DeadlineExceeded), "image_request_timeout"},
		{"network_timeout", &url.Error{Op: "Post", URL: "https://private.example", Err: &net.DNSError{IsTimeout: true}}, "image_request_timeout"},
		{"bad_in_band_response", providerError("bad_response", "upstream_error", 200), "image_result_unavailable"},
		{"archive_timeout_is_delivery_failure", types.NewError(context.DeadlineExceeded, "archive_failed"), "image_result_unavailable"},
		{"worker_token_required", ErrImageStudioTokenRequired, "image_service_unavailable"},
		{"worker_token_invalid", ErrImageStudioTokenInvalid, "image_service_unavailable"},
		{"worker_token_group_revoked", ErrImageStudioTokenGroupUnavailable, "image_service_unavailable"},
		{"worker_token_ip_revoked", ErrImageStudioTokenIPForbidden, "image_service_unavailable"},
		{"worker_token_model_revoked", ErrImageStudioTokenModelForbidden, "image_service_unavailable"},
		{"invalid_result", ErrInvalidImageRelayResponse, "image_result_unavailable"},
		{"oversized_result", ErrImageStudioResponseTooLarge, "image_result_unavailable"},
		{"plain_message_never_classified", errors.New("insufficient_quota content_policy https://advert.example"), ""},
		{"unknown_structured_message_never_classified", providerError("unrecognized", "upstream_error", http.StatusOK), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ClassifyImageGenerationError(tc.err))
		})
	}
}

func TestImageGenerationViewsExposeOnlyPublicErrors(t *testing.T) {
	for _, tc := range []struct {
		name, status, storedCode, wantCode, wantMessage string
	}{
		{"historical_provider_quota", model.ImageGenerationStatusFailed, "insufficient_quota", "image_service_unavailable", "Image generation is temporarily unavailable. Try another model or try again later."},
		{"local_account_quota", model.ImageGenerationStatusFailed, "insufficient_user_quota", "insufficient_user_quota", "Your account quota is insufficient. Top up and try again."},
		{"historical_policy", model.ImageGenerationStatusFailed, "content_policy", "upstream_content_policy", "The prompt or reference image did not pass the content safety check. Adjust the content and try again."},
		{"unknown_raw_code", model.ImageGenerationStatusFailed, "visit https://advert.example secret-key", "image_generation_failed", "Image generation could not be completed. Please try again later or contact support."},
		{"unknown_result", model.ImageGenerationStatusUnknown, "", "upstream_outcome_unknown", "The generation result could not be confirmed. Please contact support before submitting again."},
		{"unknown_timeout_is_not_retry_advice", model.ImageGenerationStatusUnknown, "image_request_timeout", "image_request_timeout", "The generation result could not be confirmed. Please contact support before submitting again."},
		{"unknown_provider_quota_is_not_retry_advice", model.ImageGenerationStatusUnknown, "image_service_unavailable", "image_service_unavailable", "The generation result could not be confirmed. Please contact support before submitting again."},
		{"archive_failed", model.ImageGenerationStatusArchiveFailed, "", "image_result_unavailable", "The image results could not be delivered. Please contact support."},
		{"partial_is_not_total_failure", model.ImageGenerationStatusPartial, "relay_failed", "image_partial_result", "Only some requested images are available. You can view the completed images."},
		{"success_clears_old_error", model.ImageGenerationStatusSucceeded, "content_policy", "", ""},
		{"pending_hides_recovery_reason", model.ImageGenerationStatusSubmitting, "image_request_timeout", "", ""},
		{"recovery_hides_old_error", model.ImageGenerationStatusRecovering, "content_policy", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newImageLibraryTestDB(t)
			generation := createImageLibraryGeneration(t, db, 7, "public-error")
			require.NoError(t, db.Model(&generation).Updates(map[string]any{
				"status": tc.status, "error_code": tc.storedCode,
				"error_message": "private-provider-diagnostic https://advert.example secret-key",
			}).Error)
			detail, err := GetImageGenerationView(context.Background(), db, 7, generation.ID)
			require.NoError(t, err)
			assert.Equal(t, tc.wantCode, detail.ErrorCode)
			assert.Equal(t, tc.wantMessage, detail.ErrorMessage)
			page, err := ListImageGenerations(context.Background(), db, 7, ImageGenerationFilter{})
			require.NoError(t, err)
			require.Len(t, page.Items, 1)
			assert.Equal(t, detail.ErrorCode, page.Items[0].ErrorCode)
			assert.Equal(t, detail.ErrorMessage, page.Items[0].ErrorMessage)
		})
	}
}
