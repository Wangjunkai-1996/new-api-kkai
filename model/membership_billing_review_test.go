package model

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipBillingReviewSettlesOriginalSourceExactlyOnce(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	_, account := buyMembershipTest(t, user.Id, catalog, "gold", "manual-review")
	require.NoError(t, DB.AutoMigrate(&Channel{}))
	channel := Channel{Name: "review-channel", Key: "test", Type: 1, Status: 1}
	require.NoError(t, DB.Create(&channel).Error)
	input := membershipReservation(t, user.Id, "unknown-call", 80, 90)
	reserved, err := ReserveMembershipBilling(input)
	require.NoError(t, err)
	resolution := MembershipBillingResolutionInput{RequestID: input.RequestID, ActorID: 1, OperationID: "verified-usage", Action: "settle", ActualQuota: 100, Reason: "Provider receipt confirms 100 quota under the stored pricing snapshot"}
	_, err = ResolveMembershipBillingReview(resolution)
	assert.ErrorIs(t, err, ErrMembershipNotFound)
	require.NoError(t, MarkMembershipBillingUncertain(input.RequestID, channel.Id, "upstream_outcome_unknown"))
	require.NoError(t, MarkMembershipBillingUncertain(input.RequestID, channel.Id, "upstream_outcome_unknown"))
	reviews, err := ListMembershipBillingReviews(false, 50)
	require.NoError(t, err)
	require.Len(t, reviews, 1)
	assert.Equal(t, "reserved", reviews[0].State)
	assert.EqualValues(t, 80, reviews[0].ReservedQuota)
	record, err := ResolveMembershipBillingReview(resolution)
	require.NoError(t, err)
	assert.Equal(t, reserved.Snapshot, record.Snapshot)
	assert.Equal(t, account.ID, record.AccountID)
	assert.EqualValues(t, 80, record.ChargedQuota)
	assert.EqualValues(t, 20, record.ExcessQuota)
	assert.NotZero(t, record.ReviewResolvedAt)
	assert.Contains(t, record.ReviewResolution, "verified-usage")
	replay, err := ResolveMembershipBillingReview(resolution)
	require.NoError(t, err)
	assert.False(t, replay.Applied)
	resolution.ActualQuota = 90
	_, err = ResolveMembershipBillingReview(resolution)
	assert.ErrorIs(t, err, ErrMembershipConflict)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.EqualValues(t, 80, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	assert.EqualValues(t, 500, user.Quota)
	require.NoError(t, DB.First(&channel, channel.Id).Error)
	assert.EqualValues(t, 80, channel.UsedQuota)
	require.NoError(t, DB.First(account, account.ID).Error)
	assert.EqualValues(t, 80, account.AmountUsed)
	assert.Zero(t, account.AmountReserved)
	reviews, err = ListMembershipBillingReviews(false, 50)
	require.NoError(t, err)
	assert.Empty(t, reviews)
	reviews, err = ListMembershipBillingReviews(true, 50)
	require.NoError(t, err)
	require.Len(t, reviews, 1)
}

func TestMembershipBillingReviewReleasesWalletWithoutChargingNewMembership(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	buyMembershipTest(t, user.Id, catalog, "gold", "review-wallet")
	input := membershipReservation(t, user.Id, "unknown-wallet", 110, 120)
	record, err := ReserveMembershipBilling(input)
	require.NoError(t, err)
	assert.Equal(t, MembershipWalletSource, record.Source)
	require.NoError(t, MarkMembershipBillingUncertain(input.RequestID, 0, "dispatch_failed_without_confirmed_usage"))
	buyMembershipTest(t, user.Id, catalog, "diamond", "review-upgrade")
	resolution := MembershipBillingResolutionInput{RequestID: input.RequestID, ActorID: 1, OperationID: "verified-no-charge", Action: "release", Reason: "Provider confirms this request was rejected before processing"}
	resolved, err := ResolveMembershipBillingReview(resolution)
	require.NoError(t, err)
	assert.Equal(t, "released", resolved.State)
	_, err = ResolveMembershipBillingReview(resolution)
	require.NoError(t, err)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.EqualValues(t, 500, user.Quota)
	assert.Zero(t, user.UsedQuota)
	assert.Zero(t, user.RequestCount)
	self, err := GetMembershipSelf(user.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 300, self.Account.Available())
}

func TestMembershipModelLimitsRequireDocumentedFiniteBounds(t *testing.T) {
	_, catalog := setupMembershipTest(t)
	catalog.ModelLimits = map[string]MembershipModelLimit{"gpt-test": {MaxInputTokens: 200000, MaxOutputTokens: 32000, MaxToolCalls: 4, Evidence: "Verified upstream per-request cumulative token contract"}}
	updated, err := PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
	require.NoError(t, err)
	restored, err := GetMembershipCatalog()
	require.NoError(t, err)
	assert.Equal(t, updated.ModelLimits, restored.ModelLimits)
	updated.ModelLimits["gpt-test"] = MembershipModelLimit{MaxInputTokens: 200000}
	_, err = PublishMembershipCatalog(*updated, updated.ReleaseID, 1)
	assert.ErrorIs(t, err, ErrMembershipInvalidInput)
}

func TestMembershipBillingReviewRecoversOrphanOnlyAfterDispatchDeadline(t *testing.T) {
	user, _ := setupMembershipTest(t)
	input := membershipReservation(t, user.Id, "orphan-dispatch", 80, 90)
	_, err := ReserveMembershipBilling(input)
	require.NoError(t, err)
	deadline := common.GetTimestamp() + 600
	require.NoError(t, MarkMembershipBillingDispatched(input.RequestID, 11, deadline))
	require.NoError(t, MarkMembershipBillingDispatched(input.RequestID, 11, deadline))
	assert.ErrorIs(t, MarkMembershipBillingDispatched(input.RequestID, 12, deadline), ErrMembershipConflict)
	assert.ErrorIs(t, MarkMembershipBillingDispatched(input.RequestID, 11, deadline+1), ErrMembershipConflict)
	record, err := GetMembershipBillingRecord(input.RequestID)
	require.NoError(t, err)
	assert.EqualValues(t, deadline+60, record.ReviewAfter)
	resolution := MembershipBillingResolutionInput{RequestID: input.RequestID, ActorID: 1, OperationID: "verified-orphan", Action: "release", Reason: "Provider confirms no processing"}
	_, err = ResolveMembershipBillingReview(resolution)
	assert.ErrorIs(t, err, ErrMembershipNotFound)
	reviews, err := ListMembershipBillingReviews(false, 50)
	require.NoError(t, err)
	assert.Empty(t, reviews)
	// No uncertainty callback occurred: the process died after durable dispatch.
	require.NoError(t, DB.Model(record).Update("review_after", common.GetTimestamp()-1).Error)
	reviews, err = ListMembershipBillingReviews(false, 50)
	require.NoError(t, err)
	require.Len(t, reviews, 1)
	assert.Equal(t, "dispatch_deadline_elapsed_without_settlement", reviews[0].ReviewReason)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.EqualValues(t, 410, user.Quota, "listing an orphan must not release money")
	resolved, err := ResolveMembershipBillingReview(resolution)
	require.NoError(t, err)
	assert.Equal(t, "released", resolved.State)
	assert.NotZero(t, resolved.ReviewStartedAt)
	assert.NotZero(t, resolved.ReviewResolvedAt)
	_, err = ResolveMembershipBillingReview(resolution)
	require.NoError(t, err)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.EqualValues(t, 500, user.Quota)
}

func TestMembershipBillingReviewRejectsActiveAndOwnedImageTaskLedgers(t *testing.T) {
	user, _ := setupMembershipTest(t)
	for _, scope := range []string{MembershipBillingScopeImage, MembershipBillingScopeTask} {
		input := membershipReservation(t, user.Id, scope+":owned", 40, 50)
		input.Scope = scope
		record, err := ReserveMembershipBilling(input)
		require.NoError(t, err)
		assert.ErrorIs(t, MarkMembershipBillingDispatched(input.RequestID, 1, common.GetTimestamp()+60), ErrMembershipConflict)
		assert.ErrorIs(t, MarkMembershipBillingUncertain(input.RequestID, 1, "unknown"), ErrMembershipConflict)
		_, err = SettleMembershipRelayBilling(input.RequestID, 30, 0, false)
		assert.ErrorIs(t, err, ErrMembershipConflict)
		require.NoError(t, DB.Model(record).Update("created_at", common.GetTimestamp()-7200).Error)
		_, err = GetMembershipBillingReview(input.RequestID)
		assert.ErrorIs(t, err, ErrMembershipNotFound)
		current, err := GetMembershipBillingRecord(input.RequestID)
		require.NoError(t, err)
		assert.Equal(t, "reserved", current.State)
	}
	reviews, err := ListMembershipBillingReviews(false, 50)
	require.NoError(t, err)
	assert.Empty(t, reviews)
}

func TestMembershipBillingReviewRecoversUndispatchedAndGuardsChannelRetry(t *testing.T) {
	user, _ := setupMembershipTest(t)
	input := membershipReservation(t, user.Id, "retry-dispatch", 40, 50)
	record, err := ReserveMembershipBilling(input)
	require.NoError(t, err)
	deadline := common.GetTimestamp() + 600
	require.NoError(t, MarkMembershipBillingDispatched(input.RequestID, 1, deadline))
	assert.ErrorIs(t, ClearMembershipBillingRejectedAttempt(input.RequestID, 2), ErrMembershipConflict)
	require.NoError(t, ClearMembershipBillingRejectedAttempt(input.RequestID, 1))
	require.NoError(t, MarkMembershipBillingDispatched(input.RequestID, 2, deadline))
	require.NoError(t, ClearMembershipBillingRejectedAttempt(input.RequestID, 2))
	require.NoError(t, DB.Model(record).Update("created_at", common.GetTimestamp()-7200).Error)
	assert.ErrorIs(t, MarkMembershipBillingDispatched(input.RequestID, 3, deadline), ErrMembershipConflict)
	review, err := GetMembershipBillingReview(input.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "authorization_stalled_before_dispatch", review.ReviewReason)
	_, err = ResolveMembershipBillingReview(MembershipBillingResolutionInput{RequestID: input.RequestID, ActorID: 1, OperationID: "undispatched-orphan", Action: "release", Reason: "Dispatch journal confirms no accepted attempt"})
	require.NoError(t, err)
	assert.ErrorIs(t, MarkMembershipBillingDispatched(input.RequestID, 3, deadline), ErrMembershipConflict)
}

func TestMembershipTaskUsageLimitsValidateCumulativeFacts(t *testing.T) {
	_, catalog := setupMembershipTest(t)
	catalog.ModelLimits = map[string]MembershipModelLimit{"task-model": {TaskUsageLimits: map[string]float64{"duration_seconds": 600, "audio_tokens": 0}, Evidence: "Provider guarantees cumulative task usage maxima"}}
	require.NoError(t, ValidateMembershipCatalog(catalog))
	for _, invalid := range []float64{-1, math.Inf(1), math.NaN(), math.MaxInt32 + 1} {
		catalog.ModelLimits["task-model"].TaskUsageLimits["duration_seconds"] = invalid
		assert.ErrorIs(t, ValidateMembershipCatalog(catalog), ErrMembershipInvalidInput)
	}
}
