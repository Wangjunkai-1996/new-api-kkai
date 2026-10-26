package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func membershipPollingTaskFixture(t *testing.T, wallet bool, upstreamID string) (*model.Task, *model.MembershipBillingRecord, model.MembershipAccount, model.Token) {
	t.Helper()
	ctx, user := setupMembershipServiceTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Task{}, &model.KKAIOutboxEvent{}, &model.Log{}))
	oldMemory, oldLogDB, oldConsumeLog := common.MemoryCacheEnabled, model.LOG_DB, common.LogConsumeEnabled
	oldTimeout, oldQueryLimit := constant.TaskTimeoutMinutes, constant.TaskQueryLimit
	common.MemoryCacheEnabled, model.LOG_DB, common.LogConsumeEnabled = false, model.DB, true
	constant.TaskTimeoutMinutes, constant.TaskQueryLimit = 1, 10
	t.Cleanup(func() {
		common.MemoryCacheEnabled, model.LOG_DB, common.LogConsumeEnabled = oldMemory, oldLogDB, oldConsumeLog
		constant.TaskTimeoutMinutes, constant.TaskQueryLimit = oldTimeout, oldQueryLimit
	})
	seedTaskPollingChannel(t, 1, true)
	token := model.Token{UserId: user.Id, Key: "membership-polling-token", RemainQuota: 1000, Status: common.TokenStatusEnabled}
	require.NoError(t, model.DB.Create(&token).Error)
	maximum := 500.0
	if wallet {
		maximum = 1500 // 150 subscription units exceed the purchased 100.
	}
	info := &relaycommon.RelayInfo{
		UserId: user.Id, TokenId: token.Id, TokenKey: token.Key, UsingGroup: "gpt", OriginModelName: "test-video",
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 1},
		PriceData:   types.PriceData{UsePrice: true, ModelPrice: maximum / common.QuotaPerUnit, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.2}},
	}
	task := &model.Task{
		TaskID: "task_membership_polling", UserId: user.Id, ChannelId: 1, Group: "gpt", Platform: constant.TaskPlatform("kling"),
		Status: model.TaskStatusNotStart, Progress: "0%", SubmitTime: time.Now().Unix(),
		PrivateData: model.TaskPrivateData{BillingState: model.TaskBillingStatePending, AccountingState: model.TaskAccountingStatePending,
			BillingContext: &model.TaskBillingContext{PerCallBilling: true}},
	}
	require.NoError(t, model.DB.Create(task).Error)
	require.Nil(t, preConsumeTaskMembership(ctx, task, info))
	claimed, won, err := model.ClaimTaskSubmission(context.Background(), task.ID, nil)
	require.NoError(t, err)
	require.True(t, won)
	task = claimed
	if upstreamID != "" {
		task, won, err = model.PersistTaskSubmissionAcceptance(context.Background(), task.ID, model.TaskSubmissionAcceptance{
			UpstreamTaskID: upstreamID, ChannelID: 1, Status: model.TaskStatusInProgress, Progress: "30%", TargetQuota: task.Quota,
		})
		require.NoError(t, err)
		require.True(t, won)
	}
	record, err := model.GetMembershipBillingRecord(task.PrivateData.MembershipBillingRequestID)
	require.NoError(t, err)
	self, err := model.GetMembershipSelf(user.Id)
	require.NoError(t, err)
	require.NotNil(t, self.Account)
	require.NoError(t, model.DB.First(&token, token.Id).Error)
	return task, record, *self.Account, token
}

