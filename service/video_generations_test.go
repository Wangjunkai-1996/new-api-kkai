package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVideoGenerationStatusKeepsUnknownTaskPollable(t *testing.T) {
	task := model.Task{Status: model.TaskStatusUnknown, Progress: "10%"}
	require.Equal(t, "processing", videoGenerationStatus(task, nil))
}

func TestDeleteVideoGenerationIsIdempotentForSameOwner(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	now := time.Now().Unix()
	task := model.Task{TaskID: "delete-idempotent", UserId: 7, Status: model.TaskStatusSuccess, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&task).Error)
	generation := model.KKAIVideoGeneration{
		UserID: 7, TaskID: task.ID, ModelProfileID: 1, Model: "model", Mode: VideoModeTextToVideo,
		Prompt: "prompt", Parameters: `{}`, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&generation).Error)

	require.NoError(t, DeleteVideoGeneration(context.Background(), db, 7, generation.ID))
	require.NoError(t, DeleteVideoGeneration(context.Background(), db, 7, generation.ID))
	require.ErrorIs(t, DeleteVideoGeneration(context.Background(), db, 8, generation.ID), ErrVideoGenerationNotFound)
}

func TestReconcileVideoTaskOutputRechecksDeletedGenerationInTransaction(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	now := time.Now().Unix()
	task := model.Task{
		TaskID: "stale-generation", UserId: 7, Status: model.TaskStatusSuccess, CreatedAt: now, UpdatedAt: now,
		PrivateData: model.TaskPrivateData{ResultURL: "https://media.example/output.mp4"},
	}
	require.NoError(t, db.Create(&task).Error)
	generation := model.KKAIVideoGeneration{
		UserID: 7, TaskID: task.ID, ModelProfileID: 1, Model: "model", Mode: VideoModeTextToVideo,
		Prompt: "prompt", Parameters: `{}`, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&generation).Error)

	require.NoError(t, db.Model(&generation).Update("deleted_at", time.Now().Unix()).Error)
	created, err := reconcileVideoTaskOutput(context.Background(), db, generation.ID)
	require.NoError(t, err)
	require.Zero(t, created)
	var assets int64
	var links int64
	require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Count(&assets).Error)
	require.NoError(t, db.Model(&model.KKAIVideoTaskAsset{}).Where("role = ?", model.VideoTaskAssetRoleOutput).Count(&links).Error)
	require.Zero(t, assets)
	require.Zero(t, links)
}

func TestReconcileVideoTaskOutputArchivesLegacyFailReasonResult(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	now := time.Now().Unix()
	legacySource := "https://provider.example/legacy-private.mp4"
	task := model.Task{
		TaskID: "legacy-fail-reason-result", UserId: 7, Status: model.TaskStatusSuccess,
		FailReason: legacySource, CreatedAt: now, UpdatedAt: now,
		PrivateData: model.TaskPrivateData{
			BillingState: model.TaskBillingStateCompleted,
			TokenQuota:   321,
		},
	}
	require.NoError(t, db.Create(&task).Error)
	generation := model.KKAIVideoGeneration{
		UserID: 7, TaskID: task.ID, ModelProfileID: 1, Model: "model", Mode: VideoModeTextToVideo,
		Prompt: "prompt", Parameters: `{}`, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&generation).Error)

	created, err := reconcileVideoTaskOutput(context.Background(), db, generation.ID)
	require.NoError(t, err)
	require.True(t, created)

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.True(t, reloaded.PrivateData.AssetHostedResult)
	require.Equal(t, legacySource, reloaded.PrivateData.ArchiveSource)
	require.Equal(t, model.TaskBillingStateCompleted, reloaded.PrivateData.BillingState)
	require.Equal(t, 321, reloaded.PrivateData.TokenQuota)
	require.Equal(t, legacySource, reloaded.FailReason)

	var assets int64
	require.NoError(t, db.Model(&model.KKAIVideoTaskAsset{}).
		Where("task_id = ? AND role = ?", task.ID, model.VideoTaskAssetRoleOutput).
		Count(&assets).Error)
	require.EqualValues(t, 1, assets)
}

