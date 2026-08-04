package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
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

	other := GenerateTextOtherInfo(context, info, 0, summary.GroupRatio, 0, 0, 0, summary.ModelPrice, -1)
	adminInfo, ok := other["admin_info"].(map[string]interface{})
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
