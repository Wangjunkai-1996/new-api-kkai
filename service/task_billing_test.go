package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	model.DB = db
	model.LOG_DB = db

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true

	if err := db.AutoMigrate(
		&model.Task{},
		&model.User{},
		&model.Token{},
		&model.Log{},
		&model.Channel{},
		&model.TopUp{},
		&model.UserSubscription{},
		&model.SubscriptionPlan{},
		&model.KKAIOutboxEvent{},
		&model.SystemTask{},
		&model.SystemTaskLock{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// Seed helpers
// ---------------------------------------------------------------------------

func truncate(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM tasks")
		model.DB.Exec("DELETE FROM users")
		model.DB.Exec("DELETE FROM tokens")
		model.DB.Exec("DELETE FROM logs")
		model.DB.Exec("DELETE FROM channels")
		model.DB.Exec("DELETE FROM top_ups")
		model.DB.Exec("DELETE FROM user_subscriptions")
		model.DB.Exec("DELETE FROM subscription_plans")
		model.DB.Exec("DELETE FROM kkai_outbox")
		model.DB.Exec("DELETE FROM system_task_locks")
		model.DB.Exec("DELETE FROM system_tasks")
	})
}

func newDurableTaskBillingContext(t *testing.T, task *model.Task, tokenKey string, preference string, isPlayground bool) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	info := &relaycommon.RelayInfo{
		UserId:          task.UserId,
		TokenId:         task.PrivateData.TokenId,
		TokenKey:        tokenKey,
		OriginModelName: task.Properties.OriginModelName,
		UsingGroup:      task.Group,
		IsPlayground:    isPlayground,
		UserSetting: dto.UserSetting{
			BillingPreference: preference,
		},
	}
	return ctx, info
}

func persistPendingTaskBilling(t *testing.T, task *model.Task, maxQuota *int) {
	t.Helper()
	task.Status = model.TaskStatusNotStart
	task.Progress = "0%"
	task.Quota = 0
	task.PrivateData.BillingState = model.TaskBillingStatePending
	task.PrivateData.BillingContext.MaxQuota = maxQuota
	require.NoError(t, model.DB.Create(task).Error)
}

func seedDurableSubscription(t *testing.T, planID int, subscriptionID int, userID int, amountTotal int64, amountUsed int64) {
	t.Helper()
	allowOverflow := true
	plan := &model.SubscriptionPlan{
		Id:                  planID,
		Title:               "durable billing plan",
		DurationUnit:        model.SubscriptionDurationMonth,
		DurationValue:       1,
		Enabled:             true,
		AllowWalletOverflow: &allowOverflow,
		TotalAmount:         amountTotal,
		QuotaResetPeriod:    model.SubscriptionResetNever,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	subscription := &model.UserSubscription{
		Id:                  subscriptionID,
		UserId:              userID,
		PlanId:              planID,
		AmountTotal:         amountTotal,
		AmountUsed:          amountUsed,
		Status:              "active",
		StartTime:           time.Now().Add(-time.Hour).Unix(),
		EndTime:             time.Now().Add(30 * 24 * time.Hour).Unix(),
		AllowWalletOverflow: true,
	}
	require.NoError(t, model.DB.Create(subscription).Error)
}

func seedUser(t *testing.T, id int, quota int64) {
	t.Helper()
	user := &model.User{Id: id, Username: "test_user", Quota: quota, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
}

func seedToken(t *testing.T, id int, userId int, key string, remainQuota int) {
	t.Helper()
	token := &model.Token{
		Id:          id,
		UserId:      userId,
		Key:         key,
		Name:        "test_token",
		Status:      common.TokenStatusEnabled,
		RemainQuota: remainQuota,
		UsedQuota:   0,
	}
	require.NoError(t, model.DB.Create(token).Error)
}

func seedSubscription(t *testing.T, id int, userId int, amountTotal int64, amountUsed int64) {
	t.Helper()
	sub := &model.UserSubscription{
		Id:          id,
		UserId:      userId,
		AmountTotal: amountTotal,
		AmountUsed:  amountUsed,
		Status:      "active",
		StartTime:   time.Now().Unix(),
		EndTime:     time.Now().Add(30 * 24 * time.Hour).Unix(),
	}
	require.NoError(t, model.DB.Create(sub).Error)
}

func seedChannel(t *testing.T, id int) {
	t.Helper()
	ch := &model.Channel{Id: id, Name: "test_channel", Key: "sk-test", Status: common.ChannelStatusEnabled}
	require.NoError(t, model.DB.Create(ch).Error)
}

func makeTask(userId, channelId, quota, tokenId int, billingSource string, subscriptionId int) *model.Task {
	return &model.Task{
		TaskID:    "task_" + time.Now().Format("150405.000"),
		UserId:    userId,
		ChannelId: channelId,
		Quota:     quota,
		Status:    model.TaskStatus(model.TaskStatusInProgress),
		Group:     "default",
		Data:      json.RawMessage(`{}`),
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		Properties: model.Properties{
			OriginModelName: "test-model",
		},
		PrivateData: model.TaskPrivateData{
			BillingSource:  billingSource,
			SubscriptionId: subscriptionId,
			TokenId:        tokenId,
			BillingContext: &model.TaskBillingContext{
				ModelPrice:      0.02,
				GroupRatio:      1.0,
				OriginModelName: "test-model",
			},
		},
	}
}

func TestPriceDataOtherRatiosFilterAndSnapshot(t *testing.T) {
	priceData := types.PriceData{}

	priceData.AddOtherRatio("zero", 0)
	priceData.AddOtherRatio("negative", -0.5)
	priceData.AddOtherRatio("nan", math.NaN())
	priceData.AddOtherRatio("inf", math.Inf(1))
	priceData.AddOtherRatio("one", 1)
	priceData.AddOtherRatio("positive", 2.5)

	ratios := priceData.OtherRatios()
	require.Len(t, ratios, 2)
	assert.Equal(t, 1.0, ratios["one"])
	assert.Equal(t, 2.5, ratios["positive"])
	assert.True(t, priceData.HasOtherRatio("one"))
	assert.False(t, priceData.HasOtherRatio("zero"))

	ratios["positive"] = 99
	ratios["new"] = 3
	nextSnapshot := priceData.OtherRatios()
	assert.Equal(t, 2.5, nextSnapshot["positive"])
	assert.NotContains(t, nextSnapshot, "new")
}

func TestPriceDataReplaceAndApplyOtherRatios(t *testing.T) {
	priceData := types.PriceData{}

	replaced := priceData.ReplaceOtherRatios(map[string]float64{
		"zero":     0,
		"negative": -3,
		"nan":      math.NaN(),
		"inf":      math.Inf(1),
		"one":      1,
		"duration": 2,
		"size":     1.5,
	})

	require.True(t, replaced)
	assert.Equal(t, 3.0, priceData.OtherRatioMultiplier())
	assert.Equal(t, 30.0, priceData.ApplyOtherRatiosToFloat(10))
	assert.Equal(t, 10.0, priceData.RemoveOtherRatiosFromFloat(30))
	assert.True(t, decimal.NewFromInt(30).Equal(priceData.ApplyOtherRatiosToDecimal(decimal.NewFromInt(10))))

	replaced = priceData.ReplaceOtherRatios(map[string]float64{
		"zero": 0,
		"nan":  math.NaN(),
	})

	require.False(t, replaced)
	assert.Nil(t, priceData.OtherRatios())
	assert.Equal(t, 1.0, priceData.OtherRatioMultiplier())
}

func TestTaskBillingOtherFiltersHistoricalOtherRatios(t *testing.T) {
	task := makeTask(1, 1, 100, 0, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.OtherRatios = map[string]float64{
		"seconds":  2,
		"identity": 1,
		"zero":     0,
		"negative": -1,
		"nan":      math.NaN(),
		"inf":      math.Inf(1),
	}

	other := taskBillingOther(task)

	assert.Equal(t, 2.0, other["seconds"])
	assert.Equal(t, 1.0, other["identity"])
	assert.NotContains(t, other, "zero")
	assert.NotContains(t, other, "negative")
	assert.NotContains(t, other, "nan")
	assert.NotContains(t, other, "inf")
}

func TestTaskBillingContextPriceDataFiltersMultiplier(t *testing.T) {
	priceData := taskBillingContextPriceData(&model.TaskBillingContext{
		OtherRatios: map[string]float64{
			"seconds":  2,
			"size":     3,
			"identity": 1,
			"zero":     0,
			"negative": -1,
			"nan":      math.NaN(),
			"inf":      math.Inf(1),
		},
	})

	require.NotNil(t, priceData)
	assert.Equal(t, 6.0, priceData.OtherRatioMultiplier())
	assert.Equal(t, map[string]float64{
		"seconds":  2,
		"size":     3,
		"identity": 1,
	}, priceData.OtherRatios())
}

func TestPreConsumeTaskBillingWalletIsExactlyOnceAcrossRetryAndRefund(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 100, 100, 100
	const initialUserQuota, initialTokenQuota, requestedQuota = 10_000, 8_000, 1_200
	const tokenKey = "sk-durable-wallet"
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(2_000))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)

	require.Nil(t, PreConsumeTaskBilling(ctx, task, requestedQuota, info))
	assert.EqualValues(t, initialUserQuota-requestedQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-requestedQuota, getTokenRemainQuota(t, tokenID))

	info.Billing = nil
	require.Nil(t, PreConsumeTaskBilling(ctx, task, requestedQuota, info))
	assert.EqualValues(t, initialUserQuota-requestedQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-requestedQuota, getTokenRemainQuota(t, tokenID))

	require.NotNil(t, info.Billing)
	info.Billing.Refund(ctx)
	info.Billing.Refund(ctx)
	assert.EqualValues(t, initialUserQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Equal(t, model.TaskBillingStateRefunded, persisted.PrivateData.BillingState)
	assert.Zero(t, persisted.Quota)
}

func TestPreConsumeTaskBillingSubscriptionIsExactlyOnce(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID, planID, subscriptionID = 101, 101, 101, 101, 101
	const initialSubscriptionUsed, initialTokenQuota, requestedQuota = 2_000, 8_000, 1_200
	const tokenKey = "sk-durable-subscription"
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	seedDurableSubscription(t, planID, subscriptionID, userID, 20_000, initialSubscriptionUsed)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(2_000))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "subscription_only", false)

	require.Nil(t, PreConsumeTaskBilling(ctx, task, requestedQuota, info))
	assert.Equal(t, int64(initialSubscriptionUsed+requestedQuota), getSubscriptionUsed(t, subscriptionID))
	assert.Equal(t, initialTokenQuota-requestedQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, BillingSourceSubscription, info.BillingSource)
	assert.Equal(t, subscriptionID, info.SubscriptionId)

	info.Billing = nil
	require.Nil(t, PreConsumeTaskBilling(ctx, task, requestedQuota, info))
	assert.Equal(t, int64(initialSubscriptionUsed+requestedQuota), getSubscriptionUsed(t, subscriptionID))
	assert.Equal(t, initialTokenQuota-requestedQuota, getTokenRemainQuota(t, tokenID))
}

