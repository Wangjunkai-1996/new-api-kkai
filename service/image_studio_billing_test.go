package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestImageStudioMaximumPreconsumeIncludesCompletionAndAllRatios(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	common.SetContextKey(c, constant.ContextKeyIsImageStudio, true)
	relayInfo := &relaycommon.RelayInfo{}
	price := types.PriceData{
		ModelRatio: 3, CompletionRatio: 2,
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.5},
	}
	price.AddOtherRatio("n", 2)

	require.NoError(t, ApplyImageStudioMaximumPreconsume(
		c, relayInfo, &price, 100, &types.TokenCountMeta{MaxTokens: 1_000},
	))
	require.Equal(t, 7_500, price.QuotaToPreConsume)
	require.Equal(t, price.QuotaToPreConsume, relayInfo.PriceData.QuotaToPreConsume)
}

func TestImageStudioMaximumPreconsumeUsesRequestCountWithoutChangingFinalRatioBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	common.SetContextKey(c, constant.ContextKeyIsImageStudio, true)
	relayInfo := &relaycommon.RelayInfo{}
	price := types.PriceData{
		ModelRatio: 1, CompletionRatio: 1,
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}

	require.NoError(t, ApplyImageStudioMaximumPreconsume(
		c, relayInfo, &price, 100, &types.TokenCountMeta{
			MaxTokens: 100, BillingRatios: map[string]float64{"n": 4},
		},
	))
	require.Equal(t, (common.PreConsumedQuota+100)*4, price.QuotaToPreConsume)
	require.Nil(t, price.OtherRatios())
	require.Nil(t, relayInfo.PriceData.OtherRatios())
}

func TestImageStudioMaximumPreconsumePreservesCompletePriceQuotes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	common.SetContextKey(c, constant.ContextKeyIsImageStudio, true)

	relayInfo := &relaycommon.RelayInfo{}
	price := types.PriceData{UsePrice: true, QuotaToPreConsume: 321}
	require.NoError(t, ApplyImageStudioMaximumPreconsume(
		c, relayInfo, &price, 100, &types.TokenCountMeta{MaxTokens: 1_000},
	))
	require.Equal(t, 321, price.QuotaToPreConsume)
	require.Equal(t, 321, relayInfo.PriceData.QuotaToPreConsume)
}

func TestImageStudioMaximumPreconsumeRejectsUnboundedTieredExpression(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	common.SetContextKey(c, constant.ContextKeyIsImageStudio, true)
	relayInfo := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{BillingMode: "tiered_expr"},
	}
	price := types.PriceData{QuotaToPreConsume: 654}
	require.ErrorIs(t, ApplyImageStudioMaximumPreconsume(
		c, relayInfo, &price, 100, &types.TokenCountMeta{MaxTokens: 1_000},
	), ErrImageModelBillingUnsupported)
}

func TestImagePricingActualCountKeepsSnapshotImmutable(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ImagePricingSnapshot: &imagepricing.Snapshot{RequestedCount: 2},
	}
	info.PriceData.AddOtherRatio("n", 1)

	actual, err := imagePricingActualCount(info)

	require.NoError(t, err)
	require.Equal(t, 1, actual)
	require.Equal(t, 2, info.ImagePricingSnapshot.RequestedCount)
}

func TestImagePricingActualCountRejectsFractionalValues(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ImagePricingSnapshot: &imagepricing.Snapshot{RequestedCount: 2},
	}
	info.PriceData.AddOtherRatio("n", 1.5)

	_, err := imagePricingActualCount(info)

	require.ErrorIs(t, err, ErrImageStudioQuoteStale)
}
