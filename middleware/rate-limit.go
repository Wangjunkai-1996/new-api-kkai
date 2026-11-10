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
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

var timeFormat = "2006-01-02T15:04:05.000Z"

var inMemoryRateLimiter common.InMemoryRateLimiter

// tenantRateLimitKey isolates branded traffic while preserving platform keys.
func tenantRateLimitKey(c *gin.Context, key string) string {
	if tenantID := common.GetContextKeyInt64(c, constant.ContextKeyTenantID); tenantID > 0 {
		return fmt.Sprintf("tenant:%d:%s", tenantID, key)
	}
	return key
}

var defNext = func(c *gin.Context) {
	c.Next()
}

// Keep the existing list key and wall-clock timestamp representation so old
// and new nodes share one sliding window. Admission, trimming and TTL refresh
// happen together; denied requests do not consume a list entry.
var redisSlidingWindowScript = redis.NewScript(`
local length = redis.call('LLEN', KEYS[1])
local maximum = tonumber(ARGV[1])
if maximum <= 0 then return redis.error_reply('invalid rate limit') end
if length >= maximum then
  local oldest = redis.call('LINDEX', KEYS[1], -1)
  local y, mo, d, h, mi, sec = string.match(oldest, '^(%d%d%d%d)%-(%d%d)%-(%d%d)T(%d%d):(%d%d):(%d%d)%.%d%d%dZ$')
  if not y then return redis.error_reply('invalid rate limit timestamp') end
  y, mo, d, h, mi, sec = tonumber(y), tonumber(mo), tonumber(d), tonumber(h), tonumber(mi), tonumber(sec)
  local days = {31,28,31,30,31,30,31,31,30,31,30,31}
  if y % 4 == 0 and (y % 100 ~= 0 or y % 400 == 0) then days[2] = 29 end
  if mo < 1 or mo > 12 or d < 1 or d > days[mo] or h > 23 or mi > 59 or sec > 59 then
    return redis.error_reply('invalid rate limit timestamp')
  end
  if oldest > ARGV[3] then
    redis.call('EXPIRE', KEYS[1], ARGV[4])
    return {0, oldest}
  end
end
redis.call('LPUSH', KEYS[1], ARGV[2])
redis.call('LTRIM', KEYS[1], 0, maximum - 1)
redis.call('EXPIRE', KEYS[1], ARGV[4])
return {1, ''}
`)

func redisSlidingWindowTake(ctx context.Context, key string, maximum int, duration int64, now time.Time) (bool, int64, error) {
	stamp := now.Format(timeFormat)
	// Legacy entries encode local wall time with a literal Z suffix.
	wallTime, err := time.Parse(timeFormat, stamp)
	if err != nil {
		return false, 0, err
	}
	cutoff := wallTime.Add(-time.Duration(duration) * time.Second).Format(timeFormat)
	result, err := redisSlidingWindowScript.Run(ctx, common.RDB, []string{key}, maximum, stamp, cutoff, int64(common.RateLimitKeyExpirationDuration/time.Second)).Slice()
	if err != nil {
		return false, 0, err
	}
	if result[0].(int64) == 1 {
		return true, 0, nil
	}
	oldest, err := time.Parse(timeFormat, result[1].(string))
	if err != nil {
		return false, 0, err
	}
	remaining := oldest.Add(time.Duration(duration) * time.Second).Sub(wallTime)
	return false, max(int64(1), int64((remaining+time.Second-1)/time.Second)), nil
}

func redisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	key := tenantRateLimitKey(c, "rateLimit:"+mark+c.ClientIP())
	userRedisRateLimiter(c, maxRequestNum, duration, key)
}

func memoryRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	key := tenantRateLimitKey(c, mark+c.ClientIP())
	if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
		c.Status(http.StatusTooManyRequests)
		c.Abort()
		return
	}
}

func rateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			redisRateLimiter(c, maxRequestNum, duration, mark)
		}
	} else {
		// It's safe to call multi times.
		inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
		return func(c *gin.Context) {
			memoryRateLimiter(c, maxRequestNum, duration, mark)
		}
	}
}

// GlobalWebRateLimit limits dashboard pages and other fallback paths per IP.
// Files that exist in the frontend build count against a separate static
// limiter instead, so lazy-loaded chunks and icons do not use up the page
// budget. The static limiter is disabled by default.
func GlobalWebRateLimit(frontendFS static.ServeFileSystem) func(c *gin.Context) {
	webLimit := defNext
	if common.GlobalWebRateLimitEnable {
		webLimit = rateLimitFactory(common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration, "GW")
	}
	staticLimit := defNext
	if common.GlobalStaticRateLimitEnable {
		staticLimit = rateLimitFactory(common.GlobalStaticRateLimitNum, common.GlobalStaticRateLimitDuration, "GS")
	}
	return func(c *gin.Context) {
		if frontendFS.Exists("/", c.Request.URL.Path) {
			staticLimit(c)
			return
		}
		webLimit(c)
	}
}

func GlobalAPIRateLimit() func(c *gin.Context) {
	if common.GlobalApiRateLimitEnable {
		return rateLimitFactory(common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration, "GA")
	}
	return defNext
}

func CriticalRateLimit() func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return rateLimitFactory(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, "CT")
	}
	return defNext
}

func UserCriticalRateLimit(scope string) func(c *gin.Context) {
	if !common.CriticalRateLimitEnable {
		return defNext
	}
	return userRateLimitFactory(
		common.CriticalRateLimitNum,
		common.CriticalRateLimitDuration,
		"UC:"+scope,
	)
}

func DownloadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(common.DownloadRateLimitNum, common.DownloadRateLimitDuration, "DW")
}

func UploadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(common.UploadRateLimitNum, common.UploadRateLimitDuration, "UP")
}

// userRateLimitFactory creates a rate limiter keyed by authenticated user ID
// instead of client IP, making it resistant to proxy rotation attacks.
// Must be used AFTER authentication middleware (UserAuth).
func userRateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			userId := c.GetInt("id")
			if userId == 0 {
				c.Status(http.StatusUnauthorized)
				c.Abort()
				return
			}
			key := tenantRateLimitKey(c, fmt.Sprintf("rateLimit:%s:user:%d", mark, userId))
			userRedisRateLimiter(c, maxRequestNum, duration, key)
		}
	}
	// It's safe to call multi times.
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		userId := c.GetInt("id")
		if userId == 0 {
			c.Status(http.StatusUnauthorized)
			c.Abort()
			return
		}
		key := tenantRateLimitKey(c, fmt.Sprintf("%s:user:%d", mark, userId))
		if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
			c.Status(http.StatusTooManyRequests)
			c.Abort()
			return
		}
	}
}

// userRedisRateLimiter is like redisRateLimiter but accepts a pre-built key
// (to support user-ID-based keys).
func userRedisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, key string) {
	allowed, retryAfter, err := redisSlidingWindowTake(c.Request.Context(), key, maxRequestNum, duration, time.Now())
	if err != nil {
		logger.LogError(c.Request.Context(), common.LogText("rate limit check failed (key=%s): %v", key, err))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	if !allowed {
		c.Header("Retry-After", strconv.FormatInt(retryAfter, 10))
		c.AbortWithStatus(http.StatusTooManyRequests)
	}
}

// SearchRateLimit returns a per-user rate limiter for search endpoints.
// Configurable via SEARCH_RATE_LIMIT_ENABLE / SEARCH_RATE_LIMIT / SEARCH_RATE_LIMIT_DURATION.
func SearchRateLimit() func(c *gin.Context) {
	if !common.SearchRateLimitEnable {
		return defNext
	}
	return userRateLimitFactory(common.SearchRateLimitNum, common.SearchRateLimitDuration, "SR")
}
