package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func TenantFeatureGate() gin.HandlerFunc {
	return func(c *gin.Context) {
		if common.GetContextKeyInt64(c, constant.ContextKeyTenantID) <= model.PlatformTenantID {
			c.Next()
			return
		}
		if _, ok := tenantBrowserOrigin(c.Request); !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "code": "AUTH_ORIGIN_FORBIDDEN", "message": "request origin is not allowed"})
			return
		}
		method, route := c.Request.Method, c.FullPath()
		if method == http.MethodOptions {
			method, route = c.GetHeader("Access-Control-Request-Method"), c.Request.URL.Path
		}
		// A new platform route is unavailable to tenants until it is explicitly
		// reviewed here. FullPath uses Gin's server-side route template, never a
		// user-provided tenant or a broad URL-prefix permission.
		if c.GetHeader("Upgrade") != "" || !tenantRouteEnabled(method, route, c.Request.URL.Path) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "code": "TENANT_FEATURE_DISABLED", "message": "this feature is unavailable on reseller sites"})
			return
		}
		c.Next()
	}
}

func tenantRouteEnabled(method, route, path string) bool {
	switch method + " " + route {
	case "GET /api/status", "GET /api/site-config", "GET /api/site-logo", "GET /api/notice",
		"GET /api/home_page_content", "GET /api/pricing", "GET /api/models",
		"GET /api/user-agreement", "GET /api/privacy-policy", "GET /api/about",
		"GET /api/verification", "GET /api/reset_password", "POST /api/user/reset",
		"GET /api/user/login/encryption-key", "POST /api/user/register", "POST /api/user/login",
		"POST /api/user/login/2fa", "POST /api/user/login/verify",
		"POST /api/user/auth/refresh", "POST /api/user/auth/logout",
		"GET /api/user/self", "PUT /api/user/self", "DELETE /api/user/self", "PUT /api/user/setting",
		"GET /api/user/groups", "GET /api/user/self/groups", "GET /api/user/models",
		"GET /api/user/sessions", "DELETE /api/user/sessions/:sid", "POST /api/user/sessions/revoke-others",
		"GET /api/verify/methods", "POST /api/verify", "GET /api/user/2fa/status",
		"POST /api/user/2fa/setup", "POST /api/user/2fa/enable", "POST /api/user/2fa/disable", "POST /api/user/2fa/backup_codes",
		"POST /api/oauth/email/bind/start", "POST /api/oauth/email/bind/resend", "POST /api/oauth/email/bind",
		"GET /api/user/topup/info", "GET /api/user/topup/self", "POST /api/user/pay", "POST /api/user/amount",
		"GET /api/user/epay/notify", "POST /api/user/epay/notify",
		"GET /api/token/", "GET /api/token/search", "GET /api/token/:id", "GET /api/token/:id/models",
		"GET /api/token/auto-groups", "PATCH /api/token/:id/group", "POST /api/token/:id/key",
		"POST /api/token/", "PUT /api/token/", "DELETE /api/token/:id", "POST /api/token/batch", "POST /api/token/batch/keys",
		"GET /api/log/self", "GET /api/log/self/stat", "GET /api/log/self/search", "GET /api/log/token",
		"GET /api/usage/token/", "GET /api/data/self", "GET /api/data/flow/self",
		"GET /dashboard/billing/subscription", "GET /dashboard/billing/usage",
		"GET /v1/dashboard/billing/subscription", "GET /v1/dashboard/billing/usage",
		"GET /api/reseller/me", "GET /api/reseller/profile", "PATCH /api/reseller/profile", "POST /api/reseller/brand/logo",
		"GET /api/reseller/catalog", "GET /api/reseller/supply", "PATCH /api/reseller/catalog", "GET /api/reseller/prices", "PATCH /api/reseller/prices",
		"GET /api/reseller/customers", "GET /api/reseller/earnings", "GET /api/reseller/withdrawals", "POST /api/reseller/withdrawals",
		"GET /v1/models", "GET /v1/models/:model", "POST /v1/chat/completions", "POST /v1/completions",
		"POST /v1/responses", "POST /v1/messages", "POST /v1/embeddings":
		return true
	}
	if route != "" || (method != http.MethodGet && method != http.MethodHead) {
		return false
	}
	// Only the explicit SPA pages and assets may reach the NoRoute chain;
	// arbitrary fallback paths can be claimed by installed task plugins.
	if strings.HasPrefix(path, "/assets/") || strings.HasPrefix(path, "/static/") || strings.HasPrefix(path, "/pricing/") {
		return true
	}
	switch path {
	case "/", "/pricing", "/sign-in", "/sign-up", "/register", "/forgot-password", "/reset-password", "/reset", "/user/reset", "/otp",
		"/terms", "/user-agreement", "/privacy-policy", "/about", "/console", "/keys", "/logs", "/topup", "/settings", "/profile",
		"/dashboard", "/dashboard/overview", "/dashboard/models", "/usage-logs", "/usage-logs/common", "/wallet", "/console/topup", "/console/log",
		"/403", "/401", "/404", "/500", "/503",
		"/reseller", "/reseller/brand", "/reseller/catalog", "/reseller/pricing", "/reseller/customers", "/reseller/earnings", "/reseller/withdrawals",
		"/favicon.ico", "/favicon.png", "/logo.png", "/manifest.json", "/robots.txt":
		return true
	}
	return false
}

func TenantModelAllowed(tenantID int64, modelName string) (bool, error) {
	if tenantID <= model.PlatformTenantID {
		return true, nil
	}
	entries, err := service.NewTenantPricingService(model.DB).ResolveCatalog(context.Background(), tenantID)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.Model == modelName {
			return true, nil
		}
	}
	return false, nil
}
