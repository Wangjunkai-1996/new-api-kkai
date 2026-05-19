package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDisableTokenById_DisablesEnabledTokenOnce(t *testing.T) {
	truncateTables(t)

	token := &Token{
		UserId: 123,
		Key:    "client-policy-token",
		Status: common.TokenStatusEnabled,
	}
	require.NoError(t, DB.Create(token).Error)

	changed, err := DisableTokenById(token.Id, token.UserId)
	require.NoError(t, err)
	assert.True(t, changed)

	var reloaded Token
	require.NoError(t, DB.First(&reloaded, token.Id).Error)
	assert.Equal(t, common.TokenStatusDisabled, reloaded.Status)

	changed, err = DisableTokenById(token.Id, token.UserId)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestDisableTokenById_AlreadyInactiveDoesNotChange(t *testing.T) {
	truncateTables(t)

	token := &Token{
		UserId: 456,
		Key:    "client-policy-disabled-token",
		Status: common.TokenStatusDisabled,
	}
	require.NoError(t, DB.Create(token).Error)

	changed, err := DisableTokenById(token.Id, token.UserId)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestDisableTokenById_ValidatesTokenAndUser(t *testing.T) {
	truncateTables(t)

	changed, err := DisableTokenById(0, 1)
	require.Error(t, err)
	assert.False(t, changed)

	token := &Token{
		UserId: 789,
		Key:    "client-policy-owned-token",
		Status: common.TokenStatusEnabled,
	}
	require.NoError(t, DB.Create(token).Error)

	changed, err = DisableTokenById(token.Id, token.UserId+1)
	require.Error(t, err)
	assert.True(t, errors.Is(err, gorm.ErrRecordNotFound))
	assert.False(t, changed)

	var reloaded Token
	require.NoError(t, DB.First(&reloaded, token.Id).Error)
	assert.Equal(t, common.TokenStatusEnabled, reloaded.Status)
}

func TestUpdateChannelStatus_MultiKeyOnlyUpdatesMatchingKey(t *testing.T) {
	truncateTables(t)

	channel := &Channel{
		Key:    "upstream-key-a\nupstream-key-b",
		Status: common.ChannelStatusEnabled,
		Name:   "multi-key-policy-channel",
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	require.NoError(t, DB.Create(channel).Error)

	changed := UpdateChannelStatus(channel.Id, "upstream-key-b", common.ChannelStatusAutoDisabled, "policy")
	assert.True(t, changed)

	reloaded, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, reloaded.Status)
	assert.NotContains(t, reloaded.ChannelInfo.MultiKeyStatusList, 0)
	assert.Equal(t, common.ChannelStatusAutoDisabled, reloaded.ChannelInfo.MultiKeyStatusList[1])
	assert.Equal(t, "policy", reloaded.ChannelInfo.MultiKeyDisabledReason[1])
}

func TestUpdateChannelStatus_MultiKeyKeepsChannelEnabledWhenAlternateKeyStillSelectable(t *testing.T) {
	truncateTables(t)

	channel := &Channel{
		Key:    "upstream-key-a\nupstream-key-b",
		Status: common.ChannelStatusEnabled,
		Name:   "multi-key-policy-channel-stale-state",
		ChannelInfo: ChannelInfo{
			IsMultiKey:             true,
			MultiKeySize:           1,
			MultiKeyStatusList:     map[int]int{99: common.ChannelStatusAutoDisabled},
			MultiKeyDisabledReason: map[int]string{99: "stale"},
		},
	}
	require.NoError(t, DB.Create(channel).Error)

	changed := UpdateChannelStatus(channel.Id, "upstream-key-a", common.ChannelStatusAutoDisabled, "policy-a")
	assert.True(t, changed)

	reloaded, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, reloaded.Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, reloaded.ChannelInfo.MultiKeyStatusList[0])
	assert.NotContains(t, reloaded.ChannelInfo.MultiKeyStatusList, 1)

	key, index, apiErr := reloaded.GetNextEnabledKey()
	require.Nil(t, apiErr)
	assert.Equal(t, "upstream-key-b", key)
	assert.Equal(t, 1, index)
}

func TestUpdateChannelStatus_MultiKeyIgnoresEmptyOrUnknownUsingKey(t *testing.T) {
	truncateTables(t)

	channel := &Channel{
		Key:    "upstream-key-a\nupstream-key-b",
		Status: common.ChannelStatusEnabled,
		Name:   "multi-key-policy-channel-safe-miss",
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	require.NoError(t, DB.Create(channel).Error)

	assert.False(t, UpdateChannelStatus(channel.Id, "", common.ChannelStatusAutoDisabled, "empty"))
	assert.False(t, UpdateChannelStatus(channel.Id, "missing-key", common.ChannelStatusAutoDisabled, "missing"))

	reloaded, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, reloaded.Status)
	assert.Empty(t, reloaded.ChannelInfo.MultiKeyStatusList)
	assert.Empty(t, reloaded.ChannelInfo.MultiKeyDisabledReason)
}

func TestUpdateChannelStatus_MultiKeyDisablesChannelWhenAllKeysDisabled(t *testing.T) {
	truncateTables(t)

	channel := &Channel{
		Key:    "upstream-key-a\nupstream-key-b",
		Status: common.ChannelStatusEnabled,
		Name:   "multi-key-policy-channel-all-disabled",
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	require.NoError(t, DB.Create(channel).Error)

	assert.True(t, UpdateChannelStatus(channel.Id, "upstream-key-a", common.ChannelStatusAutoDisabled, "policy-a"))
	assert.True(t, UpdateChannelStatus(channel.Id, "upstream-key-b", common.ChannelStatusAutoDisabled, "policy-b"))

	reloaded, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, reloaded.Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, reloaded.ChannelInfo.MultiKeyStatusList[0])
	assert.Equal(t, common.ChannelStatusAutoDisabled, reloaded.ChannelInfo.MultiKeyStatusList[1])
}

func TestUpdateChannelStatus_SingleKeyDisablesChannel(t *testing.T) {
	truncateTables(t)

	channel := &Channel{
		Key:    "single-upstream-key",
		Status: common.ChannelStatusEnabled,
		Name:   "single-key-policy-channel",
	}
	require.NoError(t, DB.Create(channel).Error)

	assert.True(t, UpdateChannelStatus(channel.Id, "", common.ChannelStatusAutoDisabled, "policy"))

	reloaded, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, reloaded.Status)
	assert.Equal(t, "policy", reloaded.GetOtherInfo()["status_reason"])
}

