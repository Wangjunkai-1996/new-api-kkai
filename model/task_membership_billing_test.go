package model

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func membershipTaskFixture(t *testing.T, subscriptionQuota, walletQuota int64) (*Task, *MembershipAccount, TaskBillingReservationRequest) {
	t.Helper()
	user, catalog := setupMembershipTest(t)
	require.NoError(t, DB.AutoMigrate(&Task{}, &Token{}, &KKAIOutboxEvent{}))
	_, account := buyMembershipTest(t, user.Id, catalog, "gold", "task-purchase")
	token := Token{UserId: user.Id, Key: "membership-task-token", Status: common.TokenStatusEnabled, RemainQuota: 1000}
	require.NoError(t, DB.Create(&token).Error)
	task := &Task{TaskID: "task_membership_test", UserId: user.Id, Group: "gpt", Status: TaskStatusNotStart,
		PrivateData: TaskPrivateData{BillingState: TaskBillingStatePending, AccountingState: TaskAccountingStatePending}}
	require.NoError(t, DB.Create(task).Error)
	input := membershipReservation(t, user.Id, "task:"+task.TaskID, subscriptionQuota, walletQuota)
	input.TokenID, input.TokenKey, input.SkipToken = token.Id, token.Key, false
	for _, member := range []bool{true, false} {
		ratio := 0.2
		if member {
			ratio = 0.19
		}
		encoded, err := common.Marshal(types.MembershipPricing{Version: 1, GroupKey: "gpt", AccountID: account.ID,
			Price: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: ratio}}})
		require.NoError(t, err)
		if member {
			input.SubscriptionSnapshot = string(encoded)
		} else {
			input.WalletSnapshot = string(encoded)
		}
	}
	return task, account, TaskBillingReservationRequest{Source: MembershipSource, TokenID: token.Id, TokenKey: token.Key, Membership: &input}
}

func TestMembershipTaskReservationWaitsForTerminalAndCapsOnlyFinalCharge(t *testing.T) {
	task, account, request := membershipTaskFixture(t, 50, 100)
	ctx := context.Background()
	reserved, err := ReserveTaskBilling(ctx, task.ID, request)
	require.NoError(t, err)
	require.NotNil(t, reserved.Membership)
	assert.Equal(t, MembershipSource, reserved.Task.PrivateData.BillingSource)
	assert.InDelta(t, 0.19, reserved.Task.PrivateData.BillingContext.GroupRatio, 1e-12)
	replayed, err := ReserveTaskBilling(ctx, task.ID, request)
	require.NoError(t, err)
	assert.False(t, replayed.Applied)
	require.NoError(t, DB.Model(task).Updates(map[string]any{"status": TaskStatusQueued}).Error)
	_, err = AdjustTaskBilling(ctx, task.ID, 50)
	require.NoError(t, err)
	record, err := GetMembershipBillingRecord(request.Membership.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "reserved", record.State)
	require.NoError(t, DB.First(account, account.ID).Error)
	assert.EqualValues(t, 50, account.AmountReserved)
	assert.Zero(t, account.AmountUsed)
	_, err = RecordTaskAccountingStatistics(ctx, task.ID)
	require.ErrorIs(t, err, ErrTaskAccountingNotReady)
	require.NoError(t, DB.Model(task).Update("status", TaskStatusSuccess).Error)
	settled, err := AdjustTaskBilling(ctx, task.ID, 80)
	require.NoError(t, err)
	assert.Equal(t, 50, settled.CurrentQuota)
	assert.EqualValues(t, 80, settled.Task.PrivateData.MembershipAssessedQuota)
	assert.EqualValues(t, 30, settled.Task.PrivateData.MembershipExcessQuota)
	_, err = AdjustTaskBilling(ctx, task.ID, 80)
	require.NoError(t, err)
	require.NoError(t, DB.First(account, account.ID).Error)
	assert.Zero(t, account.AmountReserved)
	assert.EqualValues(t, 50, account.AmountUsed)
	var user User
	require.NoError(t, DB.First(&user, task.UserId).Error)
	assert.EqualValues(t, 500, user.Quota)
	var token Token
	require.NoError(t, DB.First(&token, request.TokenID).Error)
	assert.Equal(t, 950, token.RemainQuota)
	assert.Equal(t, 50, token.UsedQuota)
}

