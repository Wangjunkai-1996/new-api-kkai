package service

import (
	"context"
	"errors"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestImageAsyncFundingUsesAcceptanceTransaction(t *testing.T) {
	db := newImageAccountingRecoveryTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}, &model.UserSubscription{}))
	require.NoError(t, db.Create(&model.User{Id: 71, Username: "async-funding", Password: "password", Quota: 1000}).Error)
	generation := seedRecoverableImageGeneration(t, db, 71, 1)
	previousDB := model.DB
	model.DB = nil
	t.Cleanup(func() { model.DB = previousDB })
	abort := errors.New("acceptance rolled back")
	err := db.Transaction(func(tx *gorm.DB) error {
		mutation, err := ReserveImageGenerationBillingForUserTx(context.Background(), tx, generation.ID, 300, "subscription_first", false)
		require.NoError(t, err)
		assert.Equal(t, model.TaskBillingSourceWallet, mutation.Generation.BillingSource)
		return abort
	})
	require.ErrorIs(t, err, abort)
	var user model.User
	require.NoError(t, db.First(&user, 71).Error)
	assert.EqualValues(t, 1000, user.Quota)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	assert.Equal(t, model.ImageGenerationBillingStatePending, generation.BillingState)
}

func TestImageAsyncRestoreDoesNotReserveAndOldFenceCannotRefund(t *testing.T) {
	db := newImageAccountingRecoveryTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.KKAIJobLease{}, &model.KKAIImageExecution{}))
	require.NoError(t, db.Create(&model.User{Id: 72, Username: "async-restore", Password: "password", Quota: 1000}).Error)
	generation := seedRecoverableImageGeneration(t, db, 72, 1)
	_, err := model.ReserveImageGenerationBilling(context.Background(), db, generation.ID, model.TaskBillingSourceWallet, 300)
	require.NoError(t, err)
	now := time.Now().Unix()
	leader := model.KKAIJobLease{LeaseName: "background", Holder: "writer", Fence: 2, LeaseUntil: now + 300, UpdatedAt: now}
	require.NoError(t, db.Create(&leader).Error)
	event := model.KKAIOutboxEvent{EventKey: "async-bill-restore", Topic: "image.generate.v1", AggregateID: strconv.FormatInt(generation.ID, 10), Payload: "{}", Status: model.KKAIOutboxStatusPending, LockedAt: now, LockedBy: "claim-1"}
	require.NoError(t, db.Create(&event).Error)
	execution := model.KKAIImageExecution{GenerationID: generation.ID, State: model.ImageGenerationExecutionPreparing, Fence: event.LockedBy, EventID: event.ID, Payload: "{}", QueueDeadlineAt: now + 300, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&execution).Error)
	fence := model.ImageExecutionFence{EventID: event.ID, LockedBy: event.LockedBy, LockTimeout: 2 * time.Minute, LeaderName: leader.LeaseName, LeaderHolder: leader.Holder, LeaderFence: leader.Fence}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/internal/image-execution", nil)
	info := &relaycommon.RelayInfo{UserId: generation.UserID, TokenId: generation.TokenID}
	require.NoError(t, RestoreImageGenerationBilling(c, db, &generation, info, fence))
	require.NoError(t, RestoreImageGenerationBilling(c, db, &generation, info, fence))
	var user model.User
	require.NoError(t, db.First(&user, 72).Error)
	assert.EqualValues(t, 700, user.Quota)
	assert.Equal(t, 300, info.Billing.GetPreConsumedQuota())
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("id = ?", event.ID).Update("locked_by", "claim-2").Error)
	info.Billing.Refund(c)
	require.NoError(t, db.First(&user, 72).Error)
	assert.EqualValues(t, 700, user.Quota)
	require.ErrorIs(t, info.Billing.Settle(200), model.ErrImageExecutionFenceLost)
}

func TestImageIdempotencyRetainsActiveAndRecentTerminalBindings(t *testing.T) {
	db, _ := newImageSubmissionTestDB(t)
	generation := seedRecoverableImageGeneration(t, db, 73, 1)
	request := IdempotencyReservationRequest{UserID: 73, Operation: model.ImageIdempotencyOperationSubmit, Key: "accepted-image", RequestHash: generation.RequestHash}
	reservation, err := ReserveIdempotencyKey(context.Background(), db, request)
	require.NoError(t, err)
	require.NoError(t, BindIdempotencyResource(context.Background(), db, reservation.Record.ID, model.ImageIdempotencyResourceGeneration, strconv.FormatInt(generation.ID, 10)))
	now := time.Now()
	require.NoError(t, db.Model(&model.KKAIIdempotencyKey{}).Where("id = ?", reservation.Record.ID).Update("expires_at", now.Add(-time.Hour).Unix()).Error)
	replay, err := ReserveIdempotencyKey(context.Background(), db, request)
	require.NoError(t, err)
	assert.False(t, replay.Created)
	assert.Equal(t, reservation.Record.ID, replay.Record.ID)
	require.NoError(t, db.Model(&model.KKAIIdempotencyKey{}).Where("id = ?", reservation.Record.ID).Update("expires_at", now.Add(-time.Hour).Unix()).Error)
	deleted, err := CleanupExpiredIdempotencyKeys(context.Background(), db, now, 10)
	require.NoError(t, err)
	assert.Zero(t, deleted)
	require.NoError(t, db.Model(&model.KKAIImageGeneration{}).Where("id = ?", generation.ID).Updates(map[string]any{"status": model.ImageGenerationStatusSucceeded, "finished_at": now.Unix()}).Error)
	require.NoError(t, db.Model(&model.KKAIIdempotencyKey{}).Where("id = ?", reservation.Record.ID).Update("expires_at", now.Add(-time.Hour).Unix()).Error)
	deleted, err = CleanupExpiredIdempotencyKeys(context.Background(), db, now, 10)
	require.NoError(t, err)
	assert.Zero(t, deleted)
	deleted, err = CleanupExpiredIdempotencyKeys(context.Background(), db, now.Add(25*time.Hour), 10)
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)
}
