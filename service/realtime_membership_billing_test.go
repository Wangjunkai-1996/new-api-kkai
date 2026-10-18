package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipRealtimeBudgetBoundsMixedAudioAndRequiresProviderEvidence(t *testing.T) {
	oldFee := model_setting.GetGrokSettings().ViolationDeductionEnabled
	model_setting.GetGrokSettings().ViolationDeductionEnabled = false
	t.Cleanup(func() { model_setting.GetGrokSettings().ViolationDeductionEnabled = oldFee })
	info := &relaycommon.RelayInfo{PriceData: types.PriceData{ModelRatio: 2, CompletionRatio: 3, AudioRatio: 10, AudioCompletionRatio: 4}}
	limits := model.MembershipModelLimit{MaxInputTokens: 100, MaxOutputTokens: 10, Evidence: "provider total response limit"}
	require.NoError(t, PrepareRealtimeMembershipBudget(info, limits, 10))
	assert.Equal(t, 2800.0, info.MembershipPricing.MaximumBeforeGroup)
	info.PriceData.AddOtherRatio("unsupported_dimension", 2)
	require.ErrorContains(t, PrepareRealtimeMembershipBudget(info, limits, 10), "additional request multipliers")
	info.PriceData.ReplaceOtherRatios(nil)
	limits.Evidence = ""
	require.Error(t, PrepareRealtimeMembershipBudget(info, limits, 10))
}

func TestMembershipRealtimeBudgetRejectsUnrepresentedRequestPredicates(t *testing.T) {
	for _, expression := range []string{
		`tier("base",p)*(param("service_tier")=="fast"?2:1)`,
		`tier("base",p)*(header("x-fast")=="yes"?2:1)`,
		`tier("base",p)*(hour("UTC")>12?2:1)`,
	} {
		info := &relaycommon.RelayInfo{TieredBillingSnapshot: &billingexpr.BillingSnapshot{ExprString: expression, ExprHash: billingexpr.ExprHashString(expression), QuotaPerUnit: 500000}}
		limits := model.MembershipModelLimit{MaxInputTokens: 100, MaxOutputTokens: 10, Evidence: "provider total response limit"}
		require.ErrorContains(t, PrepareRealtimeMembershipBudget(info, limits, 10), "token-only expression")
	}
}

func TestMembershipRealtimeFixedPriceAndZeroUsageSettleFrozenAuthorization(t *testing.T) {
	for _, zeroUsage := range []bool{false, true} {
		t.Run(map[bool]string{false: "fixed price", true: "confirmed zero usage"}[zeroUsage], func(t *testing.T) {
			ctx, user := setupMembershipServiceTest(t)
			previousLog := common.LogConsumeEnabled
			common.LogConsumeEnabled = false
			t.Cleanup(func() { common.LogConsumeEnabled = previousLog })
			info := &relaycommon.RelayInfo{UserId: user.Id, RequestId: "realtime-fixed", IsPlayground: true, UsingGroup: "gpt", OriginModelName: "test-model", ChannelMeta: &relaycommon.ChannelMeta{}, PriceData: types.PriceData{UsePrice: true, ModelPrice: 0.001, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.2}}, MembershipPricing: &types.MembershipPricing{MaximumBeforeGroup: 500, QuotaPerUnit: 500000}}
			require.Nil(t, PreConsumeBilling(ctx, 0, info))
			usage := &dto.RealtimeUsage{}
			if !zeroUsage {
				usage.TotalTokens, usage.InputTokens, usage.InputTokenDetails.TextTokens = 1, 1, 1
			}
			PostWssConsumeQuota(ctx, info, "test-model", usage, "")
			record, err := model.GetMembershipBillingRecord(info.RequestId)
			require.NoError(t, err)
			assert.Equal(t, "settled", record.State)
			if zeroUsage {
				assert.Zero(t, record.ChargedQuota)
			} else {
				assert.EqualValues(t, 50, record.ChargedQuota)
			}
		})
	}
}
