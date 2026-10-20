package service

import (
	"context"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func imageExecutionFenceFixture(t *testing.T, db *gorm.DB, generation model.KKAIImageGeneration) model.ImageExecutionFence {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&model.KKAIJobLease{}, &model.KKAIImageExecution{}))
	now := time.Now().Unix()
	leader := model.KKAIJobLease{LeaseName: "image-worker", Holder: "writer", Fence: 1, LeaseUntil: now + 300, UpdatedAt: now}
	require.NoError(t, db.Create(&leader).Error)
	event := model.KKAIOutboxEvent{EventKey: "image-fixture:" + strconv.FormatInt(generation.ID, 10), Topic: "image.generate.v1", AggregateID: strconv.FormatInt(generation.ID, 10), Payload: "{}", Status: model.KKAIOutboxStatusPending, LockedBy: "claim-1", LockedAt: now}
	require.NoError(t, db.Create(&event).Error)
	execution := model.KKAIImageExecution{GenerationID: generation.ID, State: model.ImageGenerationExecutionPreparing, Fence: event.LockedBy, EventID: event.ID, Payload: "{}", QueueDeadlineAt: now + 300, ExecutionDeadlineAt: now + 1200, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&execution).Error)
	return model.ImageExecutionFence{EventID: event.ID, LockedBy: event.LockedBy, LockTimeout: 2 * time.Minute, LeaderName: leader.LeaseName, LeaderHolder: leader.Holder, LeaderFence: leader.Fence}
}

func TestImageAsyncDispatchIntentCannotBeRepeated(t *testing.T) {
	db := newImageAccountingRecoveryTestDB(t)
	require.NoError(t, db.Create(&model.User{Id: 75, Username: "async-dispatch", Password: "password", Quota: 1000}).Error)
	generation := seedRecoverableImageGeneration(t, db, 75, 1)
	_, err := model.ReserveImageGenerationBilling(context.Background(), db, generation.ID, model.TaskBillingSourceWallet, 300)
	require.NoError(t, err)
	fence := imageExecutionFenceFixture(t, db, generation)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/internal/image", nil).WithContext(WithImageExecutionFence(context.Background(), generation.ID, fence))
	c.Set("channel_id", 49)
	require.NoError(t, SetImageStudioGenerationID(c, generation.ID))
	require.NoError(t, MarkImageStudioGenerationDispatching(c))
	require.ErrorIs(t, MarkImageStudioGenerationDispatching(c), model.ErrImageExecutionFenceLost)
	var execution model.KKAIImageExecution
	require.NoError(t, db.First(&execution, "generation_id = ?", generation.ID).Error)
	assert.Equal(t, model.ImageGenerationExecutionDispatching, execution.State)
	assert.Equal(t, 49, execution.DispatchChannelID)
	assert.Greater(t, execution.DispatchStartedAt, int64(0))
}

func TestImageAsyncAccountingIntentAndRecoverableStateCommitTogether(t *testing.T) {
	c, info, db, generation := newImageStudioDeliveryBillingFixture(t, 76, 1, 300)
	fence := imageExecutionFenceFixture(t, db, generation)
	c.Request = c.Request.WithContext(WithImageExecutionFence(context.Background(), generation.ID, fence))
	require.NoError(t, db.Model(&model.KKAIImageExecution{}).Where("generation_id = ?", generation.ID).Update("state", model.ImageGenerationExecutionDispatching).Error)
	require.NoError(t, PrepareImageStudioBillingCommit(c, info, 1, model.RecordConsumeLogParams{ChannelId: 3, ModelName: generation.Model, TokenId: generation.TokenID, Quota: 200}))
	require.NoError(t, PrepareImageStudioDeliveryBilling(c, 1, 1))
	accounting, err := model.GetImageGenerationAccounting(context.Background(), db, generation.ID)
	require.NoError(t, err)
	assert.Equal(t, 200, accounting.TargetQuota)
	var execution model.KKAIImageExecution
	require.NoError(t, db.First(&execution, "generation_id = ?", generation.ID).Error)
	assert.Equal(t, model.ImageGenerationExecutionResultReady, execution.State)
}

