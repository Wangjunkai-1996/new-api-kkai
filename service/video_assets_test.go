package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCreateVideoAssetUploadRestrictsCatalogSamplesToAdministrators(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	request := VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindSample, Filename: "sample.mp4", MIMEType: "video/mp4", SizeBytes: 1024,
	}

	_, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, request)
	require.ErrorIs(t, err, ErrInvalidVideoAssetUpload)
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, true, request)
	require.NoError(t, err)
	require.Equal(t, model.VideoAssetScopeCatalog, upload.Asset.Scope)
	require.Equal(t, model.VideoAssetKindSample, upload.Asset.Kind)
	require.Equal(t, upload.UploadLimits.SampleMaxBytes, upload.MaxSizeBytes)
	require.Equal(t, upload.UploadLimits.ArchiveMaxBytes, upload.MaxSizeBytes)
	require.Positive(t, upload.UploadLimits.ReferenceMaxBytes)
}

func TestCompleteVideoAssetUploadRejectsAnotherUser(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	now := time.Now().Unix()
	asset := model.KKAIVideoAsset{
		OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference,
		State: model.VideoAssetStatePendingUpload, ObjectKey: "users/7/reference.png",
		OriginalFilename: "reference.png", MIMEType: "image/png", SizeBytes: 10,
		UploadExpiresAt: now + 600, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)

	_, err := CompleteVideoAssetUpload(context.Background(), db, store, 8, false, asset.ID, VideoAssetCompleteRequest{})
	require.ErrorIs(t, err, ErrVideoAssetAccessDenied)
	_, err = GetVideoAssetUpload(context.Background(), db, store, 8, true, asset.ID)
	require.ErrorIs(t, err, ErrVideoAssetAccessDenied)
}

func TestSignAuthorizedVideoAssetUsesShortExpiryAndOwnership(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	now := time.Now().Unix()
	asset := model.KKAIVideoAsset{
		OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindOutput,
		State: model.VideoAssetStateReady, ObjectKey: "users/7/output.mp4",
		OriginalFilename: "output.mp4", MIMEType: "video/mp4", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)

	_, err := SignAuthorizedVideoAsset(context.Background(), db, store, 8, false, asset.ID, "", false)
	require.ErrorIs(t, err, ErrVideoAssetAccessDenied)
	location, err := SignAuthorizedVideoAsset(context.Background(), db, store, 7, false, asset.ID, "", true)
	require.NoError(t, err)
	require.Equal(t, "https://signed.example/object", location)
	require.Equal(t, asset.ObjectKey, store.downloadKey)
	require.True(t, store.downloadAttachment)
	require.Equal(t, 10*time.Minute, store.downloadExpires)
}

