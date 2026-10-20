package service

import (
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelPricingConversionPreservesTextSettlement(t *testing.T) {
	previousDB, previousQuotaPerUnit := model.DB, common.QuotaPerUnit
	previousMaster, previousSQLite := common.IsMasterNode, common.SQLitePath
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	common.IsMasterNode, common.SQLitePath = false, filepath.Join(t.TempDir(), "pricing.db")
	t.Setenv("SQL_DSN", "local")
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitDB())
	db := model.DB
	t.Cleanup(func() {
		model.DB, common.QuotaPerUnit = previousDB, previousQuotaPerUnit
		common.IsMasterNode, common.SQLitePath = previousMaster, previousSQLite
		common.SetDatabaseTypes(previousMain, previousLog)
		connection, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.Ability{}, &model.Channel{}, &model.Model{}))
	common.QuotaPerUnit = 500_000

	for _, tc := range []struct {
		name       string
		modelName  string
		writeRatio *float64
		cacheRatio float64
		usage      dto.Usage
		wantQuota  int
	}{
		{
			name: "ordinary input and output", modelName: "gpt-5.5", cacheRatio: 0.1,
			usage: dto.Usage{PromptTokens: 1000, CompletionTokens: 100}, wantQuota: 4000,
		},
		{
			name: "default GPT cache write price", modelName: "gpt-5.5", cacheRatio: 0.1,
			usage: dto.Usage{PromptTokens: 1000, PromptTokensDetails: dto.InputTokenDetails{CacheWriteTokens: 1000}}, wantQuota: 3125,
		},
		{
			name: "explicit free cache writes", modelName: "gpt-5.5", writeRatio: common.GetPointer(0.0), cacheRatio: 0.1,
			usage: dto.Usage{PromptTokens: 1000, CompletionTokens: 100, PromptTokensDetails: dto.InputTokenDetails{CacheWriteTokens: 500}}, wantQuota: 2750,
		},
		{
			name: "explicit equal-price cache writes overlap cache reads", modelName: "gpt-5.5", writeRatio: common.GetPointer(1.0), cacheRatio: 1,
			usage: dto.Usage{PromptTokens: 1000, CompletionTokens: 100, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 800, CacheWriteTokens: 500}}, wantQuota: 4750,
		},
		{
			name: "default cache writes overlap discounted reads", modelName: "gpt-5.5", cacheRatio: 0.1,
			usage: dto.Usage{PromptTokens: 3619, CompletionTokens: 36, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 2921, CacheWriteTokens: 3616}}, wantQuota: 12570,
		},
		{
			name: "default cache writes overlap equal-price reads", modelName: "gpt-5.5", cacheRatio: 1,
			usage: dto.Usage{PromptTokens: 1000, CompletionTokens: 100, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 800, CacheWriteTokens: 500}}, wantQuota: 5063,
		},
		{
			name: "default cache writes overlap equal-price image input", modelName: "gpt-5.5", cacheRatio: 1,
			usage: dto.Usage{PromptTokens: 1000, PromptTokensDetails: dto.InputTokenDetails{CacheWriteTokens: 1000, ImageTokens: 1000}}, wantQuota: 5625,
		},
		{
			name: "Claude cache write TTLs", modelName: "claude-pricing-parity", cacheRatio: 0.1,
			usage: dto.Usage{PromptTokens: 1000, CompletionTokens: 100, UsageSemantic: "anthropic", PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 200, CachedCreationTokens: 500}, ClaudeCacheCreation5mTokens: 300, ClaudeCacheCreation1hTokens: 200}, wantQuota: 5988,
		},
		{
			name: "Claude aggregate-only cache writes keep legacy settlement", modelName: "claude-pricing-parity", cacheRatio: 0.1,
			usage: dto.Usage{PromptTokens: 1000, CompletionTokens: 100, UsageSemantic: "anthropic", PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 200, CachedCreationTokens: 500}}, wantQuota: 5613,
		},
		{
			name: "Claude explicit free TTLs", modelName: "claude-pricing-parity", writeRatio: common.GetPointer(0.0), cacheRatio: 0.1,
			usage: dto.Usage{PromptTokens: 1000, CompletionTokens: 100, UsageSemantic: "anthropic", PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 200, CachedCreationTokens: 500}, ClaudeCacheCreation5mTokens: 300, ClaudeCacheCreation1hTokens: 200}, wantQuota: 4050,
		},
		{
			name: "non-Claude billing name preserves native Anthropic TTLs", modelName: "gpt-5.5", cacheRatio: 0.1,
			usage: dto.Usage{PromptTokens: 1000, CompletionTokens: 100, UsageSemantic: "anthropic", PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 200, CachedCreationTokens: 500}, ClaudeCacheCreation5mTokens: 300, ClaudeCacheCreation1hTokens: 200}, wantQuota: 5988,
		},
		{
			name: "Claude native image input exceeds text input", modelName: "claude-pricing-parity", cacheRatio: 0.1,
			usage: dto.Usage{PromptTokens: 1000, CompletionTokens: 100, UsageSemantic: "anthropic", PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 200, CachedCreationTokens: 500, ImageTokens: 1200}, ClaudeCacheCreation5mTokens: 300, ClaudeCacheCreation1hTokens: 200}, wantQuota: 6488,
		},
		{
			name: "non-Claude billing name preserves native Anthropic image clamp", modelName: "gpt-5.5", cacheRatio: 0.1,
			usage: dto.Usage{PromptTokens: 1000, CompletionTokens: 100, UsageSemantic: "anthropic", PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 200, CachedCreationTokens: 500, ImageTokens: 1200}, ClaudeCacheCreation5mTokens: 300, ClaudeCacheCreation1hTokens: 200}, wantQuota: 6488,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			draft := model.PricingValues{"ModelRatio": 2.5, "CompletionRatio": 6.0, "CacheRatio": tc.cacheRatio}
			if tc.writeRatio != nil {
				draft["CreateCacheRatio"] = *tc.writeRatio
			}
			converted, err := model.PreviewModelPricingConversion(tc.modelName, draft)
			require.NoError(t, err)
			if tc.modelName == "claude-pricing-parity" {
				require.Equal(t, "This model has provider-specific billing and must be converted manually.", converted.UnsupportedReason)
				require.Empty(t, converted.Expression)
			} else {
				require.Empty(t, converted.UnsupportedReason)
				require.NotEmpty(t, converted.Expression)
			}
			writeRatio := converted.Effective["CreateCacheRatio"].(float64)
			groupRatios := []float64{1, 0.5, 1.5, 2}
			if converted.UnsupportedReason != "" {
				groupRatios = []float64{1}
			}
			for _, groupRatio := range groupRatios {
				t.Run(fmt.Sprintf("group_%g", groupRatio), func(t *testing.T) {
					ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
					info := &relaycommon.RelayInfo{
						OriginModelName: tc.modelName,
						StartTime:       time.Now(),
						PriceData: hosttypes.PriceData{
							ModelRatio: 2.5, CompletionRatio: 6, CacheRatio: tc.cacheRatio, ImageRatio: 1,
							CacheCreationRatio: writeRatio, CacheCreation5mRatio: writeRatio, CacheCreation1hRatio: writeRatio * (6 / 3.75),
							GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: groupRatio},
						},
					}
					legacy := calculateTextQuotaSummary(ctx, info, &tc.usage)
					if groupRatio == 1 {
						assert.Equal(t, tc.wantQuota, legacy.Quota, "legacy fixture must protect the actual charge")
					}
					if converted.UnsupportedReason != "" {
						return
					}
					params := BuildTieredTokenParams(&tc.usage, legacy.IsClaudeUsageSemantic, billingexpr.UsedVars(converted.Expression))
					result, err := billingexpr.ComputeTieredQuota(&billingexpr.BillingSnapshot{
						ExprString: converted.Expression, ExprHash: billingexpr.ExprHashString(converted.Expression),
						QuotaPerUnit: common.QuotaPerUnit, GroupRatio: groupRatio,
					}, params)
					require.NoError(t, err)
					assert.Equal(t, legacy.Quota, result.ActualQuotaAfterGroup, "expression=%s; tokens=%+v", converted.Expression, params)
				})
			}
		})
	}
}
