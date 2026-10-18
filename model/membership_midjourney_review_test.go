package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipMidjourneyReviewPreservesOriginalSourceAfterRepurchase(t *testing.T) {
	for _, source := range []string{MembershipSource, MembershipWalletSource} {
		t.Run(source, func(t *testing.T) {
			user, catalog := setupMembershipTest(t)
			require.NoError(t, DB.AutoMigrate(&Midjourney{}, &Channel{}))
			_, original := buyMembershipTest(t, user.Id, catalog, "gold", "mj-review-original")
			input := membershipReservation(t, user.Id, "replaced-by-mj-id", 80, 90)
			if source == MembershipWalletSource {
				input.SubscriptionQuota = 110
			}
			task := &Midjourney{UserId: user.Id, ChannelId: 1, BillingChannelId: 1, Action: "IMAGINE", Status: "SUBMITTED"}
			record, err := CreateMidjourneyMembershipTask(task, input)
			require.NoError(t, err)
			require.Equal(t, source, record.Source)
			require.NoError(t, MarkMembershipBillingDispatched(record.RequestID, 1, common.GetTimestamp()+60))
			require.NoError(t, MarkMembershipBillingUncertain(record.RequestID, 1, "submission_outcome_unknown"))
			require.NoError(t, DB.Model(original).Update("expires_at", common.GetTimestamp()-1).Error)
			_, current := buyMembershipTest(t, user.Id, catalog, "gold", "mj-review-current")
			require.NotEqual(t, original.ID, current.ID)

			resolution := MembershipBillingResolutionInput{RequestID: record.RequestID, ActorID: 1, OperationID: "mj-confirmed-no-charge", Action: "release", Reason: "Provider receipt confirms no accepted or charged job"}
			resolved, err := ResolveMembershipBillingReview(resolution)
			require.NoError(t, err)
			assert.Equal(t, "released", resolved.State)
			assert.Equal(t, record.Source, resolved.Source)
			assert.Equal(t, record.AccountID, resolved.AccountID)
			assert.Equal(t, record.Snapshot, resolved.Snapshot)
			_, err = ResolveMembershipBillingReview(resolution)
			require.NoError(t, err)

			// A late success cannot reopen an administrator's final accounting.
			task.Status, task.Progress, task.MjId = "SUCCESS", "100%", "late-upstream-id"
			late, err := UpdateMidjourneyMembershipTask(task)
			require.NoError(t, err)
			assert.Equal(t, "released", late.State)
			assert.False(t, late.Applied)
			assert.NotEqual(t, "SUCCESS", task.Status)
			require.NoError(t, DB.First(original, original.ID).Error)
			assert.Equal(t, MembershipExpired, original.Status)
			assert.Zero(t, original.AmountReserved)
			require.NoError(t, DB.First(current, current.ID).Error)
			assert.EqualValues(t, 100, current.Available())
			require.NoError(t, DB.First(&user, user.Id).Error)
			assert.EqualValues(t, 500, user.Quota)
			assert.Zero(t, user.UsedQuota)
			assert.Zero(t, user.RequestCount)
		})
	}
}

