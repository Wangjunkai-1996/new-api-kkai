package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newLogInfoTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	return ctx
}

func newLogInfoTestRelayInfo(start time.Time) relaycommon.RelayInfo {
	return relaycommon.RelayInfo{
		StartTime: start,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.4",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
	}
}

func TestGenerateTextOtherInfoUsesUpstreamHeaderForDisplayedFRT(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	relayInfo := newLogInfoTestRelayInfo(start)
	relayInfo.UpstreamHeaderTime = start.Add(1500 * time.Millisecond)
	relayInfo.FirstResponseTime = start.Add(25 * time.Second)

	snapshot := GenerateTextOtherInfo(newLogInfoTestContext(), &relayInfo, 1, 1, 1, 0, 0, -1, -1).Snapshot()

	require.Equal(t, float64(1500), snapshot["frt"])
	require.Equal(t, float64(25000), snapshot["first_sse_ms"])
	require.Equal(t, float64(1500), snapshot["upstream_header_ms"])
	assert.NotContains(t, snapshot, "sub2_ttft_ms")
}

func TestGenerateTextOtherInfoUsesSub2TTFTSidebandForDisplayedFRT(t *testing.T) {
	for _, timingMs := range []int64{0, 1180} {
		t.Run(time.Duration(timingMs*int64(time.Millisecond)).String(), func(t *testing.T) {
			start := time.Unix(1_700_000_000, 0)
			relayInfo := newLogInfoTestRelayInfo(start)
			relayInfo.UpstreamHeaderTime = start.Add(44 * time.Second)
			relayInfo.FirstResponseTime = start.Add(45 * time.Second)
			relayInfo.SetSub2TTFTMs(timingMs)

			snapshot := GenerateTextOtherInfo(newLogInfoTestContext(), &relayInfo, 1, 1, 1, 0, 0, -1, -1).Snapshot()

			assert.Equal(t, float64(timingMs), snapshot["frt"])
			assert.Equal(t, float64(timingMs), snapshot["sub2_ttft_ms"])
			assert.Equal(t, float64(44000), snapshot["upstream_header_ms"])
			assert.Equal(t, float64(45000), snapshot["first_sse_ms"])
		})
	}
}

func TestGenerateTextOtherInfoIgnoresInvalidSub2TTFTAndUsesHeader(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	relayInfo := newLogInfoTestRelayInfo(start)
	relayInfo.UpstreamHeaderTime = start.Add(1500 * time.Millisecond)
	relayInfo.SetSub2TTFTMs(-1)

	snapshot := GenerateTextOtherInfo(newLogInfoTestContext(), &relayInfo, 1, 1, 1, 0, 0, -1, -1).Snapshot()

	assert.Equal(t, float64(1500), snapshot["frt"])
	assert.NotContains(t, snapshot, "sub2_ttft_ms")
}

func TestGenerateTextOtherInfoOmitsFRTWhenHeaderTimeMissing(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	relayInfo := newLogInfoTestRelayInfo(start)
	relayInfo.FirstResponseTime = start.Add(8 * time.Second)

	snapshot := GenerateTextOtherInfo(newLogInfoTestContext(), &relayInfo, 1, 1, 1, 0, 0, -1, -1).Snapshot()

	require.NotContains(t, snapshot, "frt")
	require.Equal(t, float64(8000), snapshot["first_sse_ms"])
	require.NotContains(t, snapshot, "upstream_header_ms")
}

func TestGenerateTextOtherInfoIgnoresInvalidHeaderTime(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	relayInfo := newLogInfoTestRelayInfo(start)
	relayInfo.UpstreamHeaderTime = start.Add(-time.Second)
	relayInfo.FirstResponseTime = start.Add(4 * time.Second)

	snapshot := GenerateTextOtherInfo(newLogInfoTestContext(), &relayInfo, 1, 1, 1, 0, 0, -1, -1).Snapshot()

	require.NotContains(t, snapshot, "frt")
	require.Equal(t, float64(4000), snapshot["first_sse_ms"])
	require.NotContains(t, snapshot, "upstream_header_ms")
}