func TestPreConsumeTaskBillingSubscriptionReplaySurvivesDeletedPlan(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID, planID, subscriptionID = 110, 110, 110, 110, 110
	const initialSubscriptionUsed, initialTokenQuota, requestedQuota = 2_000, 8_000, 1_200
	const tokenKey = "sk-durable-subscription-replay"
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	seedDurableSubscription(t, planID, subscriptionID, userID, 20_000, initialSubscriptionUsed)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(2_000))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "subscription_only", false)

	require.Nil(t, PreConsumeTaskBilling(ctx, task, requestedQuota, info))
	require.NoError(t, model.DB.Delete(&model.SubscriptionPlan{}, planID).Error)

	info.Billing = nil
	require.Nil(t, PreConsumeTaskBilling(ctx, task, requestedQuota, info))
	assert.Equal(t, int64(initialSubscriptionUsed+requestedQuota), getSubscriptionUsed(t, subscriptionID))
	assert.Equal(t, initialTokenQuota-requestedQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, BillingSourceSubscription, info.BillingSource)
	assert.Equal(t, subscriptionID, info.SubscriptionId)
}

func TestPreConsumeTaskBillingSubscriptionDoesNotExceedZeroAuthorization(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID, planID, subscriptionID = 111, 111, 111, 111, 111
	const initialSubscriptionUsed, initialTokenQuota = 2_000, 8_000
	const tokenKey = "sk-zero-authorization"
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	seedDurableSubscription(t, planID, subscriptionID, userID, 20_000, initialSubscriptionUsed)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(0))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "subscription_only", false)

	apiErr := PreConsumeTaskBilling(ctx, task, 0, info)
	require.NotNil(t, apiErr)
	assert.Equal(t, int64(initialSubscriptionUsed), getSubscriptionUsed(t, subscriptionID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Equal(t, model.TaskBillingStatePending, persisted.PrivateData.BillingState)
	assert.Zero(t, persisted.Quota)
}

func TestDurableWalletRefundAfterSubmissionFailureRestoresFundingAndToken(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 112, 112, 112
	const initialUserQuota, initialTokenQuota, requestedQuota = 10_000, 8_000, 1_200
	const tokenKey = "sk-wallet-failure-refund"
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(2_000))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
	require.Nil(t, PreConsumeTaskBilling(ctx, task, requestedQuota, info))

	info.Billing.Refund(ctx)
	failedTask, failed, err := model.FailTaskBeforeSubmission(ctx, task.ID, "task submission failed")
	require.NoError(t, err)
	require.True(t, failed)
	require.NotNil(t, failedTask)

	assert.EqualValues(t, initialUserQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.EqualValues(t, model.TaskStatusFailure, failedTask.Status)
	assert.Equal(t, model.TaskBillingStateRefunded, failedTask.PrivateData.BillingState)
	assert.Zero(t, failedTask.Quota)
	deliverTaskBillingAuditEvents(t)
	assert.Equal(t, int64(1), countLogs(t))
}

func TestDurableSubscriptionRefundAfterSubmissionFailureRestoresFundingAndToken(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID, planID, subscriptionID = 113, 113, 113, 113, 113
	const initialSubscriptionUsed, initialTokenQuota, requestedQuota = 2_000, 8_000, 1_200
	const tokenKey = "sk-subscription-failure-refund"
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	seedDurableSubscription(t, planID, subscriptionID, userID, 20_000, initialSubscriptionUsed)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(2_000))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "subscription_only", false)
	require.Nil(t, PreConsumeTaskBilling(ctx, task, requestedQuota, info))

	info.Billing.Refund(ctx)
	failedTask, failed, err := model.FailTaskBeforeSubmission(ctx, task.ID, "task submission failed")
	require.NoError(t, err)
	require.True(t, failed)
	require.NotNil(t, failedTask)

	assert.Equal(t, int64(initialSubscriptionUsed), getSubscriptionUsed(t, subscriptionID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.EqualValues(t, model.TaskStatusFailure, failedTask.Status)
	assert.Equal(t, model.TaskBillingStateRefunded, failedTask.PrivateData.BillingState)
	assert.Zero(t, failedTask.Quota)
	deliverTaskBillingAuditEvents(t)
	assert.Equal(t, int64(1), countLogs(t))
}

func TestDurableSettlementSurvivesStalePollerWriteExactlyOnce(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 114, 114, 114
	const initialUserQuota, initialTokenQuota, reservedQuota, settledQuota = 10_000, 8_000, 100, 150
	const tokenKey = "sk-stale-poller-settlement"
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(settledQuota))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
	require.Nil(t, PreConsumeTaskBilling(ctx, task, reservedQuota, info))
	task.Status = model.TaskStatusSubmitted
	task.Progress = "10%"
	task.PrivateData.BillingState = model.TaskBillingStateAccepted
	task.PrivateData.TargetQuota = common.GetPointer(settledQuota)
	require.NoError(t, task.Update())
	stalePollerTask := *task
	staleSnapshot := stalePollerTask.Snapshot()

	RecalculateTaskQuota(context.Background(), task, settledQuota, "controller settlement")
	stalePollerTask.Status = model.TaskStatusSuccess
	stalePollerTask.Progress = "100%"
	won, err := stalePollerTask.UpdateWithStatusPreservingBilling(staleSnapshot)
	require.NoError(t, err)
	require.True(t, won)
	RecalculateTaskQuota(context.Background(), &stalePollerTask, settledQuota, "poller settlement replay")

	assert.EqualValues(t, initialUserQuota-settledQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-settledQuota, getTokenRemainQuota(t, tokenID))
	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, persisted.Status)
	assert.Equal(t, settledQuota, persisted.Quota)
	assert.Equal(t, settledQuota, persisted.PrivateData.TokenQuota)
	assert.Nil(t, persisted.PrivateData.TargetQuota)
}

