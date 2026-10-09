package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelPricingDisplayExpression(t *testing.T) {
	db := modelManagementDB(t, "sqlite", "")
	const name = "cache-display-model"
	const actual = `tier("base", p * 10 + cr * 2)`
	const display = `tier("base", p * 10 + cr * 0)`
	channel := model.Channel{Name: "cache-display", Type: constant.ChannelTypeOpenAI, Key: "unused", Status: common.ChannelStatusEnabled, Models: name, Group: "default"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: name, ChannelId: channel.Id, Enabled: true}).Error)
	before, err := model.GetModelPricingSnapshot([]string{name})
	require.NoError(t, err)
	change := model.ModelPricingChange{ModelName: name, ExpectedVersion: before.EmptyVersion, Pricing: model.PricingValues{
		"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": actual,
		billing_setting.DisplayBillingExprOption: display, "CacheRatio": float64(0.2),
	}}
	require.NoError(t, model.UpdateModelPricing([]model.ModelPricingChange{change}))
	loaded, err := model.GetModelPricingSnapshot([]string{name})
	require.NoError(t, err)
	require.Len(t, loaded.Entries, 1)
	assert.Equal(t, display, loaded.Entries[0].Configured[billing_setting.DisplayBillingExprOption])
	assert.Equal(t, actual, loaded.Entries[0].Effective["billing_setting.billing_expr"])
	assert.ErrorIs(t, model.UpdateModelPricing([]model.ModelPricingChange{change}), model.ErrModelPricingConflict)

	var option model.Option
	require.NoError(t, db.Where(&model.Option{Key: billing_setting.DisplayBillingExprOption}).Take(&option).Error)
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{"display_billing_expr": "{}"}))
	assert.Equal(t, actual, billing_setting.GetDisplayBillingExpr(name, actual))
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{billing_setting.DisplayBillingExprOption: option.Value}))
	assert.Equal(t, display, billing_setting.GetDisplayBillingExpr(name, actual))
	assert.Equal(t, `tier("base", p * 20 + cr * 2)`, billing_setting.GetDisplayBillingExpr(name, `tier("base", p * 20 + cr * 2)`), "stale persisted overrides must not change an updated schedule")

	var publicPrice *model.Pricing
	for _, price := range model.GetPricing() {
		if price.ModelName == name {
			publicPrice = &price
		}
	}
	require.NotNil(t, publicPrice)
	assert.Equal(t, display, publicPrice.BillingExpr)
	assert.Nil(t, publicPrice.CacheRatio)
	base := map[string]any(ratio_setting.GetExposedData())
	sync := billing_setting.GetPricingSyncData(base)
	assert.Equal(t, display, sync["billing_expr"].(map[string]string)[name])
	assert.NotContains(t, sync["cache_ratio"], name)
	assert.Contains(t, base["cache_ratio"], name, "public projection must not mutate the ratio cache")
	assert.Equal(t, actual, getLocalPricingSyncData()["billing_expr"].(map[string]string)[name])

	// Both the legacy option endpoint and the model transaction enforce the
	// display contract. A changed input price cannot retain the old display.
	updatedActual, err := common.Marshal(map[string]string{name: `tier("base", p * 20 + cr * 2)`})
	require.NoError(t, err)
	require.ErrorContains(t, model.UpdateOption("billing_setting.billing_expr", string(updatedActual)), "display billing expression")
	invalidDisplay, err := common.Marshal(map[string]string{name: `tier("base", p * 10 + cr * -1)`})
	require.NoError(t, err)
	var response struct {
		Success bool `json:"success"`
	}
	modelManagementRequest(t, UpdateOption, http.MethodPut, "/api/option/", map[string]any{"key": billing_setting.DisplayBillingExprOption, "value": string(invalidDisplay)}, &response)
	assert.False(t, response.Success)
	after, err := model.GetModelPricingSnapshot([]string{name})
	require.NoError(t, err)
	assert.Equal(t, loaded.Entries, after.Entries)
	assert.Equal(t, display, billing_setting.GetDisplayBillingExpr(name, actual))

	change.ExpectedVersion = after.Entries[0].Version
	delete(change.Pricing, billing_setting.DisplayBillingExprOption)
	require.NoError(t, model.UpdateModelPricing([]model.ModelPricingChange{change}))
	after, err = model.GetModelPricingSnapshot([]string{name})
	require.NoError(t, err)
	assert.NotEqual(t, loaded.Entries[0].Version, after.Entries[0].Version)
	assert.Equal(t, actual, billing_setting.GetDisplayBillingExpr(name, actual))
	change.ExpectedVersion = after.Entries[0].Version
	change.Pricing[billing_setting.DisplayBillingExprOption] = display
	require.NoError(t, model.UpdateModelPricing([]model.ModelPricingChange{change}))
	after, err = model.GetModelPricingSnapshot([]string{name})
	require.NoError(t, err)
	require.NoError(t, model.UpdateModelPricing([]model.ModelPricingChange{{ModelName: name, ExpectedVersion: after.Entries[0].Version, Reset: true}}))
	after, err = model.GetModelPricingSnapshot([]string{name})
	require.NoError(t, err)
	assert.NotContains(t, after.Entries[0].Configured, billing_setting.DisplayBillingExprOption)
	assert.Equal(t, actual, billing_setting.GetDisplayBillingExpr(name, actual))
}
