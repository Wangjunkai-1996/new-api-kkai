package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupPublicErrorControllerTestDB(t *testing.T) {
	t.Helper()

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.PolicyIncidentEvent{}))

	t.Cleanup(func() {
		_ = sqlDB.Close()
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
	})
}

func newPublicErrorTestContext(requestID string) *gin.Context {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set(common.RequestIdKey, requestID)
	return ctx
}

func TestPublicOpenAIErrorPolicyIncidentIsCleanAndIncludesCaseID(t *testing.T) {
	setupPublicErrorControllerTestDB(t)
	ctx := newPublicErrorTestContext("req-policy-public")
	event := &model.PolicyIncidentEvent{RequestId: "req-policy-public"}
	require.NoError(t, event.SetMetadata(map[string]any{"case_id": "policy-case-123"}))
	require.NoError(t, model.InsertPolicyIncidentEvent(event))

	apiErr := types.NewOpenAIError(
		errors.New("upstream rejected: cyber_policy; visit https://ads.example/buy"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)

	statusCode, openAIError := publicOpenAIError(ctx, apiErr)

	require.Equal(t, http.StatusForbidden, statusCode)
	require.Equal(t, types.PublicMessageRequestBlockedByPolicy, openAIError.Message)
	require.Equal(t, types.ErrorCodePolicyBlocked, openAIError.Code)
	require.Equal(t, "policy-case-123", openAIError.CaseID)
	var metadata map[string]string
	require.NoError(t, common.Unmarshal(openAIError.Metadata, &metadata))
	require.Equal(t, "policy-case-123", metadata["case_id"])
	require.NotContains(t, openAIError.Message, "cyber_policy")
	require.NotContains(t, openAIError.Message, "ads.example")
}

func TestPublicOpenAIErrorUpstreamKeyPolicyIncidentIsUnavailable(t *testing.T) {
	ctx := newPublicErrorTestContext("req-upstream-key-policy-public")
	apiErr := types.NewOpenAIError(
		errors.New("provider rejected request because api key has been deactivated; contact https://ads.example"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)

	statusCode, openAIError := publicOpenAIError(ctx, apiErr)

	require.Equal(t, http.StatusServiceUnavailable, statusCode)
	require.Equal(t, types.PublicMessageUpstreamUnavailable, openAIError.Message)
	require.Equal(t, types.ErrorCodeUpstreamUnavailable, openAIError.Code)
	require.NotContains(t, openAIError.Message, "cyber_policy")
	require.NotContains(t, openAIError.Message, "deactivated")
	require.NotContains(t, openAIError.Message, "API key")
}

func TestPublicOpenAIErrorPolicyBreakerContextStaysUnavailable(t *testing.T) {
	ctx := newPublicErrorTestContext("req-policy-breaker-public")
	ctx.Set(service.PolicyIncidentCaseIDContextKey, "policy-case-upstream")
	common.SetContextKey(ctx, constant.ContextKeyPolicyIncidentDetected, true)
	apiErr := types.NewErrorWithStatusCode(
		errors.New("policy breaker open: upstream key temporarily isolated, buy key at https://ads.example"),
		types.ErrorCodePolicyUpstreamKeyIsolated,
		http.StatusServiceUnavailable,
	)

	statusCode, openAIError := publicOpenAIError(ctx, apiErr)

	require.Equal(t, http.StatusServiceUnavailable, statusCode)
	require.Equal(t, types.PublicMessageUpstreamUnavailable, openAIError.Message)
	require.Equal(t, types.ErrorCodeUpstreamUnavailable, openAIError.Code)
	require.NotContains(t, openAIError.Message, "cyber_policy")
	require.NotContains(t, openAIError.Message, "ads.example")
	require.NotContains(t, openAIError.Message, "disabled")
	require.NotContains(t, openAIError.Message, "key")
}

func TestPublicOpenAIErrorNoAvailableKeyIsUnavailable(t *testing.T) {
	ctx := newPublicErrorTestContext("req-no-key-public")
	apiErr := types.NewError(errors.New("no enabled keys"), types.ErrorCodeChannelNoAvailableKey)

	statusCode, openAIError := publicOpenAIError(ctx, apiErr)

	require.Equal(t, http.StatusServiceUnavailable, statusCode)
	require.Equal(t, types.PublicMessageUpstreamUnavailable, openAIError.Message)
	require.Equal(t, types.ErrorCodeUpstreamUnavailable, openAIError.Code)
	require.NotContains(t, openAIError.Message, "no enabled keys")
	require.NotContains(t, openAIError.Message, "policy")
}

func TestPublicOpenAIErrorNoisyUpstreamMessageIsSanitized(t *testing.T) {
	ctx := newPublicErrorTestContext("req-noisy-upstream-public")
	apiErr := types.NewOpenAIError(
		errors.New("provider says join our Telegram https://t.me/vendor to buy key"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadGateway,
	)

	statusCode, openAIError := publicOpenAIError(ctx, apiErr)

	require.Equal(t, http.StatusBadGateway, statusCode)
	require.Equal(t, types.PublicMessageUpstreamError, openAIError.Message)
	require.Equal(t, types.ErrorTypeUpstreamError, openAIError.Code)
	require.NotContains(t, openAIError.Message, "Telegram")
	require.NotContains(t, openAIError.Message, "https://")
}

func TestPublicOpenAIErrorUnsafeUpstreamMessageClearsPublicFields(t *testing.T) {
	ctx := newPublicErrorTestContext("req-unsafe-upstream-public")
	ctx.Request.Header.Set("Authorization", "Bearer sk-client-public-secret")
	ctx.Set("token_key", "client-public-secret")
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "upstream-public-secret")
	metadata, err := common.Marshal(map[string]any{
		"authorization": "Bearer sk-client-public-secret",
		"nested": map[string]any{
			"upstream_key": "upstream-public-secret",
		},
	})
	require.NoError(t, err)
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Message:  "provider leaked Authorization: Bearer sk-client-public-secret and upstream-public-secret",
		Type:     "upstream_error",
		Param:    "sk-client-public-secret",
		Code:     "bad_response_status_code",
		Metadata: metadata,
		CaseID:   "upstream-case-should-not-pass",
	}, http.StatusBadGateway)

	statusCode, openAIError := publicOpenAIError(ctx, apiErr)

	require.Equal(t, http.StatusBadGateway, statusCode)
	require.Equal(t, types.PublicMessageUpstreamError, openAIError.Message)
	require.Equal(t, types.ErrorTypeUpstreamError, openAIError.Code)
	require.Empty(t, openAIError.Param)
	require.Empty(t, openAIError.Metadata)
	require.Empty(t, openAIError.CaseID)
}

