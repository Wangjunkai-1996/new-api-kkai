package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func KKAIPolicyKeyCooldown() gin.HandlerFunc {
	return kkaiPolicyKeyCooldown(service.KKAIPolicyDefaultKeyCooldownStore())
}

func kkaiPolicyKeyCooldown(store service.KKAIPolicyKeyCooldownStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.CyberPolicyKeyCooldownEnabled {
			c.Next()
			return
		}
		tokenID := common.GetContextKeyInt(c, constant.ContextKeyTokenId)
		key, ok := service.KKAIPolicyKeyCooldownRedisKey(tokenID)
		if !ok {
			c.Next()
			return
		}
		if state := service.CheckKKAIPolicyEmergencyKeyCooldown(key, time.Now()); state.Blocked {
			abortKKAIPolicyKeyCooldown(c, state)
			return
		}
		if store == nil {
			if !common.RedisEnabled {
				c.Next()
				return
			}
			logger.LogWarn(c.Request.Context(), "KKAI key cooldown Redis store unavailable; failing closed")
			abortKKAIPolicyKeyCooldownUnavailable(c)
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 150*time.Millisecond)
		state, err := store.Check(ctx, key)
		cancel()
		if err != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("KKAI key cooldown check failed; failing closed: %v", err))
			abortKKAIPolicyKeyCooldownUnavailable(c)
			return
		}
		if !state.Blocked {
			c.Next()
			return
		}
		abortKKAIPolicyKeyCooldown(c, state)
	}
}

func abortKKAIPolicyKeyCooldown(c *gin.Context, state service.KKAIPolicyKeyCooldownState) {
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

func abortKKAIPolicyKeyCooldownUnavailable(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"error": gin.H{
			"message": common.MessageWithRequestId(
				service.KKAIPolicyMessageForKeyCooldownUnavailable(),
				c.GetString(common.RequestIdKey),
			),
			"type": string(types.ErrorTypeNewAPIError),
			"code": types.ErrorCodePolicyAuditUnavailable,
		},
	})
	c.Abort()
}
