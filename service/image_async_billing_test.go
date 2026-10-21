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

func TestImageAsyncPolicyCompletionPreservesResultAndFence(t *testing.T) {
	if model.MembershipEnabled() {
		t.Skip("legacy fixture runs with membership disabled")
	}
	for _, tc := range []struct {
		name        string
		readyResult bool
		staleFence  bool
	}{
		{name: "confirmed_rejection_refunds"},
		{name: "durable_result_still_settles", readyResult: true},
		{name: "old_worker_cannot_refund", staleFence: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newImageAccountingRecoveryTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.KKAIImageGenerationReference{}))
			require.NoError(t, db.Create(&model.User{Id: 74, Username: "async-policy", Password: "password", Quota: 1000}).Error)
			generation := seedRecoverableImageGeneration(t, db, 74, 1)
			_, err := model.ReserveImageGenerationBilling(context.Background(), db, generation.ID, model.TaskBillingSourceWallet, 300)
			require.NoError(t, err)
			fence := imageExecutionFenceFixture(t, db, generation)
			require.NoError(t, db.Model(&model.KKAIImageExecution{}).Where("generation_id = ?", generation.ID).Update("state", model.ImageGenerationExecutionDispatching).Error)
			if tc.readyResult {
				require.NoError(t, model.PrepareImageGenerationAccounting(context.Background(), db, generation.ID, model.ImageGenerationAccountingPayload{TargetQuota: 200, OutputCount: 1, LogParams: model.RecordConsumeLogParams{ChannelId: 3, ModelName: generation.Model, TokenId: generation.TokenID, Quota: 200}}))
				require.NoError(t, db.Create(&model.KKAIImageAsset{GenerationID: &generation.ID, OwnerUserID: generation.UserID, Kind: model.ImageAssetKindOutput, State: model.ImageAssetStateReady, ObjectKey: "durable-policy-result"}).Error)
			}
			if tc.staleFence {
				require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("id = ?", fence.EventID).Update("locked_by", "new-worker").Error)
			}
			err = CompleteImageExecution(context.Background(), db, generation.ID, fence, ImageExecutionFailureContentPolicy)
			var user model.User
			if tc.staleFence {
				require.ErrorIs(t, err, model.ErrImageExecutionFenceLost)
				require.NoError(t, db.First(&user, generation.UserID).Error)
				assert.EqualValues(t, 700, user.Quota)
				return
			}
			require.NoError(t, err)
			require.NoError(t, db.First(&generation, generation.ID).Error)
			require.NoError(t, db.First(&user, generation.UserID).Error)
			if tc.readyResult {
				assert.Equal(t, model.ImageGenerationStatusSucceeded, generation.Status)
				assert.Equal(t, model.ImageGenerationBillingStateSettled, generation.BillingState)
				assert.EqualValues(t, 800, user.Quota)
			} else {
				assert.Equal(t, model.ImageGenerationStatusFailed, generation.Status)
				assert.Equal(t, "upstream", generation.FailureStage)
				assert.Equal(t, model.ImageGenerationBillingStateRefunded, generation.BillingState)
				assert.EqualValues(t, 1000, user.Quota)
			}
		})
	}
}

func TestImageAsyncUnknownErrorClassificationPreservesBillingAndFence(t *testing.T) {
	if model.MembershipEnabled() {
		t.Skip("legacy fixture runs with membership disabled")
	}
	for _, tc := range []struct {
		name, failureCode, wantCode string
		readyResult, staleFence     bool
	}{
		{name: "recovery_retains_public_reason", failureCode: "image_service_unavailable", wantCode: "image_service_unavailable"},
		{name: "unconfirmed_policy_does_not_become_failed", failureCode: ImageExecutionFailureContentPolicy, wantCode: ImageExecutionFailureContentPolicy},
		{name: "unknown_raw_code_is_not_saved", failureCode: "https://advert.example secret", wantCode: ""},
		{name: "durable_result_clears_prior_reason", failureCode: "image_service_unavailable", wantCode: "image_service_unavailable", readyResult: true},
		{name: "stale_worker_cannot_save_reason", failureCode: "image_request_timeout", staleFence: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newImageAccountingRecoveryTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.KKAIImageGenerationReference{}))
			require.NoError(t, db.Create(&model.User{Id: 78, Username: "async-public-error", Password: "password", Quota: 1000}).Error)
			generation := seedRecoverableImageGeneration(t, db, 78, 1)
			_, err := model.ReserveImageGenerationBilling(context.Background(), db, generation.ID, model.TaskBillingSourceWallet, 300)
			require.NoError(t, err)
			fence := imageExecutionFenceFixture(t, db, generation)
			require.NoError(t, db.Model(&model.KKAIImageExecution{}).Where("generation_id = ?", generation.ID).Update("state", model.ImageGenerationExecutionDispatching).Error)
			if tc.staleFence {
				require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("id = ?", fence.EventID).Update("locked_by", "new-worker").Error)
			}
			err = MarkImageExecutionUnknown(context.Background(), db, generation.ID, fence, "provider-id", tc.failureCode)
			if tc.staleFence {
				require.ErrorIs(t, err, model.ErrImageExecutionFenceLost)
			} else {
				require.NoError(t, err)
				// A later recovery observation with no new diagnostic must retain it.
				require.NoError(t, MarkImageExecutionUnknown(context.Background(), db, generation.ID, fence, "provider-id", ""))
			}
			require.NoError(t, db.First(&generation, generation.ID).Error)
			assert.Equal(t, tc.wantCode, generation.ErrorCode)
			assert.Equal(t, model.ImageGenerationStatusSubmitting, generation.Status)
			var user model.User
			require.NoError(t, db.First(&user, generation.UserID).Error)
			assert.EqualValues(t, 700, user.Quota, "saving a diagnostic must not refund")
			if tc.staleFence {
				return
			}
			if tc.readyResult {
				require.NoError(t, model.PrepareImageGenerationAccounting(context.Background(), db, generation.ID, model.ImageGenerationAccountingPayload{TargetQuota: 200, OutputCount: 1, LogParams: model.RecordConsumeLogParams{ChannelId: 3, ModelName: generation.Model, TokenId: generation.TokenID, Quota: 200}}))
				require.NoError(t, db.Create(&model.KKAIImageAsset{GenerationID: &generation.ID, OwnerUserID: generation.UserID, Kind: model.ImageAssetKindOutput, State: model.ImageAssetStateReady, ObjectKey: "durable-public-error-result"}).Error)
			}
			require.NoError(t, CompleteImageExecution(context.Background(), db, generation.ID, fence, "upstream_outcome_unknown"))
			require.NoError(t, db.First(&generation, generation.ID).Error)
			require.NoError(t, db.First(&user, generation.UserID).Error)
			if tc.readyResult {
				assert.Equal(t, model.ImageGenerationStatusSucceeded, generation.Status)
				assert.Empty(t, generation.ErrorCode)
				assert.Empty(t, generation.ErrorMessage)
				assert.Equal(t, model.ImageGenerationBillingStateSettled, generation.BillingState)
				assert.EqualValues(t, 800, user.Quota)
			} else {
				wantCode := tc.wantCode
				if wantCode == "" {
					wantCode = "upstream_outcome_unknown"
				}
				assert.Equal(t, wantCode, generation.ErrorCode)
				assert.Equal(t, model.ImageGenerationStatusUnknown, generation.Status)
				assert.Equal(t, model.ImageGenerationBillingStateRefunded, generation.BillingState)
				assert.EqualValues(t, 1000, user.Quota)
			}
		})
	}
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
