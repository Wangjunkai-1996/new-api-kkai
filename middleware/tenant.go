package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// TenantContext resolves the request Host into a tenant scope. It is
// intentionally independent from AdminAuth/UserAuth: authentication proves
// who the caller is, while this middleware proves which branded site received
// the request. Unknown or disabled hosts fail closed and never fall back to
// the platform tenant.
func TenantContext(db *gorm.DB, platformHosts ...string) gin.HandlerFunc {
	return TenantRouting(db, true, platformHosts...)
}

// TenantRouting always validates Host. Turning the feature off disables tenant
// domains, rather than allowing their credentials to reach the platform site.
func TenantRouting(db *gorm.DB, enabled bool, platformHosts ...string) gin.HandlerFunc {
	if !enabled {
		db = nil // Only the explicitly configured platform hosts remain reachable.
	}
	return func(c *gin.Context) {
		resolved, err := model.ResolveTenantByHost(db, c.Request.Host, platformHosts...)
		if !enabled && errors.Is(err, model.ErrTenantStoreUnavailable) {
			err = model.ErrTenantUnknownHost
		}
		if err != nil {
			status := http.StatusNotFound
			code := "TENANT_HOST_UNKNOWN"
			if errors.Is(err, model.ErrTenantInvalidHost) {
				code = "TENANT_HOST_INVALID"
			} else if errors.Is(err, model.ErrTenantDisabled) {
				code = "TENANT_DISABLED"
			} else if !errors.Is(err, model.ErrTenantUnknownHost) {
				status = http.StatusInternalServerError
				code = "TENANT_RESOLUTION_ERROR"
			}
			c.AbortWithStatusJSON(status, gin.H{
				"success": false,
				"code":    code,
				"message": "tenant site is unavailable",
			})
			return
		}
		common.SetContextKey(c, constant.ContextKeyTenantID, resolved.TenantID)
		common.SetContextKey(c, constant.ContextKeyTenantHost, resolved.Hostname)
		common.SetContextKey(c, constant.ContextKeyTenantContext, resolved)
		if resolved.Status == model.TenantStatusProvisioning && !tenantProvisioningRoute(c.Request.Method, c.Request.URL.Path) {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"success": false, "code": "TENANT_PROVISIONING", "message": "tenant site is not open yet"})
			return
		}
		c.Next()
	}
}

func tenantProvisioningRoute(method, path string) bool {
	if strings.HasPrefix(path, "/api/reseller/") {
		return true // UserAuth + TenantAdminAuth still protect owner operations.
	}
	if method == http.MethodGet || method == http.MethodHead {
		switch path {
		case "/api/status", "/api/site-config", "/api/site-logo", "/api/user/login/encryption-key", "/api/user/self":
			return true
		}
		return path == "/" || path == "/sign-in" || path == "/reseller" || strings.HasPrefix(path, "/reseller/") || strings.HasPrefix(path, "/assets/") || strings.HasPrefix(path, "/static/")
	}
	if method == http.MethodPost {
		switch path {
		case "/api/user/login", "/api/user/login/2fa", "/api/user/login/verify", "/api/user/auth/refresh", "/api/user/auth/logout":
			return true
		}
	}
	return false
}

// GetTenantContext returns the trusted Host-derived context. Callers should
// use this instead of accepting tenant_id from query, path, body or headers.
func GetTenantContext(c *gin.Context) (model.TenantContext, bool) {
	return common.GetContextKeyType[model.TenantContext](c, constant.ContextKeyTenantContext)
}

// GetTenantScope returns the database filter derived from the trusted request
// context. It has no overload that accepts a client-supplied tenant ID.
func GetTenantScope(c *gin.Context) (model.TenantScope, bool) {
	context, ok := GetTenantContext(c)
	if !ok {
		return model.TenantScope{}, false
	}
	return model.TenantScope{TenantID: context.TenantID}, true
}
