package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupContextForSelectedChannelReturnsUnavailableWhenPolicyBreakerFiltersAllKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	channel := &model.Channel{
		Id:     123,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "upstream-key-a\nupstream-key-b",
		Status: common.ChannelStatusEnabled,
		Name:   "policy-breaker-channel",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}

	originalBreaker := isUpstreamKeyPolicyBreakerOpenForDistributor
	isUpstreamKeyPolicyBreakerOpenForDistributor = func(channelId int, key string) bool {
		return channelId == channel.Id
	}
	t.Cleanup(func() {
		isUpstreamKeyPolicyBreakerOpenForDistributor = originalBreaker
	})

	setupErr := SetupContextForSelectedChannel(ctx, channel, "gpt-policy")

	require.NotNil(t, setupErr)
	assert.Equal(t, http.StatusServiceUnavailable, setupErr.StatusCode)
	assert.Equal(t, types.ErrorCodePolicyUpstreamKeyIsolated, setupErr.GetErrorCode())

	statusCode, message, code := publicMiddlewareAPIError(setupErr)
	assert.Equal(t, http.StatusServiceUnavailable, statusCode)
	assert.Equal(t, types.PublicMessageUpstreamUnavailable, message)
	assert.Equal(t, types.ErrorCodeUpstreamUnavailable, code)

	_, hasChannelKey := common.GetContextKey(ctx, constant.ContextKeyChannelKey)
	assert.False(t, hasChannelKey)
}
