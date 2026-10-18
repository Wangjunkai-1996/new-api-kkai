package service

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipFailedTerminalPreservesExplicitZeroUsage(t *testing.T) {
	for _, fee := range []bool{false, true} {
		name := "zero_charge"
		if fee {
			name = "zero_tokens_with_fee"
		}
		t.Run(name, func(t *testing.T) {
			c, user := setupMembershipServiceTest(t)
			previousLogs := common.LogConsumeEnabled
			common.LogConsumeEnabled = false
			t.Cleanup(func() { common.LogConsumeEnabled = previousLogs })
			info := &relaycommon.RelayInfo{UserId: user.Id, RequestId: name, ChannelMeta: &relaycommon.ChannelMeta{}, StartTime: time.Now(), UsingGroup: "gpt", IsPlayground: true, OriginModelName: "test-model", IsStream: true, StreamStatus: relaycommon.NewStreamStatus(),
				PriceData:         types.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.2}},
				MembershipPricing: &types.MembershipPricing{QuotaPerUnit: common.QuotaPerUnit, ViolationFeeBeforeGroup: 80}}
			input, err := PrepareMembershipReservation(info, 800)
			require.NoError(t, err)
			record, err := model.ReserveMembershipBilling(input)
			require.NoError(t, err)
			require.NoError(t, ApplyMembershipReservation(info, record))
			info.Billing = &membershipBillingSession{info: info, requestID: record.RequestID, reserved: int(record.ReservedQuota), state: record.State}
			accumulator := NewResponsesUsageAccumulator(info)
			accumulator.Observe(&dto.ResponsesStreamResponse{Type: "response.failed", Response: &dto.OpenAIResponsesResponse{Usage: &dto.Usage{}}})
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
			code := relaytypes.ErrorCodeBadResponse
			if fee {
				code = relaytypes.ErrorCodeViolationFeeGrokCSAM
			}
			apiErr := relaytypes.NewError(errors.New("provider terminal failure"), code)
			SettleMembershipPartialUsage(c, info, accumulator.Finish(), apiErr)
			assert.False(t, info.MembershipOutcomeUnknown)
			require.True(t, FinalizeMembershipPartialUsage(c, info, apiErr))
			stored, err := model.GetMembershipBillingRecord(info.RequestId)
			require.NoError(t, err)
			assert.Equal(t, "settled", stored.State)
			want := int64(0)
			if fee {
				want = 8
			}
			assert.Equal(t, want, stored.ChargedQuota)
			self, err := model.GetMembershipSelf(user.Id)
			require.NoError(t, err)
			assert.Zero(t, self.Account.AmountReserved)
			require.NoError(t, model.DB.First(&user, user.Id).Error)
			assert.Equal(t, want, user.UsedQuota)
		})
	}
	info := &relaycommon.RelayInfo{MembershipBillingRequestID: "synthetic-zero"}
	SettleMembershipPartialUsage(nil, info, &dto.Usage{}, relaytypes.NewError(errors.New("missing usage"), relaytypes.ErrorCodeBadResponse))
	assert.True(t, info.MembershipOutcomeUnknown, "an unreported zero must still retain authorization")
}
