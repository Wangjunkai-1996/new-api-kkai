package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestNormalizeImageStudioSubmissionMergesDefaultsAndBuildsStrictRelayRequest(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	request := ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "  a lighthouse  ",
		Parameters: map[string]any{"count": float64(2)},
	}

	normalized, err := NormalizeImageStudioSubmission(context.Background(), db, 7, request)
	require.NoError(t, err)
	assert.Equal(t, "a lighthouse", normalized.Prompt)
	assert.Equal(t, 2, normalized.RequestedCount)
	assert.Equal(t, uint(2), *normalized.RelayRequest.N)
	assert.Equal(t, "1024x1024", normalized.RelayRequest.Size)
	require.NotNil(t, normalized.RelayRequest.Stream)
	assert.False(t, *normalized.RelayRequest.Stream)
	assert.Empty(t, normalized.RelayRequest.Extra)
	assert.Len(t, normalized.RequestHash, 64)
}

func TestImageStudioQuoteBindsNormalizedCreativeRequest(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	normalized, err := NormalizeImageStudioSubmission(context.Background(), db, 7, ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "a lighthouse",
	})
	require.NoError(t, err)
	now := time.Unix(1_800_000_000, 0)
	quote := newImageStudioQuoteAt(normalized, 123, map[string]float64{"n": 1}, now)

	normalized.MaxQuota = common.GetPointer(quote.Quota)
	normalized.QuoteHash = quote.RequestHash
	normalized.QuoteExpiresAt = quote.ExpiresAt
	require.NoError(t, ValidateImageStudioQuote(normalized, now.Add(time.Minute)))

	normalized.Prompt = "different"
	changed, err := imageStudioRequestHash(normalized, imageSubmissionSpec())
	require.NoError(t, err)
	normalized.RequestHash = changed
	require.ErrorIs(t, ValidateImageStudioQuote(normalized, now.Add(time.Minute)), ErrImageStudioQuoteMismatch)
}

func TestCreateImageGenerationAtomicallyBindsIdempotencyReservation(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	normalized, err := NormalizeImageStudioSubmission(context.Background(), db, 7, ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "a lighthouse",
	})
	require.NoError(t, err)
	reservation, err := ReserveIdempotencyKey(context.Background(), db, IdempotencyReservationRequest{
		UserID: 7, Operation: model.ImageIdempotencyOperationSubmit,
		Key: "image-submit-key", RequestHash: normalized.RequestHash,
	})
	require.NoError(t, err)

	generation, err := CreateImageGeneration(
		context.Background(), db, normalized, "req-image-generation", reservation.Record.ID,
	)
	require.NoError(t, err)
	assert.Equal(t, model.ImageGenerationStatusSubmitting, generation.Status)

	var bound model.KKAIIdempotencyKey
	require.NoError(t, db.First(&bound, reservation.Record.ID).Error)
	assert.Equal(t, model.ImageIdempotencyResourceGeneration, bound.ResourceType)
	assert.Equal(t, "1", bound.ResourceID)
}

func TestReconcileStaleImageGenerationsNeverRetriesSupplier(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	old := time.Now().Add(-10 * time.Minute).Unix()
	generation := model.KKAIImageGeneration{
		UserID: 7, TokenID: 4, ModelProfileID: profile.ID, SpecificationVersion: 1,
		Model: profile.Model, Prompt: "prompt", Parameters: "{}",
		RequestHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RequestID:   "req-stale-image", Status: model.ImageGenerationStatusSubmitting,
		RequestedCount: 1, StartedAt: old, CreatedAt: old, UpdatedAt: old,
	}
	require.NoError(t, db.Create(&generation).Error)

	updated, err := ReconcileStaleImageGenerations(context.Background(), db, time.Now().Add(-time.Minute), 10)
	require.NoError(t, err)
	assert.Equal(t, 1, updated)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	assert.Equal(t, model.ImageGenerationStatusUnknown, generation.Status)
	assert.Equal(t, "submission_interrupted", generation.ErrorCode)
}

