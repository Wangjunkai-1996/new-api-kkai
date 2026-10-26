package service

import (
	"context"
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

func TestMembershipFailedUsageAndFeeSettleOneOriginalAuthorization(t *testing.T) {
	c, user := setupMembershipServiceTest(t)
	previousLogs := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = previousLogs })
	info := &relaycommon.RelayInfo{UserId: user.Id, RequestId: "partial-with-fee", ChannelMeta: &relaycommon.ChannelMeta{}, StartTime: time.Now(), UsingGroup: "gpt", IsPlayground: true, OriginModelName: "test-model",
		PriceData:         types.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.2}},
		MembershipPricing: &types.MembershipPricing{QuotaPerUnit: common.QuotaPerUnit, ViolationFeeBeforeGroup: 80}}
	input, err := PrepareMembershipReservation(info, 800)
	require.NoError(t, err)
	record, err := model.ReserveMembershipBilling(input)
	require.NoError(t, err)
	require.NoError(t, ApplyMembershipReservation(info, record))
	info.Billing = &membershipBillingSession{info: info, requestID: record.RequestID, reserved: int(record.ReservedQuota), state: record.State}
	apiErr := relaytypes.NewError(errors.New("provider rejected the response"), relaytypes.ErrorCodeViolationFeeGrokCSAM)
	CaptureFailedRequestUsage(c, info, &dto.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}, apiErr)
	before, err := model.GetMembershipBillingRecord(info.RequestId)
	require.NoError(t, err)
	assert.Equal(t, "reserved", before.State, "wait for the final policy decision before charging a fee")
	require.True(t, FinalizeFailedRequestUsage(c, info, apiErr))
	assert.False(t, FinalizeFailedRequestUsage(c, info, apiErr))
	stored, err := model.GetMembershipBillingRecord(info.RequestId)
	require.NoError(t, err)
	assert.Equal(t, "settled", stored.State)
	assert.Equal(t, model.MembershipSource, stored.Source)
	assert.EqualValues(t, 20, stored.ActualQuota, "12 usage + 8 frozen fee")
	assert.EqualValues(t, 20, stored.ChargedQuota)
	self, err := model.GetMembershipSelf(user.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 20, self.Account.AmountUsed)
	assert.Zero(t, self.Account.AmountReserved)
	var updated model.User
	require.NoError(t, model.DB.First(&updated, user.Id).Error)
	assert.EqualValues(t, 10000, updated.Quota, "the violation fee cannot debit another source")
	assert.Equal(t, 1, updated.RequestCount)
	assert.Error(t, info.Billing.Settle(21), "a conflicting repeated settlement is not silently accepted")
}

func TestMembershipAcceptedResponseWithoutUsageRemainsUncertain(t *testing.T) {
	info := &relaycommon.RelayInfo{MembershipBillingRequestID: "accepted-request"}
	apiErr := relaytypes.NewError(errors.New("malformed upstream response"), relaytypes.ErrorCodeBadResponse)
	CaptureFailedRequestUsage(nil, info, nil, apiErr)
	assert.True(t, info.BillingOutcomeUnknown)
	assert.True(t, relaytypes.IsSkipRetryError(apiErr))
}

func TestMembershipTruncatedStreamCannotSettleEstimatedPartialUsage(t *testing.T) {
	c, user := setupMembershipServiceTest(t)
	info := &relaycommon.RelayInfo{UserId: user.Id, RequestId: "truncated-stream", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 3}, UsingGroup: "gpt", IsPlayground: true, IsStream: true, StreamStatus: relaycommon.NewStreamStatus(), PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.2}}}
	input, err := PrepareMembershipReservation(info, 100)
	require.NoError(t, err)
	record, err := model.ReserveMembershipBilling(input)
	require.NoError(t, err)
	require.NoError(t, ApplyMembershipReservation(info, record))
	info.Billing = &membershipBillingSession{info: info, requestID: record.RequestID, reserved: int(record.ReservedQuota), state: record.State}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, context.DeadlineExceeded)
	apiErr := relaytypes.NewError(context.DeadlineExceeded, relaytypes.ErrorCodeBadResponse)
	CaptureFailedRequestUsage(c, info, &dto.Usage{PromptTokens: 100, CompletionTokens: 5, TotalTokens: 105}, apiErr)
	assert.True(t, info.BillingOutcomeUnknown)
	assert.Nil(t, info.BillingFailureUsage)
	require.Error(t, SettleBilling(c, info, 1))
	stored, err := model.GetMembershipBillingRecord(info.RequestId)
	require.NoError(t, err)
	assert.Equal(t, "reserved", stored.State)
	assert.Equal(t, "upstream_final_usage_unknown", stored.ReviewReason)
	self, err := model.GetMembershipSelf(user.Id)
	require.NoError(t, err)
	assert.Zero(t, self.Account.AmountUsed)
	assert.Equal(t, record.ReservedQuota, self.Account.AmountReserved)
}