func TestSignAuthorizedVideoAssetHidesUnpublishedCatalogAssets(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	now := time.Now().Unix()
	profile := model.KKAIVideoModelProfile{
		Model: "catalog-model", DisplayName: "Catalog model", SpecificationVersion: 1,
		Specification: `{}`, DefaultParameters: `{}`, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&profile).Error)
	asset := model.KKAIVideoAsset{
		OwnerUserID: 7, Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindSample,
		State: model.VideoAssetStateReady, ObjectKey: "catalog/draft.mp4", MIMEType: "video/mp4",
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)
	sample := model.KKAIVideoSample{
		ModelProfileID: profile.ID, Title: "Draft", Prompt: "draft", Mode: VideoModeTextToVideo,
		ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`, VideoAssetID: asset.ID,
		AspectRatio: 1, Status: model.VideoSampleStatusDraft, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&sample).Error)

	_, err := SignAuthorizedVideoAsset(context.Background(), db, store, 8, false, asset.ID, "", false)
	require.ErrorIs(t, err, ErrVideoAssetAccessDenied)
	_, err = SignAuthorizedVideoAsset(context.Background(), db, store, 8, true, asset.ID, "", false)
	require.NoError(t, err)

	require.NoError(t, db.Model(&sample).Update("status", model.VideoSampleStatusPublished).Error)
	_, err = SignAuthorizedVideoAsset(context.Background(), db, store, 8, false, asset.ID, "", false)
	require.NoError(t, err)
}

func TestGetVideoAssetMetadataUsesOwnerAndPublishedCatalogAuthorization(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	now := time.Now().Unix()
	profile := model.KKAIVideoModelProfile{
		Model: "asset-metadata-model", DisplayName: "Asset metadata", SpecificationVersion: 1,
		Specification: `{}`, DefaultParameters: `{}`, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&profile).Error)
	assets := []model.KKAIVideoAsset{
		{OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateProcessing, ObjectKey: "users/7/metadata.png", MIMEType: "image/png", CreatedAt: now, UpdatedAt: now},
		{OwnerUserID: 7, Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindSample, State: model.VideoAssetStateReady, ObjectKey: "catalog/published.mp4", MIMEType: "video/mp4", CreatedAt: now, UpdatedAt: now},
		{OwnerUserID: 7, Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindSample, State: model.VideoAssetStateReady, ObjectKey: "catalog/draft.mp4", MIMEType: "video/mp4", CreatedAt: now, UpdatedAt: now},
	}
	for index := range assets {
		require.NoError(t, db.Create(&assets[index]).Error)
	}
	require.NoError(t, db.Create(&model.KKAIVideoSample{
		ModelProfileID: profile.ID, Title: "Published", Prompt: "prompt", Mode: VideoModeTextToVideo,
		ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`, VideoAssetID: assets[1].ID,
		AspectRatio: 1, Status: model.VideoSampleStatusPublished, CreatedAt: now, UpdatedAt: now,
	}).Error)
	require.NoError(t, db.Create(&model.KKAIVideoSample{
		ModelProfileID: profile.ID, Title: "Draft", Prompt: "prompt", Mode: VideoModeTextToVideo,
		ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`, VideoAssetID: assets[2].ID,
		AspectRatio: 1, Status: model.VideoSampleStatusDraft, CreatedAt: now, UpdatedAt: now,
	}).Error)

	owned, err := GetVideoAsset(context.Background(), db, 7, false, assets[0].ID)
	require.NoError(t, err)
	require.Equal(t, model.VideoAssetStateProcessing, owned.State)
	_, err = GetVideoAsset(context.Background(), db, 8, false, assets[0].ID)
	require.ErrorIs(t, err, ErrVideoAssetAccessDenied)
	published, err := GetVideoAsset(context.Background(), db, 8, false, assets[1].ID)
	require.NoError(t, err)
	require.Equal(t, assets[1].ID, published.ID)
	_, err = GetVideoAsset(context.Background(), db, 8, false, assets[2].ID)
	require.ErrorIs(t, err, ErrVideoAssetAccessDenied)
}

func TestDeleteVideoReferenceAssetAuditsTaskAndSampleReferences(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	now := time.Now().Unix()
	assets := []model.KKAIVideoAsset{
		{OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "users/7/free.png", MIMEType: "image/png", CreatedAt: now, UpdatedAt: now},
		{OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "users/7/active-task.png", MIMEType: "image/png", CreatedAt: now, UpdatedAt: now},
		{OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "users/7/terminal-task.png", MIMEType: "image/png", CreatedAt: now, UpdatedAt: now},
		{OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "users/7/draft-sample.png", MIMEType: "image/png", CreatedAt: now, UpdatedAt: now},
		{OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "users/7/published-sample.png", MIMEType: "image/png", CreatedAt: now, UpdatedAt: now},
	}
	for index := range assets {
		require.NoError(t, db.Create(&assets[index]).Error)
	}
	activeTask := model.Task{TaskID: "active-reference", UserId: 7, Status: model.TaskStatusInProgress, CreatedAt: now, UpdatedAt: now}
	terminalTask := model.Task{TaskID: "terminal-reference", UserId: 7, Status: model.TaskStatusSuccess, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&activeTask).Error)
	require.NoError(t, db.Create(&terminalTask).Error)
	require.NoError(t, db.Create(&model.KKAIVideoTaskAsset{
		TaskID: activeTask.ID, AssetID: assets[1].ID, Role: model.VideoTaskAssetRoleReference, CreatedAt: now,
	}).Error)
	require.NoError(t, db.Create(&model.KKAIVideoTaskAsset{
		TaskID: terminalTask.ID, AssetID: assets[2].ID, Role: model.VideoTaskAssetRoleReference, CreatedAt: now,
	}).Error)
	draftReferenceJSON, err := common.Marshal([]int64{assets[3].ID})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.KKAIVideoSample{
		ModelProfileID: 1, Title: "draft", Prompt: "draft", Mode: VideoModeImageToVideo,
		ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: string(draftReferenceJSON), VideoAssetID: 999,
		AspectRatio: 1, Status: model.VideoSampleStatusDraft, CreatedAt: now, UpdatedAt: now,
	}).Error)
	publishedReferenceJSON, err := common.Marshal([]int64{assets[4].ID})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.KKAIVideoSample{
		ModelProfileID: 1, Title: "published", Prompt: "published", Mode: VideoModeImageToVideo,
		ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: string(publishedReferenceJSON), VideoAssetID: 998,
		AspectRatio: 1, Status: model.VideoSampleStatusPublished, CreatedAt: now, UpdatedAt: now,
	}).Error)

	deleted, err := DeleteVideoReferenceAsset(context.Background(), db, 7, assets[0].ID)
	require.NoError(t, err)
	require.Equal(t, model.VideoAssetStateDeleting, deleted.State)
	_, err = DeleteVideoReferenceAsset(context.Background(), db, 8, assets[0].ID)
	require.ErrorIs(t, err, ErrVideoAssetAccessDenied)
	_, err = DeleteVideoReferenceAsset(context.Background(), db, 7, assets[1].ID)
	require.ErrorIs(t, err, ErrVideoAssetInUse)
	_, err = DeleteVideoReferenceAsset(context.Background(), db, 7, assets[2].ID)
	require.NoError(t, err)
	_, err = DeleteVideoReferenceAsset(context.Background(), db, 7, assets[3].ID)
	require.NoError(t, err)
	_, err = DeleteVideoReferenceAsset(context.Background(), db, 7, assets[4].ID)
	require.ErrorIs(t, err, ErrVideoAssetInUse)

	var events int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicDelete).Count(&events).Error)
	require.EqualValues(t, 3, events)
}

func TestDeleteVideoReferenceAssetRemovesMediaButKeepsTerminalGenerationAudit(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	now := time.Now().Unix()
	task := model.Task{TaskID: "terminal-deleted-generation", UserId: 7, Status: model.TaskStatusFailure, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&task).Error)
	generation := model.KKAIVideoGeneration{
		UserID: 7, TaskID: task.ID, ModelProfileID: 1, Model: "model", Mode: VideoModeImageToVideo,
		Prompt: "prompt", Parameters: `{}`, CreatedAt: now, UpdatedAt: now, DeletedAt: now,
	}
	require.NoError(t, db.Create(&generation).Error)
	asset := model.KKAIVideoAsset{
		OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference,
		State: model.VideoAssetStateReady, ObjectKey: "users/7/history/reference.png", MIMEType: "image/png",
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)
	require.NoError(t, db.Create(&model.KKAIVideoTaskAsset{
		TaskID: task.ID, AssetID: asset.ID, Role: model.VideoTaskAssetRoleReference, CreatedAt: now,
	}).Error)
	store.objects[asset.ObjectKey] = []byte("reference")
	store.contentType[asset.ObjectKey] = asset.MIMEType

	deleted, err := DeleteVideoReferenceAsset(context.Background(), db, 7, asset.ID)
	require.NoError(t, err)
	require.Equal(t, model.VideoAssetStateDeleting, deleted.State)
	pipeline, err := NewVideoAssetPipeline(db, store, staticVideoMediaProcessor{}, &staticVideoArchiveFetcher{}, t.TempDir())
	require.NoError(t, err)
	payload, err := common.Marshal(VideoAssetEventPayload{AssetID: asset.ID})
	require.NoError(t, err)
	require.NoError(t, pipeline.HandleDelete(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)}))

	require.NoError(t, db.First(&asset, asset.ID).Error)
	require.Equal(t, model.VideoAssetStateDeleted, asset.State)
	require.NotZero(t, asset.DeletedAt)
	require.NotContains(t, store.objects, asset.ObjectKey)
	var generationCount int64
	var linkCount int64
	require.NoError(t, db.Model(&model.KKAIVideoGeneration{}).Where("id = ?", generation.ID).Count(&generationCount).Error)
	require.NoError(t, db.Model(&model.KKAIVideoTaskAsset{}).Where("task_id = ? AND asset_id = ?", task.ID, asset.ID).Count(&linkCount).Error)
	require.EqualValues(t, 1, generationCount)
	require.EqualValues(t, 1, linkCount)

	newTask := model.Task{TaskID: "reuse-deleted-reference", UserId: 7, Status: model.TaskStatusNotStart, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&newTask).Error)
	normalized := &NormalizedVideoStudioSubmission{
		UserID: 7, ProfileID: 1, SpecificationVersion: 1, Model: "model", Mode: VideoModeImageToVideo,
		Prompt: "prompt", Parameters: map[string]any{},
		ReferenceAssets: []NormalizedVideoReferenceAsset{{Role: model.VideoTaskAssetRoleReference, Asset: asset}},
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		_, createErr := CreateVideoGeneration(context.Background(), tx, normalized, newTask.ID)
		return createErr
	})
	require.ErrorIs(t, err, ErrInvalidVideoStudioSubmission)
}

func TestCleanupAbandonedVideoReferenceAssetsSkipsRecentAndReferencedAssets(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	now := time.Now()
	assets := []model.KKAIVideoAsset{
		{OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "users/7/orphan.png", MIMEType: "image/png", CreatedAt: now.Add(-48 * time.Hour).Unix(), UpdatedAt: now.Unix()},
		{OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "users/7/recent.png", MIMEType: "image/png", CreatedAt: now.Add(-time.Hour).Unix(), UpdatedAt: now.Unix()},
		{OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "users/7/used.png", MIMEType: "image/png", CreatedAt: now.Add(-48 * time.Hour).Unix(), UpdatedAt: now.Unix()},
	}
	for index := range assets {
		require.NoError(t, db.Create(&assets[index]).Error)
	}
	activeTask := model.Task{
		TaskID: "cleanup-active-reference", UserId: 7, Status: model.TaskStatusQueued,
		CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
	}
	require.NoError(t, db.Create(&activeTask).Error)
	require.NoError(t, db.Create(&model.KKAIVideoTaskAsset{
		TaskID: activeTask.ID, AssetID: assets[2].ID, Role: model.VideoTaskAssetRoleFirstFrame, CreatedAt: now.Unix(),
	}).Error)

	cleaned, err := CleanupAbandonedVideoReferenceAssets(context.Background(), db, now.Add(-24*time.Hour), 100)
	require.NoError(t, err)
	require.Equal(t, 1, cleaned)
	for index, expected := range []string{model.VideoAssetStateDeleting, model.VideoAssetStateReady, model.VideoAssetStateReady} {
		require.NoError(t, db.First(&assets[index], assets[index].ID).Error)
		require.Equal(t, expected, assets[index].State)
	}
}

func TestCleanupAbandonedVideoReferenceAssetsDoesNotStarveBehindUsedHeadRows(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	now := time.Now()
	assets := []model.KKAIVideoAsset{
		{OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "users/7/unknown-task.png", MIMEType: "image/png", CreatedAt: now.Add(-72 * time.Hour).Unix(), UpdatedAt: now.Unix()},
		{OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "users/7/published-sample.png", MIMEType: "image/png", CreatedAt: now.Add(-71 * time.Hour).Unix(), UpdatedAt: now.Unix()},
		{OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "users/7/orphan-after-used-head.png", MIMEType: "image/png", CreatedAt: now.Add(-70 * time.Hour).Unix(), UpdatedAt: now.Unix()},
	}
	for index := range assets {
		require.NoError(t, db.Create(&assets[index]).Error)
	}
	unknownTask := model.Task{
		TaskID: "cleanup-unknown-reference", UserId: 7, Status: model.TaskStatusUnknown,
		CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
	}
	require.NoError(t, db.Create(&unknownTask).Error)
	require.NoError(t, db.Create(&model.KKAIVideoTaskAsset{
		TaskID: unknownTask.ID, AssetID: assets[0].ID, Role: model.VideoTaskAssetRoleReference, CreatedAt: now.Unix(),
	}).Error)
	referenceIDs, err := common.Marshal([]int64{assets[1].ID})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.KKAIVideoSample{
		ModelProfileID: 1, Title: "Published sample", Prompt: "prompt", Mode: VideoModeImageToVideo,
		ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: string(referenceIDs), VideoAssetID: 999,
		AspectRatio: 1, Status: model.VideoSampleStatusPublished, CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
	}).Error)

	cleaned, err := CleanupAbandonedVideoReferenceAssets(context.Background(), db, now.Add(-24*time.Hour), 1)
	require.NoError(t, err)
	require.Equal(t, 1, cleaned)
	for index, expected := range []string{model.VideoAssetStateReady, model.VideoAssetStateReady, model.VideoAssetStateDeleting} {
		require.NoError(t, db.First(&assets[index], assets[index].ID).Error)
		require.Equal(t, expected, assets[index].State)
	}
}