func TestGetNextEnabledKeyWithFilterSkipsRejectedKeys(t *testing.T) {
	channel := &Channel{
		Key: "upstream-key-a\nupstream-key-b\nupstream-key-c",
		ChannelInfo: ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       3,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
		},
	}

	key, index, apiErr := channel.GetNextEnabledKeyWithFilter(func(key string, index int) bool {
		return key != "upstream-key-b"
	})

	require.Nil(t, apiErr)
	assert.Equal(t, "upstream-key-c", key)
	assert.Equal(t, 2, index)
}

func TestGetNextEnabledKeyWithFilterReturnsErrorWhenAllFiltered(t *testing.T) {
	channel := &Channel{
		Key: "upstream-key-a\nupstream-key-b",
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}

	key, index, apiErr := channel.GetNextEnabledKeyWithFilter(func(key string, index int) bool {
		return false
	})

	assert.Empty(t, key)
	assert.Zero(t, index)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeChannelNoAvailableKey, apiErr.GetErrorCode())
	assert.Contains(t, apiErr.Error(), "no available keys")
}

func TestGetNextEnabledKeyWithFilterPollingSkipsRejectedKeys(t *testing.T) {
	truncateTables(t)

	channel := &Channel{
		Key:    "upstream-key-a\nupstream-key-b\nupstream-key-c",
		Status: common.ChannelStatusEnabled,
		Name:   "multi-key-policy-selector-polling",
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         3,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 0,
		},
	}
	require.NoError(t, DB.Create(channel).Error)

	key, index, apiErr := channel.GetNextEnabledKeyWithFilter(func(key string, index int) bool {
		return key == "upstream-key-b"
	})

	require.Nil(t, apiErr)
	assert.Equal(t, "upstream-key-b", key)
	assert.Equal(t, 1, index)
	assert.Equal(t, 2, channel.ChannelInfo.MultiKeyPollingIndex)
}

func TestGetNextEnabledKeyWithFilterRandomUsesAllowedCandidates(t *testing.T) {
	channel := &Channel{
		Key: "upstream-key-a\nupstream-key-b\nupstream-key-c",
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 3,
			MultiKeyMode: constant.MultiKeyModeRandom,
		},
	}

	for i := 0; i < 10; i++ {
		key, index, apiErr := channel.GetNextEnabledKeyWithFilter(func(key string, index int) bool {
			return key == "upstream-key-b"
		})
		require.Nil(t, apiErr)
		assert.Equal(t, "upstream-key-b", key)
		assert.Equal(t, 1, index)
	}
}
