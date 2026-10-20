package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelPricingResetPreview(t *testing.T) {
	modelManagementDB(t, "sqlite", "")
	const savedExpression = `tier("saved", p * 99 + c * 99)`
	cases := []struct {
		name       string
		configured model.PricingValues
	}{
		{"gpt-4o", model.PricingValues{"ModelRatio": 1.25, "CacheRatio": 0.5}},
		{"glm-4-flash", model.PricingValues{"ModelRatio": 0.0}},
		{"dall-e-3", model.PricingValues{"ModelPrice": 0.04}},
		{"gpt-6-astra", model.PricingValues{}},
		{"no-default-price", model.PricingValues{}},
	}
	names := make([]string, 0, len(cases))
	for _, tc := range cases {
		names = append(names, tc.name)
	}
	snapshot, err := model.GetModelPricingSnapshot(names)
	require.NoError(t, err)
	changes := make([]model.ModelPricingChange, 0, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		changes = append(changes, model.ModelPricingChange{
			ModelName: entry.ModelName, ExpectedVersion: entry.Version,
			Pricing: model.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": savedExpression},
		})
	}
	require.NoError(t, model.UpdateModelPricing(changes))
	before, err := model.GetModelPricingSnapshot(names)
	require.NoError(t, err)
	optionsBefore, err := model.AllOption()
	require.NoError(t, err)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var response struct {
				Success bool
				Data    struct {
					model.ModelPricingDescription
					Configured model.PricingValues `json:"configured"`
				}
			}
			modelManagementRequest(t, PreviewModelPricing, http.MethodPost, "/api/option/model_pricing/preview", map[string]any{
				"model_name": tc.name, "reset": true, "pricing": model.PricingValues{"ModelRatio": -1.0},
			}, &response)
			require.True(t, response.Success)
			assert.Equal(t, tc.configured, response.Data.Configured)
			assert.Equal(t, model.ResolveCacheWriteMode(tc.name, tc.configured), response.Data.CacheWriteMode)
			assert.Equal(t, model.ResolveLegacyBillingDetails(tc.name, response.Data.Effective, tc.configured), response.Data.BillingDetails)
			switch tc.name {
			case "gpt-4o":
				assert.Equal(t, 1.25, response.Data.Effective["ModelRatio"])
				assert.Equal(t, 4.0, response.Data.Effective["CompletionRatio"])
				assert.Equal(t, 0.5, response.Data.Effective["CacheRatio"])
			case "glm-4-flash":
				assert.Equal(t, 0.0, response.Data.Effective["ModelRatio"])
			case "dall-e-3":
				assert.Equal(t, 0.04, response.Data.Effective["ModelPrice"])
				assert.True(t, response.Data.BillingDetails.ImageCount)
			case "gpt-6-astra":
				expression, exists := billing_setting.GetBuiltinBillingExpr(tc.name)
				require.True(t, exists)
				assert.Equal(t, "tiered_expr", response.Data.Effective["billing_setting.billing_mode"])
				assert.Equal(t, expression, response.Data.Effective["billing_setting.billing_expr"])
			case "no-default-price":
				assert.NotContains(t, response.Data.Effective, "ModelRatio")
				assert.NotContains(t, response.Data.Effective, "ModelPrice")
				assert.NotContains(t, response.Data.Effective, "billing_setting.billing_expr")
			}
			expression, exists := billing_setting.GetBillingExpr(tc.name)
			require.True(t, exists)
			assert.Equal(t, savedExpression, expression, "preview must retain the running price")
		})
	}

	var draftResponse struct {
		Success bool
		Data    struct {
			Configured model.PricingValues `json:"configured"`
		}
	}
	draft := model.PricingValues{"ModelRatio": 4.0, "CompletionRatio": 3.5, "CacheRatio": 0.25}
	modelManagementRequest(t, PreviewModelPricing, http.MethodPost, "/api/option/model_pricing/preview", map[string]any{"model_name": "gpt-4o", "pricing": draft}, &draftResponse)
	require.True(t, draftResponse.Success)
	assert.Equal(t, draft, draftResponse.Data.Configured)
	after, err := model.GetModelPricingSnapshot(names)
	require.NoError(t, err)
	assert.Equal(t, before, after, "default and draft previews must not persist prices")
	optionsAfter, err := model.AllOption()
	require.NoError(t, err)
	assert.ElementsMatch(t, optionsBefore, optionsAfter)
}
