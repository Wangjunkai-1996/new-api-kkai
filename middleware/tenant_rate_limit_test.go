package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestTenantRateLimitsIsolateSameIPAndOwnerID(t *testing.T) {
	previousRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedis })
	for _, tc := range []struct {
		name    string
		handler gin.HandlerFunc
	}{
		{"ip", rateLimitFactory(1, 60, t.Name()+"-ip")},
		{"user", userRateLimitFactory(1, 60, t.Name()+"-user")},
		{"model", memoryRateLimitHandler(60, 1, 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, request := range []struct {
				tenant int64
				status int
			}{{42, 204}, {42, 429}, {43, 204}, {0, 204}} {
				response := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(response)
				c.Request = httptest.NewRequest(http.MethodGet, "http://same-host/", nil)
				c.Request.RemoteAddr = "192.0.2.180:1234"
				c.Set("id", 991234)
				common.SetContextKey(c, constant.ContextKeyTenantID, request.tenant)
				tc.handler(c)
				if !c.IsAborted() {
					c.Status(http.StatusNoContent)
				}
				c.Writer.WriteHeaderNow()
				assert.Equal(t, request.status, response.Code)
			}
		})
	}
}
