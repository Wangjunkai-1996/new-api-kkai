package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// WalletPaymentRateLimit isolates normal wallet interactions from login and
// session renewal. GlobalAPIRateLimit remains the shared IP abuse guard.
func WalletPaymentRateLimit(scope string) gin.HandlerFunc {
	if !common.CriticalRateLimitEnable {
		return defNext
	}
	var requests int
	var seconds int64
	switch scope {
	case "quote":
		requests, seconds = 60, 60
	case "checkout":
		requests, seconds = 30, 600
	case "state":
		requests, seconds = 20, 60
	case "check":
		requests, seconds = common.CriticalRateLimitNum, common.CriticalRateLimitDuration
	default:
		panic("unknown wallet payment rate limit scope")
	}
	limit := userRateLimitFactory(requests, seconds, "UC:wallet-payment:"+scope)
	return func(c *gin.Context) {
		limit(c)
		if c.IsAborted() && c.Writer.Status() == http.StatusTooManyRequests {
			// The shared limiter does not return the remaining window. Do not
			// invent a Retry-After value that could restart a payment too early.
			c.Header("Cache-Control", "no-store")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"code":    "WALLET_RATE_LIMITED",
				"message": "WALLET_RATE_LIMITED",
			})
		}
	}
}

// WalletPaymentMutationGuard preserves scoped API access while applying the
// browser session's existing Origin policy to wallet mutations.
func WalletPaymentMutationGuard() gin.HandlerFunc {
	originGuard := SessionCookieOriginGuard()
	return func(c *gin.Context) {
		if c.GetBool("use_access_token") {
			c.Next()
			return
		}
		originGuard(c)
	}
}

// Historical financial maintenance requires an interactive root session even
// when the root account also owns a broad personal access token.
func WalletPaymentBatchGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		_, session := GetSessionAuthIdentity(c)
		if !session || c.GetBool("use_access_token") || c.GetInt("role") != common.RoleRootUser || common.GetContextKeyInt64(c, constant.ContextKeyTenantID) != model.PlatformTenantID {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "code": "AUTH_SESSION_REQUIRED", "message": "a platform root dashboard session is required"})
			return
		}
		origin, ok := requestBrowserOrigin(c.Request)
		if !ok || !isAllowedSessionOrigin(c.Request, origin) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "code": "AUTH_ORIGIN_FORBIDDEN", "message": "request origin is not allowed"})
			return
		}
		c.Next()
	}
}
