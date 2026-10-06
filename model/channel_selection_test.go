package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSelectionFiltersBeforePriorityAcrossCacheModes(t *testing.T) {
	truncateTables(t)
	previousCache := common.MemoryCacheEnabled
	t.Cleanup(func() { common.MemoryCacheEnabled = previousCache; InitChannelCache() })
	for _, item := range []struct {
		id       int
		model    string
		priority int64
		plugin   string
	}{
		{901001, "gpt-5@effort:high", 100, "other"},
		{901002, "gpt-5", 80, "other"},
		{901003, "gpt-5", 50, "target"},
		{901004, "gpt-5", 10, "target"},
	} {
		channel := Channel{Id: item.id, Type: constant.ChannelTypeTaskPlugin, Status: common.ChannelStatusEnabled,
			Name: item.plugin, Key: "key", Models: item.model, Group: "default", Priority: &item.priority}
		channel.SetSetting(kitdto.ChannelSettings{TaskPluginKey: item.plugin})
		require.NoError(t, channel.Insert())
	}
	filters := []dto.ChannelFilter{{Kind: dto.FilterTaskPluginIdentity, TaskPluginKey: "target"}}
	for _, cache := range []bool{false, true} {
		common.MemoryCacheEnabled = cache
		InitChannelCache()
		for _, test := range []struct {
			retry    int
			excluded []int
			want     int
		}{
			{0, nil, 901003},
			{1, nil, 901004},
			{0, []int{901003}, 901004},
		} {
			requestFilters := append([]dto.ChannelFilter(nil), filters...)
			requestFilters = append(requestFilters, dto.ChannelFilter{Kind: dto.FilterExcludedChannels, ExcludedChannelIDs: test.excluded})
			selected, err := GetRandomSatisfiedChannel("default", "gpt-5@effort:high", test.retry, requestFilters)
			require.NoError(t, err)
			require.NotNil(t, selected, "cache=%v retry=%d", cache, test.retry)
			assert.Equal(t, test.want, selected.Id)
		}
		selected, err := GetRandomSatisfiedChannel("default", "gpt-5", 0, append(filters,
			dto.ChannelFilter{Kind: dto.FilterAllowedChannelTypes, AllowedChannelTypes: []int{constant.ChannelTypeOpenAI}}))
		require.NoError(t, err)
		assert.Nil(t, selected)
	}
}

func TestChannelWebSocketFilterRequiresNativeResponses(t *testing.T) {
	filters := []dto.ChannelFilter{{Kind: dto.FilterResponsesWebSocket}}
	for _, test := range []struct {
		channelType int
		converter   string
		enabled     bool
		want        bool
	}{
		{constant.ChannelTypeOpenAI, "", true, true},
		{constant.ChannelTypeGemini, "", true, false},
		{constant.ChannelTypeOpenAI, "", false, false},
		{constant.ChannelTypeAdvancedCustom, "none", true, true},
		{constant.ChannelTypeAdvancedCustom, "responses_to_chat", true, false},
		{constant.ChannelTypeVLLM, "", true, true},
		{constant.ChannelTypeSGLang, "", true, true},
	} {
		channel := &Channel{Type: test.channelType}
		channel.SetSetting(kitdto.ChannelSettings{ResponsesWebSocketEnabled: test.enabled})
		if test.channelType == constant.ChannelTypeAdvancedCustom {
			channel.SetOtherSettings(kitdto.ChannelOtherSettings{AdvancedCustom: &kitdto.AdvancedCustomConfig{
				Routes: []kitdto.AdvancedCustomRoute{{IncomingPath: "/v1/responses", UpstreamPath: "/v1/responses", Converter: test.converter}},
			}})
		}
		got, _ := ChannelSatisfiesFilters(channel, "gpt-5", filters)
		assert.Equal(t, test.want, got, "type=%d converter=%s", test.channelType, test.converter)
	}
}

func TestBatchDeleteChannelsReportsOnlyPersistedRows(t *testing.T) {
	truncateTables(t)
	channel := Channel{Type: constant.ChannelTypeOpenAI, Key: "key", Status: common.ChannelStatusEnabled,
		Name: "delete-once", Models: "gpt-5", Group: "default"}
	require.NoError(t, channel.Insert())
	deleted, err := BatchDeleteChannels([]int{channel.Id, channel.Id, channel.Id + 1})
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)
	var remaining int64
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", channel.Id).Count(&remaining).Error)
	assert.Zero(t, remaining)
	deleted, err = BatchDeleteChannels([]int{channel.Id})
	require.NoError(t, err)
	assert.Zero(t, deleted)
}
