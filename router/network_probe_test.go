package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func networkProbeRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)
	return engine
}

func networkProbeRequest(engine *gin.Engine, method, origin, ip string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "https://api.kkrich.ltd/api/network-probe?n=1", nil)
	request.Host = "api.kkrich.ltd"
	request.RemoteAddr = ip + ":12345"
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func TestNetworkProbeResponseAndCORS(t *testing.T) {
	engine := networkProbeRouter(t)

	for _, test := range []struct {
		name       string
		method     string
		origin     string
		preflight  string
		wantStatus int
		wantOrigin string
	}{
		{name: "same origin", method: http.MethodGet, wantStatus: http.StatusNoContent},
		{name: "main console", method: http.MethodGet, origin: "https://api.kkrich.ltd", wantStatus: http.StatusNoContent, wantOrigin: "https://api.kkrich.ltd"},
		{name: "backup console", method: http.MethodGet, origin: "https://api.kkai.win", wantStatus: http.StatusNoContent, wantOrigin: "https://api.kkai.win"},
		{name: "other origin", method: http.MethodGet, origin: "https://evil.example", wantStatus: http.StatusForbidden},
		{name: "opaque origin", method: http.MethodGet, origin: "null", wantStatus: http.StatusForbidden},
		{name: "preflight", method: http.MethodOptions, origin: "https://api.kkai.win", preflight: http.MethodGet, wantStatus: http.StatusNoContent, wantOrigin: "https://api.kkai.win"},
		{name: "wrong preflight method", method: http.MethodOptions, origin: "https://api.kkai.win", preflight: http.MethodPost, wantStatus: http.StatusForbidden, wantOrigin: "https://api.kkai.win"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "https://api.kkrich.ltd/api/network-probe?n=1", nil)
			request.Host = "api.kkrich.ltd"
			request.RemoteAddr = "203.0.113.247:12345"
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if test.preflight != "" {
				request.Header.Set("Access-Control-Request-Method", test.preflight)
			}
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			require.Equal(t, test.wantStatus, recorder.Code)
			assert.Empty(t, recorder.Body.String())
			assert.Contains(t, recorder.Header().Get("Cache-Control"), "no-store")
			assert.Equal(t, test.wantOrigin, recorder.Header().Get("Access-Control-Allow-Origin"))
			assert.Empty(t, recorder.Header().Get("Access-Control-Allow-Credentials"))
			assert.Contains(t, recorder.Header().Values("Vary"), "Origin")
			assert.Empty(t, recorder.Header().Get("Location"))
		})
	}
}

func TestNetworkProbeRateLimitIsIndependentOfRedisAndGlobalAPILimit(t *testing.T) {
	previous := common.GlobalApiRateLimitEnable
	previousRedis := common.RedisEnabled
	previousClient := common.RDB
	common.GlobalApiRateLimitEnable = false
	common.RedisEnabled = true
	common.RDB = nil
	t.Cleanup(func() {
		common.GlobalApiRateLimitEnable = previous
		common.RedisEnabled = previousRedis
		common.RDB = previousClient
	})
	engine := networkProbeRouter(t)

	for range 120 {
		recorder := networkProbeRequest(engine, http.MethodGet, "", "203.0.113.248")
		require.Equal(t, http.StatusNoContent, recorder.Code)
	}
	limited := networkProbeRequest(engine, http.MethodGet, "https://api.kkai.win", "203.0.113.248")
	assert.Equal(t, http.StatusTooManyRequests, limited.Code)
	assert.Contains(t, limited.Header().Get("Cache-Control"), "no-store")
	assert.Equal(t, "https://api.kkai.win", limited.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, http.StatusNoContent, networkProbeRequest(engine, http.MethodGet, "", "203.0.113.249").Code)
}

func TestNetworkProbeIsUnavailableToTenants(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyTenantID, int64(12))
		c.Next()
	}, middleware.TenantFeatureGate())
	SetApiRouter(engine)

	recorder := networkProbeRequest(engine, http.MethodGet, "", "203.0.113.250")
	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "TENANT_FEATURE_DISABLED")
}

func TestNetworkProbeUsesPlatformHostWithoutDatabase(t *testing.T) {
	t.Setenv("FRONTEND_MODE", "external")
	t.Setenv("TENANT_ROUTING_ENABLED", "false")
	t.Setenv("TENANT_PLATFORM_HOSTS", "api.kkrich.ltd")
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetRouter(engine, ThemeAssets{})

	recorder := networkProbeRequest(engine, http.MethodGet, "", "203.0.113.251")
	require.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Empty(t, recorder.Body.String())
	assert.Contains(t, recorder.Header().Get("Cache-Control"), "no-store")
}