func TestMembershipTaskWalletFallbackAndFailureReleaseDoNotMixFunds(t *testing.T) {
	task, account, request := membershipTaskFixture(t, 120, 150)
	ctx := context.Background()
	reserved, err := ReserveTaskBilling(ctx, task.ID, request)
	require.NoError(t, err)
	assert.Equal(t, MembershipWalletSource, reserved.Task.PrivateData.BillingSource)
	assert.InDelta(t, 0.2, reserved.Task.PrivateData.BillingContext.GroupRatio, 1e-12)
	require.NoError(t, DB.First(account, account.ID).Error)
	assert.Zero(t, account.AmountReserved)
	var user User
	require.NoError(t, DB.First(&user, task.UserId).Error)
	assert.EqualValues(t, 350, user.Quota)
	require.NoError(t, DB.Model(task).Update("status", TaskStatusFailure).Error)
	refunded, err := RefundTaskBilling(ctx, task.ID)
	require.NoError(t, err)
	assert.Zero(t, refunded.CurrentQuota)
	_, err = RefundTaskBilling(ctx, task.ID)
	require.NoError(t, err)
	require.NoError(t, DB.First(&user, task.UserId).Error)
	assert.EqualValues(t, 500, user.Quota)
	var token Token
	require.NoError(t, DB.First(&token, request.TokenID).Error)
	assert.Equal(t, 1000, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)
	record, err := GetMembershipBillingRecord(request.Membership.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "released", record.State)
}

func TestMembershipTaskWriteFailureRollsBackFundsAndLedger(t *testing.T) {
	task, account, request := membershipTaskFixture(t, 50, 100)
	callback := "test:membership_task_write_failure"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "tasks" {
			tx.AddError(errors.New("task persistence unavailable"))
		}
	}))
	t.Cleanup(func() { DB.Callback().Update().Remove(callback) })
	_, err := ReserveTaskBilling(context.Background(), task.ID, request)
	require.Error(t, err)
	require.NoError(t, DB.First(account, account.ID).Error)
	assert.Zero(t, account.AmountReserved)
	var count int64
	require.NoError(t, DB.Model(&MembershipBillingRecord{}).Count(&count).Error)
	assert.Zero(t, count)
	var token Token
	require.NoError(t, DB.First(&token, request.TokenID).Error)
	assert.Equal(t, 1000, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)
}

func TestMembershipTaskReviewFinalizesOriginalFundingAndRejectsLateResults(t *testing.T) {
	for _, source := range []string{MembershipSource, MembershipWalletSource} {
		for _, action := range []string{"settle", "release"} {
			t.Run(source+"/"+action, func(t *testing.T) {
				memberQuota := int64(50)
				if source == MembershipWalletSource {
					memberQuota = 120
				}
				task, account, request := membershipTaskFixture(t, memberQuota, 100)
				require.NoError(t, DB.AutoMigrate(&Channel{}))
				channel := Channel{Name: "task-review", Key: "test", Type: 1, Status: 1}
				require.NoError(t, DB.Create(&channel).Error)
				ctx := context.Background()
				reserved, err := ReserveTaskBilling(ctx, task.ID, request)
				require.NoError(t, err)
				require.Equal(t, source, reserved.Membership.Source)
				_, claimed, err := ClaimTaskSubmission(ctx, task.ID, &TaskSubmissionAttempt{ChannelID: channel.Id})
				require.NoError(t, err)
				require.True(t, claimed)
				stale, marked, err := MarkTaskSubmissionAmbiguous(ctx, task.ID, TaskSubmissionAmbiguity{Reason: "connection lost"})
				require.NoError(t, err)
				require.True(t, marked)
				reviews, err := ListMembershipBillingReviews(false, 50)
				require.NoError(t, err)
				require.Len(t, reviews, 1)
				assert.Equal(t, request.Membership.RequestID, reviews[0].RequestID)
				assert.Equal(t, "reserved", reviews[0].State)
				_, err = RecordTaskAccountingStatistics(ctx, task.ID)
				require.ErrorIs(t, err, ErrTaskAccountingNotReady, "unknown reserved work must not record consumption before manual review")

				// A new membership must never receive an old request's release.
				require.NoError(t, DB.Model(account).Update("expires_at", common.GetTimestamp()-1).Error)
				catalog, err := GetMembershipCatalog()
				require.NoError(t, err)
				_, current := buyMembershipTest(t, task.UserId, catalog, "gold", "task-review-repurchase")
				require.NotEqual(t, account.ID, current.ID)
				resolution := MembershipBillingResolutionInput{RequestID: request.Membership.RequestID, ActorID: 1, OperationID: "provider-reconciled", Action: action, Reason: "private verified provider receipt"}
				charged := int64(0)
				if action == "settle" {
					charged = reserved.Membership.ReservedQuota
					resolution.ActualQuota = charged + 20
				}
				resolved, err := ResolveMembershipBillingReview(resolution)
				require.NoError(t, err)
				assert.Equal(t, reserved.Membership.AccountID, resolved.AccountID)
				assert.Equal(t, reserved.Membership.Snapshot, resolved.Snapshot)
				assert.Equal(t, charged, resolved.ChargedQuota)
				if action == "settle" {
					assert.EqualValues(t, 20, resolved.ExcessQuota)
				}
				replay, err := ResolveMembershipBillingReview(resolution)
				require.NoError(t, err)
				assert.False(t, replay.Applied)
				conflict := resolution
				conflict.OperationID = "different-conclusion"
				_, err = ResolveMembershipBillingReview(conflict)
				assert.ErrorIs(t, err, ErrMembershipConflict)

				// Both an old poll snapshot and a fresh one must respect closure.
				for _, fresh := range []bool{false, true} {
					if fresh {
						require.NoError(t, DB.First(stale, task.ID).Error)
					}
					snap := stale.Snapshot()
					stale.Status, stale.Progress = TaskStatusSuccess, "100%"
					won, err := stale.UpdateWithStatusPreservingBilling(snap)
					require.NoError(t, err)
					assert.False(t, won)
				}
				_, recovered, err := RecoverTaskSubmissionAcceptance(ctx, task.ID, TaskSubmissionAcceptance{UpstreamTaskID: "late-receipt", Status: TaskStatusSubmitted, TargetQuota: 1})
				require.NoError(t, err)
				assert.False(t, recovered)
				adjusted, err := AdjustTaskBilling(ctx, task.ID, 1)
				require.NoError(t, err)
				assert.False(t, adjusted.Applied)
				accounting, err := RecordTaskAccountingStatistics(ctx, task.ID)
				require.NoError(t, err)
				assert.False(t, accounting.StatsRecorded)
				require.NoError(t, DB.First(task, task.ID).Error)
				assert.Equal(t, TaskStatus(TaskStatusUnknown), task.Status)
				assert.Equal(t, "100%", task.Progress)
				assert.NotContains(t, task.FailReason, resolution.Reason)
				assert.Nil(t, task.PrivateData.TargetQuota)
				assert.EqualValues(t, charged, task.Quota)
				require.NoError(t, DB.First(current, current.ID).Error)
				assert.EqualValues(t, 100, current.Available())
				require.NoError(t, DB.First(account, account.ID).Error)
				assert.Zero(t, account.AmountReserved)
				walletBalance, memberUsed := int64(500), int64(0)
				if source == MembershipWalletSource {
					walletBalance -= charged
				} else {
					memberUsed = charged
				}
				assert.Equal(t, memberUsed, account.AmountUsed)
				var user User
				require.NoError(t, DB.First(&user, task.UserId).Error)
				assert.EqualValues(t, walletBalance, user.Quota)
				assert.EqualValues(t, charged, user.UsedQuota)
				if action == "settle" {
					assert.Equal(t, 1, user.RequestCount)
				} else {
					assert.Zero(t, user.RequestCount)
				}
				require.NoError(t, DB.First(&channel, channel.Id).Error)
				assert.EqualValues(t, charged, channel.UsedQuota)
				var token Token
				require.NoError(t, DB.First(&token, request.TokenID).Error)
				assert.EqualValues(t, 1000-charged, token.RemainQuota)
				assert.EqualValues(t, charged, token.UsedQuota)
				var eventCount int64
				require.NoError(t, DB.Model(&KKAIOutboxEvent{}).Where("topic = ? AND aggregate_id = ?", KKAIOutboxTopicTaskAccounting, task.TaskID).Count(&eventCount).Error)
				assert.EqualValues(t, 1, eventCount)
			})
		}
	}
}