func TestMembershipTaskPollingTimeoutRetainsAuthorizationAndKnownTaskPolling(t *testing.T) {
	for _, wallet := range []bool{false, true} {
		source := model.MembershipSource
		if wallet {
			source = model.MembershipWalletSource
		}
		t.Run(source, func(t *testing.T) {
			task, initial, account, token := membershipPollingTaskFixture(t, wallet, "provider-known-task")
			require.Equal(t, source, initial.Source)
			require.NoError(t, model.DB.Model(task).Update("submit_time", time.Now().Add(-2*time.Hour).Unix()).Error)
			sweepTimedOutTasks(context.Background())
			var stored model.Task
			require.NoError(t, model.DB.First(&stored, task.ID).Error)
			assert.EqualValues(t, model.TaskStatusUnknown, stored.Status)
			assert.NotEqual(t, "100%", stored.Progress)
			assert.Equal(t, model.TaskBillingStateAccepted, stored.PrivateData.BillingState)
			assert.Equal(t, task.Quota, stored.Quota)
			review, err := model.GetMembershipBillingReview(initial.RequestID)
			require.NoError(t, err)
			assert.Equal(t, "reserved", review.State)
			assert.Equal(t, "task_polling_deadline_elapsed", review.ReviewReason)
			assert.Equal(t, initial.Source, review.Source)
			assert.Equal(t, initial.AccountID, review.AccountID)
			var accountAfter model.MembershipAccount
			require.NoError(t, model.DB.First(&accountAfter, account.ID).Error)
			assert.Equal(t, account.AmountReserved, accountAfter.AmountReserved)
			assert.Zero(t, accountAfter.AmountUsed)
			expectedWallet := int64(10000)
			if wallet {
				expectedWallet -= initial.ReservedQuota
			}
			assert.EqualValues(t, expectedWallet, getUserQuota(t, task.UserId))
			assert.Equal(t, token.RemainQuota, getTokenRemainQuota(t, token.Id))
			assert.Equal(t, token.UsedQuota, getTokenUsedQuota(t, token.Id))

			adaptor := &taskPollingFetchAdaptor{}
			previousFactory := GetTaskAdaptorFunc
			GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
			t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })
			summary := RunTaskPollingOnce(context.Background(), nil)
			assert.Equal(t, 1, summary.UnfinishedTasks)
			assert.Equal(t, []string{"provider-known-task"}, adaptor.fetchedTaskIDs())
			record, err := model.GetMembershipBillingRecord(initial.RequestID)
			require.NoError(t, err)
			assert.Equal(t, "reserved", record.State)
			assert.Zero(t, record.ChargedQuota)
			assert.Zero(t, countLogs(t))
		})
	}
}

func TestMembershipTaskPollingMissingProviderIDStopsBlindPollingWithoutRefund(t *testing.T) {
	task, initial, account, token := membershipPollingTaskFixture(t, false, "accepted-provider-id")
	task.PrivateData.UpstreamTaskID = ""
	require.NoError(t, model.DB.Save(task).Error)
	adaptor := &taskPollingFetchAdaptor{}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })
	summary := RunTaskPollingOnce(context.Background(), nil)
	assert.Zero(t, summary.NullTasksFailed, "an unknown dispatch is not a confirmed failure")
	assert.Empty(t, adaptor.fetchedTaskIDs(), "the public local task ID must never be sent as a provider ID")
	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusUnknown, stored.Status)
	assert.Equal(t, "100%", stored.Progress)
	assert.Equal(t, model.TaskBillingStateAccepted, stored.PrivateData.BillingState)
	assert.Equal(t, task.Quota, stored.Quota)
	review, err := model.GetMembershipBillingReview(initial.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "reserved", review.State)
	assert.Equal(t, "task_polling_result_unavailable", review.ReviewReason)
	require.NoError(t, model.DB.First(&account, account.ID).Error)
	assert.Equal(t, initial.ReservedQuota, account.AmountReserved)
	assert.Zero(t, account.AmountUsed)
	assert.EqualValues(t, 10000, getUserQuota(t, task.UserId))
	assert.Equal(t, token.RemainQuota, getTokenRemainQuota(t, token.Id))
	assert.Empty(t, model.GetAllUnFinishSyncTasks(10))
	RunTaskPollingOnce(context.Background(), nil)
	assert.Empty(t, adaptor.fetchedTaskIDs())
	assert.Zero(t, countLogs(t))
}

