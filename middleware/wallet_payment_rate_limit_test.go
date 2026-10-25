package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalletPaymentRateLimitIsolatesBudgetsAndIdentities(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldRedis, oldRDB := common.RedisEnabled, common.RDB
	oldEnabled, oldLimit, oldDuration := common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration
	common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration = true, 20, 1200
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB = oldRedis, oldRDB
		common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration = oldEnabled, oldLimit, oldDuration
	})
	for _, useRedis := range []bool{false, true} {
		name := "memory"
		if useRedis {
			name = "redis"
		}
		t.Run(name, func(t *testing.T) {
			common.RedisEnabled = useRedis
			if useRedis {
				server := miniredis.RunT(t)
				client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
				common.RDB = client
				t.Cleanup(func() { require.NoError(t, client.Close()) })
			}
			for index, budget := range []struct {
				scope string
				limit int
			}{{"quote", 60}, {"checkout", 30}, {"state", 20}, {"check", 20}} {
				t.Run(budget.scope, func(t *testing.T) {
					engine := gin.New()
					userID, tenantID := 871200+index, int64(0)
					engine.Use(func(c *gin.Context) {
						c.Set("id", userID)
						common.SetContextKey(c, constant.ContextKeyTenantID, tenantID)
					})
					for _, scope := range []string{"quote", "checkout", "state", "check"} {
						engine.POST("/"+scope, WalletPaymentRateLimit(scope), func(c *gin.Context) { c.Status(http.StatusNoContent) })
					}
					engine.POST("/auth", CriticalRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
					request := func(path, address string) *httptest.ResponseRecorder {
						response := httptest.NewRecorder()
						req := httptest.NewRequest(http.MethodPost, path, nil)
						req.RemoteAddr = address
						engine.ServeHTTP(response, req)
						return response
					}
					for accepted := 0; accepted < budget.limit; accepted++ {
						require.Equal(t, http.StatusNoContent, request("/"+budget.scope, "192.0.2.191:1234").Code)
					}
					limited := request("/"+budget.scope, "192.0.2.191:1234")
					assert.Equal(t, http.StatusTooManyRequests, limited.Code)
					assert.JSONEq(t, `{"success":false,"code":"WALLET_RATE_LIMITED","message":"WALLET_RATE_LIMITED"}`, limited.Body.String())
					assert.Empty(t, limited.Header().Get("Retry-After"), "the shared limiter does not provide a precise retry time")
					assert.Equal(t, "no-store", limited.Header().Get("Cache-Control"))
					assert.Equal(t, http.StatusTooManyRequests, request("/"+budget.scope, "192.0.2.192:1234").Code, "changing IP cannot bypass a user's wallet budget")
					for _, other := range []string{"quote", "checkout", "state", "check", "auth"} {
						if other != budget.scope {
							assert.Equal(t, http.StatusNoContent, request("/"+other, "192.0.2.191:1234").Code, other)
						}
					}
					userID += 100
					assert.Equal(t, http.StatusNoContent, request("/"+budget.scope, "192.0.2.191:1234").Code, "another user sharing the IP retains their budget")
					userID -= 100
					tenantID = 42
					assert.Equal(t, http.StatusNoContent, request("/"+budget.scope, "192.0.2.191:1234").Code, "tenant wallets retain separate budgets")
					userID = 0
					assert.Equal(t, http.StatusUnauthorized, request("/"+budget.scope, "192.0.2.191:1234").Code)
				})
			}
		})
	}
}