func TestMembershipTaskReviewRollsBackAccountingAndFundsTogether(t *testing.T) {
	task, account, request := membershipTaskFixture(t, 50, 100)
	ctx := context.Background()
	_, err := ReserveTaskBilling(ctx, task.ID, request)
	require.NoError(t, err)
	_, claimed, err := ClaimTaskSubmission(ctx, task.ID, nil)
	require.NoError(t, err)
	require.True(t, claimed)
	_, marked, err := MarkTaskSubmissionAmbiguous(ctx, task.ID, TaskSubmissionAmbiguity{Reason: "lost response"})
	require.NoError(t, err)
	require.True(t, marked)
	callback := "test:membership_task_review_stats_failure"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			tx.AddError(errors.New("accounting unavailable"))
		}
	}))
	t.Cleanup(func() { DB.Callback().Update().Remove(callback) })
	_, err = ResolveMembershipBillingReview(MembershipBillingResolutionInput{RequestID: request.Membership.RequestID, ActorID: 1, OperationID: "verified", Action: "settle", ActualQuota: 40, Reason: "verified cost"})
	require.Error(t, err)
	record, err := GetMembershipBillingRecord(request.Membership.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "reserved", record.State)
	assert.Zero(t, record.ReviewResolvedAt)
	require.NoError(t, DB.First(account, account.ID).Error)
	assert.EqualValues(t, 50, account.AmountReserved)
	assert.Zero(t, account.AmountUsed)
	require.NoError(t, DB.First(task, task.ID).Error)
	assert.Equal(t, TaskBillingStateAmbiguous, task.PrivateData.BillingState)
	assert.Equal(t, TaskAccountingStatePending, task.PrivateData.AccountingState)
	var token Token
	require.NoError(t, DB.First(&token, request.TokenID).Error)
	assert.Equal(t, 950, token.RemainQuota)
}
