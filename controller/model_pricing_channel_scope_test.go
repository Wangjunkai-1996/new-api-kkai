package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelPricingSnapshotChannelScope(t *testing.T) {
	db := modelManagementDB(t, "sqlite", "")
	require.NoError(t, db.Create(&[]model.Channel{
		{Name: "Enabled", Type: 1, Models: " scoped-b,scoped-a,scoped-b, ,", Status: common.ChannelStatusEnabled, Key: "private-key"},
		{Name: "Disabled", Type: 1, Models: "scoped-disabled, scoped-a", Status: common.ChannelStatusManuallyDisabled},
	}).Error)
	require.NoError(t, db.Create(&model.Option{Key: "ModelRatio", Value: `{"historical-only":2}`}).Error)
	before, err := model.AllOption()
	require.NoError(t, err)

	var response struct {
		Success bool
		Data    model.ModelPricingSnapshot
	}
	modelManagementRequest(t, GetModelPricingConfig, http.MethodGet, "/api/option/model_pricing", nil, &response)
	require.True(t, response.Success)
	assert.Equal(t, []string{"scoped-a", "scoped-b", "scoped-disabled"}, response.Data.ChannelModels)
	entries := make(map[string]model.ModelPricingEntry)
	for _, entry := range response.Data.Entries {
		entries[entry.ModelName] = entry
	}
	for _, name := range response.Data.ChannelModels {
		entry, exists := entries[name]
		require.True(t, exists, "channel models must be editable before receiving a price")
		assert.Empty(t, entry.Configured)
	}
	assert.Equal(t, 2.0, entries["historical-only"].Configured["ModelRatio"], "historical pricing remains intact outside channel scope")

	modelManagementRequest(t, GetModelPricingConfig, http.MethodGet, "/api/option/model_pricing?model=scoped-a", nil, &response)
	require.True(t, response.Success)
	require.Len(t, response.Data.Entries, 1)
	assert.Equal(t, "scoped-a", response.Data.Entries[0].ModelName)
	assert.Equal(t, []string{"scoped-a", "scoped-b", "scoped-disabled"}, response.Data.ChannelModels)
	after, err := model.AllOption()
	require.NoError(t, err)
	assert.ElementsMatch(t, before, after)
}