func TestPublicOpenAIErrorNewAPIUpstreamErrorMessageIsSanitized(t *testing.T) {
	ctx := newPublicErrorTestContext("req-newapi-upstream-public")
	apiErr := types.NewError(
		errors.New("provider leaked upstream key Authorization: Bearer sk-upstream-public-secret"),
		types.ErrorCodeBadResponseBody,
	)

	statusCode, openAIError := publicOpenAIError(ctx, apiErr)

	require.Equal(t, http.StatusInternalServerError, statusCode)
	require.Equal(t, types.PublicMessageUpstreamError, openAIError.Message)
	require.Equal(t, types.ErrorTypeUpstreamError, openAIError.Code)
	require.NotContains(t, openAIError.Message, "sk-upstream-public-secret")
	require.NotContains(t, openAIError.Message, "Authorization")
}

func TestPublicOpenAIErrorPassthroughScrubsParamSecret(t *testing.T) {
	ctx := newPublicErrorTestContext("req-passthrough-scrub-public")
	ctx.Request.Header.Set("Authorization", "Bearer sk-client-visible-secret")
	ctx.Set("token_key", "client-visible-secret")
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "upstream-visible-secret")
	metadata, err := common.Marshal(map[string]any{
		"client_token":  "sk-client-visible-secret",
		"safe_property": "kept",
	})
	require.NoError(t, err)
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Message:  "ordinary upstream validation failed",
		Type:     "invalid_request_error",
		Param:    "Bearer sk-client-visible-secret",
		Code:     "invalid_request",
		Metadata: metadata,
	}, http.StatusBadRequest)

	statusCode, openAIError := publicOpenAIError(ctx, apiErr)

	require.Equal(t, http.StatusBadRequest, statusCode)
	require.Equal(t, types.PublicMessageUpstreamError, openAIError.Message)
	require.Empty(t, openAIError.Param)
	require.Empty(t, openAIError.Metadata)
}

