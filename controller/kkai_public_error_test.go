package controller

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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
	ctx.Set("token_id", 42)
	ctx.Set(service.KKAIPolicyCaseContextKey, "policy-case-1")
	return ctx
}

func TestKKAIPublicOpenAIErrorDoesNotClaimKeyCooldownWithoutKey(t *testing.T) {
	ctx := newKKAIPublicErrorTestContext(t)
	ctx.Set("token_id", 0)
	apiErr := newControllerUpstreamPolicyError("cyber_policy", types.ErrorCode("cyber_policy"), http.StatusForbidden)

	status, publicErr := kkaiPublicOpenAIError(ctx, apiErr)

	require.Equal(t, http.StatusForbidden, status)
	assert.Equal(t, service.KKAIPolicyMessageForCyberWithoutKey(), publicErr.Message)
	assert.NotContains(t, publicErr.Message, "API Key")
	assert.Contains(t, publicErr.Message, "请勿破甲或滥用，否则将封禁账号")
}

func newControllerUpstreamPolicyError(message string, code types.ErrorCode, statusCode int) *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New(message),
		code,
		statusCode,
		types.ErrOptionWithOriginalStatusCode(statusCode),
		types.ErrOptionWithPolicyEvidence(message+" "+string(code)),
	)
}

func TestKKAIPublicOpenAIErrorRedactsClientPolicyIncident(t *testing.T) {
	ctx := newKKAIPublicErrorTestContext(t)
	apiErr := newControllerUpstreamPolicyError(
		"cyber_policy visit https://ads.example with Bearer sk-client-secret",
		types.ErrorCode("cyber_policy"),
		http.StatusForbidden,
	)

	status, publicErr := kkaiPublicOpenAIError(ctx, apiErr)
	require.Equal(t, http.StatusForbidden, status)
	assert.Equal(t, service.KKAIPolicyMessageForCyberWithoutKey(), publicErr.Message)
	assert.Contains(t, publicErr.Message, "请勿破甲或滥用，否则将封禁账号")
	assert.Equal(t, types.ErrorCodeRequestPolicyWarning, publicErr.Code)
	assert.Empty(t, ctx.Writer.Header().Get("Retry-After"))
	assert.NotContains(t, publicErr.Message, "cyber_policy")
	assert.NotContains(t, publicErr.Message, "secret")
	assert.Contains(t, string(publicErr.Metadata), "policy-case-1")
}

func TestKKAIPublicOpenAIErrorTreatsUpstreamKeyAsUnavailable(t *testing.T) {
	ctx := newKKAIPublicErrorTestContext(t)
	apiErr := newControllerUpstreamPolicyError(
		"provider API key has been disabled",
		types.ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)

	status, publicErr := kkaiPublicOpenAIError(ctx, apiErr)
	require.Equal(t, http.StatusServiceUnavailable, status)
	assert.Equal(t, "服务暂时不可用，请稍后重试。", publicErr.Message)
	assert.Equal(t, "upstream_unavailable", publicErr.Code)
	assert.NotContains(t, publicErr.Message, "key")
	assert.NotContains(t, publicErr.Message, "上游")
	assert.NotContains(t, publicErr.Message, "已停用")
}

func TestKKAIPublicOpenAIErrorTreatsAmbiguousPolicyAsUnavailable(t *testing.T) {
	ctx := newKKAIPublicErrorTestContext(t)
	apiErr := newControllerUpstreamPolicyError(
		"cyber_policy; provider API key has been disabled",
		types.ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)

	status, publicErr := kkaiPublicOpenAIError(ctx, apiErr)
	require.Equal(t, http.StatusServiceUnavailable, status)
	assert.Equal(t, kkaiPublicUpstreamUnavailable, publicErr.Message)
	assert.Equal(t, kkaiUpstreamUnavailableCode, publicErr.Code)
	assert.NotContains(t, publicErr.Message, "已停用")
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
		types.ErrOptionWithOriginalStatusCode(http.StatusForbidden),
		types.ErrOptionWithOriginalErrorCode(types.ErrorCode("cyber_policy")),
		types.ErrOptionWithPolicyEvidence("cyber_policy"),
	)

	status, publicErr := kkaiPublicOpenAIError(ctx, apiErr)
	require.Equal(t, http.StatusForbidden, status)
	assert.Equal(t, service.KKAIPolicyMessageForCyberWithoutKey(), publicErr.Message)
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
	apiErr := newControllerUpstreamPolicyError("cyber_policy", types.ErrorCode("cyber_policy"), http.StatusForbidden)

	status, publicErr := kkaiPublicClaudeError(ctx, apiErr)
	require.Equal(t, http.StatusForbidden, status)
	assert.Equal(t, string(types.ErrorCodeRequestPolicyWarning), publicErr.Type)
	assert.Equal(t, service.KKAIPolicyMessageForCyberWithoutKey(), publicErr.Message)
	assert.Empty(t, ctx.Writer.Header().Get("Retry-After"))
}