func TestPreConsumeTaskBillingPlaygroundSkipsTokenQuota(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 102, 102, 102
	const initialUserQuota, initialTokenQuota, requestedQuota = 10_000, 8_000, 1_200
	const tokenKey = "sk-durable-playground"
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(2_000))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", true)

	require.Nil(t, PreConsumeTaskBilling(ctx, task, requestedQuota, info))
	assert.EqualValues(t, initialUserQuota-requestedQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
}

func TestPreConsumeTaskBillingRollsBackFundingWhenTaskStateWriteFails(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 103, 103, 103
	const initialUserQuota, initialTokenQuota, requestedQuota = 10_000, 8_000, 1_200
	const tokenKey = "sk-durable-rollback"
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(2_000))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)

	callbackName := "test:fail_durable_task_billing_update"
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		updatedTask, ok := tx.Statement.Dest.(*model.Task)
		if ok && updatedTask.PrivateData.BillingState == model.TaskBillingStateReserved {
			tx.AddError(errors.New("forced durable task state failure"))
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Update().Remove(callbackName) })

	apiErr := PreConsumeTaskBilling(ctx, task, requestedQuota, info)
	require.NotNil(t, apiErr)
	assert.EqualValues(t, initialUserQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Equal(t, model.TaskBillingStatePending, persisted.PrivateData.BillingState)
	assert.Zero(t, persisted.Quota)
}

func TestTaskBillingRejectsStaleUnlimitedSnapshotForFiniteToken(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 126, 126, 126
	const initialUserQuota, initialTokenQuota, requestedQuota = 1_000, 50, 100
	const tokenKey = "sk-stale-unlimited-snapshot"
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(requestedQuota))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
	info.TokenUnlimited = true

	apiErr := PreConsumeTaskBilling(ctx, task, requestedQuota, info)
	require.NotNil(t, apiErr)
	assert.EqualValues(t, initialUserQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Equal(t, model.TaskBillingStatePending, persisted.PrivateData.BillingState)
	assert.Zero(t, persisted.Quota)
}

func TestRecalculateTaskQuotaHonorsFrozenAuthorizationCeilingExactlyOnce(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 104, 104, 104
	const initialUserQuota, initialTokenQuota, reservedQuota, maxQuota = 10_000, 8_000, 100, 150
	const tokenKey = "sk-durable-cap"
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(maxQuota))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
	require.Nil(t, PreConsumeTaskBilling(ctx, task, reservedQuota, info))

	RecalculateTaskQuota(context.Background(), task, 500, "completion overrun")
	RecalculateTaskQuota(context.Background(), task, 500, "completion replay")

	assert.EqualValues(t, initialUserQuota-maxQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-maxQuota, getTokenRemainQuota(t, tokenID))
	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Equal(t, maxQuota, persisted.Quota)
}

func TestAdjustTaskBillingRejectsInsufficientWalletWithoutPartialTokenCharge(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 118, 118, 118
	const initialUserQuota, initialTokenQuota, reservedQuota, targetQuota = 120, 1_000, 100, 150
	const tokenKey = "sk-wallet-settlement-limit"
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(targetQuota))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
	require.Nil(t, PreConsumeTaskBilling(ctx, task, reservedQuota, info))

	_, err := model.AdjustTaskBilling(context.Background(), task.ID, targetQuota)
	require.ErrorIs(t, err, model.ErrTaskBillingInsufficientWallet)
	assert.EqualValues(t, initialUserQuota-reservedQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-reservedQuota, getTokenRemainQuota(t, tokenID))

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Equal(t, reservedQuota, persisted.Quota)
}

func TestAdjustTaskBillingRejectsInsufficientSubscriptionWithoutPartialTokenCharge(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID, planID, subscriptionID = 119, 119, 119, 119, 119
	const subscriptionTotal, initialTokenQuota, reservedQuota, targetQuota = 120, 1_000, 100, 150
	const tokenKey = "sk-subscription-settlement-limit"
	seedUser(t, userID, 10_000)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	seedDurableSubscription(t, planID, subscriptionID, userID, subscriptionTotal, 0)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(targetQuota))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "subscription_only", false)
	require.Nil(t, PreConsumeTaskBilling(ctx, task, reservedQuota, info))

	_, err := model.AdjustTaskBilling(context.Background(), task.ID, targetQuota)
	require.ErrorIs(t, err, model.ErrTaskBillingInsufficientSubscription)
	assert.Equal(t, int64(reservedQuota), getSubscriptionUsed(t, subscriptionID))
	assert.Equal(t, initialTokenQuota-reservedQuota, getTokenRemainQuota(t, tokenID))

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Equal(t, reservedQuota, persisted.Quota)
}

func TestAdjustTaskBillingRejectsInsufficientFiniteTokenAndRollsBackWallet(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 120, 120, 120
	const initialUserQuota, initialTokenQuota, reservedQuota, targetQuota = 1_000, 120, 100, 150
	const tokenKey = "sk-finite-token-settlement-limit"
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(targetQuota))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
	require.Nil(t, PreConsumeTaskBilling(ctx, task, reservedQuota, info))

	_, err := model.AdjustTaskBilling(context.Background(), task.ID, targetQuota)
	require.ErrorIs(t, err, model.ErrTaskBillingInsufficientToken)
	assert.EqualValues(t, initialUserQuota-reservedQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-reservedQuota, getTokenRemainQuota(t, tokenID))

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Equal(t, reservedQuota, persisted.Quota)
}

