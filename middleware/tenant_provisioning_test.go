package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantProvisioningAllowsOwnerSetupButBlocksCustomerTraffic(t *testing.T) {
	db := newTenantMiddlewareTestDB(t)
	require.NoError(t, db.Create(&model.Tenant{ID: 42, Status: model.TenantStatusProvisioning}).Error)
	require.NoError(t, db.Create(&model.TenantDomain{TenantID: 42, Hostname: "kimi.example.com", Status: model.TenantDomainStatusActive}).Error)
	engine := gin.New()
	engine.Use(TenantContext(db, "main.example.com"))
	engine.NoRoute(func(c *gin.Context) { c.Status(http.StatusOK) })
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/api/status", 200}, {"GET", "/api/site-config", 200}, {"POST", "/api/user/login", 200},
		{"GET", "/reseller/pricing", 200}, {"PATCH", "/api/reseller/prices", 200},
		{"POST", "/api/user/register", 503}, {"POST", "/api/user/topup", 503}, {"POST", "/v1/chat/completions", 503},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(tc.method, "http://kimi.example.com"+tc.path, nil))
			assert.Equal(t, tc.status, response.Code)
		})
	}
}

func TestTenantSessionOriginRequiresOwnHTTPSOrigin(t *testing.T) {
	oldSecure, oldTrusted := common.SessionCookieSecure, common.SessionCookieTrustedURLs
	common.SessionCookieSecure = false
	common.SessionCookieTrustedURLs = []string{"https://main.example.com"}
	t.Cleanup(func() { common.SessionCookieSecure, common.SessionCookieTrustedURLs = oldSecure, oldTrusted })
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
		common.SetContextKey(c, constant.ContextKeyTenantHost, "kimi.example.com")
		c.Next()
	})
	engine.POST("/api/user/auth/refresh", SessionCookieOriginGuard(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for _, tc := range []struct {
		origin string
		status int
	}{
		{"https://kimi.example.com", 204}, {"https://main.example.com", 403}, {"http://kimi.example.com", 403}, {"https://other.example.com", 403}, {"", 403},
	} {
		t.Run(tc.origin, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://kimi.example.com/api/user/auth/refresh", nil)
			if tc.origin != "" {
				request.Header.Set("Origin", tc.origin)
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			assert.Equal(t, tc.status, response.Code)
		})
	}
}
