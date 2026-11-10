package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestBudgetCoversLongTTLCacheSettlement(t *testing.T) {
	openai := dto.Usage{PromptTokens: 100, TotalTokens: 100, PromptTokensDetails: dto.InputTokenDetails{
		CacheWriteTokens: 100, CacheWriteTokensDetails: &dto.CacheWriteTokenDetails{TTL12hTokens: 100},
	}}
	claude := dto.Usage{UsageSemantic: "anthropic", ClaudeCacheCreation1hTokens: 100,
		PromptTokensDetails: dto.InputTokenDetails{CachedCreationTokens: 100}}
	for _, tc := range []struct {
		name, expr string
		usage      dto.Usage
		want       float64
	}{
		{"explicit 12h price", `tier("cache", cc12h * 25)`, openai, 1250},
		{"12h folded into cache price", `tier("cache", cc * 12.5)`, openai, 1250},
		{"12h folded into input price", `tier("input", p * 10)`, openai, 1000},
		{"explicit free long writes keep input bound", `tier("input", p * 10 + cc1h * 0 + cc12h * 0)`, openai, 500},
		{"one-hour folded into cache price", `tier("cache", cc * 3.75 + cc12h * 0)`, claude, 300},
		{"one-hour folded into input price", `tier("input", p * 3 + cc12h * 0)`, claude, 240},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				OriginModelName: "cache-budget-model", Request: &dto.OpenAIResponsesRequest{},
				TieredBillingSnapshot: makeSnapshot(tc.expr, 1, 100, 1),
			}
			price := types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}
			budget, err := prepareRequestBudget(info, &price, 100, &relaytypes.TokenCountMeta{MaxTokens: 1})
			require.NoError(t, err)
			assert.Equal(t, tc.want, budget.MaximumBeforeGroup)

			params := BuildTieredTokenParams(&tc.usage, tc.usage.UsageSemantic == "anthropic", billingexpr.UsedVars(tc.expr))
			result, err := billingexpr.ComputeTieredQuotaWithRequest(info.TieredBillingSnapshot, params, billingexpr.RequestInput{})
			require.NoError(t, err)
			assert.GreaterOrEqual(t, budget.MaximumBeforeGroup, float64(result.ActualQuotaAfterGroup), "the frozen authorization must cover the final long-lived cache charge")
		})
	}
}

func TestRequestBudgetBounds12hRatioWithoutAddingAnotherWriteCategory(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	for _, tc := range []struct {
		name    string
		price   types.PriceData
		maximum float64
	}{
		{"derived 12h ratio already covered", types.PriceData{CacheCreationRatio: 1.25, CacheCreation5mRatio: 1.25, CacheCreation1hRatio: 2, CacheCreation12hRatio: 2.5}, 550},
		{"larger 12h ratio raises bound", types.PriceData{CacheCreation12hRatio: 40}, 4100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			price := tc.price
			price.ModelRatio, price.GroupRatioInfo.GroupRatio = 1, 1
			info := &relaycommon.RelayInfo{OriginModelName: "cache-ratio-budget-model", Request: &dto.OpenAIResponsesRequest{}, PriceData: price}
			budget, err := prepareRequestBudget(info, &price, 100, &relaytypes.TokenCountMeta{MaxTokens: 1})
			require.NoError(t, err)
			assert.Equal(t, tc.maximum, budget.MaximumBeforeGroup)
			usage := &dto.Usage{PromptTokens: 100, TotalTokens: 100, PromptTokensDetails: dto.InputTokenDetails{
				CacheWriteTokens: 100, CacheWriteTokensDetails: &dto.CacheWriteTokenDetails{TTL12hTokens: 100},
			}}
			settled := calculateTextQuotaSummary(ctx, info, usage)
			assert.GreaterOrEqual(t, budget.MaximumBeforeGroup, float64(settled.Quota))
		})
	}
}