func TestMembershipTaskPollingDoesNotReviewAnActiveDispatcherWithoutProviderID(t *testing.T) {
	task, initial, account, token := membershipPollingTaskFixture(t, false, "")
	adaptor := &taskPollingFetchAdaptor{}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })
	RunTaskPollingOnce(context.Background(), nil)
	assert.Empty(t, adaptor.fetchedTaskIDs())
	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	assert.Equal(t, task.Status, stored.Status)
	assert.Equal(t, task.Progress, stored.Progress)
	assert.Equal(t, model.TaskBillingStateDispatching, stored.PrivateData.BillingState)
	_, err := model.GetMembershipBillingReview(initial.RequestID)
	assert.ErrorIs(t, err, model.ErrMembershipNotFound)
	require.NoError(t, model.DB.First(&account, account.ID).Error)
	assert.Equal(t, initial.ReservedQuota, account.AmountReserved)
	assert.Zero(t, account.AmountUsed)
	assert.Equal(t, token.RemainQuota, getTokenRemainQuota(t, token.Id))
	assert.EqualValues(t, 10000, getUserQuota(t, task.UserId))
}

func TestMembershipTaskPollingPersistenceFailureNeverReleasesFunds(t *testing.T) {
	for _, trigger := range []string{"timeout", "missing_provider_id"} {
		t.Run(trigger, func(t *testing.T) {
			upstreamID := "provider-task"
			if trigger == "missing_provider_id" {
				upstreamID = ""
			}
			task, initial, account, token := membershipPollingTaskFixture(t, false, upstreamID)
			require.NoError(t, model.DB.Model(task).Update("submit_time", time.Now().Add(-2*time.Hour).Unix()).Error)
			callback := "test:membership_polling_write_failure"
			db := model.DB
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "tasks" {
					tx.AddError(errors.New("task persistence unavailable"))
				}
			}))
			t.Cleanup(func() { _ = db.Callback().Update().Remove(callback) })
			if trigger == "timeout" {
				sweepTimedOutTasks(context.Background())
			} else {
				won, err := failPollingTask(context.Background(), task, "missing upstream ID")
				require.Error(t, err)
				assert.False(t, won)
			}
			var stored model.Task
			require.NoError(t, db.First(&stored, task.ID).Error)
			assert.Equal(t, task.Status, stored.Status)
			assert.Equal(t, task.Progress, stored.Progress)
			assert.Equal(t, task.Quota, stored.Quota)
			record, err := model.GetMembershipBillingRecord(initial.RequestID)
			require.NoError(t, err)
			assert.Equal(t, "reserved", record.State)
			assert.Zero(t, record.ReviewStartedAt, "review and task updates must roll back together")
			require.NoError(t, db.First(&account, account.ID).Error)
			assert.Equal(t, initial.ReservedQuota, account.AmountReserved)
			assert.Zero(t, account.AmountUsed)
			assert.EqualValues(t, 10000, getUserQuota(t, task.UserId))
			assert.Equal(t, token.RemainQuota, getTokenRemainQuota(t, token.Id))
			assert.Equal(t, token.UsedQuota, getTokenUsedQuota(t, token.Id))
			assert.Zero(t, countLogs(t))
		})
	}
}

type membershipSunoPollingAdaptor struct {
	taskPollingFetchAdaptor
	result *BatchTaskResult
}

func (a *membershipSunoPollingAdaptor) FetchMode() string { return "batch" }
func (a *membershipSunoPollingAdaptor) FetchBatchTasks(_, _ string, _ []*model.Task, _ string) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
}
func (a *membershipSunoPollingAdaptor) ParseBatchResult(_ []*model.Task, _ *http.Response, _ []byte) (map[string]*BatchTaskResult, error) {
	return map[string]*BatchTaskResult{a.result.TaskInfo.TaskID: a.result}, nil
}

