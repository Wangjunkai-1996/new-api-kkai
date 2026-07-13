package model

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pricingSnapshotJSON(t *testing.T) string {
	t.Helper()
	data, err := json.Marshal(GetPricing())
	require.NoError(t, err)
	return string(data)
}

func TestStandbyReadOnlySyncConvergesPricingWithinTwoCycles(t *testing.T) {
	resetPricingEndpointTestTables(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))

	const modelName = "standby-sync-model"
	oldModelRatioJSON := `{"standby-sync-model":1}`
	newModelRatioJSON := `{"standby-sync-model":2}`
	oldCompletionRatioJSON := `{"standby-sync-model":1}`
	newCompletionRatioJSON := `{"standby-sync-model":3}`

	originalModelRatios := ratio_setting.ModelRatio2JSONString()
	originalCompletionRatios := ratio_setting.CompletionRatio2JSONString()
	var originalOptions []Option
	require.NoError(t, DB.Where("key IN ?", []string{"ModelRatio", "CompletionRatio"}).Find(&originalOptions).Error)
	require.NoError(t, DB.Where("key IN ?", []string{"ModelRatio", "CompletionRatio"}).Delete(&Option{}).Error)
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatios))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(originalCompletionRatios))
		require.NoError(t, DB.Where("key IN ?", []string{"ModelRatio", "CompletionRatio"}).Delete(&Option{}).Error)
		if len(originalOptions) > 0 {
			require.NoError(t, DB.Create(&originalOptions).Error)
		}
		InvalidatePricingCache()
	})

	staleChannel := &Channel{
		Id:     501,
		Type:   constant.ChannelTypeAdvancedCustom,
		Key:    "key-501",
		Status: common.ChannelStatusEnabled,
		Name:   "channel-501",
	}
	staleChannel.SetOtherSettings(pricingEndpointAdvancedCustomConfig(dto.AdvancedCustomRoute{
		IncomingPath: "/v1/chat/completions",
		UpstreamPath: "/v1/chat/completions",
	}))
	require.NoError(t, DB.Create(staleChannel).Error)
	insertPricingEndpointAbility(t, 501, modelName)
	require.NoError(t, DB.Create([]Option{
		{Key: "ModelRatio", Value: oldModelRatioJSON},
		{Key: "CompletionRatio", Value: oldCompletionRatioJSON},
	}).Error)

	syncOptionsOnce()
	syncChannelCacheOnce()
	stalePricing := pricingSnapshotJSON(t)

	activeChannel := *staleChannel
	activeChannel.SetOtherSettings(pricingEndpointAdvancedCustomConfig(
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/chat/completions",
			UpstreamPath: "/v1/chat/completions",
		},
		dto.AdvancedCustomRoute{
			IncomingPath: "/v1/responses",
			UpstreamPath: "/v1/responses",
		},
	))
	require.NoError(t, DB.Model(&Option{}).Where("key = ?", "ModelRatio").Update("value", newModelRatioJSON).Error)
	require.NoError(t, DB.Model(&Option{}).Where("key = ?", "CompletionRatio").Update("value", newCompletionRatioJSON).Error)
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", activeChannel.Id).Update("settings", activeChannel.OtherSettings).Error)

	syncOptionsOnce()
	syncChannelCacheOnce()
	activePricing := pricingSnapshotJSON(t)
	assert.NotEqual(t, stalePricing, activePricing)

	require.NoError(t, updateOptionMap("ModelRatio", oldModelRatioJSON))
	require.NoError(t, updateOptionMap("CompletionRatio", oldCompletionRatioJSON))
	CacheUpdateChannel(staleChannel)
	InvalidatePricingCache()
	assert.Equal(t, stalePricing, pricingSnapshotJSON(t))

	convergedOnCycle := 0
	for cycle := 1; cycle <= 2; cycle++ {
		syncOptionsOnce()
		syncChannelCacheOnce()
		if pricingSnapshotJSON(t) == activePricing {
			convergedOnCycle = cycle
			break
		}
	}
	assert.NotZero(t, convergedOnCycle, "standby pricing did not converge within two sync cycles")
}