func TestPublicClaudeErrorUsesUnifiedSanitizedClassifier(t *testing.T) {
	ctx := newPublicErrorTestContext("req-claude-scrub-public")
	apiErr := types.WithClaudeError(types.ClaudeError{
		Type:    "upstream_error",
		Message: "provider says buy key at https://ads.example with Bearer sk-client-secret",
	}, http.StatusBadGateway)

	statusCode, claudeError := publicClaudeError(ctx, apiErr)

	require.Equal(t, http.StatusBadGateway, statusCode)
	require.Equal(t, types.PublicMessageUpstreamError, claudeError.Message)
	require.Equal(t, string(types.ErrorTypeUpstreamError), claudeError.Type)
	require.Empty(t, claudeError.CaseID)
}

func TestPublicTaskErrorNoisyUpstreamMessageIsSanitized(t *testing.T) {
	ctx := newPublicErrorTestContext("req-noisy-task-public")
	taskErr := &dto.TaskError{
		Code:       "upstream_rejected",
		Message:    "provider says 联系我们 https://ads.example 购买 key",
		StatusCode: http.StatusBadGateway,
		Error:      errors.New("provider says 联系我们 https://ads.example 购买 key"),
	}

	publicErr := publicTaskError(ctx, taskErr)

	require.Equal(t, http.StatusBadGateway, publicErr.StatusCode)
	require.Equal(t, types.PublicMessageUpstreamError, publicErr.Message)
	require.Equal(t, string(types.ErrorTypeUpstreamError), publicErr.Code)
	require.NotContains(t, publicErr.Message, "ads.example")
	require.NotContains(t, taskErr.Message, types.PublicMessageUpstreamError)
}

func TestPublicTaskErrorPolicyBreakerIsUnavailable(t *testing.T) {
	ctx := newPublicErrorTestContext("req-task-breaker-public")
	taskErr := &dto.TaskError{
		Code:       "policy_breaker_open",
		Message:    "upstream key is temporarily isolated by cyber policy breaker",
		StatusCode: http.StatusServiceUnavailable,
		LocalError: true,
		Error:      errors.New("upstream key is temporarily isolated by cyber policy breaker"),
	}

	publicErr := publicTaskError(ctx, taskErr)

	require.Equal(t, http.StatusServiceUnavailable, publicErr.StatusCode)
	require.Equal(t, types.PublicMessageUpstreamUnavailable, publicErr.Message)
	require.Equal(t, string(types.ErrorCodeUpstreamUnavailable), publicErr.Code)
	require.NotContains(t, publicErr.Message, "policy")
	require.NotContains(t, publicErr.Message, "key")
}

func TestPublicTaskErrorUpstreamKeyPolicySignalIsUnavailableWithCaseID(t *testing.T) {
	ctx := newPublicErrorTestContext("req-task-upstream-key-public")
	ctx.Set(service.PolicyIncidentCaseIDContextKey, "policy-case-context")
	taskErr := &dto.TaskError{
		Code:       "bad_response_status_code",
		Message:    "provider api key has been deactivated",
		StatusCode: http.StatusForbidden,
		Error:      errors.New("provider api key has been deactivated"),
	}

	publicErr := publicTaskError(ctx, taskErr)

	require.Equal(t, http.StatusServiceUnavailable, publicErr.StatusCode)
	require.Equal(t, types.PublicMessageUpstreamUnavailable, publicErr.Message)
	require.Equal(t, string(types.ErrorCodeUpstreamUnavailable), publicErr.Code)
	require.Equal(t, "policy-case-context", publicErr.CaseID)
	require.NotContains(t, publicErr.Message, "deactivated")
	require.NotContains(t, publicErr.Message, "key")
}
