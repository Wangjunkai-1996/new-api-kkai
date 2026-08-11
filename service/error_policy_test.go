package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRelayErrorHandlerClassifiesPlainTextCyberWithoutExposingBody(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(strings.NewReader("cyber_policy Bearer sk-client-secret")),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)
	classification := ClassifyKKAIUpstreamPolicyError(newAPIError)

	require.True(t, classification.Detected)
	require.Equal(t, KKAIPolicyCausalityAmbiguous, classification.Causality)
	require.Equal(t, http.StatusForbidden, newAPIError.GetOriginalStatusCode())
	require.Equal(t, "bad response status code 403", newAPIError.Error())
	require.Equal(t, "cyber_policy", newAPIError.GetPolicyEvidence())
}

func TestRelayErrorHandlerPreservesCodeOnlyCyberAcrossMappingAndNormalization(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Body: io.NopCloser(strings.NewReader(
			`{"error":{"message":"Failed check: SAFETY_CHECK_TYPE","type":"policy_error","code":"cyber_policy"}}`,
		)),
	}

	apiErr := RelayErrorHandler(context.Background(), resp, false)
	ResetStatusCode(apiErr, `{"403":401}`)
	normalized := NormalizeViolationFeeError(apiErr)
	classification := ClassifyKKAIUpstreamPolicyError(normalized)

	require.True(t, classification.Detected)
	require.Equal(t, KKAIPolicyCausalityClientToken, classification.Causality)
	require.Equal(t, http.StatusUnauthorized, normalized.StatusCode)
	require.Equal(t, http.StatusForbidden, normalized.GetOriginalStatusCode())
	require.Equal(t, types.ErrorCode("cyber_policy"), normalized.GetOriginalErrorCode())
}

func TestRelayAndTaskErrorHandlersConfirmStructuredCyberOnBadRequest(t *testing.T) {
	body := `{"error":{"message":"request rejected","type":"policy_error","code":"cyber_policy"}}`
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	apiErr := RelayErrorHandler(context.Background(), resp, false)
	classification := ClassifyKKAIUpstreamPolicyError(apiErr)
	require.True(t, classification.Detected)
	require.Equal(t, KKAIPolicyCausalityClientToken, classification.Causality)
	require.Equal(t, http.StatusBadRequest, classification.StatusCode)
	require.Equal(t, types.ErrorCode("cyber_policy"), apiErr.GetOriginalErrorCode())

	taskErr := TaskErrorWrapperUpstream(errors.New(body), "fail_to_fetch_task", http.StatusBadRequest)
	taskClassification := ClassifyKKAITaskPolicyError(taskErr)
	require.True(t, taskClassification.Detected)
	require.Equal(t, KKAIPolicyCausalityClientToken, taskClassification.Causality)
	require.Equal(t, http.StatusBadRequest, taskClassification.StatusCode)
	require.Equal(t, "cyber_policy", taskErr.UpstreamErrorCode)
}

func TestRelayAndTaskErrorHandlersDetectUnauthorizedUpstreamKeyWithoutClientPenalty(t *testing.T) {
	body := `{"error":{"message":"provider API key has been permanently disabled","type":"authentication_error","code":"invalid_api_key"}}`
	resp := &http.Response{
		StatusCode: http.StatusUnauthorized,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	apiErr := RelayErrorHandler(context.Background(), resp, false)
	classification := ClassifyKKAIUpstreamPolicyError(apiErr)
	require.True(t, classification.Detected)
	require.Equal(t, KKAIPolicyCausalityUpstreamKey, classification.Causality)
	require.Equal(t, http.StatusUnauthorized, classification.StatusCode)

	taskErr := TaskErrorWrapperUpstream(errors.New(body), "fail_to_fetch_task", http.StatusUnauthorized)
	taskClassification := ClassifyKKAITaskPolicyError(taskErr)
	require.True(t, taskClassification.Detected)
	require.Equal(t, KKAIPolicyCausalityUpstreamKey, taskClassification.Causality)
	require.Equal(t, http.StatusUnauthorized, taskClassification.StatusCode)
}

func TestRelayErrorHandlerClassifiesCyberMarkerInUnsupportedJSON(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(strings.NewReader(`{"unexpected":"cyber_policy Bearer sk-client-secret"}`)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)
	classification := ClassifyKKAIUpstreamPolicyError(newAPIError)

	require.True(t, classification.Detected)
	require.Equal(t, KKAIPolicyCausalityAmbiguous, classification.Causality)
	require.Equal(t, http.StatusForbidden, newAPIError.GetOriginalStatusCode())
	require.Equal(t, "cyber_policy", newAPIError.GetPolicyEvidence())
	require.NotContains(t, newAPIError.Error(), "sk-client-secret")
}

func TestRelayErrorHandlerDoesNotLogCyberResponseBody(t *testing.T) {
	withDebugEnabled(t, false)

	var logBuffer bytes.Buffer
	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(strings.NewReader("cyber_policy Bearer sk-client-secret")),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.Equal(t, "bad response status code 403", newAPIError.Error())
	require.NotContains(t, logBuffer.String(), "sk-client-secret")
	require.NotContains(t, logBuffer.String(), "Bearer")
}

func TestRelayErrorHandlerDoesNotExposeCyberBodyWhenBodyDisplayRequested(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(strings.NewReader("cyber_policy Bearer sk-client-secret")),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, true)

	require.Equal(t, "bad response status code 403", newAPIError.Error())
	require.Equal(t, "cyber_policy", newAPIError.GetPolicyEvidence())
	require.NotContains(t, newAPIError.Error(), "sk-client-secret")
}