func TestKKAIPublicTaskErrorDoesNotMutateOrLeakOriginal(t *testing.T) {
	ctx := newKKAIPublicErrorTestContext(t)
	original := &dto.TaskError{
		Code:               "bad_response_status_code",
		UpstreamErrorCode:  "cyber_policy",
		Message:            "cyber_policy Bearer sk-client-secret",
		StatusCode:         http.StatusForbidden,
		UpstreamStatusCode: http.StatusForbidden,
		PolicyEvidence:     "cyber_policy",
		Error:              errors.New("cyber_policy Bearer sk-client-secret"),
	}

	publicErr := kkaiPublicTaskError(ctx, original)
	require.NotSame(t, original, publicErr)
	assert.Equal(t, http.StatusForbidden, publicErr.StatusCode)
	assert.Equal(t, service.KKAIPolicyMessageForCyberWithoutKey(), publicErr.Message)
	assert.Equal(t, string(types.ErrorCodeRequestPolicyWarning), publicErr.Code)
	assert.Empty(t, ctx.Writer.Header().Get("Retry-After"))
	assert.Equal(t, "cyber_policy Bearer sk-client-secret", original.Message)
}

func TestKKAIPublicTaskErrorPrioritizesCyberOverKeywordCode(t *testing.T) {
	ctx := newKKAIPublicErrorTestContext(t)
	publicErr := kkaiPublicTaskError(ctx, &dto.TaskError{
		Code:               string(types.ErrorCodeSensitiveWordsDetected),
		UpstreamErrorCode:  "cyber_policy",
		Message:            "cyber_policy",
		StatusCode:         http.StatusForbidden,
		UpstreamStatusCode: http.StatusForbidden,
		PolicyEvidence:     "cyber_policy",
	})

	require.NotNil(t, publicErr)
	assert.Equal(t, http.StatusForbidden, publicErr.StatusCode)
	assert.Equal(t, string(types.ErrorCodeRequestPolicyWarning), publicErr.Code)
	assert.Equal(t, service.KKAIPolicyMessageForCyberWithoutKey(), publicErr.Message)
	assert.Empty(t, ctx.Writer.Header().Get("Retry-After"))
}

func TestKKAIPublicTaskErrorTreatsNonClientPolicyAsUnavailable(t *testing.T) {
	tests := []struct {
		name     string
		evidence string
	}{
		{name: "upstream key", evidence: "provider API key has been disabled"},
		{name: "ambiguous", evidence: "cyber_policy; provider API key has been disabled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := newKKAIPublicErrorTestContext(t)
			publicErr := kkaiPublicTaskError(ctx, &dto.TaskError{
				Code:               "bad_response_status_code",
				Message:            test.evidence,
				StatusCode:         http.StatusForbidden,
				UpstreamStatusCode: http.StatusForbidden,
				PolicyEvidence:     test.evidence,
			})

			require.NotNil(t, publicErr)
			assert.Equal(t, http.StatusServiceUnavailable, publicErr.StatusCode)
			assert.Equal(t, kkaiUpstreamUnavailableCode, publicErr.Code)
			assert.Equal(t, kkaiPublicUpstreamUnavailable, publicErr.Message)
			assert.NotContains(t, publicErr.Message, "已停用")
		})
	}
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
				Code:               "bad_response_status_code",
				UpstreamErrorCode:  "cyber_policy",
				Message:            "cyber_policy",
				StatusCode:         http.StatusForbidden,
				UpstreamStatusCode: http.StatusForbidden,
				PolicyEvidence:     "cyber_policy",
			},
			expectedStatus:  http.StatusForbidden,
			expectedMessage: service.KKAIPolicyMessageForCyberWithoutKey(),
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