func TestCreateVideoGenerationRejectsDeletingReference(t *testing.T) {
	db := newVideoGenerationTestDB(t)
	now := time.Now().Unix()
	task := model.Task{TaskID: "reference-delete-race", UserId: 7, Status: model.TaskStatusNotStart, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&task).Error)
	asset := model.KKAIVideoAsset{
		OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference,
		State: model.VideoAssetStateDeleting, ObjectKey: "reference.png", MIMEType: "image/png", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)
	normalized := &NormalizedVideoStudioSubmission{
		UserID: 7, ProfileID: 1, SpecificationVersion: 1, Model: "model", Mode: VideoModeImageToVideo,
		Prompt: "prompt", Parameters: map[string]any{},
		ReferenceAssets: []NormalizedVideoReferenceAsset{{Role: model.VideoTaskAssetRoleReference, Asset: asset}},
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		_, createErr := CreateVideoGeneration(context.Background(), tx, normalized, task.ID)
		return createErr
	})
	require.ErrorIs(t, err, ErrInvalidVideoStudioSubmission)
	var links int64
	require.NoError(t, db.Model(&model.KKAIVideoTaskAsset{}).Count(&links).Error)
	require.Zero(t, links)
	var reloadedTask model.Task
	require.NoError(t, db.First(&reloadedTask, task.ID).Error)
	require.False(t, reloadedTask.PrivateData.AssetHostedResult, "generation failure must roll back the task marker")
}

func TestCreateVideoGenerationMarksTaskResultAsAssetHostedInTransaction(t *testing.T) {
	db := newVideoGenerationTestDB(t)
	now := time.Now().Unix()
	task := model.Task{TaskID: "asset-hosted-generation", UserId: 7, Status: model.TaskStatusNotStart, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&task).Error)
	normalized := &NormalizedVideoStudioSubmission{
		UserID: 7, ProfileID: 1, SpecificationVersion: 1, Model: "model", Mode: VideoModeTextToVideo,
		Prompt: "prompt", Parameters: map[string]any{},
	}

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := CreateVideoGeneration(context.Background(), tx, normalized, task.ID)
		return err
	}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.True(t, reloaded.PrivateData.AssetHostedResult)
}

func TestGetVideoGenerationUsesSafePublicFailureDetails(t *testing.T) {
	tests := []struct {
		name         string
		providerData string
		wantCode     string
	}{
		{
			name:         "unknown provider failure",
			providerData: `{"error":{"code":"provider_error","message":"copyright restrictions https://provider.example/private.mp4?token=secret"}}`,
		},
		{
			name:         "copyright restriction",
			providerData: `{"error":{"code":"video_generation_failed","message":"The output video may be related to copyright restrictions; OutputVideoSensitiveContentDetected.PolicyViolation; https://provider.example/private.mp4?token=secret"}}`,
			wantCode:     videoGenerationFailureCodeCopyrightRestriction,
		},
		{
			name:         "privacy restriction",
			providerData: `{"error":{"code":"video_generation_failed","message":"InputImageSensitiveContentDetected.PrivacyInformation"}}`,
			wantCode:     videoGenerationFailureCodePrivacyRestriction,
		},
		{
			name:         "content policy restriction",
			providerData: `{"error":{"code":"video_generation_failed","message":"OutputVideoSensitiveContentDetected.PolicyViolation"}}`,
			wantCode:     videoGenerationFailureCodeContentPolicy,
		},
		{
			name:         "unclassified generation failure",
			providerData: `{"error":{"code":"video_generation_failed","message":"provider failed without a recognized marker"}}`,
		},
		{
			name:         "non object provider data",
			providerData: `[]`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newVideoGenerationTestDB(t)
			now := time.Now().Unix()
			task := model.Task{
				TaskID: "hosted-failure", UserId: 7, Status: model.TaskStatusFailure,
				FailReason: "provider rejected the request", Data: []byte(test.providerData),
				CreatedAt: now, UpdatedAt: now,
			}
			require.NoError(t, db.Create(&task).Error)
			generation := model.KKAIVideoGeneration{
				UserID: 7, TaskID: task.ID, ModelProfileID: 1, Model: "model", Mode: VideoModeTextToVideo,
				Prompt: "prompt", Parameters: `{}`, CreatedAt: now, UpdatedAt: now,
			}
			require.NoError(t, db.Create(&generation).Error)

			view, err := GetVideoGeneration(context.Background(), db, 7, generation.ID)
			require.NoError(t, err)
			require.Equal(t, model.AssetHostedTaskPublicFailureReason, view.FailureReason)
			require.Equal(t, test.wantCode, view.FailureCode)
			response, err := common.Marshal(view)
			require.NoError(t, err)
			require.NotContains(t, string(response), "provider.example")
			require.NotContains(t, string(response), "secret")
		})
	}
}