func TestReconcileStaleImageGenerationsIgnoresDeletedGeneration(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	old := time.Now().Add(-10 * time.Minute).Unix()
	generation := model.KKAIImageGeneration{
		UserID: 7, TokenID: 4, ModelProfileID: profile.ID, SpecificationVersion: 1,
		Model: profile.Model, Prompt: "deleted prompt", Parameters: "{}",
		RequestHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		RequestID:   "req-stale-deleted-image", Status: model.ImageGenerationStatusSubmitting,
		RequestedCount: 1, HeartbeatAt: old, StartedAt: old, CreatedAt: old, UpdatedAt: old,
		DeletedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&generation).Error)

	updated, err := ReconcileStaleImageGenerations(
		context.Background(), db, time.Now().Add(-time.Minute), 10,
	)
	require.NoError(t, err)
	assert.Zero(t, updated)
	claimed, err := reconcileStaleImageGeneration(
		context.Background(), db, generation.ID, time.Now().Add(-time.Minute),
	)
	require.NoError(t, err)
	assert.False(t, claimed)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	assert.Equal(t, model.ImageGenerationStatusSubmitting, generation.Status)
	assert.NotZero(t, generation.DeletedAt)
}

func TestFinishRecoveringImageGenerationCASRejectsConcurrentDeletion(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	now := time.Now().Unix()
	generation := model.KKAIImageGeneration{
		UserID: 7, TokenID: 4, ModelProfileID: profile.ID, SpecificationVersion: 1,
		Model: profile.Model, Prompt: "recovering delete race", Parameters: "{}",
		RequestHash: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		RequestID:   "req-recovering-delete-race", Status: model.ImageGenerationStatusRecovering,
		RequestedCount: 1, BillingState: model.ImageGenerationBillingStateRefunded,
		HeartbeatAt: now, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&generation).Error)
	injected := false
	callbackName := "test:image_generation_recovery_delete_race"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if injected || tx.Statement.Table != (model.KKAIImageGeneration{}).TableName() {
			return
		}
		injected = true
		if err := tx.Exec(
			"UPDATE kkai_image_generations SET deleted_at = ? WHERE id = ?",
			time.Now().Unix(), generation.ID,
		).Error; err != nil {
			tx.AddError(err)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })

	err := finishRecoveringImageGeneration(
		context.Background(), db, generation.ID, model.ImageGenerationStatusUnknown,
		0, 0, "reconcile", "submission_interrupted", "submission outcome is unknown",
	)
	require.ErrorIs(t, err, ErrImageGenerationConflict)
	require.True(t, injected)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	assert.Equal(t, model.ImageGenerationStatusRecovering, generation.Status)
	assert.Zero(t, generation.FinishedAt)
	assert.Zero(t, generation.DeletedAt)
}

