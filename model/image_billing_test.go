package model

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestImageGenerationWalletBillingIsDurableAndIdempotent(t *testing.T) {
	db := newImageBillingTestDB(t)
	require.NoError(t, db.Create(&User{Id: 41, Username: "image-wallet", Password: "password", Quota: 1_000}).Error)
	generation := seedImageBillingGeneration(t, db, 41)

	reserved, err := ReserveImageGenerationBilling(context.Background(), db, generation.ID, TaskBillingSourceWallet, 300)
	require.NoError(t, err)
	require.True(t, reserved.Applied)
	reservedAgain, err := ReserveImageGenerationBilling(context.Background(), db, generation.ID, TaskBillingSourceWallet, 300)
	require.NoError(t, err)
	require.False(t, reservedAgain.Applied)
	require.NoError(t, MarkImageGenerationDispatching(context.Background(), db, generation.ID))
	prepareImageBillingAccounting(t, db, generation, 200, false)

	settled, err := SettleImageGenerationBilling(context.Background(), db, generation.ID, 200)
	require.NoError(t, err)
	require.True(t, settled.Applied)
	settledAgain, err := SettleImageGenerationBilling(context.Background(), db, generation.ID, 200)
	require.NoError(t, err)
	require.False(t, settledAgain.Applied)
	var user User
	require.NoError(t, db.First(&user, 41).Error)
	require.EqualValues(t, 800, user.Quota)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	require.Equal(t, ImageGenerationBillingStateSettled, generation.BillingState)
	require.Equal(t, 200, generation.ReservedQuota)
	require.Equal(t, 200, generation.FinalQuota)
}

func TestImageGenerationSettlementRequiresMatchingDurableAccounting(t *testing.T) {
	db := newImageBillingTestDB(t)
	require.NoError(t, db.Create(&User{Id: 42, Username: "image-intent", Password: "password", Quota: 1_000}).Error)
	generation := seedImageBillingGeneration(t, db, 42)
	_, err := ReserveImageGenerationBilling(context.Background(), db, generation.ID, TaskBillingSourceWallet, 300)
	require.NoError(t, err)
	require.Error(t, func() error {
		_, settleErr := SettleImageGenerationBilling(context.Background(), db, generation.ID, 200)
		return settleErr
	}())
	prepareImageBillingAccounting(t, db, generation, 200, false)
	_, err = SettleImageGenerationBilling(context.Background(), db, generation.ID, 201)
	require.ErrorIs(t, err, ErrImageBillingStateConflict)
	_, err = SettleImageGenerationBilling(context.Background(), db, generation.ID, 200)
	require.NoError(t, err)
}

func TestImageGenerationSettledBillingCannotBeRefunded(t *testing.T) {
	db := newImageBillingTestDB(t)
	require.NoError(t, db.Create(&User{Id: 45, Username: "image-no-refund", Password: "password", Quota: 1_000}).Error)
	require.NoError(t, db.Create(&Channel{Id: 1, Name: "image-no-refund", Key: "test-key", Status: 1, Type: 1}).Error)
	generation := seedImageBillingGeneration(t, db, 45)
	_, err := ReserveImageGenerationBilling(context.Background(), db, generation.ID, TaskBillingSourceWallet, 300)
	require.NoError(t, err)
	prepareImageBillingAccounting(t, db, generation, 200, true)
	_, err = SettleImageGenerationBilling(context.Background(), db, generation.ID, 200)
	require.NoError(t, err)

	_, err = RefundImageGenerationBilling(context.Background(), db, generation.ID)
	require.ErrorIs(t, err, ErrImageBillingStateConflict)

	var user User
	require.NoError(t, db.First(&user, 45).Error)
	require.EqualValues(t, 800, user.Quota)
	require.EqualValues(t, 200, user.UsedQuota)
	require.EqualValues(t, 1, user.RequestCount)
	var channel Channel
	require.NoError(t, db.First(&channel, 1).Error)
	require.EqualValues(t, 200, channel.UsedQuota)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	require.Equal(t, ImageGenerationBillingStateSettled, generation.BillingState)
	require.Equal(t, 200, generation.FinalQuota)
}