func TestTaskBillingUsesLockedUnlimitedTokenDespiteStaleFiniteSnapshot(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 121, 121, 121
	const initialUserQuota, reservedQuota, targetQuota = 1_000, 100, 150
	const tokenKey = "sk-unlimited-token-settlement"
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, tokenKey, 0)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("unlimited_quota", true).Error)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(targetQuota))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
	info.TokenUnlimited = false
	require.Nil(t, PreConsumeTaskBilling(ctx, task, reservedQuota, info))

	mutation, err := model.AdjustTaskBilling(context.Background(), task.ID, targetQuota)
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, targetQuota, mutation.CurrentQuota)
	assert.EqualValues(t, initialUserQuota-targetQuota, getUserQuota(t, userID))
	assert.Equal(t, -targetQuota, getTokenRemainQuota(t, tokenID))
}

func TestDurableTaskBillingAuditRecoversAfterLogWriteFailureExactlyOnce(t *testing.T) {
	testCases := []struct {
		name              string
		reservedQuota     int
		targetQuota       int
		refund            bool
		expectedLogType   int
		expectedLogQuota  int
		expectedTaskQuota int
	}{
		{name: "refund", reservedQuota: 100, refund: true, expectedLogType: model.LogTypeRefund, expectedLogQuota: 100},
		{name: "positive settlement", reservedQuota: 100, targetQuota: 150, expectedLogType: model.LogTypeConsume, expectedLogQuota: 50, expectedTaskQuota: 150},
		{name: "negative settlement", reservedQuota: 150, targetQuota: 100, expectedLogType: model.LogTypeRefund, expectedLogQuota: 50, expectedTaskQuota: 100},
	}

	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			truncate(t)
			useSeparateTaskBillingLogDB(t)

			userID := 130 + index
			tokenID := 130 + index
			channelID := 130 + index
			const initialQuota = 1_000
			tokenKey := fmt.Sprintf("sk-task-billing-audit-%d", index)
			seedUser(t, userID, initialQuota)
			seedToken(t, tokenID, userID, tokenKey, initialQuota)
			seedChannel(t, channelID)
			task := makeTask(userID, channelID, 0, tokenID, "", 0)
			task.PrivateData.AccountingState = model.TaskAccountingStatePending
			persistPendingTaskBilling(t, task, common.GetPointer(200))
			ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
			require.Nil(t, PreConsumeTaskBilling(ctx, task, testCase.reservedQuota, info))

			if testCase.refund {
				require.NoError(t, refundDurableTaskQuota(context.Background(), task, "submission rejected"))
			} else {
				RecalculateTaskQuota(context.Background(), task, testCase.targetQuota, "polling settlement")
			}

			assert.Zero(t, countLogs(t), "billing commit must not depend on a synchronous log write")
			var event model.KKAIOutboxEvent
			require.NoError(t, model.DB.Where("topic = ?", model.KKAIOutboxTopicTaskBillingAudit).Order("id DESC").First(&event).Error)

			userQuotaAfterCommit := getUserQuota(t, userID)
			tokenQuotaAfterCommit := getTokenRemainQuota(t, tokenID)
			var persisted model.Task
			require.NoError(t, model.DB.First(&persisted, task.ID).Error)
			assert.Equal(t, testCase.expectedTaskQuota, persisted.Quota)

			failFirstLogWrite := true
			callbackName := fmt.Sprintf("test:fail_task_billing_audit_log_%d", index)
			require.NoError(t, model.LOG_DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
				log, ok := tx.Statement.Dest.(*model.Log)
				if ok && failFirstLogWrite && log.Type == testCase.expectedLogType && log.Quota == testCase.expectedLogQuota {
					failFirstLogWrite = false
					tx.AddError(errors.New("forced task billing audit log failure"))
				}
			}))
			t.Cleanup(func() { _ = model.LOG_DB.Callback().Create().Remove(callbackName) })

			handler := TaskBillingAuditHandler{}
			require.Error(t, handler.Handle(context.Background(), event))
			assert.Zero(t, countLogs(t))
			assert.EqualValues(t, userQuotaAfterCommit, getUserQuota(t, userID))
			assert.Equal(t, tokenQuotaAfterCommit, getTokenRemainQuota(t, tokenID))

			require.NoError(t, handler.Handle(context.Background(), event))
			require.NoError(t, handler.Handle(context.Background(), event))
			assert.Equal(t, int64(1), countLogs(t))
			log := getLastLog(t)
			require.NotNil(t, log)
			assert.Equal(t, testCase.expectedLogType, log.Type)
			assert.Equal(t, testCase.expectedLogQuota, log.Quota)
			assert.NotEmpty(t, log.RequestId)
			assert.EqualValues(t, userQuotaAfterCommit, getUserQuota(t, userID))
			assert.Equal(t, tokenQuotaAfterCommit, getTokenRemainQuota(t, tokenID))
		})
	}
}

func TestTaskBillingAuditEnqueueFailureRollsBackSettlement(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 133, 133, 133
	const initialQuota, reservedQuota, targetQuota = 1_000, 100, 150
	const tokenKey = "sk-task-billing-audit-enqueue-failure"
	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(targetQuota))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
	require.Nil(t, PreConsumeTaskBilling(ctx, task, reservedQuota, info))

	callbackName := "test:fail_task_billing_audit_enqueue"
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		event, ok := tx.Statement.Dest.(*model.KKAIOutboxEvent)
		if ok && event.Topic == model.KKAIOutboxTopicTaskBillingAudit {
			tx.AddError(errors.New("forced task billing audit enqueue failure"))
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Create().Remove(callbackName) })

	mutation, err := model.AdjustTaskBillingWithAudit(context.Background(), task.ID, targetQuota, model.TaskBillingAuditRequest{
		Reason: "polling settlement",
	})
	require.ErrorContains(t, err, "forced task billing audit enqueue failure")
	assert.Nil(t, mutation)
	assert.EqualValues(t, initialQuota-reservedQuota, getUserQuota(t, userID))
	assert.Equal(t, initialQuota-reservedQuota, getTokenRemainQuota(t, tokenID))

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Equal(t, reservedQuota, persisted.Quota)
	assert.Equal(t, model.TaskBillingStateReserved, persisted.PrivateData.BillingState)
	assert.Equal(t, int64(1), persisted.PrivateData.BillingRevision)

	var auditEvents int64
	require.NoError(t, model.DB.Model(&model.KKAIOutboxEvent{}).
		Where("topic = ?", model.KKAIOutboxTopicTaskBillingAudit).
		Count(&auditEvents).Error)
	assert.Zero(t, auditEvents)
}

func TestConcurrentTaskSettlementAllowsOnlyOneConsumerOfRemainingQuota(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 122, 122, 122
	const initialQuota, reservedQuota, targetQuota = 250, 100, 150
	const tokenKey = "sk-concurrent-settlement-limit"
	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialQuota)
	seedChannel(t, channelID)

	tasks := make([]*model.Task, 2)
	for index := range tasks {
		task := makeTask(userID, channelID, 0, tokenID, "", 0)
		task.TaskID = fmt.Sprintf("task_concurrent_settlement_%d", index)
		persistPendingTaskBilling(t, task, common.GetPointer(targetQuota))
		ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
		require.Nil(t, PreConsumeTaskBilling(ctx, task, reservedQuota, info))
		tasks[index] = task
	}

	results := make(chan error, len(tasks))
	for _, task := range tasks {
		go func(taskID int64) {
			_, err := model.AdjustTaskBilling(context.Background(), taskID, targetQuota)
			results <- err
		}(task.ID)
	}

	successes := 0
	failures := 0
	for range tasks {
		err := <-results
		if err == nil {
			successes++
			continue
		}
		require.ErrorIs(t, err, model.ErrTaskBillingInsufficientWallet)
		failures++
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, failures)
	assert.Zero(t, getUserQuota(t, userID))
	assert.Zero(t, getTokenRemainQuota(t, tokenID))

	var persisted []model.Task
	require.NoError(t, model.DB.Where("id IN ?", []int64{tasks[0].ID, tasks[1].ID}).Find(&persisted).Error)
	require.Len(t, persisted, 2)
	assert.Equal(t, initialQuota, persisted[0].Quota+persisted[1].Quota)
}

