package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/image_pricing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelPricingConversionPreservesResolutionPricing(t *testing.T) {
	modelManagementDB(t, "sqlite", "")
	previousPolicy := image_pricing_setting.JSON()
	previousGroups := ratio_setting.GroupRatio2JSONString()
	previousQuotaPerUnit := common.QuotaPerUnit
	t.Cleanup(func() {
		require.NoError(t, image_pricing_setting.UpdateByJSONString(previousPolicy))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroups))
		common.QuotaPerUnit = previousQuotaPerUnit
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	common.QuotaPerUnit = 500000
	policy := image_pricing_setting.DefaultConfig()
	policy.Enabled = true
	config := policy.Models["gpt-image-2"]
	for tier, price := range map[string]float64{"1k": 0.02, "2k": 0.02, "4k": 0.04} {
		value := config.Tiers[tier]
		value.UnitPrice = price
		config.Tiers[tier] = value
	}
	policy.Models["gpt-image-2"] = config
	raw, err := common.Marshal(policy)
	require.NoError(t, err)
	require.NoError(t, image_pricing_setting.UpdateByJSONString(string(raw)))
	policyHash := image_pricing_setting.PolicyHash()
	snapshot, err := model.GetModelPricingSnapshot([]string{"gpt-image-2"})
	require.NoError(t, err)
	draft := model.PricingValues{"ModelPrice": 0.2}
	require.NoError(t, model.UpdateModelPricing([]model.ModelPricingChange{{ModelName: "gpt-image-2", ExpectedVersion: snapshot.Entries[0].Version, Pricing: draft}}))
	snapshot, err = model.GetModelPricingSnapshot([]string{"gpt-image-2"})
	require.NoError(t, err)
	assert.Equal(t, &config, snapshot.Entries[0].ImagePricingPolicy)
	assert.Equal(t, 0.2, snapshot.Entries[0].Configured["ModelPrice"])

	var response struct {
		Success bool
		Data    model.ModelPricingConversion
	}
	modelManagementRequest(t, PreviewModelPricingConversion, http.MethodPost, "/api/option/model_pricing/convert", map[string]any{"model_name": "gpt-image-2", "pricing": draft}, &response)
	require.True(t, response.Success)
	assert.Empty(t, response.Data.Expression)
	assert.Contains(t, response.Data.UnsupportedReason, "Resolution-based image pricing takes precedence")
	for _, tc := range []struct {
		size  string
		price float64
		quota int
	}{
		{"1024x1024", 0.02, 10000},
		{"1792x1024", 0.02, 10000},
		{"3840x2160", 0.04, 20000},
	} {
		t.Run(tc.size, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			request := &dto.ImageRequest{Model: "gpt-image-2", Size: tc.size}
			info := &relaycommon.RelayInfo{OriginModelName: "gpt-image-2", UsingGroup: "default", UserGroup: "default", Request: request}
			require.NoError(t, service.PrepareImagePricing(ctx, info))
			price, err := helper.ModelPriceHelper(ctx, info, 0, request.GetTokenCountMeta())
			require.NoError(t, err)
			assert.Equal(t, tc.price, price.ModelPrice, "resolution price must override the stale fixed model price")
			assert.Equal(t, tc.quota, price.QuotaToPreConsume)
			assert.Nil(t, info.TieredBillingSnapshot)
		})
	}
	assert.Equal(t, policyHash, image_pricing_setting.PolicyHash())
	allowCustom, configured := image_pricing_setting.AllowsCustomSize("gpt-image-2")
	assert.True(t, configured)
	assert.True(t, allowCustom)

	policy.Enabled = false
	raw, err = common.Marshal(policy)
	require.NoError(t, err)
	require.NoError(t, image_pricing_setting.UpdateByJSONString(string(raw)))
	snapshot, err = model.GetModelPricingSnapshot([]string{"gpt-image-2"})
	require.NoError(t, err)
	assert.Nil(t, snapshot.Entries[0].ImagePricingPolicy)
	preview, err := model.PreviewModelPricingConversion("gpt-image-2", draft)
	require.NoError(t, err)
	assert.Empty(t, preview.UnsupportedReason)
	assert.Equal(t, `tier("image", fixed(0.2)) * image_count`, preview.Expression)
}