func TestImageGenerationRecoveryFenceRejectsOriginalWorker(t *testing.T) {
	db := newImageBillingTestDB(t)
	require.NoError(t, db.Create(&User{Id: 46, Username: "image-fence", Password: "password", Quota: 1_000}).Error)
	generation := seedImageBillingGeneration(t, db, 46)
	_, err := ReserveImageGenerationBilling(context.Background(), db, generation.ID, TaskBillingSourceWallet, 300)
	require.NoError(t, err)
	require.NoError(t, MarkImageGenerationDispatching(context.Background(), db, generation.ID))
	prepareImageBillingAccounting(t, db, generation, 200, false)
	require.NoError(t, db.Model(&KKAIImageGeneration{}).Where("id = ?", generation.ID).
		Update("status", ImageGenerationStatusRecovering).Error)
	accounting, err := GetImageGenerationAccounting(context.Background(), db, generation.ID)
	require.NoError(t, err)
	_, err = RecordImageGenerationAccountingLog(context.Background(), db, *accounting)
	require.ErrorIs(t, err, ErrImageAccountingNotReady)

	alive, err := TouchImageGeneration(context.Background(), db, generation.ID)
	require.NoError(t, err)
	require.False(t, alive)
	_, err = SettleImageGenerationBilling(context.Background(), db, generation.ID, 200)
	require.ErrorIs(t, err, ErrImageBillingStateConflict)
	_, err = RefundImageGenerationBilling(context.Background(), db, generation.ID)
	require.ErrorIs(t, err, ErrImageBillingStateConflict)

	_, err = SettleRecoveringImageGenerationBilling(context.Background(), db, generation.ID, 200)
	require.NoError(t, err)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	require.Equal(t, ImageGenerationStatusRecovering, generation.Status)
	require.Equal(t, ImageGenerationBillingStateSettled, generation.BillingState)
}

func TestImageGenerationSubscriptionBillingSettlesAgainstLockedLedger(t *testing.T) {
	db := newImageBillingTestDB(t)
	plan := SubscriptionPlan{Title: "Image plan", TotalAmount: 1_000, QuotaResetPeriod: SubscriptionResetNever}
	require.NoError(t, db.Create(&plan).Error)
	subscription := UserSubscription{
		UserId: 43, PlanId: plan.Id, AmountTotal: 1_000, AmountUsed: 100,
		StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(time.Hour).Unix(), Status: "active",
	}
	require.NoError(t, db.Create(&subscription).Error)
	generation := seedImageBillingGeneration(t, db, 43)

	reserved, err := ReserveImageGenerationBilling(
		context.Background(), db, generation.ID, TaskBillingSourceSubscription, 300,
	)
	require.NoError(t, err)
	require.Equal(t, subscription.Id, reserved.Generation.SubscriptionID)
	prepareImageBillingAccounting(t, db, generation, 200, false)
	_, err = SettleImageGenerationBilling(context.Background(), db, generation.ID, 200)
	require.NoError(t, err)

	require.NoError(t, db.First(&subscription, subscription.Id).Error)
	require.EqualValues(t, 300, subscription.AmountUsed)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	require.Equal(t, ImageGenerationBillingStateSettled, generation.BillingState)
	require.Equal(t, 200, generation.ReservedQuota)
}