func TestTaskBillingMutationsEnqueueMonotonicCacheReconcileEvents(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 123, 123, 123
	const initialQuota, reservedQuota, settledQuota = 1_000, 100, 150
	const tokenKey = "sk-cache-reconcile-revisions"
	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(settledQuota))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
	require.Nil(t, PreConsumeTaskBilling(ctx, task, reservedQuota, info))
	task.Status = model.TaskStatusSubmitted
	task.PrivateData.BillingState = model.TaskBillingStateAccepted
	task.PrivateData.TargetQuota = common.GetPointer(settledQuota)
	require.NoError(t, task.Update())

	_, err := model.AdjustTaskBilling(context.Background(), task.ID, settledQuota)
	require.NoError(t, err)
	_, err = model.RefundTaskBilling(context.Background(), task.ID)
	require.ErrorIs(t, err, model.ErrTaskBillingRefundNotAllowed)

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	persisted.Status = model.TaskStatusFailure
	require.NoError(t, persisted.Update())
	_, err = model.RefundTaskBilling(context.Background(), task.ID)
	require.NoError(t, err)
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)

	var events []model.KKAIOutboxEvent
	require.NoError(t, model.DB.Where("topic = ?", model.KKAIOutboxTopicTaskBillingCacheReconcile).Order("id ASC").Find(&events).Error)
	require.Len(t, events, 3)
	assert.Equal(t, int64(3), persisted.PrivateData.BillingRevision)
	for index, event := range events {
		assert.Contains(t, event.EventKey, fmt.Sprintf(":%d", index+1))
		var payload model.TaskBillingCacheReconcilePayload
		require.NoError(t, common.UnmarshalJsonStr(event.Payload, &payload))
		assert.Equal(t, task.ID, payload.TaskID)
	}
}

func TestOldCacheReconcileEventReadsCurrentDBStateAfterRefund(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 124, 124, 124
	const initialQuota, reservedQuota = 1_000, 100
	const tokenKey = "sk-cache-reconcile-crash"
	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(reservedQuota))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
	require.Nil(t, PreConsumeTaskBilling(ctx, task, reservedQuota, info))
	require.NoError(t, refundDurableTaskQuota(context.Background(), task, "submission rejected"))

	var oldest model.KKAIOutboxEvent
	require.NoError(t, model.DB.Where("topic = ?", model.KKAIOutboxTopicTaskBillingCacheReconcile).Order("id ASC").First(&oldest).Error)
	observedCalls := 0
	handler := TaskBillingCacheReconcileHandler{
		reconcile: func(_ context.Context, taskID int64) error {
			observedCalls++
			assert.Equal(t, task.ID, taskID)
			assert.EqualValues(t, initialQuota, getUserQuota(t, userID))
			assert.Equal(t, initialQuota, getTokenRemainQuota(t, tokenID))
			return nil
		},
	}
	require.NoError(t, handler.Handle(context.Background(), oldest))
	require.NoError(t, handler.Handle(context.Background(), oldest))
	assert.Equal(t, 2, observedCalls)
}

func TestAcceptedSettlementRecoveryAccountsSuccessfulRequestExactlyOnce(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 125, 125, 125
	const initialQuota, reservedQuota, targetQuota = 1_000, 100, 150
	const tokenKey = "sk-accounting-recovery"
	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, tokenKey, initialQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	task.PrivateData.AccountingState = model.TaskAccountingStatePending
	task.PrivateData.AccountingContext = &model.TaskAccountingContext{RequestPath: "/v1/videos"}
	persistPendingTaskBilling(t, task, common.GetPointer(targetQuota))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
	require.Nil(t, PreConsumeTaskBilling(ctx, task, reservedQuota, info))
	_, claimed, err := model.ClaimTaskSubmission(context.Background(), task.ID, nil)
	require.NoError(t, err)
	require.True(t, claimed)
	_, accepted, err := model.PersistTaskSubmissionAcceptance(context.Background(), task.ID, model.TaskSubmissionAcceptance{
		UpstreamTaskID: "upstream-accounting-recovery",
		TaskData:       json.RawMessage(`{"id":"upstream-accounting-recovery"}`),
		Status:         model.TaskStatusSubmitted,
		Progress:       "10%",
		TargetQuota:    targetQuota,
	})
	require.NoError(t, err)
	require.True(t, accepted)

	failFirstSettlement := true
	callbackName := "test:fail_first_accounting_settlement"
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		updatedTask, ok := tx.Statement.Dest.(*model.Task)
		if ok && failFirstSettlement && updatedTask.PrivateData.BillingState == model.TaskBillingStateAccepted && updatedTask.PrivateData.TargetQuota == nil {
			failFirstSettlement = false
			tx.AddError(errors.New("forced first settlement failure"))
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Update().Remove(callbackName) })

	require.Error(t, info.Billing.Settle(targetQuota))
	assert.Zero(t, countLogs(t))

	recoveryPayload, err := common.Marshal(TaskBillingRecoveryPayload{TaskID: task.ID})
	require.NoError(t, err)
	err = (TaskBillingRecoveryHandler{}).Handle(context.Background(), model.KKAIOutboxEvent{Payload: string(recoveryPayload)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepted task")

	accountingPayload, err := common.Marshal(TaskAccountingPayload{TaskID: task.ID})
	require.NoError(t, err)
	accountingEvent := model.KKAIOutboxEvent{Payload: string(accountingPayload)}
	handler := TaskAccountingHandler{}
	require.NoError(t, handler.Handle(context.Background(), accountingEvent))
	require.NoError(t, handler.Handle(context.Background(), accountingEvent))

	var user model.User
	require.NoError(t, model.DB.Select("used_quota", "request_count").First(&user, userID).Error)
	assert.EqualValues(t, targetQuota, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	var channel model.Channel
	require.NoError(t, model.DB.Select("used_quota").First(&channel, channelID).Error)
	assert.EqualValues(t, targetQuota, channel.UsedQuota)
	assert.Equal(t, int64(1), countLogs(t))
}

func TestRecalculateTaskQuotaByTokensUsesFrozenRatios(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 105, 105, 105
	const initialUserQuota, initialTokenQuota, reservedQuota = 10_000, 8_000, 100
	const tokenKey = "sk-frozen-ratios"
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	task.PrivateData.BillingContext.ModelRatio = 2
	task.PrivateData.BillingContext.GroupRatio = 3
	task.PrivateData.BillingContext.OtherRatios = map[string]float64{"duration": 2}
	persistPendingTaskBilling(t, task, common.GetPointer(1_000))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
	require.Nil(t, PreConsumeTaskBilling(ctx, task, reservedQuota, info))

	RecalculateTaskQuotaByTokens(context.Background(), task, 10)

	const expectedQuota = 120
	assert.EqualValues(t, initialUserQuota-expectedQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-expectedQuota, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, expectedQuota, task.Quota)
}

func TestTaskBillingRecoveryRefundsReservedCrashExactlyOnce(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 106, 106, 106
	const initialUserQuota, initialTokenQuota, reservedQuota = 10_000, 8_000, 900
	const tokenKey = "sk-recovery-reserved"
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(1_000))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
	require.Nil(t, PreConsumeTaskBilling(ctx, task, reservedQuota, info))

	payload, err := common.Marshal(TaskBillingRecoveryPayload{TaskID: task.ID})
	require.NoError(t, err)
	event := model.KKAIOutboxEvent{Payload: string(payload)}
	handler := TaskBillingRecoveryHandler{}
	require.NoError(t, handler.Handle(context.Background(), event))
	require.NoError(t, handler.Handle(context.Background(), event))

	assert.EqualValues(t, initialUserQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, persisted.Status)
	assert.Equal(t, model.TaskBillingStateRefunded, persisted.PrivateData.BillingState)
	assert.Zero(t, persisted.Quota)
	deliverTaskBillingAuditEvents(t)
	assert.Equal(t, int64(1), countLogs(t))
}

func TestTaskBillingRecoveryCompletesPendingTaskWithoutRefund(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 115, 115, 115
	const initialUserQuota, initialTokenQuota = 10_000, 8_000
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, "sk-pending-recovery", initialTokenQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(1_000))

	payload, err := common.Marshal(TaskBillingRecoveryPayload{TaskID: task.ID})
	require.NoError(t, err)
	require.NoError(t, (TaskBillingRecoveryHandler{}).Handle(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)}))

	assert.EqualValues(t, initialUserQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, persisted.Status)
	assert.Equal(t, model.TaskBillingStateCompleted, persisted.PrivateData.BillingState)
	assert.Zero(t, persisted.Quota)
	assert.Zero(t, countLogs(t))
}

