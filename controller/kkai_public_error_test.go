package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type kkaiControllerPolicyTestApplier struct{}

func (kkaiControllerPolicyTestApplier) Apply(context.Context, service.RiskActionInput) (*service.RiskActionResult, error) {
	return &service.RiskActionResult{IncidentID: 1}, nil
}

func newKKAIPublicErrorTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("Authorization", "Bearer sk-client-secret")
	ctx.Set("token_key", "client-secret")
	ctx.Set(service.KKAIPolicyCaseContextKey, "policy-case-1")
	return ctx
}

func TestKKAIPublicOpenAIErrorRedactsClientPolicyIncident(t *testing.T) {
	ctx := newKKAIPublicErrorTestContext(t)
	apiErr := types.NewErrorWithStatusCode(
		errors.New("cyber_policy visit https://ads.example with Bearer sk-client-secret"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)

	status, publicErr := kkaiPublicOpenAIError(ctx, apiErr)
	require.Equal(t, http.StatusForbidden, status)
	assert.Equal(t, "request blocked by policy", publicErr.Message)
	assert.Equal(t, "policy_blocked", publicErr.Code)
	assert.NotContains(t, publicErr.Message, "cyber_policy")
	assert.NotContains(t, publicErr.Message, "secret")
	assert.Contains(t, string(publicErr.Metadata), "policy-case-1")
}

func TestKKAIPublicOpenAIErrorTreatsUpstreamKeyAsUnavailable(t *testing.T) {
	ctx := newKKAIPublicErrorTestContext(t)
	apiErr := types.NewErrorWithStatusCode(
		errors.New("provider API key has been disabled"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)

	status, publicErr := kkaiPublicOpenAIError(ctx, apiErr)
	require.Equal(t, http.StatusServiceUnavailable, status)
	assert.Equal(t, "upstream unavailable", publicErr.Message)
	assert.Equal(t, "upstream_unavailable", publicErr.Code)
	assert.NotContains(t, publicErr.Message, "key")
}

func TestKKAIPublicTaskErrorDoesNotMutateOrLeakOriginal(t *testing.T) {
	ctx := newKKAIPublicErrorTestContext(t)
	original := &dto.TaskError{
		Code:       "bad_response_status_code",
		Message:    "cyber_policy Bearer sk-client-secret",
		StatusCode: http.StatusForbidden,
		Error:      errors.New("cyber_policy Bearer sk-client-secret"),
	}

	publicErr := kkaiPublicTaskError(ctx, original)
	require.NotSame(t, original, publicErr)
	assert.Equal(t, "request blocked by policy", publicErr.Message)
	assert.Equal(t, "policy_blocked", publicErr.Code)
	assert.Equal(t, "cyber_policy Bearer sk-client-secret", original.Message)
}

func TestKKAIPublicErrorSanitizesUnsafeUpstreamPayload(t *testing.T) {
	ctx := newKKAIPublicErrorTestContext(t)
	metadata, err := common.Marshal(map[string]any{"authorization": "Bearer sk-client-secret"})
	require.NoError(t, err)
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Message:  "provider says buy key at https://ads.example",
		Param:    "Bearer sk-client-secret",
		Code:     "invalid_request",
		Metadata: metadata,
	}, http.StatusBadGateway)

	status, publicErr := kkaiPublicOpenAIError(ctx, apiErr)
	assert.Equal(t, http.StatusBadGateway, status)
	assert.Equal(t, "upstream error", publicErr.Message)
	assert.Empty(t, publicErr.Param)
	assert.Empty(t, publicErr.Metadata)
}

func TestKKAIPolicyContextStopsNormalAndTaskRetries(t *testing.T) {
	ctx := newKKAIPublicErrorTestContext(t)
	ctx.Set(common.RequestIdKey, "req-policy-retry")
	ctx.Set("id", 10)
	ctx.Set("token_id", 11)
	apiErr := types.NewErrorWithStatusCode(errors.New("cyber_policy"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)
	guard := service.NewKKAIPolicyIncidentGuard(kkaiControllerPolicyTestApplier{})
	detected, err := guard.HandleAPIError(ctx, types.ChannelError{}, apiErr)
	require.NoError(t, err)
	require.True(t, detected)

	assert.False(t, shouldRetry(ctx, apiErr, 3))
	assert.False(t, shouldRetryTaskRelay(ctx, 1, &dto.TaskError{StatusCode: http.StatusInternalServerError}, 3))
}