func TestMembershipResponsesAccountingDistinguishesReportedZeroFromEstimates(t *testing.T) {
	for _, reported := range []bool{false, true} {
		info := &relaycommon.RelayInfo{MembershipBillingRequestID: "response", StreamStatus: relaycommon.NewStreamStatus()}
		info.SetEstimatePromptTokens(100)
		accumulator := NewResponsesUsageAccumulator(info)
		response := &dto.OpenAIResponsesResponse{}
		if reported {
			response.Usage = &dto.Usage{}
		}
		accumulator.Observe(&dto.ResponsesStreamResponse{Type: "response.completed", Response: response})
		usage := accumulator.Finish()
		assert.Equal(t, reported, accumulator.HasReportedUsage())
		assert.Equal(t, !reported, info.BillingOutcomeUnknown)
		if reported {
			assert.Zero(t, usage.TotalTokens, "an explicit zero bill is not replaced by a local prompt estimate")
		}
	}
}

func TestMembershipSpeechUsesInputEstimateWithoutClientMaxTokens(t *testing.T) {
	c, user := setupMembershipServiceTest(t)
	catalog, err := model.GetMembershipCatalog()
	require.NoError(t, err)
	delete(catalog.ModelLimits, "test-model")
	catalog, err = model.PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{UserId: user.Id, OriginModelName: "test-model", Request: &dto.AudioRequest{}}
	price := types.PriceData{ModelRatio: 1, AudioRatio: 2, AudioCompletionRatio: 4}
	require.NoError(t, PrepareMembershipBudget(c, info, &price, 2, &relaytypes.TokenCountMeta{}))
	require.NotNil(t, info.MembershipPricing)
	assert.False(t, info.MembershipPricing.RequiresOutputBound)
	assert.Equal(t, 6.0, info.MembershipPricing.MaximumBeforeGroup-info.MembershipPricing.ViolationFeeBeforeGroup)
}

func TestMembershipDispatchPersistsAndEnforcesTheSameDeadline(t *testing.T) {
	_, user := setupMembershipServiceTest(t)
	info := &relaycommon.RelayInfo{UserId: user.Id, RequestId: "dispatch-lifecycle", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 3}, UsingGroup: "gpt", IsPlayground: true, OriginModelName: "test-model", PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.2}}}
	input, err := PrepareMembershipReservation(info, 100)
	require.NoError(t, err)
	record, err := model.ReserveMembershipBilling(input)
	require.NoError(t, err)
	require.NoError(t, ApplyMembershipReservation(info, record))
	deadline := time.Now().Add(time.Minute)
	parent, cancelParent := context.WithDeadline(context.Background(), deadline)
	defer cancelParent()
	bounded, cancel, err := PrepareMembershipDispatch(parent, info)
	require.NoError(t, err)
	defer cancel()
	actualDeadline, ok := bounded.Deadline()
	require.True(t, ok)
	assert.Equal(t, deadline, actualDeadline)
	stored, err := model.GetMembershipBillingRecord(info.RequestId)
	require.NoError(t, err)
	assert.Positive(t, stored.DispatchedAt)
	assert.Equal(t, deadline.Unix()+60, stored.ReviewAfter)
	require.NoError(t, model.ClearMembershipBillingRejectedAttempt(info.RequestId, 3))
	second, cancelSecond, err := PrepareMembershipDispatch(context.Background(), info)
	require.NoError(t, err)
	defer cancelSecond()
	secondDeadline, ok := second.Deadline()
	require.True(t, ok)
	assert.Equal(t, deadline, secondDeadline, "a safe retry must not extend the original authorization deadline")
}