func TestTaskBillingRecoveryDoesNotRefundDispatchingCrash(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 107, 107, 107
	const initialUserQuota, initialTokenQuota, reservedQuota = 10_000, 8_000, 900
	const tokenKey = "sk-recovery-dispatching"
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	task.PrivateData.AccountingState = model.TaskAccountingStatePending
	task.PrivateData.AccountingContext = &model.TaskAccountingContext{RequestPath: "/v1/videos"}
	persistPendingTaskBilling(t, task, common.GetPointer(1_000))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
	require.Nil(t, PreConsumeTaskBilling(ctx, task, reservedQuota, info))
	claimedTask, claimed, err := model.ClaimTaskSubmission(context.Background(), task.ID, nil)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotNil(t, claimedTask)

	payload, err := common.Marshal(TaskBillingRecoveryPayload{TaskID: task.ID})
	require.NoError(t, err)
	require.NoError(t, (TaskBillingRecoveryHandler{}).Handle(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)}))

	assert.EqualValues(t, initialUserQuota-reservedQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-reservedQuota, getTokenRemainQuota(t, tokenID))
	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusUnknown, persisted.Status)
	assert.Equal(t, model.TaskBillingStateCompleted, persisted.PrivateData.BillingState)
	assert.True(t, persisted.PrivateData.AccountingRequired)
	assert.Equal(t, reservedQuota, persisted.Quota)

	accountingPayload, err := common.Marshal(TaskAccountingPayload{TaskID: task.ID})
	require.NoError(t, err)
	accountingEvent := model.KKAIOutboxEvent{Payload: string(accountingPayload)}
	accountingHandler := TaskAccountingHandler{}
	require.NoError(t, accountingHandler.Handle(context.Background(), accountingEvent))
	require.NoError(t, accountingHandler.Handle(context.Background(), accountingEvent))

	var user model.User
	require.NoError(t, model.DB.Select("used_quota", "request_count").First(&user, userID).Error)
	assert.EqualValues(t, reservedQuota, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	var channel model.Channel
	require.NoError(t, model.DB.Select("used_quota").First(&channel, channelID).Error)
	assert.EqualValues(t, reservedQuota, channel.UsedQuota)
	assert.Equal(t, int64(1), countLogs(t))
}

func TestTaskBillingRecoveryRetriesCompletionAfterAmbiguousStatusPersists(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 117, 117, 117
	const initialUserQuota, initialTokenQuota, reservedQuota = 10_000, 8_000, 900
	const tokenKey = "sk-ambiguous-recovery-retry"
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(1_000))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
	require.Nil(t, PreConsumeTaskBilling(ctx, task, reservedQuota, info))
	_, claimed, err := model.ClaimTaskSubmission(context.Background(), task.ID, nil)
	require.NoError(t, err)
	require.True(t, claimed)

	failCompletion := true
	callbackName := "test:fail_first_ambiguous_billing_completion"
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		updatedTask, ok := tx.Statement.Dest.(*model.Task)
		if ok && failCompletion && updatedTask.PrivateData.BillingState == model.TaskBillingStateCompleted {
			failCompletion = false
			tx.AddError(errors.New("forced billing completion failure"))
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Update().Remove(callbackName) })

	payload, err := common.Marshal(TaskBillingRecoveryPayload{TaskID: task.ID})
	require.NoError(t, err)
	handler := TaskBillingRecoveryHandler{}
	require.Error(t, handler.Handle(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)}))
	require.NoError(t, handler.Handle(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)}))

	assert.EqualValues(t, initialUserQuota-reservedQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-reservedQuota, getTokenRemainQuota(t, tokenID))
	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusUnknown, persisted.Status)
	assert.Equal(t, "100%", persisted.Progress)
	assert.Equal(t, model.TaskBillingStateCompleted, persisted.PrivateData.BillingState)
	assert.Equal(t, reservedQuota, persisted.Quota)
	assert.Zero(t, countLogs(t))
}

func TestTaskBillingRecoveryDefersAcceptedUnknownWithUpstreamID(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 108, 108, 108
	const initialUserQuota, initialTokenQuota, reservedQuota = 10_000, 8_000, 900
	const tokenKey = "sk-recovery-accepted"
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(1_000))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
	require.Nil(t, PreConsumeTaskBilling(ctx, task, reservedQuota, info))
	claimedTask, claimed, err := model.ClaimTaskSubmission(context.Background(), task.ID, nil)
	require.NoError(t, err)
	require.True(t, claimed)
	claimedTask.Status = model.TaskStatusUnknown
	claimedTask.Progress = "10%"
	claimedTask.PrivateData.BillingState = model.TaskBillingStateAccepted
	claimedTask.PrivateData.UpstreamTaskID = "upstream-recoverable"
	require.NoError(t, claimedTask.Update())

	payload, err := common.Marshal(TaskBillingRecoveryPayload{TaskID: task.ID})
	require.NoError(t, err)
	err = (TaskBillingRecoveryHandler{}).Handle(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepted task")

	assert.EqualValues(t, initialUserQuota-reservedQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-reservedQuota, getTokenRemainQuota(t, tokenID))
	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusUnknown, persisted.Status)
	assert.Equal(t, model.TaskBillingStateAccepted, persisted.PrivateData.BillingState)
	assert.Equal(t, "10%", persisted.Progress)
	assert.Zero(t, countLogs(t))
}

func TestTaskBillingRecoveryRefundsAcceptedFailureBeforePendingSettlement(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 116, 116, 116
	const initialUserQuota, initialTokenQuota, reservedQuota, targetQuota = 10_000, 8_000, 100, 150
	const tokenKey = "sk-accepted-failure-recovery"
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, tokenKey, initialTokenQuota)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(targetQuota))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
	require.Nil(t, PreConsumeTaskBilling(ctx, task, reservedQuota, info))
	claimedTask, claimed, err := model.ClaimTaskSubmission(context.Background(), task.ID, nil)
	require.NoError(t, err)
	require.True(t, claimed)
	claimedTask.Status = model.TaskStatusFailure
	claimedTask.Progress = "100%"
	claimedTask.FailReason = "upstream rejected the task"
	claimedTask.PrivateData.BillingState = model.TaskBillingStateAccepted
	claimedTask.PrivateData.UpstreamTaskID = "upstream-failed"
	claimedTask.PrivateData.TargetQuota = common.GetPointer(targetQuota)
	require.NoError(t, claimedTask.Update())

	payload, err := common.Marshal(TaskBillingRecoveryPayload{TaskID: task.ID})
	require.NoError(t, err)
	require.NoError(t, (TaskBillingRecoveryHandler{}).Handle(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)}))

	assert.EqualValues(t, initialUserQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, persisted.Status)
	assert.Equal(t, model.TaskBillingStateRefunded, persisted.PrivateData.BillingState)
	assert.Zero(t, persisted.Quota)
	assert.Nil(t, persisted.PrivateData.TargetQuota)
	deliverTaskBillingAuditEvents(t)
	assert.Equal(t, int64(1), countLogs(t))
}

