package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
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
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false

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
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
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
	require.NotContains(t, openAIError.Message, "cyber_policy")
	require.NotContains(t, openAIError.Message, "ads.example")
}

func TestPublicOpenAIErrorUpstreamKeyPolicyIncidentIsUnavailable(t *testing.T) {
	ctx := newPublicErrorTestContext("req-upstream-key-policy-public")
	apiErr := types.NewOpenAIError(
		errors.New("网络滥用封禁：上游返回 cyber_policy，当前 API key 已永久禁用，请联系 https://ads.example"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)

	statusCode, openAIError := publicOpenAIError(ctx, apiErr)

	require.Equal(t, http.StatusServiceUnavailable, statusCode)
	require.Equal(t, types.PublicMessageUpstreamUnavailable, openAIError.Message)
	require.Equal(t, types.ErrorCodeUpstreamUnavailable, openAIError.Code)
	require.NotContains(t, openAIError.Message, "cyber_policy")
	require.NotContains(t, openAIError.Message, "API key")
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
