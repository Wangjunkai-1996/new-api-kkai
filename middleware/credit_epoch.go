package middleware

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// CreditEpochGuard rejects monetary form writes from a page rendered before the
// currency cutover. Payment callbacks, relay clients and internal ledgers have
// independent, persisted currency contracts and must not depend on a browser.
func CreditEpochGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions || !creditEpochConsoleRoute(c.FullPath()) {
			c.Next()
			return
		}
		epoch, err := common.CurrentCreditEpoch()
		if err != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"success": false, "code": "credit_epoch_invalid", "message": "Credit currency configuration is unavailable"})
			return
		}
		if epoch != nil && c.GetHeader("X-KKAI-Credit-Epoch") != epoch.TargetEpoch+":"+epoch.MigrationID {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"success": false, "code": "credit_epoch_mismatch", "message": "Balance currency changed; reload this page before continuing"})
			return
		}
		c.Next()
	}
}

func creditEpochConsoleRoute(path string) bool {
	switch path {
	case "/api/user/", "/api/user/manage", "/api/user/topup", "/api/user/topup/complete",
		"/api/user/pay", "/api/user/amount", "/api/user/stripe/pay", "/api/user/stripe/amount",
		"/api/user/creem/pay", "/api/user/waffo/pay", "/api/user/waffo/amount",
		"/api/user/waffo-pancake/pay", "/api/user/waffo-pancake/amount", "/api/user/aff_transfer",
		"/api/token/", "/api/redemption/":
		return true
	case "/api/subscription/epay/notify", "/api/subscription/epay/return":
		return false
	}
	return strings.HasPrefix(path, "/api/option/") || strings.HasPrefix(path, "/api/subscription/")
}
