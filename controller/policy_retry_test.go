package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newPolicyRetryTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	return ctx
}

func TestPolicyNoRetryFlagPreventsRetry(t *testing.T) {
	ctx := newPolicyRetryTestContext()
	common.SetContextKey(ctx, constant.ContextKeyPolicyNoRetry, true)

	apiErr := types.NewOpenAIError(errors.New("upstream rate limited"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)
	require.False(t, shouldRetry(ctx, apiErr, 1))

	taskErr := &dto.TaskError{
		Code:       "rate_limited",
		Message:    "upstream rate limited",
		StatusCode: http.StatusTooManyRequests,
		Error:      errors.New("upstream rate limited"),
	}
	require.False(t, shouldRetryTaskRelay(ctx, 1, taskErr, 1))
}

func TestRetryWithoutPolicyFlagKeepsRetryableStatusBehavior(t *testing.T) {
	ctx := newPolicyRetryTestContext()

	apiErr := types.NewOpenAIError(errors.New("upstream rate limited"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)
	require.True(t, shouldRetry(ctx, apiErr, 1))

	taskErr := &dto.TaskError{
		Code:       "rate_limited",
		Message:    "upstream rate limited",
		StatusCode: http.StatusTooManyRequests,
		Error:      errors.New("upstream rate limited"),
	}
	require.True(t, shouldRetryTaskRelay(ctx, 1, taskErr, 1))
}

func TestTaskErrorFromAPIErrorPreservesStatusAndCode(t *testing.T) {
	apiErr := types.NewErrorWithStatusCode(
		errors.New("setup failed"),
		types.ErrorCodeChannelModelMappedError,
		http.StatusUnprocessableEntity,
	)

	taskErr := taskErrorFromAPIError(apiErr)

	require.Equal(t, http.StatusUnprocessableEntity, taskErr.StatusCode)
	require.Equal(t, string(types.ErrorCodeChannelModelMappedError), taskErr.Code)
	require.False(t, taskErr.LocalError)
}

func TestTaskErrorFromAPIErrorMapsPolicyBreakerCode(t *testing.T) {
	apiErr := types.NewErrorWithStatusCode(
		errors.New("upstream key is temporarily isolated by cyber policy breaker"),
		types.ErrorCodeChannelNoAvailableKey,
		http.StatusServiceUnavailable,
	)

	taskErr := taskErrorFromAPIError(apiErr)

	require.Equal(t, http.StatusServiceUnavailable, taskErr.StatusCode)
	require.Equal(t, "policy_breaker_open", taskErr.Code)
	require.True(t, taskErr.LocalError)
}
