package service

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMembershipAudioSettlementKeepsFundingAndLogsConsistent(t *testing.T) {
	for _, transport := range []string{"audio", "realtime"} {
		for _, scenario := range []struct {
			name                       string
			zero, missing, fail, fixed bool
			fee, want                  int
		}{
			{name: "frozen mixed audio price", want: 25},
			{name: "frozen fixed price and conversion unit", fixed: true, want: 50},
			{name: "confirmed zero usage", zero: true},
			{name: "zero usage retains assessed fee", zero: true, fee: 7, want: 7},
			{name: "missing usage retains authorization", missing: true},
			{name: "failed zero settlement emits no consumption", zero: true, fail: true},
		} {
			t.Run(transport+"/"+scenario.name, func(t *testing.T) {
				ctx, user := setupMembershipServiceTest(t)
				require.NoError(t, model.DB.AutoMigrate(&model.Log{}))
				oldLogDB, oldLogs, oldExport := model.LOG_DB, common.LogConsumeEnabled, common.DataExportEnabled
				model.LOG_DB, common.LogConsumeEnabled, common.DataExportEnabled = model.DB, true, false
				t.Cleanup(func() {
					model.LOG_DB, common.LogConsumeEnabled, common.DataExportEnabled = oldLogDB, oldLogs, oldExport
				})
				info := &relaycommon.RelayInfo{UserId: user.Id, RequestId: "audio-settlement", IsPlayground: true, UsingGroup: "gpt", OriginModelName: "test-model", ChannelMeta: &relaycommon.ChannelMeta{}, PriceData: types.PriceData{ModelRatio: 2, CompletionRatio: 3, AudioRatio: 4, AudioCompletionRatio: 5, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.2}}, MembershipPricing: &types.MembershipPricing{MaximumBeforeGroup: 500, QuotaPerUnit: 500000}}
				if scenario.fixed {
					info.PriceData.UsePrice, info.PriceData.ModelPrice = true, 0.001
				}
				require.Nil(t, PreConsumeBilling(ctx, 0, info))
				previousUnit := common.QuotaPerUnit
				common.QuotaPerUnit = 1000000
				t.Cleanup(func() { common.QuotaPerUnit = previousUnit })
				info.MembershipViolationFee = scenario.fee
				info.SetEstimatePromptTokens(100)
				// Neither a later mutable quote nor live conversion units may replace
				// the price that authorized this upstream response.
				info.PriceData = types.PriceData{UsePrice: true, ModelPrice: 99, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 10}}
				if scenario.fail {
					require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("audio_settlement_failure", func(tx *gorm.DB) {
						if tx.Statement.Table == (model.MembershipBillingRecord{}).TableName() {
							tx.AddError(errors.New("injected durable settlement failure"))
						}
					}))
					t.Cleanup(func() { require.NoError(t, model.DB.Callback().Update().Remove("audio_settlement_failure")) })
				}
				usage := &dto.Usage{PromptTokens: 5, CompletionTokens: 9, TotalTokens: 14, PromptTokensDetails: dto.InputTokenDetails{TextTokens: 2, AudioTokens: 3}, CompletionTokenDetails: dto.OutputTokenDetails{TextTokens: 4, AudioTokens: 5}}
				if scenario.zero {
					usage = &dto.Usage{}
				}
				if scenario.missing {
					usage = nil
				}
				if transport == "audio" {
					PostAudioConsumeQuota(ctx, info, usage, "")
				} else {
					var realtimeUsage *dto.RealtimeUsage
					if usage != nil {
						realtimeUsage = &dto.RealtimeUsage{InputTokens: usage.PromptTokens, OutputTokens: usage.CompletionTokens, TotalTokens: usage.TotalTokens, InputTokenDetails: usage.PromptTokensDetails, OutputTokenDetails: usage.CompletionTokenDetails}
					}
					PostWssConsumeQuota(ctx, info, "test-model", realtimeUsage, "")
				}
				record, err := model.GetMembershipBillingRecord(info.RequestId)
				require.NoError(t, err)
				var logs []model.Log
				require.NoError(t, model.LOG_DB.Where("user_id = ?", user.Id).Find(&logs).Error)
				var updated model.User
				require.NoError(t, model.DB.First(&updated, user.Id).Error)
				if scenario.missing || scenario.fail {
					assert.Equal(t, "reserved", record.State)
					assert.Zero(t, record.ChargedQuota)
					assert.Zero(t, updated.RequestCount)
					assert.Zero(t, updated.UsedQuota)
					assert.Empty(t, logs)
					return
				}
				assert.Equal(t, "settled", record.State)
				assert.EqualValues(t, scenario.want, record.ChargedQuota)
				assert.EqualValues(t, scenario.want, updated.UsedQuota)
				if scenario.want > 0 {
					assert.Equal(t, 1, updated.RequestCount)
				} else {
					assert.Zero(t, updated.RequestCount)
				}
				require.Len(t, logs, 1)
				assert.Equal(t, scenario.want, logs[0].Quota)
				assert.Contains(t, logs[0].Content, "分组倍率 0.10")
			})
		}
	}
}

func TestMembershipRealtimeExpressionDoesNotDoubleCountAudioAndCache(t *testing.T) {
	ctx, user := setupMembershipServiceTest(t)
	oldLog := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = oldLog })
	expression := `tier("mixed",p+cr*0.5+ai*2+c*3+ao*4)`
	info := &relaycommon.RelayInfo{UserId: user.Id, RequestId: "audio-expression", IsPlayground: true, UsingGroup: "gpt", OriginModelName: "test-model", ChannelMeta: &relaycommon.ChannelMeta{}, PriceData: types.PriceData{ModelRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.2}}, MembershipPricing: &types.MembershipPricing{MaximumBeforeGroup: 1000, QuotaPerUnit: 500000}, TieredBillingSnapshot: &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ExprString: expression, ExprHash: billingexpr.ExprHashString(expression), QuotaPerUnit: 500000}}
	require.Nil(t, PreConsumeBilling(ctx, 0, info))
	PostWssConsumeQuota(ctx, info, "test-model", &dto.RealtimeUsage{InputTokens: 100, OutputTokens: 30, TotalTokens: 130, InputTokenDetails: dto.InputTokenDetails{TextTokens: 80, AudioTokens: 20, CachedTokens: 10}, OutputTokenDetails: dto.OutputTokenDetails{TextTokens: 25, AudioTokens: 5}}, "")
	record, err := model.GetMembershipBillingRecord(info.RequestId)
	require.NoError(t, err)
	assert.Equal(t, "settled", record.State)
	assert.EqualValues(t, 11, record.ChargedQuota, "(70 + 10*0.5 + 20*2 + 25*3 + 5*4) * 0.5 * 0.1 rounds to 11")
}
