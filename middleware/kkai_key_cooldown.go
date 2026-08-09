package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

var kkaiPolicyKeyCooldownUnavailableWarning sync.Once

func KKAIPolicyKeyCooldown() gin.HandlerFunc {
	return kkaiPolicyKeyCooldown(service.KKAIPolicyDefaultKeyCooldownStore())
}

func kkaiPolicyKeyCooldown(store service.KKAIPolicyKeyCooldownStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenID := common.GetContextKeyInt(c, constant.ContextKeyTokenId)
		key, ok := service.KKAIPolicyKeyCooldownRedisKey(tokenID)
		if !ok {
			c.Next()
			return
		}
		if store == nil {
			kkaiPolicyKeyCooldownUnavailableWarning.Do(func() {
				logger.LogWarn(c.Request.Context(), "KKAI key cooldown Redis store unavailable; failing open")
			})
			c.Next()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 150*time.Millisecond)
		state, err := store.Check(ctx, key)
		cancel()
		if err != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("KKAI key cooldown check failed; failing open: %v", err))
			c.Next()
			return
		}
		if !state.Blocked {
			c.Next()
			return
		}

		retryAfter := state.RetryAfter
		if retryAfter < 1 {
			retryAfter = 1
		}
		c.Header("Retry-After", strconv.Itoa(retryAfter))
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": gin.H{
				"message": common.MessageWithRequestId(
					service.KKAIPolicyMessageForKeyCooldown(retryAfter),
					c.GetString(common.RequestIdKey),
				),
				"type": string(types.ErrorTypeNewAPIError),
				"code": types.ErrorCodeKeyCooldown,
			},
		})
		c.Abort()
	}
}
