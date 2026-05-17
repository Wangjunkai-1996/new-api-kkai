package relay

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestPolicyBreakerTaskErrorContract(t *testing.T) {
	taskErr := policyBreakerTaskError()

	require.Equal(t, http.StatusServiceUnavailable, taskErr.StatusCode)
	require.Equal(t, "policy_breaker_open", taskErr.Code)
	require.True(t, taskErr.LocalError)
}

func TestGetNextEnabledTaskKeySkipsBreakerOpenKey(t *testing.T) {
	ch := &model.Channel{
		Id:  10,
		Key: "upstream-a\nupstream-b",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	original := isUpstreamKeyPolicyBreakerOpenForTask
	isUpstreamKeyPolicyBreakerOpenForTask = func(channelId int, key string) bool {
		return channelId == ch.Id && key == "upstream-a"
	}
	t.Cleanup(func() {
		isUpstreamKeyPolicyBreakerOpenForTask = original
	})

	key, index, taskErr := getNextEnabledTaskKey(ch)

	require.Nil(t, taskErr)
	require.Equal(t, "upstream-b", key)
	require.Equal(t, 1, index)
}

func TestGetNextEnabledTaskKeyReturnsPolicyErrorWhenAllKeysBreakerOpen(t *testing.T) {
	ch := &model.Channel{
		Id:  11,
		Key: "upstream-a\nupstream-b",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	original := isUpstreamKeyPolicyBreakerOpenForTask
	isUpstreamKeyPolicyBreakerOpenForTask = func(channelId int, key string) bool {
		return channelId == ch.Id
	}
	t.Cleanup(func() {
		isUpstreamKeyPolicyBreakerOpenForTask = original
	})

	key, index, taskErr := getNextEnabledTaskKey(ch)

	require.Empty(t, key)
	require.Zero(t, index)
	require.NotNil(t, taskErr)
	require.Equal(t, http.StatusServiceUnavailable, taskErr.StatusCode)
	require.Equal(t, "policy_breaker_open", taskErr.Code)
	require.True(t, taskErr.LocalError)
}
