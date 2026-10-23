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
)

func TestWalletPaymentAccessScopesAndTenantBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []struct {
		method, path, scope string
		tenant              bool
	}{
		{"GET", "/api/user/topup/current", "wallet:read", true},
		{"GET", "/api/user/topup/summary", "wallet:read", true},
		{"GET", "/api/user/topup/orders/:trade_no", "wallet:read", true},
		{"POST", "/api/user/topup/current/restore", "wallet:write", true},
		{"POST", "/api/user/topup/quote", "wallet:read", true},
		{"POST", "/api/user/topup/orders", "wallet:write", true},
		{"POST", "/api/user/topup/orders/:trade_no/resume", "wallet:write", true},
		{"POST", "/api/user/topup/orders/:trade_no/cancel", "wallet:write", true},
		{"POST", "/api/user/topup/orders/:trade_no/end", "wallet:write", true},
		{"POST", "/api/user/topup/orders/:trade_no/reconcile", "wallet:write", true},
		{"POST", "/api/user/topup/current/acknowledge", "wallet:write", true},
		{"GET", "/api/user/topup/orders/:trade_no/admin", "billing:read", false},
		{"POST", "/api/user/topup/orders/:trade_no/admin-reconcile", "billing:write", false},
	} {
		for _, granted := range []bool{false, true} {
			engine := gin.New()
			engine.Handle(endpoint.method, endpoint.path, func(c *gin.Context) {
				scopes := "[]"
				if granted {
					scopes = `["` + endpoint.scope + `"]`
				}
				if enforceAccessTokenRoute(c, &accessTokenLookup{token: &model.UserAccessToken{Scopes: scopes}}, true) {
					c.Status(http.StatusNoContent)
				}
			})
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(endpoint.method, endpoint.path, nil))
			want := http.StatusForbidden
			if granted {
				want = http.StatusNoContent
			}
			assert.Equal(t, want, response.Code, endpoint)
		}
		assert.Equal(t, endpoint.tenant, tenantRouteEnabled(endpoint.method, endpoint.path, endpoint.path), endpoint)
	}

	for _, endpoint := range []struct{ method, path string }{
		{"POST", "/api/user/topup/reconcile-runs/report"},
		{"POST", "/api/user/topup/reconcile-runs"},
		{"GET", "/api/user/topup/reconcile-runs/:run_id"},
		{"POST", "/api/user/topup/reconcile-runs/:run_id/pause"},
		{"POST", "/api/user/topup/reconcile-runs/:run_id/resume"},
	} {
		for _, legacy := range []bool{false, true} {
			engine := gin.New()
			engine.Handle(endpoint.method, endpoint.path, func(c *gin.Context) {
				lookup := &accessTokenLookup{legacy: legacy, token: &model.UserAccessToken{Scopes: `["wallet:write","billing:write","ops:write"]`}}
				if enforceAccessTokenRoute(c, lookup, true) {
					c.Status(http.StatusNoContent)
				}
			})
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(endpoint.method, endpoint.path, nil))
			assert.Equal(t, http.StatusForbidden, response.Code, endpoint)
			assert.Contains(t, response.Body.String(), "AUTH_SESSION_REQUIRED")
		}
		assert.False(t, tenantRouteEnabled(endpoint.method, endpoint.path, endpoint.path), endpoint)
	}
}

func TestWalletPaymentBatchGuardRequiresPlatformRootSessionAndOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, origin string
		role         int
		tenant       int64
		pat, session bool
		want         int
	}{
		{"root", "https://wallet.example.test", common.RoleRootUser, 0, false, true, http.StatusNoContent},
		{"admin", "https://wallet.example.test", common.RoleAdminUser, 0, false, true, http.StatusForbidden},
		{"tenant", "https://wallet.example.test", common.RoleRootUser, 42, false, true, http.StatusForbidden},
		{"PAT", "https://wallet.example.test", common.RoleRootUser, 0, true, true, http.StatusForbidden},
		{"no session", "https://wallet.example.test", common.RoleRootUser, 0, false, false, http.StatusForbidden},
		{"missing origin", "", common.RoleRootUser, 0, false, true, http.StatusForbidden},
		{"cross origin", "https://attacker.example.test", common.RoleRootUser, 0, false, true, http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := gin.New()
			engine.Use(func(c *gin.Context) {
				c.Set("id", 7)
				c.Set("role", test.role)
				c.Set("use_access_token", test.pat)
				if test.session {
					c.Set("session_id", "live-session")
					c.Set("auth_version", int64(1))
					c.Set("session_version", int64(1))
				}
				common.SetContextKey(c, constant.ContextKeyTenantID, test.tenant)
			}, WalletPaymentBatchGuard())
			engine.POST("/run", func(c *gin.Context) { c.Status(http.StatusNoContent) })
			request := httptest.NewRequest(http.MethodPost, "https://wallet.example.test/run", nil)
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			assert.Equal(t, test.want, response.Code)
		})
	}
}
