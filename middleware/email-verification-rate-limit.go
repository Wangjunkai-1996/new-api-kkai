package middleware

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const (
	EmailVerificationRateLimitMark = "EV"
	EmailVerificationMaxRequests   = 2  // 30秒内最多2次
	EmailVerificationDuration      = 30 // 30秒时间窗口
)

var emailVerificationCounterScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
local ttl = redis.call('TTL', KEYS[1])
if ttl < 0 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
  ttl = tonumber(ARGV[1])
end
return {count, ttl}
`)

func redisEmailVerificationRateLimiter(c *gin.Context) {
	key := tenantRateLimitKey(c, "emailVerification:"+EmailVerificationRateLimitMark+":"+c.ClientIP())
	result, err := emailVerificationCounterScript.Run(c.Request.Context(), common.RDB, []string{key}, EmailVerificationDuration).Int64Slice()
	if err != nil {
		inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
		memoryEmailVerificationRateLimiter(c)
		return
	}
	if result[0] <= int64(EmailVerificationMaxRequests) {
		c.Next()
		return
	}
	waitSeconds := max(int64(1), result[1])
	c.Header("Retry-After", strconv.FormatInt(waitSeconds, 10))
	common.ApiErrorStatus(c, http.StatusTooManyRequests, common.NewMessage("Please wait {{seconds}} seconds before requesting another verification code.", map[string]any{"seconds": waitSeconds}))
	c.Abort()
}

func memoryEmailVerificationRateLimiter(c *gin.Context) {
	key := tenantRateLimitKey(c, EmailVerificationRateLimitMark+":"+c.ClientIP())

	if !inMemoryRateLimiter.Request(key, EmailVerificationMaxRequests, EmailVerificationDuration) {
		common.ApiErrorStatus(c, http.StatusTooManyRequests, common.NewMessage("Please wait before requesting another verification code."))
		c.Abort()
		return
	}

	c.Next()
}

func EmailVerificationRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if common.RedisEnabled {
			redisEmailVerificationRateLimiter(c)
		} else {
			inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
			memoryEmailVerificationRateLimiter(c)
		}
	}
}