func TestMembershipMidjourneyReviewCapsManualChargeAndIgnoresLateFailure(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	require.NoError(t, DB.AutoMigrate(&Midjourney{}, &Channel{}))
	channel := Channel{Name: "mj-review", Key: "test", Type: 1, Status: 1}
	require.NoError(t, DB.Create(&channel).Error)
	_, account := buyMembershipTest(t, user.Id, catalog, "gold", "mj-review-charge")
	task := &Midjourney{UserId: user.Id, ChannelId: channel.Id, BillingChannelId: channel.Id, Action: "IMAGINE", Status: "SUBMITTED"}
	record, err := CreateMidjourneyMembershipTask(task, membershipReservation(t, user.Id, "mj-review", 80, 90))
	require.NoError(t, err)
	require.NoError(t, MarkMembershipBillingDispatched(record.RequestID, channel.Id, common.GetTimestamp()+60))
	require.NoError(t, MarkMembershipBillingUncertain(record.RequestID, channel.Id, "upstream_connection_lost"))
	resolution := MembershipBillingResolutionInput{RequestID: record.RequestID, ActorID: 1, OperationID: "mj-confirmed-charge", Action: "settle", ActualQuota: 100, Reason: "Verified provider receipt assessed using the stored pricing snapshot"}
	resolved, err := ResolveMembershipBillingReview(resolution)
	require.NoError(t, err)
	assert.EqualValues(t, 80, resolved.ChargedQuota)
	assert.EqualValues(t, 20, resolved.ExcessQuota)
	assert.Equal(t, record.Snapshot, resolved.Snapshot)
	replay, err := ResolveMembershipBillingReview(resolution)
	require.NoError(t, err)
	assert.False(t, replay.Applied)
	changed := resolution
	changed.ActualQuota = 90
	_, err = ResolveMembershipBillingReview(changed)
	assert.ErrorIs(t, err, ErrMembershipConflict)
	task.Status, task.Progress, task.FailReason = "FAILURE", "100%", "late provider event"
	late, err := UpdateMidjourneyMembershipTask(task)
	require.NoError(t, err)
	assert.False(t, late.Applied)
	assert.Equal(t, "settled", late.State)
	assert.NotEqual(t, "FAILURE", task.Status)
	assert.Equal(t, 80, task.Quota)
	require.NoError(t, DB.First(account, account.ID).Error)
	assert.EqualValues(t, 80, account.AmountUsed)
	assert.Zero(t, account.AmountReserved)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.EqualValues(t, 80, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	assert.EqualValues(t, 500, user.Quota)
	require.NoError(t, DB.First(&channel, channel.Id).Error)
	assert.EqualValues(t, 80, channel.UsedQuota)
}

func TestMembershipMidjourneyReviewWaitsForAcceptedTaskInvestigationThreshold(t *testing.T) {
	user, _ := setupMembershipTest(t)
	require.NoError(t, DB.AutoMigrate(&Midjourney{}, &Channel{}))
	task := &Midjourney{UserId: user.Id, ChannelId: 1, BillingChannelId: 1, Action: "IMAGINE", Status: "SUBMITTED"}
	record, err := CreateMidjourneyMembershipTask(task, membershipReservation(t, user.Id, "mj-active", 40, 50))
	require.NoError(t, err)
	require.NoError(t, MarkMembershipBillingDispatched(record.RequestID, 1, common.GetTimestamp()+60))
	task.MjId, task.Status = "accepted-upstream-id", "IN_PROGRESS"
	_, err = UpdateMidjourneyMembershipTask(task)
	require.NoError(t, err)
	record, err = GetMembershipBillingRecord(record.RequestID)
	require.NoError(t, err)
	assert.Greater(t, record.ReviewAfter, common.GetTimestamp()+60)
	_, err = GetMembershipBillingReview(record.RequestID)
	assert.ErrorIs(t, err, ErrMembershipNotFound)
	resolution := MembershipBillingResolutionInput{RequestID: record.RequestID, ActorID: 1, OperationID: "active-mj-release", Action: "release", Reason: "Not yet eligible for investigation"}
	_, err = ResolveMembershipBillingReview(resolution)
	assert.ErrorIs(t, err, ErrMembershipNotFound)
	_, err = SettleMembershipRelayBilling(record.RequestID, 40, 1, true)
	assert.ErrorIs(t, err, ErrMembershipConflict, "the synchronous relay path cannot bypass MJ task state")
	assert.ErrorIs(t, ClearMembershipBillingRejectedAttempt(record.RequestID, 1), ErrMembershipConflict)

	require.NoError(t, DB.Model(record).Update("review_after", common.GetTimestamp()-1).Error)
	reviews, err := ListMembershipBillingReviews(false, 50)
	require.NoError(t, err)
	require.Len(t, reviews, 1)
	assert.Equal(t, record.RequestID, reviews[0].RequestID)
	assert.Equal(t, "reserved", reviews[0].State)
	assert.Equal(t, "midjourney_outcome_requires_review", reviews[0].ReviewReason)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.EqualValues(t, 450, user.Quota, "investigation must not move reserved wallet funds")
}
