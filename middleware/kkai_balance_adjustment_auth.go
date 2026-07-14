package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const KKAIInvitationsInternalSecretEnv = "INVITATIONS_INTERNAL_SECRET"

func KKAIBalanceAdjustmentAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		secret := os.Getenv(KKAIInvitationsInternalSecretEnv)
		if len(secret) < 32 || strings.TrimSpace(secret) != secret || strings.ContainsAny(secret, " \t\r\n") {
			writeKKAIBalanceAuthError(c, http.StatusServiceUnavailable, "internal_auth_unavailable")
			return
		}

		parts := strings.Fields(c.GetHeader("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			writeKKAIBalanceAuthError(c, http.StatusUnauthorized, "internal_auth_failed")
			return
		}
		expected := sha256.Sum256([]byte(secret))
		provided := sha256.Sum256([]byte(parts[1]))
		if subtle.ConstantTimeCompare(expected[:], provided[:]) != 1 {
			writeKKAIBalanceAuthError(c, http.StatusUnauthorized, "internal_auth_failed")
			return
		}
		if common.IsStandbyReadonly() {
			writeKKAIBalanceAuthError(c, http.StatusServiceUnavailable, "internal_write_unavailable")
			return
		}
		c.Next()
	}
}

func writeKKAIBalanceAuthError(c *gin.Context, status int, code string) {
	c.AbortWithStatusJSON(status, gin.H{
		"success": false,
		"error": gin.H{
			"code":    code,
			"message": "internal authentication failed",
		},
	})
}