func TestTaskErrorFromAPIErrorMarksLocalBillingFailures(t *testing.T) {
	apiErr := types.NewErrorWithStatusCode(
		errors.New("cyber_policy"),
		types.ErrorCodePreConsumeTokenQuotaFailed,
		http.StatusForbidden,
	)

	taskErr := TaskErrorFromAPIError(apiErr)

	require.True(t, taskErr.LocalError)
	require.Zero(t, taskErr.UpstreamStatusCode)
	require.False(t, ClassifyKKAITaskPolicyError(taskErr).Detected)
}

func TestRelayAndTaskErrorHandlersPreserveLocalPolicyCodesWithoutCyberClassification(t *testing.T) {
	for _, code := range []types.ErrorCode{
		types.ErrorCodeRequestPolicyBlocked,
		types.ErrorCodePolicyContextIncomplete,
		types.ErrorCodePolicyAuditUnavailable,
		types.ErrorCodeSessionBlockedByCyberPolicy,
	} {
		t.Run(string(code), func(t *testing.T) {
			resp := &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body: io.NopCloser(strings.NewReader(
					`{"error":{"code":"` + string(code) + `","message":"cyber_policy"}}`,
				)),
			}

			apiErr := RelayErrorHandler(context.Background(), resp, false)
			require.Equal(t, code, apiErr.GetErrorCode())
			require.Equal(t, http.StatusServiceUnavailable, apiErr.GetOriginalStatusCode())
			require.True(t, types.IsSkipRetryError(apiErr))
			require.False(t, ClassifyKKAIUpstreamPolicyError(apiErr).Detected)

			taskErr := TaskErrorWrapperUpstream(
				errors.New(`{"error":{"code":"`+string(code)+`","message":"cyber_policy"}}`),
				"fail_to_fetch_task",
				http.StatusServiceUnavailable,
			)
			require.Equal(t, KKAILocalPolicyStatus(code), apiErr.StatusCode)
			require.Equal(t, string(code), taskErr.Code)
			require.Equal(t, http.StatusServiceUnavailable, taskErr.UpstreamStatusCode)
			require.False(t, ClassifyKKAITaskPolicyError(taskErr).Detected)
		})
	}
}

func TestRelayAndTaskErrorHandlersUseStructuredDetailReason(t *testing.T) {
	tests := []struct {
		code   types.ErrorCode
		status int
	}{
		{code: types.ErrorCodeRequestPolicyBlocked, status: http.StatusForbidden},
		{code: types.ErrorCodePolicyContextIncomplete, status: http.StatusUnprocessableEntity},
		{code: types.ErrorCodePolicyAuditUnavailable, status: http.StatusServiceUnavailable},
	}

	for _, test := range tests {
		t.Run(string(test.code), func(t *testing.T) {
			body := `{"error":{"code":` + fmt.Sprint(test.status) + `,"message":"policy rejected","status":"FAILED_PRECONDITION","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"` + string(test.code) + `","domain":"sub2api"}]}}`
			resp := &http.Response{
				StatusCode: test.status,
				Body:       io.NopCloser(strings.NewReader(body)),
			}

			apiErr := RelayErrorHandler(context.Background(), resp, false)
			require.Equal(t, test.code, apiErr.GetErrorCode())
			require.Equal(t, test.status, apiErr.StatusCode)
			require.Equal(t, test.status, apiErr.GetOriginalStatusCode())
			require.True(t, types.IsSkipRetryError(apiErr))
			require.False(t, ClassifyKKAIUpstreamPolicyError(apiErr).Detected)

			taskErr := TaskErrorWrapperUpstream(errors.New(body), "fail_to_fetch_task", test.status)
			require.Equal(t, string(test.code), taskErr.Code)
			require.Equal(t, string(test.code), taskErr.UpstreamErrorCode)
			require.Equal(t, string(test.code), taskErr.PolicyEvidence)
			require.False(t, ClassifyKKAITaskPolicyError(taskErr).Detected)
		})
	}
}

