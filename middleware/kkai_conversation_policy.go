package middleware

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func ConversationPolicyCooldown() gin.HandlerFunc {
	return conversationPolicyCooldown(service.KKAIPolicyDefaultCooldownStore())
}

func conversationPolicyCooldown(store service.KKAIPolicyCooldownStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c == nil || c.Request == nil || !service.IsKKAIPolicyConversationPath(c.Request.URL.Path) {
			c.Next()
			return
		}

		scope, err := service.ResolveKKAIPolicyConversationScope(c)
		// This is a soft control. A body parsing or Redis failure must never
		// broaden the scope to a token, account, IP, or channel.
		if err != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("conversation policy scope unavailable: %v", err))
			c.Request.Header.Del(service.KKAIPolicyConversationIDHeader)
			c.Next()
			return
		}
		if scope.Key == "" {
			c.Request.Header.Del(service.KKAIPolicyConversationIDHeader)
			c.Next()
			return
		}
		c.Set(service.KKAIPolicyScopeContextKey, scope)
		// The identifier is an internal routing hint and must not reach providers.
		c.Request.Header.Del(service.KKAIPolicyConversationIDHeader)

		if store == nil {
			c.Next()
			return
		}
		state, err := store.Check(c.Request.Context(), scope.Key)
		if err != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("conversation policy cooldown check failed: %v", err))
			c.Next()
			return
		}
		if !state.Blocked {
			c.Next()
			return
		}

		state.Scope = scope.PublicScope()
		state.Reason = service.KKAIPolicyCooldownReasonCyber
		state.StoreAvailable = true
		service.SetKKAIPolicyCooldownState(c, state)
		if state.RetryAfter < 1 {
			state.RetryAfter = 1
		}
		c.Header("Retry-After", strconv.Itoa(state.RetryAfter))
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": gin.H{
				"message": common.MessageWithRequestId(
					service.KKAIPolicyMessageForCooldown(state.RetryAfter, scope.Stable),
					c.GetString(common.RequestIdKey),
				),
				"type":     "new_api_error",
				"code":     types.ErrorCodeConversationCooldown,
				"metadata": gin.H{"scope": scope.PublicScope(), "retry_after_seconds": state.RetryAfter, "cooldown_level": state.Strike},
			},
		})
		c.Abort()
	}
}