func TestKKAIPublicLocalPolicyCodesRemainStableAndNeverRetry(t *testing.T) {
	tests := []struct {
		code   types.ErrorCode
		status int
	}{
		{code: types.ErrorCodeRequestPolicyBlocked, status: http.StatusForbidden},
		{code: types.ErrorCodePolicyContextIncomplete, status: http.StatusUnprocessableEntity},
		{code: types.ErrorCodePolicyAuditUnavailable, status: http.StatusServiceUnavailable},
		{code: types.ErrorCodeSessionBlockedByCyberPolicy, status: http.StatusForbidden},
	}

	for _, test := range tests {
		t.Run(string(test.code), func(t *testing.T) {
			ctx := newKKAIPublicErrorTestContext(t)
			apiErr := types.NewErrorWithStatusCode(
				errors.New(string(test.code)),
				test.code,
				http.StatusTeapot,
				types.ErrOptionWithOriginalStatusCode(http.StatusTeapot),
				types.ErrOptionWithPolicyEvidence(string(test.code)),
			)

			status, publicErr := kkaiPublicOpenAIError(ctx, apiErr)
			assert.Equal(t, test.status, status)
			assert.Equal(t, test.code, publicErr.Code)
			assert.True(t, processKKAIPolicyAPIError(ctx, types.ChannelError{}, apiErr))
			assert.False(t, shouldRetry(ctx, apiErr, 3))

			taskErr := &dto.TaskError{
				Code:               string(test.code),
				Message:            string(test.code),
				StatusCode:         http.StatusTeapot,
				UpstreamStatusCode: http.StatusTeapot,
				PolicyEvidence:     string(test.code),
			}
			publicTaskErr := kkaiPublicTaskError(ctx, taskErr)
			require.NotNil(t, publicTaskErr)
			assert.Equal(t, test.status, publicTaskErr.StatusCode)
			assert.Equal(t, string(test.code), publicTaskErr.Code)
			assert.True(t, processKKAIPolicyTaskError(ctx, types.ChannelError{}, taskErr))
			assert.False(t, shouldRetryTaskRelay(ctx, 1, taskErr, 3))
			taskAttempts := 0
			for remaining := common.RetryTimes; remaining >= 0; remaining-- {
				taskAttempts++
				if !shouldRetryTaskRelay(ctx, taskAttempts, taskErr, remaining) {
					break
				}
			}
			assert.Equal(t, 1, taskAttempts, "local policy errors must not attempt another channel")
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
	apiErr := newControllerUpstreamPolicyError("cyber_policy", types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)
	guard := service.NewKKAIPolicyIncidentGuard(kkaiControllerPolicyTestApplier{})
	detected, err := guard.HandleAPIError(ctx, types.ChannelError{}, apiErr)
	require.NoError(t, err)
	require.True(t, detected)

	assert.False(t, shouldRetry(ctx, apiErr, 3))
	assert.False(t, shouldRetryTaskRelay(ctx, 1, &dto.TaskError{StatusCode: http.StatusInternalServerError}, 3))
}

func TestProcessChannelErrorAfterPolicyDoesNotLogCredentials(t *testing.T) {
	ctx := newKKAIPublicErrorTestContext(t)
	apiErr := newControllerUpstreamPolicyError(
		"cyber_policy Bearer sk-client-secret",
		types.ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)

	oldErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = false
	t.Cleanup(func() { constant.ErrorLogEnabled = oldErrorLogEnabled })

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

	processChannelErrorAfterKKAIPolicy(ctx, types.ChannelError{ChannelId: 12}, apiErr, true)

	require.Contains(t, logBuffer.String(), "policy event detected")
	require.NotContains(t, logBuffer.String(), "sk-client-secret")
	require.NotContains(t, logBuffer.String(), "Bearer")
}

func TestKKAITaskAPIErrorPreservesUpstreamStatus(t *testing.T) {
	apiErr := kkaiTaskAPIError(&dto.TaskError{
		Code:               "cyber_policy",
		Message:            "cyber_policy",
		StatusCode:         http.StatusUnauthorized,
		UpstreamStatusCode: http.StatusForbidden,
		PolicyEvidence:     "cyber_policy",
	})

	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
	require.Equal(t, http.StatusForbidden, apiErr.GetOriginalStatusCode())
	require.Equal(t, "cyber_policy", apiErr.GetPolicyEvidence())
}