func TestMembershipSunoPollingInformationalReasonKeepsTaskPendingUntilSuccess(t *testing.T) {
	task, initial, account, _ := membershipPollingTaskFixture(t, false, "suno-known-task")
	require.NoError(t, model.DB.Model(task).Update("platform", constant.TaskPlatformSuno).Error)
	adaptor := &membershipSunoPollingAdaptor{result: &BatchTaskResult{TaskInfo: relaycommon.TaskInfo{
		TaskID: "suno-known-task", Status: model.TaskStatusInProgress, Progress: "30%", Reason: "rendering remains in progress",
	}}}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })
	RunTaskPollingOnce(context.Background(), nil)
	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusInProgress, stored.Status)
	assert.NotEqual(t, "100%", stored.Progress)
	assert.Equal(t, model.TaskBillingStateAccepted, stored.PrivateData.BillingState)
	record, err := model.GetMembershipBillingRecord(initial.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "reserved", record.State)
	assert.Len(t, model.GetAllUnFinishSyncTasks(10), 1)

	adaptor.result.TaskInfo.Status = model.TaskStatusSuccess
	adaptor.result.TaskInfo.Reason = "rendering completed"
	RunTaskPollingOnce(context.Background(), nil)
	record, err = model.GetMembershipBillingRecord(initial.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "settled", record.State)
	assert.Equal(t, initial.ReservedQuota, record.ChargedQuota)
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, stored.Status)
	assert.Equal(t, "100%", stored.Progress)
	require.NotNil(t, stored.PrivateData.MembershipCompletionQuota)
	assert.EqualValues(t, initial.ReservedQuota, *stored.PrivateData.MembershipCompletionQuota)
	require.NoError(t, model.DB.First(&account, account.ID).Error)
	assert.Zero(t, account.AmountReserved)
	assert.Equal(t, initial.ReservedQuota, account.AmountUsed)
	assert.EqualValues(t, 10000, getUserQuota(t, task.UserId))
	var accountingEvent model.KKAIOutboxEvent
	require.NoError(t, model.DB.Where("topic = ? AND aggregate_id = ?", model.KKAIOutboxTopicTaskAccounting, task.TaskID).First(&accountingEvent).Error)
	handler := TaskAccountingHandler{}
	require.NoError(t, handler.Handle(context.Background(), accountingEvent))
	require.NoError(t, handler.Handle(context.Background(), accountingEvent))
	used, requests := getUserUsageAccounting(t, task.UserId)
	assert.EqualValues(t, initial.ReservedQuota, used)
	assert.Equal(t, 1, requests)
	assert.EqualValues(t, initial.ReservedQuota, getChannelUsedQuota(t, task.ChannelId))
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", task.UserId, model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, task.TaskID, logs[0].RequestId)
	assert.EqualValues(t, initial.ReservedQuota, logs[0].Quota)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
	assert.Equal(t, model.MembershipSource, other["billing_source"])
	assert.Equal(t, "Gold", other["membership_tier_name"])
	assert.Equal(t, 0.5, other["membership_discount"])
	assert.EqualValues(t, initial.ReservedQuota, other["membership_estimated_saved_quota"])
}

func TestMembershipSunoPollingFailureDoesNotRecordConsumptionAfterRelease(t *testing.T) {
	task, initial, account, token := membershipPollingTaskFixture(t, false, "suno-failed-task")
	require.NoError(t, model.DB.Model(task).Update("platform", constant.TaskPlatformSuno).Error)
	adaptor := &membershipSunoPollingAdaptor{result: &BatchTaskResult{TaskInfo: relaycommon.TaskInfo{
		TaskID: "suno-failed-task", Status: model.TaskStatusFailure, Reason: "provider confirmed failure",
	}}}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })
	RunTaskPollingOnce(context.Background(), nil)
	record, err := model.GetMembershipBillingRecord(initial.RequestID)
	require.NoError(t, err)
	require.Equal(t, "released", record.State)
	var accountingEvent model.KKAIOutboxEvent
	require.NoError(t, model.DB.Where("topic = ? AND aggregate_id = ?", model.KKAIOutboxTopicTaskAccounting, task.TaskID).First(&accountingEvent).Error)
	handler := TaskAccountingHandler{}
	require.NoError(t, handler.Handle(context.Background(), accountingEvent))
	require.NoError(t, handler.Handle(context.Background(), accountingEvent))
	used, requests := getUserUsageAccounting(t, task.UserId)
	assert.Zero(t, used)
	assert.Zero(t, requests)
	assert.Zero(t, getChannelUsedQuota(t, task.ChannelId))
	var consumptionLogs int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("user_id = ? AND type = ?", task.UserId, model.LogTypeConsume).Count(&consumptionLogs).Error)
	assert.Zero(t, consumptionLogs)
	require.NoError(t, model.DB.First(&account, account.ID).Error)
	assert.Zero(t, account.AmountReserved)
	assert.Zero(t, account.AmountUsed)
	assert.EqualValues(t, 10000, getUserQuota(t, task.UserId))
	assert.EqualValues(t, int64(token.RemainQuota)+initial.ReservedQuota, getTokenRemainQuota(t, token.Id))
	assert.Zero(t, getTokenUsedQuota(t, token.Id))
}