func TestImageAsyncArchiveRejectsWorkerThatLosesClaimDuringObjectPut(t *testing.T) {
	db := newImagePipelineTestDB(t)
	generation := imagePipelineGeneration(t, db)
	fence := imageExecutionFenceFixture(t, db, generation)
	store := &imagePipelineStore{objects: map[string][]byte{}}
	store.afterPut = func() {
		require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("id = ?", fence.EventID).Update("locked_by", "claim-2").Error)
	}
	pipeline, err := NewImageAssetPipeline(db, store, imagePipelineFetcher{payload: imageArchiveTestPNG(t, 2, 1)}, 1<<20, 100)
	require.NoError(t, err)
	_, err = pipeline.ArchiveGeneration(WithImageExecutionFence(context.Background(), generation.ID, fence), generation, []ImageRelayResult{{Base64: "ignored"}})
	require.ErrorIs(t, err, model.ErrImageExecutionFenceLost)
	var ready int64
	require.NoError(t, db.Model(&model.KKAIImageAsset{}).Where("state = ?", model.ImageAssetStateReady).Count(&ready).Error)
	assert.Zero(t, ready)
	assert.Empty(t, store.objects)
	var compensation int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ? AND status = ?", ImageAssetDeleteTopic, model.KKAIOutboxStatusPending).Count(&compensation).Error)
	assert.EqualValues(t, 1, compensation)
}

func TestImageAsyncQueuedTaskIsNotRefundedByLegacyReconciler(t *testing.T) {
	db := newImageAccountingRecoveryTestDB(t)
	generation := seedRecoverableImageGeneration(t, db, 77, 1)
	imageExecutionFenceFixture(t, db, generation)
	require.NoError(t, db.Model(&model.KKAIImageGeneration{}).Where("id = ?", generation.ID).Update("heartbeat_at", time.Now().Add(-time.Hour).Unix()).Error)
	count, err := ReconcileStaleImageGenerations(context.Background(), db, time.Now().Add(-time.Minute), 10)
	require.NoError(t, err)
	assert.Zero(t, count)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	assert.Equal(t, model.ImageGenerationStatusSubmitting, generation.Status)
	assert.Equal(t, model.ImageGenerationBillingStatePending, generation.BillingState)
}

func TestImageGenerationAdmissionHasNoGlobalOrPerUserTaskLimit(t *testing.T) {
	db, profile := newImageSubmissionTestDB(t)
	for index := 0; index < 32; index++ {
		generation := seedRecoverableImageGeneration(t, db, 80, 1)
		if index%2 == 0 {
			require.NoError(t, db.Model(&generation).Update("status", model.ImageGenerationStatusRecovering).Error)
		}
	}
	normalized, err := NormalizeImageStudioSubmission(context.Background(), db, 80, ImageStudioSubmissionRequest{TokenID: 4, Model: profile.Model, Prompt: "queued image"})
	require.NoError(t, err)
	reservation, err := ReserveIdempotencyKey(context.Background(), db, IdempotencyReservationRequest{UserID: 80, Operation: model.ImageIdempotencyOperationSubmit, Key: "unlimited-task-admission", RequestHash: normalized.RequestHash})
	require.NoError(t, err)
	generation, err := CreateImageGeneration(context.Background(), db, normalized, "unlimited-task-admission", reservation.Record.ID)
	require.NoError(t, err)
	assert.Equal(t, model.ImageGenerationStatusSubmitting, generation.Status)
	var count int64
	require.NoError(t, db.Model(&model.KKAIImageGeneration{}).Where("user_id = ?", 80).Count(&count).Error)
	assert.EqualValues(t, 33, count)
}
