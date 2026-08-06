package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func imagePricingSnapshot(unitPrice float64) *imagepricing.Snapshot {
	return &imagepricing.Snapshot{
		PolicyVersion:  "policy-v1",
		PolicyHash:     "policy-hash",
		Model:          "gpt-image-2",
		Size:           "1024x1024",
		Tier:           "test-tier",
		UnitPrice:      unitPrice,
		QuotaPerUnit:   100,
		GroupRatio:     1,
		RequestedCount: 2,
	}
}

func imagePricingHelperContext(group string) *gin.Context {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("group", group)
	return context
}

func TestModelPriceHelperUsesAbsoluteImageTierPrices(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name      string
		unitPrice float64
		wantQuota int
	}{
		{name: "1k", unitPrice: 0.67, wantQuota: 134},
		{name: "2k", unitPrice: 1, wantQuota: 200},
		{name: "4k", unitPrice: 1.34, wantQuota: 268},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				OriginModelName:      "gpt-image-2",
				UserGroup:            "default",
				UsingGroup:           "default",
				ImagePricingSnapshot: imagePricingSnapshot(test.unitPrice),
			}
			meta := &types.TokenCountMeta{
				ImagePriceRatio: 99,
				BillingRatios:   map[string]float64{"n": 99, "unexpected": 99},
			}

			price, err := ModelPriceHelper(imagePricingHelperContext("default"), info, 1, meta)

			require.NoError(t, err)
			assert.Equal(t, test.wantQuota, price.QuotaToPreConsume)
			assert.Equal(t, test.unitPrice, price.ModelPrice)
			assert.Equal(t, map[string]float64{"n": 2}, price.OtherRatios())
		})
	}
}

func TestModelPriceHelperFreezesImageInputsAcrossRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalQuotaPerUnit := common.QuotaPerUnit
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})

	common.QuotaPerUnit = 100
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	snapshot := imagePricingSnapshot(1)
	snapshot.QuotaPerUnit = 0
	info := &relaycommon.RelayInfo{
		OriginModelName:      "gpt-image-2",
		UserGroup:            "default",
		UsingGroup:           "default",
		ImagePricingSnapshot: snapshot,
	}

	first, err := ModelPriceHelper(imagePricingHelperContext("default"), info, 1, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.NotNil(t, info.ImagePricingSnapshot)
	assert.Equal(t, float64(100), info.ImagePricingSnapshot.QuotaPerUnit)
	assert.Equal(t, float64(1), info.ImagePricingSnapshot.GroupRatio)
	assert.Equal(t, 200, first.QuotaToPreConsume)

	common.QuotaPerUnit = 999
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":7}`))
	second, err := ModelPriceHelper(imagePricingHelperContext("default"), info, 1, &types.TokenCountMeta{})
	require.NoError(t, err)
	assert.Equal(t, 200, second.QuotaToPreConsume)
	assert.Equal(t, float64(100), info.ImagePricingSnapshot.QuotaPerUnit)
	assert.Equal(t, float64(1), second.GroupRatioInfo.GroupRatio)
}

func TestModelPriceHelperRoundsImageQuotaExactlyLikeSettlement(t *testing.T) {
	snapshot := imagePricingSnapshot(0.67)
	snapshot.QuotaPerUnit = 500000
	snapshot.GroupRatio = 1.000002
	snapshot.RequestedCount = 1
	info := &relaycommon.RelayInfo{
		OriginModelName:      "gpt-image-2",
		UserGroup:            "default",
		UsingGroup:           "default",
		ImagePricingSnapshot: snapshot,
	}

	price, err := ModelPriceHelper(imagePricingHelperContext("default"), info, 1, &types.TokenCountMeta{})

	require.NoError(t, err)
	assert.Equal(t, 335001, price.QuotaToPreConsume)
}