func TestReconcileStaleImageGenerationSettlesPreparedQuotaExactlyOnce(t *testing.T) {
	db := newImageAccountingRecoveryTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id: 71, Username: "image-recovery", Password: "password", Quota: 1_000,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id: 3, Name: "image-recovery-channel", Key: "test-key", Status: 1, Type: 1,
	}).Error)
	generation := seedRecoverableImageGeneration(t, db, 71, 2)
	_, err := model.ReserveImageGenerationBilling(
		context.Background(), db, generation.ID, model.TaskBillingSourceWallet, 500,
	)
	require.NoError(t, err)
	require.NoError(t, model.MarkImageGenerationDispatching(context.Background(), db, generation.ID))
	require.NoError(t, model.PrepareImageGenerationAccounting(
		context.Background(), db, generation.ID, model.ImageGenerationAccountingPayload{
			TargetQuota: 200, CountStatistics: true,
			LogParams: model.RecordConsumeLogParams{
				ChannelId: 3, ModelName: generation.Model, TokenId: generation.TokenID, Quota: 200,
			},
		},
	))
	old := time.Now().Add(-10 * time.Minute).Unix()
	for position := 0; position < 2; position++ {
		require.NoError(t, db.Create(&model.KKAIImageAsset{
			GenerationID: &generation.ID, OwnerUserID: 71, Scope: model.ImageAssetScopeUser,
			Kind: model.ImageAssetKindOutput, State: model.ImageAssetStateReady, Position: position,
			ObjectKey:      fmt.Sprintf("image-recovery/%d", position),
			ThumbnailState: model.ImageThumbnailStatePending, CreatedAt: old, UpdatedAt: old,
		}).Error)
	}
	require.NoError(t, db.Model(&model.KKAIImageGeneration{}).Where("id = ?", generation.ID).
		Update("heartbeat_at", old).Error)

	updated, err := ReconcileStaleImageGenerations(
		context.Background(), db, time.Now().Add(-time.Minute), 10,
	)
	require.NoError(t, err)
	require.Equal(t, 1, updated)
	updated, err = ReconcileStaleImageGenerations(
		context.Background(), db, time.Now().Add(-time.Minute), 10,
	)
	require.NoError(t, err)
	require.Zero(t, updated)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	require.Equal(t, model.ImageGenerationStatusSucceeded, generation.Status)
	require.Equal(t, model.ImageGenerationBillingStateSettled, generation.BillingState)
	require.Equal(t, 200, generation.FinalQuota)
	var user model.User
	require.NoError(t, db.First(&user, 71).Error)
	require.EqualValues(t, 800, user.Quota)
	require.EqualValues(t, 200, user.UsedQuota)
	require.EqualValues(t, 1, user.RequestCount)
}

func TestReconcileStaleImageGenerationDiscardsPartialDeliveryAndRefunds(t *testing.T) {
	db := newImageAccountingRecoveryTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id: 72, Username: "image-partial", Password: "password", Quota: 1_000,
	}).Error)
	generation := seedRecoverableImageGeneration(t, db, 72, 2)
	_, err := model.ReserveImageGenerationBilling(
		context.Background(), db, generation.ID, model.TaskBillingSourceWallet, 500,
	)
	require.NoError(t, err)
	require.NoError(t, model.MarkImageGenerationDispatching(context.Background(), db, generation.ID))
	require.NoError(t, model.PrepareImageGenerationAccounting(
		context.Background(), db, generation.ID, model.ImageGenerationAccountingPayload{
			TargetQuota: 200,
			LogParams: model.RecordConsumeLogParams{
				ChannelId: 3, ModelName: generation.Model, TokenId: generation.TokenID, Quota: 200,
			},
		},
	))
	old := time.Now().Add(-10 * time.Minute).Unix()
	asset := model.KKAIImageAsset{
		GenerationID: &generation.ID, OwnerUserID: 72, Scope: model.ImageAssetScopeUser,
		Kind: model.ImageAssetKindOutput, State: model.ImageAssetStateReady,
		ObjectKey: "image-partial/ready", ThumbnailState: model.ImageThumbnailStatePending,
		CreatedAt: old, UpdatedAt: old,
	}
	require.NoError(t, db.Create(&asset).Error)
	require.NoError(t, db.Model(&model.KKAIImageGeneration{}).Where("id = ?", generation.ID).
		Update("heartbeat_at", old).Error)

	updated, err := ReconcileStaleImageGenerations(
		context.Background(), db, time.Now().Add(-time.Minute), 10,
	)
	require.NoError(t, err)
	require.Equal(t, 1, updated)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	require.Equal(t, model.ImageGenerationStatusArchiveFailed, generation.Status)
	require.Equal(t, model.ImageGenerationBillingStateRefunded, generation.BillingState)
	require.Zero(t, generation.FinalQuota)
	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Equal(t, model.ImageAssetStateDeleted, asset.State)
	require.NotZero(t, asset.DeletedAt)
	var user model.User
	require.NoError(t, db.First(&user, 72).Error)
	require.EqualValues(t, 1_000, user.Quota)
	var deletionEvents int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where(
		"topic = ?", ImageAssetDeleteTopic,
	).Count(&deletionEvents).Error)
	require.EqualValues(t, 1, deletionEvents)
}