func TestImageGenerationSettlementPersistsStatisticsAndIdempotentConsumeLog(t *testing.T) {
	db := newImageBillingTestDB(t)
	previousLogDB := LOG_DB
	LOG_DB = db
	t.Cleanup(func() { LOG_DB = previousLogDB })
	require.NoError(t, db.Create(&User{
		Id: 44, Username: "image-accounting", Password: "password",
		Quota: 1_000, UsedQuota: 5, RequestCount: 2,
	}).Error)
	require.NoError(t, db.Create(&Channel{
		Id: 9, Name: "image-channel", Key: "test-key", Status: 1, Type: 1, UsedQuota: 7,
	}).Error)
	generation := seedImageBillingGeneration(t, db, 44)
	_, err := ReserveImageGenerationBilling(
		context.Background(), db, generation.ID, TaskBillingSourceWallet, 300,
	)
	require.NoError(t, err)
	payload := ImageGenerationAccountingPayload{
		TargetQuota: 200, CountStatistics: true, Username: "image-accounting",
		LogParams: RecordConsumeLogParams{
			ChannelId: 9, PromptTokens: 10, CompletionTokens: 20,
			ModelName: generation.Model, TokenName: "image-token", TokenId: generation.TokenID,
			Quota: 200, Content: "image generation", Group: "image-group",
		},
	}
	require.NoError(t, PrepareImageGenerationAccounting(context.Background(), db, generation.ID, payload))
	stored, err := GetImageGenerationAccounting(context.Background(), db, generation.ID)
	require.NoError(t, err)

	_, err = SettleImageGenerationBilling(context.Background(), db, generation.ID, 200)
	require.NoError(t, err)
	_, err = SettleImageGenerationBilling(context.Background(), db, generation.ID, 200)
	require.NoError(t, err)
	var user User
	require.NoError(t, db.First(&user, 44).Error)
	require.EqualValues(t, 800, user.Quota)
	require.EqualValues(t, 205, user.UsedQuota)
	require.EqualValues(t, 3, user.RequestCount)
	var channel Channel
	require.NoError(t, db.First(&channel, 9).Error)
	require.EqualValues(t, 207, channel.UsedQuota)

	_, err = RecordImageGenerationAccountingLog(context.Background(), db, *stored)
	require.ErrorIs(t, err, ErrImageAccountingNotReady)
	require.NoError(t, db.Model(&KKAIImageGeneration{}).Where("id = ?", generation.ID).
		Updates(map[string]any{"status": ImageGenerationStatusSucceeded, "finished_at": time.Now().Unix()}).Error)
	created, err := RecordImageGenerationAccountingLog(context.Background(), db, *stored)
	require.NoError(t, err)
	require.True(t, created)
	created, err = RecordImageGenerationAccountingLog(context.Background(), db, *stored)
	require.NoError(t, err)
	require.False(t, created)
	var logs int64
	require.NoError(t, db.Model(&Log{}).Where(
		"request_id = ? AND type = ?", generation.RequestID, LogTypeConsume,
	).Count(&logs).Error)
	require.EqualValues(t, 1, logs)
}

func prepareImageBillingAccounting(
	t *testing.T, db *gorm.DB, generation KKAIImageGeneration, targetQuota int, countStatistics bool,
) {
	t.Helper()
	require.NoError(t, PrepareImageGenerationAccounting(
		context.Background(), db, generation.ID, ImageGenerationAccountingPayload{
			TargetQuota: targetQuota, CountStatistics: countStatistics,
			LogParams: RecordConsumeLogParams{
				ChannelId: 1, ModelName: generation.Model, TokenId: generation.TokenID, Quota: targetQuota,
			},
		},
	))
}

func newImageBillingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:image-billing-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&User{}, &Channel{}, &Log{}, &SubscriptionPlan{}, &UserSubscription{},
		&KKAIImageGeneration{}, &KKAIImageAsset{}, &KKAIOutboxEvent{},
	))
	return db
}

func seedImageBillingGeneration(t *testing.T, db *gorm.DB, userID int) KKAIImageGeneration {
	t.Helper()
	now := time.Now().Unix()
	generation := KKAIImageGeneration{
		UserID: userID, TokenID: 1, ModelProfileID: 1, SpecificationVersion: 1,
		Model: "gpt-image-1", Prompt: "prompt", Parameters: "{}",
		RequestHash: fmt.Sprintf("%064d", time.Now().UnixNano()),
		RequestID:   fmt.Sprintf("image-billing-%d", time.Now().UnixNano()),
		Status:      ImageGenerationStatusSubmitting, RequestedCount: 1,
		BillingState: ImageGenerationBillingStatePending,
		HeartbeatAt:  now, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&generation).Error)
	return generation
}
