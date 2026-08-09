package service

import (
	"bytes"
	"context"
	"errors"
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
	require.Equal(t, KKAIPolicyCausalityClientToken, classification.Causality)
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

func TestRelayErrorHandlerClassifiesCyberMarkerInUnsupportedJSON(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(strings.NewReader(`{"unexpected":"cyber_policy Bearer sk-client-secret"}`)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)
	classification := ClassifyKKAIUpstreamPolicyError(newAPIError)

	require.True(t, classification.Detected)
	require.Equal(t, KKAIPolicyCausalityClientToken, classification.Causality)
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