func TestPublicImageResolutionPricingOverridesLegacyAndSurvivesCleanup(t *testing.T) {
	const modelName = "resolution-only-image"
	db := modelManagementDB(t, "sqlite", "")
	withSelfUseModeDisabled(t)
	previousPolicy := image_pricing_setting.JSON()
	t.Cleanup(func() {
		require.NoError(t, image_pricing_setting.UpdateByJSONString(previousPolicy))
		model.InvalidatePricingCache()
	})
	policy := image_pricing_setting.DefaultConfig()
	policy.Enabled = true
	config := policy.Models["gpt-image-2"]
	for tier, price := range map[string]float64{"1k": 0.02, "2k": 0.02, "4k": 0.04} {
		value := config.Tiers[tier]
		value.UnitPrice = price
		config.Tiers[tier] = value
	}
	policy.Models[modelName] = config
	raw, err := common.Marshal(policy)
	require.NoError(t, err)
	require.NoError(t, image_pricing_setting.UpdateByJSONString(string(raw)))
	require.NoError(t, (&model.Channel{Name: "Image policy route", Type: constant.ChannelTypeOpenAI, Models: modelName, Group: "default", Status: common.ChannelStatusEnabled}).Insert())
	require.NoError(t, db.Create(&model.User{Id: 1001, Username: "policy-catalog", Password: "password", Group: "default", Status: common.UserStatusEnabled}).Error)

	for _, tc := range []struct {
		name    string
		pricing model.PricingValues
	}{
		{"stale legacy and expression", model.PricingValues{
			"ModelPrice": 0.2, "ModelRatio": 2.0, "CompletionRatio": 4.0, "CacheRatio": 0.5,
			"CreateCacheRatio": 1.25, "ImageRatio": 1.0, "AudioRatio": 3.0, "AudioCompletionRatio": 5.0,
			"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `tier("old", p * 99 + c * 99)`,
		}},
		{"no legacy pricing", model.PricingValues{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, err := model.GetModelPricingSnapshot([]string{modelName})
			require.NoError(t, err)
			require.NoError(t, model.UpdateModelPricing([]model.ModelPricingChange{{ModelName: modelName, ExpectedVersion: before.Entries[0].Version, Pricing: tc.pricing}}))
			model.InvalidatePricingCache()
			var response struct {
				Success bool
				Data    []model.Pricing
			}
			modelManagementRequest(t, GetPricing, http.MethodGet, "/api/pricing", nil, &response)
			require.True(t, response.Success)
			require.Len(t, response.Data, 1)
			pricing := response.Data[0]
			assert.Equal(t, modelName, pricing.ModelName)
			assert.True(t, pricing.PricingConfigured)
			assert.Equal(t, 1, pricing.QuotaType)
			assert.Equal(t, 0.02, pricing.ModelPrice)
			assert.Equal(t, &config, pricing.ImagePricingPolicy)
			assert.Empty(t, pricing.BillingExpr)
			assert.Empty(t, pricing.BillingMode)
			assert.Nil(t, pricing.CacheRatio)
			assert.Nil(t, pricing.AudioRatio)
			assert.Zero(t, pricing.ModelRatio)
			assert.True(t, helper.HasModelBillingConfig(modelName))
			imageContext, _ := gin.CreateTestContext(httptest.NewRecorder())
			imageContext.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			imageRequest := &dto.ImageRequest{Model: modelName, Size: "1792x1024"}
			info := &relaycommon.RelayInfo{OriginModelName: modelName, UsingGroup: "default", UserGroup: "default", Request: imageRequest}
			require.NoError(t, service.PrepareImagePricing(imageContext, info))
			price, err := helper.ModelPriceHelper(imageContext, info, 0, imageRequest.GetTokenCountMeta())
			require.NoError(t, err)
			assert.True(t, price.UsePrice)
			assert.Equal(t, 0.02, price.ModelPrice)
			assert.Nil(t, info.TieredBillingSnapshot)

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			ctx.Set("id", 1001)
			ListModels(ctx, constant.ChannelTypeOpenAI)
			assert.Contains(t, decodeListModelsResponse(t, recorder), modelName)

			// Publishing a resolution tariff must not rewrite the underlying
			// chat-compatible expression or legacy settings.
			after, err := model.GetModelPricingSnapshot([]string{modelName})
			require.NoError(t, err)
			assert.Equal(t, tc.pricing, after.Entries[0].Configured)
		})
	}

	policy.Enabled = false
	raw, err = common.Marshal(policy)
	require.NoError(t, err)
	require.NoError(t, model.UpdateOption(image_pricing_setting.OptionKey, string(raw)))
	pricing := pricingByModelName(model.GetPricing())[modelName]
	assert.Nil(t, pricing.ImagePricingPolicy)
	assert.False(t, pricing.PricingConfigured)
	assert.False(t, helper.HasModelBillingConfig(modelName))
}