func TestClaimTaskSubmissionAllowsOnlyOneConcurrentDispatcher(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 109, 109, 109
	const tokenKey = "sk-single-dispatcher"
	seedUser(t, userID, 10_000)
	seedToken(t, tokenID, userID, tokenKey, 8_000)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 0, tokenID, "", 0)
	persistPendingTaskBilling(t, task, common.GetPointer(1_000))
	ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
	require.Nil(t, PreConsumeTaskBilling(ctx, task, 900, info))

	claims := make(chan bool, 2)
	errorsCh := make(chan error, 2)
	for range 2 {
		go func() {
			_, claimed, err := model.ClaimTaskSubmission(context.Background(), task.ID, nil)
			claims <- claimed
			errorsCh <- err
		}()
	}
	firstClaim := <-claims
	secondClaim := <-claims
	require.NoError(t, <-errorsCh)
	require.NoError(t, <-errorsCh)
	assert.NotEqual(t, firstClaim, secondClaim)

	require.NoError(t, model.ResetTaskSubmissionClaim(context.Background(), task.ID))
	_, claimed, err := model.ClaimTaskSubmission(context.Background(), task.ID, nil)
	require.NoError(t, err)
	assert.True(t, claimed)
}

// ---------------------------------------------------------------------------
// Read-back helpers
// ---------------------------------------------------------------------------

func getUserQuota(t *testing.T, id int) int64 {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", id).First(&user).Error)
	return user.Quota
}

func getTokenRemainQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota").Where("id = ?", id).First(&token).Error)
	return token.RemainQuota
}

func getTokenUsedQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("used_quota").Where("id = ?", id).First(&token).Error)
	return token.UsedQuota
}

func getSubscriptionUsed(t *testing.T, id int) int64 {
	t.Helper()
	var sub model.UserSubscription
	require.NoError(t, model.DB.Select("amount_used").Where("id = ?", id).First(&sub).Error)
	return sub.AmountUsed
}

func getTaskQuota(t *testing.T, id int64) int {
	t.Helper()
	var task model.Task
	require.NoError(t, model.DB.Select("quota").Where("id = ?", id).First(&task).Error)
	return task.Quota
}

func getLastLog(t *testing.T) *model.Log {
	t.Helper()
	var log model.Log
	err := model.LOG_DB.Order("id desc").First(&log).Error
	if err != nil {
		return nil
	}
	return &log
}

func countLogs(t *testing.T) int64 {
	t.Helper()
	var count int64
	model.LOG_DB.Model(&model.Log{}).Count(&count)
	return count
}

func deliverTaskBillingAuditEvents(t *testing.T) {
	t.Helper()
	var events []model.KKAIOutboxEvent
	require.NoError(t, model.DB.
		Where("topic = ?", model.KKAIOutboxTopicTaskBillingAudit).
		Order("id ASC").
		Find(&events).Error)

	handler := TaskBillingAuditHandler{}
	for _, event := range events {
		require.NoError(t, handler.Handle(context.Background(), event))
	}
}

func useSeparateTaskBillingLogDB(t *testing.T) {
	t.Helper()
	logDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := logDB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, logDB.AutoMigrate(&model.Log{}))

	previousLogDB := model.LOG_DB
	model.LOG_DB = logDB
	t.Cleanup(func() {
		model.LOG_DB = previousLogDB
		require.NoError(t, sqlDB.Close())
	})
}

// ===========================================================================
// RefundTaskQuota tests
// ===========================================================================

func TestRefundTaskQuota_Wallet(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1, 1, 1
	const initQuota, preConsumed = 10000, 3000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-test-key", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	assert.True(t, RefundTaskQuota(ctx, task, "task failed: upstream error"))

	// User quota should increase by preConsumed
	assert.EqualValues(t, initQuota+preConsumed, getUserQuota(t, userID))

	// Token remain_quota should increase, used_quota should decrease
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, -preConsumed, getTokenUsedQuota(t, tokenID))

	// A refund log should be created
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed, log.Quota)
	assert.Equal(t, "test-model", log.ModelName)
	assert.Zero(t, task.Quota)
	assert.Zero(t, getTaskQuota(t, task.ID))
}

func TestRefundTaskQuota_Subscription(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 2, 2, 2, 1
	const preConsumed = 2000
	const subTotal, subUsed int64 = 100000, 50000
	const tokenRemain = 8000

	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "sk-sub-key", tokenRemain)
	seedChannel(t, channelID)
	seedSubscription(t, subID, userID, subTotal, subUsed)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceSubscription, subID)
	require.NoError(t, model.DB.Create(task).Error)

	assert.True(t, RefundTaskQuota(ctx, task, "subscription task failed"))

	// Subscription used should decrease by preConsumed
	assert.Equal(t, subUsed-int64(preConsumed), getSubscriptionUsed(t, subID))

	// Token should also be refunded
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Zero(t, getTaskQuota(t, task.ID))
}

func TestRefundTaskQuota_ZeroQuota(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 3
	seedUser(t, userID, 5000)

	task := makeTask(userID, 0, 0, 0, BillingSourceWallet, 0)

	assert.True(t, RefundTaskQuota(ctx, task, "zero quota task"))

	// No change to user quota
	assert.Equal(t, int64(5000), getUserQuota(t, userID))

	// No log created
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRefundTaskQuota_NoToken(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 4, 4
	const initQuota, preConsumed = 10000, 1500

	seedUser(t, userID, initQuota)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0) // TokenId=0
	require.NoError(t, model.DB.Create(task).Error)

	assert.True(t, RefundTaskQuota(ctx, task, "no token task failed"))

	// User quota refunded
	assert.EqualValues(t, initQuota+preConsumed, getUserQuota(t, userID))

	// Log created
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Zero(t, getTaskQuota(t, task.ID))
}

func TestRefundTaskQuota_FundingFailureKeepsPendingMarker(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, preConsumed = 5, 1200
	seedUser(t, userID, 5000)
	task := makeTask(userID, 0, preConsumed, 0, BillingSourceSubscription, 9999)
	task.Status = model.TaskStatusFailure
	require.NoError(t, model.DB.Create(task).Error)

	assert.False(t, RefundTaskQuota(ctx, task, "subscription missing"))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, preConsumed, getTaskQuota(t, task.ID))
	assert.Equal(t, int64(0), countLogs(t))
}

// ===========================================================================
// RecalculateTaskQuota tests
// ===========================================================================

func TestRecalculate_PositiveDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 10, 10, 10
	const initQuota, preConsumed = 10000, 2000
	const actualQuota = 3000 // under-charged by 1000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-pos", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment")

	// User quota should decrease by the delta (1000 additional charge)
	assert.EqualValues(t, initQuota-(actualQuota-preConsumed), getUserQuota(t, userID))

	// Token should also be charged the delta
	assert.Equal(t, tokenRemain-(actualQuota-preConsumed), getTokenRemainQuota(t, tokenID))

	// task.Quota should be updated to actualQuota
	assert.Equal(t, actualQuota, task.Quota)

	// Log type should be Consume (additional charge)
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeConsume, log.Type)
	assert.Equal(t, actualQuota-preConsumed, log.Quota)
}

