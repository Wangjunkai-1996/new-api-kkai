package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAuthPolicyTestDB(t *testing.T) {
	t.Helper()

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalRedisEnabled := common.RedisEnabled
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalSQLitePath := common.SQLitePath
	originalIsMasterNode := common.IsMasterNode

	common.SQLitePath = "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	common.IsMasterNode = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, model.InitDB())
	db := model.DB
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}))

	model.LOG_DB = db
	common.RedisEnabled = false

	t.Cleanup(func() {
		_ = sqlDB.Close()
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.RedisEnabled = originalRedisEnabled
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		common.SQLitePath = originalSQLitePath
		common.IsMasterNode = originalIsMasterNode
	})
}

func TestTokenAuthReturnsCleanTemporaryUnavailableForEnabledBreakerToken(t *testing.T) {
	setupAuthPolicyTestDB(t)
	gin.SetMode(gin.TestMode)

	user := &model.User{Id: 1001, Username: "policy_user", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, model.DB.Create(user).Error)
	token := &model.Token{
		UserId:         user.Id,
		Key:            "policy",
		Status:         common.TokenStatusEnabled,
		Name:           "policy-token",
		ExpiredTime:    -1,
		UnlimitedQuota: true,
	}
	require.NoError(t, model.DB.Create(token).Error)

	originalBreaker := isPolicyTokenBreakerOpen
	isPolicyTokenBreakerOpen = func(tokenId int) bool {
		return tokenId == token.Id
	}
	t.Cleanup(func() {
		isPolicyTokenBreakerOpen = originalBreaker
	})

	router := gin.New()
	router.Use(TokenAuth())
	router.GET("/v1/test", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	req.Header.Set("Authorization", "Bearer sk-policy-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusLocked, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "request temporarily unavailable")
	assert.NotContains(t, recorder.Body.String(), "policy_token_isolated")
	assert.NotContains(t, recorder.Body.String(), "cyber_policy")
}

func TestTokenAuthKeepsDisabledTokenInvalidBeforePolicyBreaker(t *testing.T) {
	setupAuthPolicyTestDB(t)
	gin.SetMode(gin.TestMode)

	user := &model.User{Id: 1002, Username: "disabled_policy_user", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, model.DB.Create(user).Error)
	token := &model.Token{
		UserId:         user.Id,
		Key:            "disabled",
		Status:         common.TokenStatusDisabled,
		Name:           "disabled-policy-token",
		ExpiredTime:    -1,
		UnlimitedQuota: true,
	}
	require.NoError(t, model.DB.Create(token).Error)

	originalBreaker := isPolicyTokenBreakerOpen
	isPolicyTokenBreakerOpen = func(tokenId int) bool {
		return tokenId == token.Id
	}
	t.Cleanup(func() {
		isPolicyTokenBreakerOpen = originalBreaker
	})

	router := gin.New()
	router.Use(TokenAuth())
	router.GET("/v1/test", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	req.Header.Set("Authorization", "Bearer sk-disabled-policy-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "policy_token_isolated")
}
