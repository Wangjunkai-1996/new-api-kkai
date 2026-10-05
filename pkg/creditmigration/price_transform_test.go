package creditmigration

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPriceMigrationPreservesTierBoundariesAndRelativePrices(t *testing.T) {
	source := `v1:len <= 200000 ? tier("standard", p*21+c*105+cr*2.1) : tier("long", p*42+c*210+cr*4.2)`
	target, err := DivideMonetaryExpression(source)
	require.NoError(t, err)
	for _, length := range []float64{199999, 200000, 200001} {
		params := billingexpr.TokenParams{P: 1000, C: 500, CR: 400, Len: length}
		before, oldTrace, err := billingexpr.RunExpr(source, params)
		require.NoError(t, err)
		after, newTrace, err := billingexpr.RunExpr(target, params)
		require.NoError(t, err)
		assert.InDelta(t, before/7, after, 1e-9)
		assert.Equal(t, oldTrace.MatchedTier, newTrace.MatchedTier)
	}
	_, err = DivideMonetaryExpression(`tier("request", fixed(0.7))`)
	require.ErrorContains(t, err, "explicit monetary literal adapter")
	old := `{"opus":2.5}`
	assert.ErrorContains(t, validateMonetaryTransforms(map[string]OptionChange{"ModelRatio": {Key: "ModelRatio", Before: &old, After: old}}), "divide the current price by 7")
	old = `{"opus":5}`
	assert.ErrorContains(t, validateMonetaryTransforms(map[string]OptionChange{"CompletionRatio": {Key: "CompletionRatio", Before: &old, After: `{"opus":2.5}`}}), "must remain unchanged")
}

func TestImagePolicyMigrationScalesCurrentPricesAndPreservesSizes(t *testing.T) {
	source := `{"version":"legacy","enabled":true,"models":{"image":{"default_size":"1024x1024","tiers":{"standard":{"unit_price":0.66,"sizes":["1024x1024"]}}}}}`
	target, err := DivideImagePricingPolicy(source, "usd-v1")
	require.NoError(t, err)
	var config imagepricing.Config
	require.NoError(t, common.UnmarshalJsonStr(target, &config))
	assert.Equal(t, 0.66/7, config.Models["image"].Tiers["standard"].UnitPrice)
	assert.Equal(t, []string{"1024x1024"}, config.Models["image"].Tiers["standard"].Sizes)
	require.NoError(t, validateMonetaryTransforms(map[string]OptionChange{"ImagePricingPolicy": {Key: "ImagePricingPolicy", Before: &source, After: target}}))
	config.Enabled = false
	changed, err := common.Marshal(config)
	require.NoError(t, err)
	assert.ErrorContains(t, validateMonetaryTransforms(map[string]OptionChange{"ImagePricingPolicy": {Key: "ImagePricingPolicy", Before: &source, After: string(changed)}}), "only change its version and monetary unit prices")
}
