package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

var networkProbeLimiter common.InMemoryRateLimiter

// NetworkProbeCORS permits only the known first-party console origins to read
// the public, empty probe response. The primary console is same-origin.
func NetworkProbeCORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Add("Vary", "Origin")
		origins := c.Request.Header.Values("Origin")
		if len(origins) == 0 {
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
			return
		}
		if len(origins) != 1 {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		origin, err := common.NormalizeOrigin(origins[0])
		if err != nil || (origin != "https://api.kkrich.ltd" && origin != "https://api.kkai.win") {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Header("Access-Control-Allow-Origin", origin)
		if c.Request.Method == http.MethodOptions {
			if c.GetHeader("Access-Control-Request-Method") != http.MethodGet {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Header("Access-Control-Allow-Methods", http.MethodGet)
			c.Header("Access-Control-Allow-Headers", "Cache-Control")
		}
		c.Next()
	}
}

func NetworkProbeRateLimit() gin.HandlerFunc {
	// This lightweight probe does not depend on Redis. Limits are per process,
	// so separate serving slots do not share a single budget.
	networkProbeLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		if !networkProbeLimiter.Request(c.ClientIP(), 120, 60) {
			c.AbortWithStatus(http.StatusTooManyRequests)
			return
		}
		c.Next()
	}
}