func TestMembershipConsumeLogShowsFrozenEffectiveSavingsOnlyWhenMembershipPays(t *testing.T) {
	for _, tc := range []struct {
		name      string
		source    string
		ratio     float64
		assessed  int
		charged   int
		savings   float64
		candidate float64
	}{
		{name: "discounted member", source: model.MembershipSource, ratio: 0.18, assessed: 180, charged: 180, savings: 0.375, candidate: 0.18},
		{name: "cheaper wallet price retained", source: model.MembershipSource, ratio: 0.1, assessed: 100, charged: 100, savings: 0, candidate: 0.18},
		{name: "no extra discount", source: model.MembershipSource, ratio: 0.288, assessed: 288, charged: 288, savings: 0, candidate: 0.288},
		{name: "platform absorbs overrun", source: model.MembershipSource, ratio: 0.18, assessed: 280, charged: 180, savings: 0.375, candidate: 0.18},
		{name: "wallet fallback", source: model.MembershipWalletSource, ratio: 0.288, assessed: 288, charged: 288},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := relaycommon.RelayInfo{
				BillingSource: tc.source, MembershipBillingRequestID: "member-request",
				MembershipPricing: &types.MembershipPricing{
					AccountID: 12, TierID: "gold", TierName: "Gold", TierStyle: "gold", BaseRatio: 0.3, Discount: 0.6, WalletRatio: 0.288,
					Price: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: tc.ratio}},
				},
			}
			info.MembershipAssessedQuota = tc.assessed
			info.MembershipChargedQuota = tc.charged
			if tc.name == "cheaper wallet price retained" {
				info.MembershipPricing.WalletRatio = 0.1
			}
			if tc.name == "no extra discount" {
				info.MembershipPricing.Discount = 1
			}
			other := model.NewLogOther()
			attachQuotaSaturation(newLogInfoTestContext(), &info, other)
			snapshot := other.Snapshot()
			require.Equal(t, tc.source, snapshot["billing_source"])
			if tc.source != model.MembershipSource {
				assert.Equal(t, int64(12), snapshot["membership_account_id"])
				assert.Equal(t, true, snapshot["membership_wallet_fallback"])
				assert.NotContains(t, snapshot, "membership_discount")
				assert.NotContains(t, snapshot, "membership_savings")
				return
			}
			assert.Equal(t, "gold", snapshot["membership_tier_id"])
			assert.Equal(t, "Gold", snapshot["membership_tier_name"])
			assert.Equal(t, "gold", snapshot["membership_tier_style"])
			assert.Equal(t, info.MembershipPricing.Discount, snapshot["membership_discount"])
			assert.Equal(t, info.MembershipPricing.WalletRatio, snapshot["membership_wallet_ratio"])
			assert.InDelta(t, tc.candidate, snapshot["membership_candidate_ratio"], 1e-9)
			assert.InDelta(t, tc.savings, snapshot["membership_savings"], 1e-9)
			if tc.name == "discounted member" {
				assert.Equal(t, 288, snapshot["membership_estimated_wallet_quota"])
				assert.Equal(t, int64(108), snapshot["membership_estimated_saved_quota"])
			} else {
				assert.NotContains(t, snapshot, "membership_estimated_saved_quota")
			}
		})
	}
	other := model.NewLogOther()
	appendMembershipPricingLogInfo(other, model.MembershipWalletSource, &types.MembershipPricing{}, 100, 100)
	assert.NotContains(t, other.Snapshot(), "membership_wallet_fallback", "ordinary wallet calls are not membership fallbacks")
}

func TestGenerateTextOtherInfoOmitsInvalidResponseTimings(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	relayInfo := newLogInfoTestRelayInfo(start)
	relayInfo.FirstResponseTime = start.Add(-time.Second)

	snapshot := GenerateTextOtherInfo(newLogInfoTestContext(), &relayInfo, 1, 1, 1, 0, 0, -1, -1).Snapshot()

	require.NotContains(t, snapshot, "frt")
	require.NotContains(t, snapshot, "first_sse_ms")
}