func TestCreateVideoGenerationRejectsUnpublishedCatalogReference(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	now := time.Now().Unix()
	task := model.Task{TaskID: "unpublished-catalog-reference", UserId: 7, Status: model.TaskStatusNotStart, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&task).Error)
	asset := model.KKAIVideoAsset{
		OwnerUserID: 1, Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindReference,
		State: model.VideoAssetStateReady, ObjectKey: "hidden-reference.png", MIMEType: "image/png",
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)
	normalized := &NormalizedVideoStudioSubmission{
		UserID: 7, ProfileID: 1, SpecificationVersion: 1, Model: "model", Mode: VideoModeImageToVideo,
		Prompt: "prompt", Parameters: map[string]any{},
		ReferenceAssets: []NormalizedVideoReferenceAsset{{Role: model.VideoTaskAssetRoleReference, Asset: asset}},
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		_, createErr := CreateVideoGeneration(context.Background(), tx, normalized, task.ID)
		return createErr
	})
	require.ErrorIs(t, err, ErrInvalidVideoStudioSubmission)
	var generations int64
	var links int64
	require.NoError(t, db.Model(&model.KKAIVideoGeneration{}).Count(&generations).Error)
	require.NoError(t, db.Model(&model.KKAIVideoTaskAsset{}).Count(&links).Error)
	require.Zero(t, generations)
	require.Zero(t, links)
}

func TestListVideoGenerationsFiltersReadyBeforeCursorPagination(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	now := time.Now().Unix()
	createGeneration := func(index int, taskStatus model.TaskStatus, assetState string) model.KKAIVideoGeneration {
		task := model.Task{
			TaskID: fmt.Sprintf("ready-filter-%d", index), UserId: 7, Status: taskStatus,
			CreatedAt: now + int64(index), UpdatedAt: now + int64(index),
		}
		require.NoError(t, db.Create(&task).Error)
		generation := model.KKAIVideoGeneration{
			UserID: 7, TaskID: task.ID, ModelProfileID: 1, Model: "model", Mode: VideoModeTextToVideo,
			Prompt: "prompt", Parameters: `{}`, CreatedAt: now + int64(index), UpdatedAt: now + int64(index),
		}
		require.NoError(t, db.Create(&generation).Error)
		if assetState != "" {
			asset := model.KKAIVideoAsset{
				OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
				State: assetState, ObjectKey: fmt.Sprintf("output-%d.mp4", index), MIMEType: "video/mp4",
				PosterObjectKey: fmt.Sprintf("poster-%d.jpg", index), CreatedAt: now, UpdatedAt: now,
			}
			require.NoError(t, db.Create(&asset).Error)
			require.NoError(t, db.Create(&model.KKAIVideoTaskAsset{
				TaskID: task.ID, AssetID: asset.ID, Role: model.VideoTaskAssetRoleOutput, Position: 0, CreatedAt: now,
			}).Error)
		}
		return generation
	}
	readyOldest := createGeneration(1, model.TaskStatusSuccess, model.VideoAssetStateReady)
	readyMiddle := createGeneration(2, model.TaskStatusSuccess, model.VideoAssetStateReady)
	_ = createGeneration(3, model.TaskStatusInProgress, "")
	readyNewest := createGeneration(4, model.TaskStatusSuccess, model.VideoAssetStateReady)
	_ = createGeneration(5, model.TaskStatusSuccess, model.VideoAssetStateProcessing)

	queryCount := 0
	callbackName := "test:count_video_generation_list_queries"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(*gorm.DB) { queryCount++ }))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	first, err := ListVideoGenerations(context.Background(), db, 7, VideoGenerationListRequest{Status: "ready", Limit: 2})
	require.NoError(t, err)
	require.Equal(t, []int64{readyNewest.ID, readyMiddle.ID}, []int64{first.Items[0].ID, first.Items[1].ID})
	require.Equal(t, fmt.Sprint(readyMiddle.ID), first.NextCursor)
	require.LessOrEqual(t, queryCount, 4)

	second, err := ListVideoGenerations(context.Background(), db, 7, VideoGenerationListRequest{
		Status: "ready", Cursor: first.NextCursor, Limit: 2,
	})
	require.NoError(t, err)
	require.Len(t, second.Items, 1)
	require.Equal(t, readyOldest.ID, second.Items[0].ID)
	require.Empty(t, second.NextCursor)
}

func newVideoGenerationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:video-generation-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Task{}, &model.KKAIVideoGeneration{}, &model.KKAIVideoAsset{}, &model.KKAIVideoTaskAsset{},
	))
	return db
}
