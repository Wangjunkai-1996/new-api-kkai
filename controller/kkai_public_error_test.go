package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
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
	assert.Equal(t, service.KKAIPolicyMessageForCyber(), publicErr.Message)
	assert.Equal(t, types.ErrorCodeRequestPolicyWarning, publicErr.Code)
	assert.Empty(t, ctx.Writer.Header().Get("Retry-After"))
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
	assert.Equal(t, "服务暂时不可用，请稍后重试。", publicErr.Message)
	assert.Equal(t, "upstream_unavailable", publicErr.Code)
	assert.NotContains(t, publicErr.Message, "key")
	assert.NotContains(t, publicErr.Message, "上游")
}

func TestKKAIPublicOpenAIErrorUsesKeywordWarning(t *testing.T) {
	ctx := newKKAIPublicErrorTestContext(t)
	apiErr := types.NewErrorWithStatusCode(
		errors.New("prompt blocked; matched internal rule"),
		types.ErrorCodeSensitiveWordsDetected,
		http.StatusBadRequest,
	)

	status, publicErr := kkaiPublicOpenAIError(ctx, apiErr)
	require.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, service.KKAIPolicyMessageForKeyword(), publicErr.Message)
	assert.Equal(t, types.ErrorCodePromptBlocked, publicErr.Code)
	assert.NotContains(t, publicErr.Message, "internal rule")
}

func TestKKAIPublicOpenAIErrorPrioritizesCyberOverKeywordCode(t *testing.T) {
	ctx := newKKAIPublicErrorTestContext(t)
	apiErr := types.NewErrorWithStatusCode(
		errors.New("cyber_policy"),
		types.ErrorCodeSensitiveWordsDetected,
		http.StatusForbidden,
	)

	status, publicErr := kkaiPublicOpenAIError(ctx, apiErr)
	require.Equal(t, http.StatusForbidden, status)
	assert.Equal(t, service.KKAIPolicyMessageForCyber(), publicErr.Message)
	assert.Equal(t, types.ErrorCodeRequestPolicyWarning, publicErr.Code)
	assert.Empty(t, ctx.Writer.Header().Get("Retry-After"))
}

func TestKKAIPublicOpenAIErrorPreservesUpstreamPromptBlocked(t *testing.T) {
	ctx := newKKAIPublicErrorTestContext(t)
	apiErr := types.NewErrorWithStatusCode(
		errors.New("request blocked by Gemini API: SAFETY"),
		types.ErrorCodePromptBlocked,
		http.StatusBadRequest,
	)

	status, publicErr := kkaiPublicOpenAIError(ctx, apiErr)
	require.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, types.ErrorCodePromptBlocked, publicErr.Code)
	assert.Contains(t, publicErr.Message, "request blocked by Gemini API")
	assert.NotContains(t, publicErr.Message, "你输入的内容包含暂不支持的信息")
}

func TestKKAIPublicClaudeErrorKeepsPolicyCode(t *testing.T) {
	ctx := newKKAIPublicErrorTestContext(t)
	apiErr := types.NewErrorWithStatusCode(errors.New("cyber_policy"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)

	status, publicErr := kkaiPublicClaudeError(ctx, apiErr)
	require.Equal(t, http.StatusForbidden, status)
	assert.Equal(t, string(types.ErrorCodeRequestPolicyWarning), publicErr.Type)
	assert.Equal(t, service.KKAIPolicyMessageForCyber(), publicErr.Message)
	assert.Empty(t, ctx.Writer.Header().Get("Retry-After"))
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
	assert.Equal(t, http.StatusForbidden, publicErr.StatusCode)
	assert.Equal(t, service.KKAIPolicyMessageForCyber(), publicErr.Message)
	assert.Equal(t, string(types.ErrorCodeRequestPolicyWarning), publicErr.Code)
	assert.Empty(t, ctx.Writer.Header().Get("Retry-After"))
	assert.Equal(t, "cyber_policy Bearer sk-client-secret", original.Message)
}

func TestKKAIPublicTaskErrorPrioritizesCyberOverKeywordCode(t *testing.T) {
	ctx := newKKAIPublicErrorTestContext(t)
	publicErr := kkaiPublicTaskError(ctx, &dto.TaskError{
		Code:       string(types.ErrorCodeSensitiveWordsDetected),
		Message:    "cyber_policy",
		StatusCode: http.StatusForbidden,
	})

	require.NotNil(t, publicErr)
	assert.Equal(t, http.StatusForbidden, publicErr.StatusCode)
	assert.Equal(t, string(types.ErrorCodeRequestPolicyWarning), publicErr.Code)
	assert.Equal(t, service.KKAIPolicyMessageForCyber(), publicErr.Message)
	assert.Empty(t, ctx.Writer.Header().Get("Retry-After"))
}

func TestRespondTaskErrorPreservesPolicyWarnings(t *testing.T) {
	tests := []struct {
		name            string
		taskErr         *dto.TaskError
		expectedStatus  int
		expectedMessage string
	}{
		{
			name: "cyber warning",
			taskErr: &dto.TaskError{
				Code:       "bad_response_status_code",
				Message:    "cyber_policy",
				StatusCode: http.StatusForbidden,
			},
			expectedStatus:  http.StatusForbidden,
			expectedMessage: service.KKAIPolicyMessageForCyber(),
		},
		{
			name: "upstream capacity",
			taskErr: &dto.TaskError{
				Code:       "rate_limit_exceeded",
				Message:    "rate limit exceeded",
				StatusCode: http.StatusTooManyRequests,
			},
			expectedStatus:  http.StatusTooManyRequests,
			expectedMessage: "当前分组上游负载已饱和，请稍后再试",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

			respondTaskError(ctx, test.taskErr)

			require.Equal(t, test.expectedStatus, recorder.Code)
			var response dto.TaskError
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Equal(t, test.expectedMessage, response.Message)
		})
	}
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
