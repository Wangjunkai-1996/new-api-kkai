package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImagePricingSettlementUsesFrozenInputsAndActualCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalQuotaPerUnit := common.QuotaPerUnit
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })
	common.QuotaPerUnit = 999999

	priceData := types.PriceData{
		ModelPrice: 99,
		UsePrice:   true,
		GroupRatioInfo: types.GroupRatioInfo{
			GroupRatio: 99,
		},
	}
	priceData.AddOtherRatio("n", 2)
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-image-2",
		StartTime:       time.Now(),
		PriceData:       priceData,
		ChannelMeta:     &relaycommon.ChannelMeta{},
		ImagePricingSnapshot: &imagepricing.Snapshot{
			PolicyVersion:  "policy-v1",
			PolicyHash:     "policy-hash",
			Model:          "gpt-image-2",
			Size:           "3840x2160",
			Tier:           "4k",
			UnitPrice:      1.34,
			QuotaPerUnit:   100,
			GroupRatio:     1.5,
			RequestedCount: 2,
		},
	}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	usage := &dto.Usage{PromptTokens: 1, TotalTokens: 1}

	summary := calculateTextQuotaSummary(context, info, usage)
	assert.Equal(t, 402, summary.Quota)
	assert.Equal(t, 1.34, summary.ModelPrice)
	assert.Equal(t, 1.5, summary.GroupRatio)

	info.PriceData.AddOtherRatio("n", 1)
	summary = calculateTextQuotaSummary(context, info, usage)
	assert.Equal(t, 201, summary.Quota)

	snapshot := GenerateTextOtherInfo(context, info, 0, summary.GroupRatio, 0, 0, 0, summary.ModelPrice, -1).Snapshot()
	adminInfo, ok := snapshot["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, info.ImagePricingSnapshot, adminInfo["image_pricing"])
}

func TestImagePricingSettlementMatchesRoundedQuoteWithFractionalGroupRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	priceData := types.PriceData{ModelPrice: 0.67, UsePrice: true, QuotaToPreConsume: 335001}
	priceData.AddOtherRatio("n", 1)
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-image-2",
		StartTime:       time.Now(),
		PriceData:       priceData,
		ChannelMeta:     &relaycommon.ChannelMeta{},
		ImagePricingSnapshot: &imagepricing.Snapshot{
			PolicyVersion: "policy-v1", PolicyHash: "policy-hash", Model: "gpt-image-2",
			Size: "1024x1024", Tier: "1k", UnitPrice: 0.67, QuotaPerUnit: 500000,
			GroupRatio: 1.000002, RequestedCount: 1,
		},
	}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())

	summary := calculateTextQuotaSummary(context, info, &dto.Usage{PromptTokens: 1, TotalTokens: 1})

	assert.Equal(t, 335001, summary.Quota)
	assert.Equal(t, info.PriceData.QuotaToPreConsume, summary.Quota)
}

func TestImagePricingRetryCannotChangeConfirmedQuantity(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		PriceData: types.PriceData{QuotaToPreConsume: 200},
		ImagePricingSnapshot: &imagepricing.Snapshot{
			PolicyVersion: "v1", PolicyHash: "quote", Model: "image-test",
			Size: "1024x1024", Tier: "1k", UnitPrice: 1,
			QuotaPerUnit: 100, GroupRatio: 1, RequestedCount: 2,
		},
	}

	err := PrepareImageBillingForRequest(ctx, info, 3)

	require.NotNil(t, err)
	assert.Equal(t, relaytypes.ErrorCodeQuoteStale, err.GetErrorCode())
	assert.Equal(t, 200, info.PriceData.QuotaToPreConsume)
	assert.Nil(t, info.Billing)
}