func TestMembershipTaskRecoveryDefersUnknownUntilManualReleaseWithoutConsumption(t *testing.T) {
	task, initial, account, _ := membershipPollingTaskFixture(t, false, "")
	ctx := context.Background()
	require.NoError(t, EnqueueTaskBillingRecovery(ctx, model.DB, task))
	var recoveryEvent model.KKAIOutboxEvent
	require.NoError(t, model.DB.Where("topic = ? AND aggregate_id = ?", KKAIOutboxTopicTaskBillingRecovery, task.TaskID).First(&recoveryEvent).Error)
	recovery := TaskBillingRecoveryHandler{}
	err := recovery.Handle(ctx, recoveryEvent)
	var deferred deferredKKAIOutboxError
	require.ErrorAs(t, err, &deferred)
	assert.ErrorIs(t, err, model.ErrTaskAccountingNotReady)
	err = recovery.Handle(ctx, recoveryEvent)
	require.ErrorAs(t, err, &deferred)
	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusUnknown, stored.Status)
	assert.Equal(t, model.TaskBillingStateAmbiguous, stored.PrivateData.BillingState)
	assert.Nil(t, stored.PrivateData.TargetQuota)
	review, err := model.GetMembershipBillingReview(initial.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "reserved", review.State)
	var accountingEvent model.KKAIOutboxEvent
	require.NoError(t, model.DB.Where("topic = ? AND aggregate_id = ?", model.KKAIOutboxTopicTaskAccounting, task.TaskID).First(&accountingEvent).Error)
	accounting := TaskAccountingHandler{}
	err = accounting.Handle(ctx, accountingEvent)
	require.ErrorAs(t, err, &deferred)
	assert.ErrorIs(t, err, model.ErrTaskAccountingNotReady)
	used, requests := getUserUsageAccounting(t, task.UserId)
	assert.Zero(t, used)
	assert.Zero(t, requests)
	assert.Zero(t, countLogs(t))
	require.NoError(t, model.DB.First(&account, account.ID).Error)
	assert.Equal(t, initial.ReservedQuota, account.AmountReserved)
	assert.Zero(t, account.AmountUsed)

	resolved, err := model.ResolveMembershipBillingReview(model.MembershipBillingResolutionInput{
		RequestID: initial.RequestID, ActorID: 1, OperationID: "provider-confirms-no-charge", Action: "release", Reason: "Provider confirms no accepted task or billed usage",
	})
	require.NoError(t, err)
	assert.Equal(t, "released", resolved.State)
	require.NoError(t, recovery.Handle(ctx, recoveryEvent))
	require.NoError(t, recovery.Handle(ctx, recoveryEvent))
	require.NoError(t, accounting.Handle(ctx, accountingEvent))
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusUnknown, stored.Status, "a completed financial review must not invent a provider result")
	assert.Equal(t, model.TaskBillingStateRefunded, stored.PrivateData.BillingState)
	used, requests = getUserUsageAccounting(t, task.UserId)
	assert.Zero(t, used)
	assert.Zero(t, requests)
	assert.Zero(t, getChannelUsedQuota(t, task.ChannelId))
	assert.Zero(t, countLogs(t))
	require.NoError(t, model.DB.First(&account, account.ID).Error)
	assert.Zero(t, account.AmountReserved)
	assert.Zero(t, account.AmountUsed)
	assert.EqualValues(t, 10000, getUserQuota(t, task.UserId))
}
