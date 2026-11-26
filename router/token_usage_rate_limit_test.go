package router

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenUsageRateLimitIsIndependentOfLogin(t *testing.T) {
	setupRelayRouterTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.UserSession{}, &model.TwoFA{}, &model.PasskeyCredential{}, &model.Log{}))
	password, err := common.Password2Hash("usage-test-password")
	require.NoError(t, err)
	owner := model.User{Username: "usage-owner", AffCode: "usage1", Password: password, Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	neighbor := model.User{Username: "usage-neighbor", AffCode: "usage2", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&neighbor).Error)
	keys := []model.Token{
		{UserId: owner.Id, Key: "usagefirst", Status: common.TokenStatusEnabled},
		{UserId: owner.Id, Key: "usagesecond", Status: common.TokenStatusEnabled},
		{UserId: neighbor.Id, Key: "usageneighbor", Status: common.TokenStatusEnabled},
	}
	require.NoError(t, model.DB.Create(&keys).Error)

	oldClient, oldCritical := common.RDB, common.CriticalRateLimitEnable
	oldCriticalCount, oldCriticalWindow := common.CriticalRateLimitNum, common.CriticalRateLimitDuration
	oldGlobal, oldGlobalCount, oldGlobalWindow := common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration
	oldPasswordLogin, oldEncryption, oldTurnstile := common.PasswordLoginEnabled, common.PasswordLoginEncryptionEnabled, common.TurnstileCheckEnabled
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	common.RDB, common.RedisEnabled, common.CriticalRateLimitEnable = client, true, true
	common.CriticalRateLimitNum, common.CriticalRateLimitDuration = 20, 1200
	common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration = true, 360, 180
	common.PasswordLoginEnabled, common.PasswordLoginEncryptionEnabled, common.TurnstileCheckEnabled = true, false, false
	t.Cleanup(func() {
		common.RDB, common.CriticalRateLimitEnable = oldClient, oldCritical
		common.CriticalRateLimitNum, common.CriticalRateLimitDuration = oldCriticalCount, oldCriticalWindow
		common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration = oldGlobal, oldGlobalCount, oldGlobalWindow
		common.PasswordLoginEnabled, common.PasswordLoginEncryptionEnabled, common.TurnstileCheckEnabled = oldPasswordLogin, oldEncryption, oldTurnstile
		require.NoError(t, client.Close())
	})
	engine := gin.New()
	SetApiRouter(engine)
	usage := func(key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/usage/token/", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Origin", "https://client.example")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		return w
	}

	require.Equal(t, http.StatusUnauthorized, usage("unknown").Code)
	for range 30 {
		require.Equal(t, http.StatusOK, usage(keys[0].Key).Code)
	}
	limited := usage(keys[1].Key).Result()
	t.Cleanup(func() { require.NoError(t, limited.Body.Close()) })
	require.Equal(t, http.StatusTooManyRequests, limited.StatusCode)
	wait, err := strconv.Atoi(limited.Header.Get("Retry-After"))
	require.NoError(t, err)
	assert.Positive(t, wait)
	assert.LessOrEqual(t, wait, 60)
	assert.Contains(t, limited.Header.Values("Access-Control-Expose-Headers"), "Retry-After")
	require.Equal(t, http.StatusOK, usage(keys[2].Key).Code)

	login := httptest.NewRequest(http.MethodPost, "/api/user/login", strings.NewReader(`{"username":"usage-owner","password":"usage-test-password"}`))
	login.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, login)
	require.Equal(t, http.StatusOK, w.Code)
	var result struct {
		Success bool
		Data    struct {
			AccessToken string `json:"access_token"`
		}
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
	require.True(t, result.Success)
	assert.NotEmpty(t, result.Data.AccessToken)
}
