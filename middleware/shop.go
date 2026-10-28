package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// ShopMainSiteGuard must run after trusted Host resolution, including for the
// public catalogue. A missing context is not evidence of the platform site.
func ShopMainSiteGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Referrer-Policy", "no-referrer")
		scope, ok := GetTenantContext(c)
		if !ok || scope.TenantID != model.PlatformTenantID {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "code": "SHOP_MAIN_SITE_ONLY", "message": "SHOP_MAIN_SITE_ONLY"})
			return
		}
		c.Next()
	}
}

// ShopSessionGuard supplements UserAuth/AdminAuth, which support PATs for
// other APIs. Authentication has already checked enabled status and versions.
func ShopSessionGuard(admin bool) gin.HandlerFunc {
	origin := SessionCookieOriginGuard()
	return func(c *gin.Context) {
		identity, session := GetSessionAuthIdentity(c)
		_, pat := model.ParseAccessTokenSessionID(identity.SessionID)
		if !session || pat || c.GetBool("use_access_token") || admin && c.GetInt("role") < common.RoleAdminUser {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "code": "SHOP_SESSION_REQUIRED", "message": "SHOP_SESSION_REQUIRED"})
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			origin(c)
			return
		}
		c.Next()
	}
}

func ShopRateLimit(scope string) gin.HandlerFunc {
	var requests int
	var seconds int64
	switch scope {
	case "order", "payment":
		requests, seconds = 20, 600
	case "reconcile":
		requests, seconds = 10, 60
	case "reveal":
		requests, seconds = 30, 60
	case "import":
		requests, seconds = 10, 60
	case "read", "admin":
		requests, seconds = 60, 60
	default:
		panic("unknown shop rate limit scope")
	}
	if !common.CriticalRateLimitEnable {
		return defNext
	}
	limit := userRateLimitFactory(requests, seconds, "UC:shop:"+scope)
	return func(c *gin.Context) {
		limit(c)
		if c.IsAborted() && c.Writer.Status() == http.StatusTooManyRequests {
			c.Header("Cache-Control", "no-store")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"success": false, "code": "SHOP_RATE_LIMITED", "message": "SHOP_RATE_LIMITED"})
		}
	}
}

// SensitiveResponseAudit must precede AdminAuth for a secret-returning route.
// Audit metadata is retained; only the auxiliary response copy is disabled.
func SensitiveResponseAudit() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("audit_metadata_only", true)
		c.Next()
	}
}
