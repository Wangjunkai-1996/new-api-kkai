package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestLookupVideoSubmissionIsReadOnlyAndOwnerScoped(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.KKAIIdempotencyKey{}, &model.Task{}, &model.KKAIVideoGeneration{}))
	now := time.Now().Unix()
	task := model.Task{TaskID: "video-task-accepted", UserId: 7, Status: model.TaskStatusInProgress, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&task).Error)
	generation := model.KKAIVideoGeneration{UserID: 7, TaskID: task.ID, ModelProfileID: 1, Model: "model", Mode: VideoModeReferenceToVideo, Prompt: "p", Parameters: `{}`, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&generation).Error)
	bindings := []model.KKAIIdempotencyKey{
		{UserID: 7, Operation: model.VideoIdempotencyOperationSubmit, Key: "accepted", ResourceType: model.VideoIdempotencyResourceTask, ResourceID: task.TaskID, ExpiresAt: now - 1},
		{UserID: 7, Operation: model.VideoIdempotencyOperationSubmit, Key: "pending", ExpiresAt: now - 1},
		{UserID: 8, Operation: model.VideoIdempotencyOperationSubmit, Key: "other-user", ResourceType: model.VideoIdempotencyResourceTask, ResourceID: task.TaskID, ExpiresAt: now + 100},
	}
	require.NoError(t, db.Create(&bindings).Error)

	accepted, err := LookupVideoSubmission(context.Background(), db, 7, "accepted")
	require.NoError(t, err)
	require.Equal(t, VideoSubmissionStatusAccepted, accepted.Status)
	require.Equal(t, task.TaskID, accepted.TaskID)
	require.NotNil(t, accepted.GenerationID)
	require.Equal(t, generation.ID, *accepted.GenerationID)

	pending, err := LookupVideoSubmission(context.Background(), db, 7, "pending")
	require.NoError(t, err)
	require.Equal(t, VideoSubmissionStatusPending, pending.Status)

	missing, err := LookupVideoSubmission(context.Background(), db, 7, "missing")
	require.NoError(t, err)
	require.Equal(t, VideoSubmissionStatusNotFound, missing.Status)

	other, err := LookupVideoSubmission(context.Background(), db, 8, "accepted")
	require.NoError(t, err)
	require.Equal(t, VideoSubmissionStatusNotFound, other.Status)
	var after []model.KKAIIdempotencyKey
	require.NoError(t, db.Order("id").Find(&after).Error)
	require.Equal(t, bindings, after)
}

func TestLookupVideoSubmissionTreatsBoundMissingTaskAsPending(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.KKAIIdempotencyKey{}, &model.Task{}))
	now := time.Now().Unix()
	binding := model.KKAIIdempotencyKey{
		UserID: 7, Operation: model.VideoIdempotencyOperationSubmit, Key: "missing-task",
		ResourceType: model.VideoIdempotencyResourceTask, ResourceID: "task-not-visible-yet", ExpiresAt: now - 1,
	}
	require.NoError(t, db.Create(&binding).Error)
	result, err := LookupVideoSubmission(context.Background(), db, 7, binding.Key)
	require.NoError(t, err)
	require.Equal(t, VideoSubmissionStatusPending, result.Status)
	require.Empty(t, result.TaskID)
	var after model.KKAIIdempotencyKey
	require.NoError(t, db.First(&after, binding.ID).Error)
	require.Equal(t, binding.ExpiresAt, after.ExpiresAt)
}