func TestImageStudioBillingGuardRejectsPriceIncreaseWithoutChargingConfirmedMaximum(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	require.NoError(t, SetImageStudioBillingGuard(c, 100))
	require.NoError(t, EnforceImageStudioPreconsumeLimit(c, 100))
	require.ErrorIs(t, EnforceImageStudioPreconsumeLimit(c, 101), ErrImageStudioQuoteStale)

	quota, capped := applyImageStudioFinalQuota(c, 120)
	assert.Equal(t, 0, quota)
	assert.True(t, capped)
	finalQuota, settled, recordedCapped := ImageStudioFinalQuota(c)
	assert.Equal(t, 0, finalQuota)
	assert.False(t, settled)
	assert.True(t, recordedCapped)
	require.True(t, completeImageStudioBilling(c, nil))
	finalQuota, settled, recordedCapped = ImageStudioFinalQuota(c)
	assert.Equal(t, 0, finalQuota)
	assert.True(t, settled)
	assert.True(t, recordedCapped)
}

func newImageSubmissionTestDB(t *testing.T) (*gorm.DB, model.KKAIImageModelProfile) {
	t.Helper()
	dsn := "file:image-submission-" + time.Now().Format("150405.000000000") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.KKAIImageModelProfile{}, &model.KKAIImageSample{}, &model.KKAIImageGeneration{},
		&model.KKAIImageAsset{}, &model.KKAIIdempotencyKey{}, &model.KKAIOutboxEvent{},
	))
	specification, err := common.Marshal(imageSubmissionSpec())
	require.NoError(t, err)
	defaults, err := common.Marshal(map[string]any{"count": 1, "size": "1024x1024"})
	require.NoError(t, err)
	now := time.Now().Unix()
	profile := model.KKAIImageModelProfile{
		Model: "image-model-v1", DisplayName: "Image model", SpecificationVersion: 1,
		Specification: string(specification), DefaultParameters: string(defaults), Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&profile).Error)
	return db, profile
}

func newImageAccountingRecoveryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:image-accounting-recovery-" + time.Now().Format("150405.000000000") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Channel{}, &model.KKAIImageGeneration{},
		&model.KKAIImageAsset{}, &model.KKAIOutboxEvent{},
	))
	return db
}

func seedRecoverableImageGeneration(
	t *testing.T, db *gorm.DB, userID int, requestedCount int,
) model.KKAIImageGeneration {
	t.Helper()
	now := time.Now().Unix()
	generation := model.KKAIImageGeneration{
		UserID: userID, TokenID: 4, ModelProfileID: 1, SpecificationVersion: 1,
		Model: "gpt-image-1", Prompt: "prompt", Parameters: "{}",
		RequestHash: fmt.Sprintf("%064d", time.Now().UnixNano()),
		RequestID:   fmt.Sprintf("req-image-recovery-%d", time.Now().UnixNano()),
		Status:      model.ImageGenerationStatusSubmitting, RequestedCount: requestedCount,
		BillingState: model.ImageGenerationBillingStatePending,
		HeartbeatAt:  now, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&generation).Error)
	return generation
}

func imageSubmissionSpec() ImageModelSpec {
	minimum := 1
	maximum := 4
	return ImageModelSpec{Version: 1, Parameters: []ImageParameterSpec{
		{Key: "count", Label: "Count", Control: ImageControlInteger, RequestKey: "n", Min: &minimum, Max: &maximum},
		{Key: "size", Label: "Size", Control: ImageControlSelect, RequestKey: "size", Options: []ImageParameterOption{
			{Label: "Square", Value: "1024x1024"},
		}},
	}}
}