func TestRecalculate_NegativeDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 11, 11, 11
	const initQuota, preConsumed = 10000, 5000
	const actualQuota = 3000 // over-charged by 2000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-neg", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment")

	// User quota should increase by abs(delta) = 2000 (refund overpayment)
	assert.EqualValues(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))

	// Token should be refunded the difference
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))

	// task.Quota updated
	assert.Equal(t, actualQuota, task.Quota)

	// Log type should be Refund
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed-actualQuota, log.Quota)
}

func TestRecalculate_ZeroDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 12
	const initQuota, preConsumed = 10000, 3000

	seedUser(t, userID, initQuota)

	task := makeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, preConsumed, "exact match")

	// No change to user quota
	assert.EqualValues(t, initQuota, getUserQuota(t, userID))

	// No log created (delta is zero)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRecalculate_ActualQuotaZero(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 13
	const initQuota = 10000

	seedUser(t, userID, initQuota)

	task := makeTask(userID, 0, 5000, 0, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, 0, "zero actual")

	// No change (early return)
	assert.EqualValues(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRecalculate_Subscription_NegativeDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 14, 14, 14, 2
	const preConsumed = 5000
	const actualQuota = 2000 // over-charged by 3000
	const subTotal, subUsed int64 = 100000, 50000
	const tokenRemain = 8000

	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "sk-sub-recalc", tokenRemain)
	seedChannel(t, channelID)
	seedSubscription(t, subID, userID, subTotal, subUsed)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceSubscription, subID)

	RecalculateTaskQuota(ctx, task, actualQuota, "subscription over-charge")

	// Subscription used should decrease by delta (refund 3000)
	assert.Equal(t, subUsed-int64(preConsumed-actualQuota), getSubscriptionUsed(t, subID))

	// Token refunded
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))

	assert.Equal(t, actualQuota, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

// ===========================================================================
// CAS + Billing integration tests
// Simulates the flow in updateVideoSingleTask (service/task_polling.go)
// ===========================================================================

// simulatePollBilling reproduces the CAS + billing logic from updateVideoSingleTask.
// It takes a persisted task (already in DB), applies the new status, and performs
// the conditional update + billing exactly as the polling loop does.
func simulatePollBilling(ctx context.Context, task *model.Task, newStatus model.TaskStatus, actualQuota int) {
	snap := task.Snapshot()

	shouldRefund := false
	shouldSettle := false
	quota := task.Quota

	task.Status = newStatus
	switch string(newStatus) {
	case model.TaskStatusSuccess:
		task.Progress = "100%"
		task.FinishTime = 9999
		shouldSettle = true
	case model.TaskStatusFailure:
		task.Progress = "100%"
		task.FinishTime = 9999
		task.FailReason = "upstream error"
		if quota != 0 {
			shouldRefund = true
		}
	default:
		task.Progress = "50%"
	}

	isDone := task.Status == model.TaskStatus(model.TaskStatusSuccess) || task.Status == model.TaskStatus(model.TaskStatusFailure)
	if isDone && snap.Status != task.Status {
		won, err := task.UpdateWithStatus(snap.Status)
		if err != nil {
			shouldRefund = false
			shouldSettle = false
		} else if !won {
			shouldRefund = false
			shouldSettle = false
		}
	} else if !snap.Equal(task.Snapshot()) {
		_, _ = task.UpdateWithStatus(snap.Status)
	}

	if shouldSettle && actualQuota > 0 {
		RecalculateTaskQuota(ctx, task, actualQuota, "test settle")
	}
	if shouldRefund {
		RefundTaskQuota(ctx, task, task.FailReason)
	}
}

func TestCASGuardedRefund_Win(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 20, 20, 20
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 6000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-refund-win", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusFailure), 0)

	// CAS wins: task in DB should now be FAILURE
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloaded.Status)
	assert.Zero(t, reloaded.Quota)

	// Refund should have happened
	assert.EqualValues(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

func TestCASGuardedRefund_Lose(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 21, 21, 21
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 6000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-refund-lose", tokenRemain)
	seedChannel(t, channelID)

	// Create task with IN_PROGRESS in DB
	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	// Simulate another process already transitioning to FAILURE
	model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", model.TaskStatusFailure)

	// Our process still has the old in-memory state (IN_PROGRESS) and tries to transition
	// task.Status is still IN_PROGRESS in the snapshot
	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusFailure), 0)

	// CAS lost: user quota should NOT change (no double refund)
	assert.EqualValues(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))

	// No billing log should be created
	assert.Equal(t, int64(0), countLogs(t))
}

func TestCASGuardedSettle_Win(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 22, 22, 22
	const initQuota, preConsumed = 10000, 5000
	const actualQuota = 3000 // over-charged, should get partial refund
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-settle-win", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusSuccess), actualQuota)

	// CAS wins: task should be SUCCESS
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, reloaded.Status)

	// Settlement should refund the over-charge (5000 - 3000 = 2000 back to user)
	assert.EqualValues(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))

	// task.Quota should be updated to actualQuota
	assert.Equal(t, actualQuota, task.Quota)
}

func TestNonTerminalUpdate_NoBilling(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 23, 23
	const initQuota, preConsumed = 10000, 3000

	seedUser(t, userID, initQuota)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	task.Progress = "20%"
	require.NoError(t, model.DB.Create(task).Error)

	// Simulate a non-terminal poll update (still IN_PROGRESS, progress changed)
	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusInProgress), 0)

	// User quota should NOT change
	assert.EqualValues(t, initQuota, getUserQuota(t, userID))

	// No billing log
	assert.Equal(t, int64(0), countLogs(t))

	// Task progress should be updated in DB
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, "50%", reloaded.Progress)
}

// ===========================================================================
// Mock adaptor for settleTaskBillingOnComplete tests
// ===========================================================================

type mockAdaptor struct {
	adjustReturn int
}

func (m *mockAdaptor) Init(_ *relaycommon.RelayInfo) {}
func (m *mockAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return nil, nil
}
func (m *mockAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) { return nil, nil }
func (m *mockAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return m.adjustReturn
}

// ===========================================================================
// PerCallBilling tests — settleTaskBillingOnComplete
// ===========================================================================

func TestSettle_PerCallBilling_SkipsAdaptorAdjust(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 30, 30, 30
	const initQuota, preConsumed = 10000, 5000
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-percall-adaptor", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.PerCallBilling = true

	adaptor := &mockAdaptor{adjustReturn: 2000}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	// Per-call: no adjustment despite adaptor returning 2000
	assert.EqualValues(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_PerCallBilling_SkipsTotalTokens(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 31, 31, 31
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 7000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-percall-tokens", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.PerCallBilling = true

	adaptor := &mockAdaptor{adjustReturn: 0}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: 9999}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	// Per-call: no recalculation by tokens
	assert.EqualValues(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_NonPerCallBilling_AppliesAdaptorAdjustment(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 32, 32, 32
	const initQuota, preConsumed = 10000, 5000
	const adaptorQuota = 3000
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-nonpercall-adj", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	// PerCallBilling defaults to false

	adaptor := &mockAdaptor{adjustReturn: adaptorQuota}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	// Non-per-call: adaptor adjustment applies (refund 2000)
	assert.EqualValues(t, initQuota+(preConsumed-adaptorQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-adaptorQuota), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, adaptorQuota, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}
