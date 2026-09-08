package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveModelRequestRateLimitPrecedence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalGroup := setting.ModelRequestRateLimitGroup2JSONString()
	originalUser := setting.ModelRequestRateLimitUser2JSONString()
	originalTotal := setting.ModelRequestRateLimitCount
	originalSuccess := setting.ModelRequestRateLimitSuccessCount
	t.Cleanup(func() {
		setting.ModelRequestRateLimitCount = originalTotal
		setting.ModelRequestRateLimitSuccessCount = originalSuccess
		require.NoError(t, setting.UpdateModelRequestRateLimitGroupByJSONString(originalGroup))
		require.NoError(t, setting.UpdateModelRequestRateLimitUserByJSONString(originalUser))
	})

	setting.ModelRequestRateLimitCount = 100
	setting.ModelRequestRateLimitSuccessCount = 90
	require.NoError(t, setting.UpdateModelRequestRateLimitGroupByJSONString(`{"vip":[80,70]}`))
	require.NoError(t, setting.UpdateModelRequestRateLimitUserByJSONString(`{
		"username:alice":[60,50],
		"id:42":[40,30]
	}`))

	newContext := func(id int, username, group string) *gin.Context {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Set("id", id)
		common.SetContextKey(context, constant.ContextKeyUserName, username)
		common.SetContextKey(context, constant.ContextKeyUserGroup, group)
		return context
	}

	total, success := resolveModelRequestRateLimit(newContext(42, "alice", "vip"))
	assert.Equal(t, 40, total)
	assert.Equal(t, 30, success)

	total, success = resolveModelRequestRateLimit(newContext(7, "alice", "vip"))
	assert.Equal(t, 60, total)
	assert.Equal(t, 50, success)

	total, success = resolveModelRequestRateLimit(newContext(7, "bob", "vip"))
	assert.Equal(t, 80, total)
	assert.Equal(t, 70, success)

	total, success = resolveModelRequestRateLimit(newContext(7, "bob", "default"))
	assert.Equal(t, 100, total)
	assert.Equal(t, 90, success)
}
