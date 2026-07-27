package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func createVideoReconcileCandidate(
	t *testing.T,
	db *gorm.DB,
	generationID int64,
	taskKey string,
	archiveSource string,
) model.Task {
	t.Helper()
	now := time.Now().Unix()
	task := model.Task{
		TaskID: taskKey, UserId: 9,
		Status: model.TaskStatusSuccess, Progress: "100%", CreatedAt: now, UpdatedAt: now,
		PrivateData: model.TaskPrivateData{ArchiveSource: archiveSource, AssetHostedResult: true},
	}
	require.NoError(t, db.Create(&task).Error)
	require.NoError(t, db.Create(&model.KKAIVideoGeneration{
		ID: generationID, UserID: 9, TaskID: task.ID, ModelProfileID: 3, Model: "video-model", Mode: VideoModeTextToVideo,
		Prompt: "test", Parameters: `{}`, CreatedAt: now, UpdatedAt: now,
	}).Error)
	return task
}

func TestReconcileVideoTaskOutputsScansPastFullPageWithoutArchiveSources(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	now := time.Now().Unix()
	var archiveableTask model.Task
	for index := 0; index <= 50; index++ {
		task := model.Task{
			TaskID: fmt.Sprintf("reconcile-starvation-%02d", index), UserId: 9,
			Status: model.TaskStatusSuccess, Progress: "100%", CreatedAt: now, UpdatedAt: now,
			PrivateData: model.TaskPrivateData{AssetHostedResult: true},
		}
		if index == 50 {
			task.PrivateData.ArchiveSource = "https://media.example/archiveable.mp4"
		}
		require.NoError(t, db.Create(&task).Error)
		require.NoError(t, db.Create(&model.KKAIVideoGeneration{
			UserID: 9, TaskID: task.ID, ModelProfileID: 3, Model: "video-model", Mode: VideoModeTextToVideo,
			Prompt: "test", Parameters: `{}`, CreatedAt: now, UpdatedAt: now,
		}).Error)
		if index == 50 {
			archiveableTask = task
		}
	}

	created, err := ReconcileVideoTaskOutputs(context.Background(), db, 50)
	require.NoError(t, err)
	require.Equal(t, 1, created)

	var links []model.KKAIVideoTaskAsset
	require.NoError(t, db.Where("role = ?", model.VideoTaskAssetRoleOutput).Find(&links).Error)
	require.Len(t, links, 1)
	require.Equal(t, archiveableTask.ID, links[0].TaskID)
	var assets int64
	var events int64
	require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Count(&assets).Error)
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicArchive).Count(&events).Error)
	require.EqualValues(t, 1, assets)
	require.EqualValues(t, 1, events)

	created, err = ReconcileVideoTaskOutputs(context.Background(), db, 50)
	require.NoError(t, err)
	require.Zero(t, created)
	require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Count(&assets).Error)
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicArchive).Count(&events).Error)
	require.EqualValues(t, 1, assets)
	require.EqualValues(t, 1, events)

	var successfulTasks int64
	require.NoError(t, db.Model(&model.Task{}).Where("status = ?", model.TaskStatusSuccess).Count(&successfulTasks).Error)
	require.EqualValues(t, 51, successfulTasks)
}

func TestVideoTaskOutputReconcilerEventuallyScansPastPermanentBlockers(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	for index := 0; index < 251; index++ {
		createVideoReconcileCandidate(t, db, 0, fmt.Sprintf("reconcile-permanent-blocker-%03d", index), "")
	}
	archiveableTask := createVideoReconcileCandidate(
		t, db, 0, "reconcile-after-permanent-blockers", "https://media.example/deep-archiveable.mp4",
	)

	reconciler := &videoTaskOutputReconciler{}
	totalCreated := 0
	rounds := 0
	for rounds < 10 && totalCreated == 0 {
		created, err := reconciler.Reconcile(context.Background(), db, 50)
		require.NoError(t, err)
		totalCreated += created
		rounds++
	}
	require.Greater(t, rounds, 1)
	require.Equal(t, 1, totalCreated)

	for range 4 {
		created, err := reconciler.Reconcile(context.Background(), db, 50)
		require.NoError(t, err)
		require.Zero(t, created)
	}
	var links []model.KKAIVideoTaskAsset
	require.NoError(t, db.Where("role = ?", model.VideoTaskAssetRoleOutput).Find(&links).Error)
	require.Len(t, links, 1)
	require.Equal(t, archiveableTask.ID, links[0].TaskID)
	var assets int64
	var events int64
	var successfulTasks int64
	require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Count(&assets).Error)
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicArchive).Count(&events).Error)
	require.NoError(t, db.Model(&model.Task{}).Where("status = ?", model.TaskStatusSuccess).Count(&successfulTasks).Error)
	require.EqualValues(t, 1, assets)
	require.EqualValues(t, 1, events)
	require.EqualValues(t, 252, successfulTasks)
}

func TestVideoTaskOutputReconcilerWrapsToNewLowerGenerationID(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	for index := int64(1001); index <= 1003; index++ {
		createVideoReconcileCandidate(t, db, index, fmt.Sprintf("reconcile-high-%d", index), "")
	}
	reconciler := &videoTaskOutputReconciler{}
	created, err := reconciler.Reconcile(context.Background(), db, 1)
	require.NoError(t, err)
	require.Zero(t, created)

	archiveableTask := createVideoReconcileCandidate(
		t, db, 500, "reconcile-new-lower-id", "https://media.example/lower-id.mp4",
	)
	for round := 0; round < 3 && created == 0; round++ {
		created, err = reconciler.Reconcile(context.Background(), db, 1)
		require.NoError(t, err)
	}
	require.Equal(t, 1, created)

	created, err = reconciler.Reconcile(context.Background(), db, 1)
	require.NoError(t, err)
	require.Zero(t, created)
	var links []model.KKAIVideoTaskAsset
	require.NoError(t, db.Where("role = ?", model.VideoTaskAssetRoleOutput).Find(&links).Error)
	require.Len(t, links, 1)
	require.Equal(t, archiveableTask.ID, links[0].TaskID)
	var successfulTasks int64
	require.NoError(t, db.Model(&model.Task{}).Where("status = ?", model.TaskStatusSuccess).Count(&successfulTasks).Error)
	require.EqualValues(t, 4, successfulTasks)
}