func TestRelayErrorHandlerDoesNotTrustLocalPolicyCodeMentionedInUpstreamMessage(t *testing.T) {
	for _, message := range []string{
		"request_policy_blocked",
		"session_blocked_by_cyber_policy",
	} {
		t.Run(message, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: http.StatusForbidden,
				Body: io.NopCloser(strings.NewReader(
					`{"error":{"code":"cyber_policy","message":"` + message + `"}}`,
				)),
			}

			apiErr := RelayErrorHandler(context.Background(), resp, false)
			classification := ClassifyKKAIUpstreamPolicyError(apiErr)

			require.NotEqual(t, types.ErrorCode(message), apiErr.GetErrorCode())
			require.True(t, classification.Detected)
			require.Equal(t, KKAIPolicyCausalityClientToken, classification.Causality)
		})
	}
}

func TestTaskErrorWrapperDoesNotTrustLocalPolicyCodeMentionedInUpstreamMessage(t *testing.T) {
	for _, message := range []string{
		"request_policy_blocked",
		"session_blocked_by_cyber_policy",
	} {
		t.Run(message, func(t *testing.T) {
			taskErr := TaskErrorWrapperUpstream(
				errors.New(`{"error":{"code":"cyber_policy","message":"`+message+`"}}`),
				"fail_to_fetch_task",
				http.StatusForbidden,
			)

			require.Equal(t, "fail_to_fetch_task", taskErr.Code)
			classification := ClassifyKKAITaskPolicyError(taskErr)
			require.True(t, classification.Detected)
			require.Equal(t, KKAIPolicyCausalityClientToken, classification.Causality)
		})
	}
}

func TestTaskErrorWrapperKeepsPlainTextCyberAmbiguous(t *testing.T) {
	taskErr := TaskErrorWrapperUpstream(
		errors.New("cyber_policy"),
		"fail_to_fetch_task",
		http.StatusForbidden,
	)

	classification := ClassifyKKAITaskPolicyError(taskErr)
	require.True(t, classification.Detected)
	require.Equal(t, KKAIPolicyCausalityAmbiguous, classification.Causality)
}

func TestNewKKAIStructuredRelayErrorRequiresPolicyEvidenceForNoRetry(t *testing.T) {
	ordinary := NewKKAIStructuredRelayError(&types.OpenAIError{
		Code:    "invalid_request",
		Message: "ordinary validation error",
	})
	require.NotNil(t, ordinary)
	require.False(t, types.IsSkipRetryError(ordinary))
	require.False(t, ClassifyKKAIUpstreamPolicyError(ordinary).Detected)

	ambiguous := NewKKAIStructuredRelayError(&types.OpenAIError{
		Code:    "invalid_request",
		Message: "cyber_policy appeared in an untrusted message",
	})
	require.NotNil(t, ambiguous)
	require.True(t, types.IsSkipRetryError(ambiguous))
	classification := ClassifyKKAIUpstreamPolicyError(ambiguous)
	require.True(t, classification.Detected)
	require.Equal(t, KKAIPolicyCausalityAmbiguous, classification.Causality)

	confirmed := NewKKAIStructuredRelayError(&types.OpenAIError{
		Code:    "cyber_policy",
		Message: "request rejected",
	})
	require.NotNil(t, confirmed)
	require.True(t, types.IsSkipRetryError(confirmed))
	classification = ClassifyKKAIUpstreamPolicyError(confirmed)
	require.True(t, classification.Detected)
	require.Equal(t, KKAIPolicyCausalityClientToken, classification.Causality)
}
