package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedVideoCatalogLifecycle(t *testing.T) (*gorm.DB, model.KKAIVideoModelProfile, []model.KKAIVideoAsset) {
	t.Helper()
	db := newVideoPipelineTestDB(t)
	now := time.Now().Unix()
	specification, err := common.Marshal(VideoModelSpec{
		Version: 1,
		Modes:   []string{VideoModeFirstLastFrame},
		ReferenceInputs: []VideoReferenceInputSpec{
			{Role: model.VideoTaskAssetRoleFirstFrame, RequestKey: "first_frame", Required: true},
			{Role: model.VideoTaskAssetRoleLastFrame, RequestKey: "last_frame", Required: true},
		},
	})
	require.NoError(t, err)
	profile := model.KKAIVideoModelProfile{
		Model: "catalog-lifecycle-model", DisplayName: "Catalog lifecycle", SpecificationVersion: 1,
		Specification: string(specification), DefaultParameters: `{}`, Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&profile).Error)
	assets := []model.KKAIVideoAsset{
		{
			OwnerUserID: 7, Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindSample,
			State: model.VideoAssetStateReady, ObjectKey: "catalog/old/source.mp4",
			PosterObjectKey: "catalog/old/poster.jpg", PreviewObjectKey: "catalog/old/preview.mp4",
			MIMEType: "video/mp4", Width: 1280, Height: 720, CreatedAt: now, UpdatedAt: now,
		},
		{
			OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindSample,
			State: model.VideoAssetStateReady, ObjectKey: "users/7/new/source.mp4",
			PosterObjectKey: "users/7/new/poster.jpg", PreviewObjectKey: "users/7/new/preview.mp4",
			MIMEType: "video/mp4", Width: 1280, Height: 720, CreatedAt: now, UpdatedAt: now,
		},
		{OwnerUserID: 7, Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "catalog/old-first.jpg", MIMEType: "image/jpeg", CreatedAt: now, UpdatedAt: now},
		{OwnerUserID: 7, Scope: model.VideoAssetScopeCatalog, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "catalog/old-last.jpg", MIMEType: "image/jpeg", CreatedAt: now, UpdatedAt: now},
		{OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "users/7/new-first.jpg", MIMEType: "image/jpeg", CreatedAt: now, UpdatedAt: now},
		{OwnerUserID: 7, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference, State: model.VideoAssetStateReady, ObjectKey: "users/7/new-last.jpg", MIMEType: "image/jpeg", CreatedAt: now, UpdatedAt: now},
	}
	for index := range assets {
		require.NoError(t, db.Create(&assets[index]).Error)
	}
	return db, profile, assets
}

func videoCatalogLifecycleInput(
	profileID int64,
	videoAssetID int64,
	referenceAssetIDs []int64,
	status string,
) VideoSampleInput {
	return VideoSampleInput{
		ModelProfileID: profileID, Title: "Lifecycle sample", Prompt: "A controlled camera move",
		Mode: VideoModeFirstLastFrame, Parameters: map[string]any{}, ReferenceAssetIDs: referenceAssetIDs,
		VideoAssetID: videoAssetID, AspectRatio: 16.0 / 9.0, Category: model.VideoSampleCategoryOther,
		Status: status,
	}
}

func TestUpdateVideoSampleQueuesOnlyAssetsRemovedFromCatalog(t *testing.T) {
	tests := []struct {
		name             string
		referenceIndexes []int
		deletingIndexes  []int
		readyIndexes     []int
	}{
		{name: "replaces some references", referenceIndexes: []int{2, 4}, deletingIndexes: []int{0, 3}, readyIndexes: []int{2}},
		{name: "replaces all references", referenceIndexes: []int{4, 5}, deletingIndexes: []int{0, 2, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, profile, assets := seedVideoCatalogLifecycle(t)
			created, err := CreateVideoSample(context.Background(), db, 7,
				videoCatalogLifecycleInput(profile.ID, assets[0].ID, []int64{assets[2].ID, assets[3].ID}, model.VideoSampleStatusDraft))
			require.NoError(t, err)
			references := make([]int64, 0, len(tt.referenceIndexes))
			for _, index := range tt.referenceIndexes {
				references = append(references, assets[index].ID)
			}

			_, err = UpdateVideoSample(context.Background(), db, created.ID, 7,
				videoCatalogLifecycleInput(profile.ID, assets[1].ID, references, model.VideoSampleStatusDraft))
			require.NoError(t, err)
			for _, index := range tt.deletingIndexes {
				require.NoError(t, db.First(&assets[index], assets[index].ID).Error)
				require.Equal(t, model.VideoAssetStateDeleting, assets[index].State)
			}
			for _, index := range tt.readyIndexes {
				require.NoError(t, db.First(&assets[index], assets[index].ID).Error)
				require.Equal(t, model.VideoAssetStateReady, assets[index].State)
			}
			for _, index := range append([]int{1}, tt.referenceIndexes...) {
				require.NoError(t, db.First(&assets[index], assets[index].ID).Error)
				require.Equal(t, model.VideoAssetScopeCatalog, assets[index].Scope)
			}
			var deleteEvents int64
			require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicDelete).Count(&deleteEvents).Error)
			require.EqualValues(t, len(tt.deletingIndexes), deleteEvents)
		})
	}
}

func TestUpdateVideoSampleReusingAssetsDoesNotQueueDeletion(t *testing.T) {
	db, profile, assets := seedVideoCatalogLifecycle(t)
	input := videoCatalogLifecycleInput(profile.ID, assets[0].ID, []int64{assets[2].ID, assets[3].ID}, model.VideoSampleStatusDraft)
	created, err := CreateVideoSample(context.Background(), db, 7, input)
	require.NoError(t, err)
	input.Title = "Renamed lifecycle sample"

	_, err = UpdateVideoSample(context.Background(), db, created.ID, 7, input)
	require.NoError(t, err)
	var deleteEvents int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicDelete).Count(&deleteEvents).Error)
	require.Zero(t, deleteEvents)
	for _, index := range []int{0, 2, 3} {
		require.NoError(t, db.First(&assets[index], assets[index].ID).Error)
		require.Equal(t, model.VideoAssetStateReady, assets[index].State)
	}
}

func TestDeleteVideoSampleKeepsAssetsUsedByAnotherSample(t *testing.T) {
	for _, remainingStatus := range []string{model.VideoSampleStatusDraft, model.VideoSampleStatusPublished} {
		t.Run(remainingStatus, func(t *testing.T) {
			db, profile, assets := seedVideoCatalogLifecycle(t)
			input := videoCatalogLifecycleInput(profile.ID, assets[0].ID, []int64{assets[2].ID, assets[3].ID}, model.VideoSampleStatusDraft)
			removed, err := CreateVideoSample(context.Background(), db, 7, input)
			require.NoError(t, err)
			input.Title = "Shared asset sample"
			input.Status = remainingStatus
			_, err = CreateVideoSample(context.Background(), db, 7, input)
			require.NoError(t, err)

			require.NoError(t, DeleteVideoSample(context.Background(), db, removed.ID))
			var deleteEvents int64
			require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicDelete).Count(&deleteEvents).Error)
			require.Zero(t, deleteEvents)
			for _, index := range []int{0, 2, 3} {
				require.NoError(t, db.First(&assets[index], assets[index].ID).Error)
				require.Equal(t, model.VideoAssetStateReady, assets[index].State)
			}
		})
	}
}

func TestDeleteVideoSampleQueuesEveryUniqueUnusedAssetOnce(t *testing.T) {
	db, profile, assets := seedVideoCatalogLifecycle(t)
	created, err := CreateVideoSample(context.Background(), db, 7,
		videoCatalogLifecycleInput(profile.ID, assets[0].ID, []int64{assets[2].ID, assets[3].ID}, model.VideoSampleStatusDraft))
	require.NoError(t, err)

	require.NoError(t, DeleteVideoSample(context.Background(), db, created.ID))
	var events []model.KKAIOutboxEvent
	require.NoError(t, db.Where("topic = ?", VideoOutboxTopicDelete).Order("aggregate_id ASC").Find(&events).Error)
	require.Len(t, events, 3)
	eventsByAsset := make(map[int64]int, len(events))
	for _, event := range events {
		assetID, parseErr := strconv.ParseInt(event.AggregateID, 10, 64)
		require.NoError(t, parseErr)
		eventsByAsset[assetID]++
	}
	for _, index := range []int{0, 2, 3} {
		require.Equal(t, 1, eventsByAsset[assets[index].ID])
		require.NoError(t, db.First(&assets[index], assets[index].ID).Error)
		require.Equal(t, model.VideoAssetStateDeleting, assets[index].State)
	}
}

func TestPublishedVideoSampleInvariantRollsBackAssetLifecycle(t *testing.T) {
	db, profile, assets := seedVideoCatalogLifecycle(t)
	created, err := CreateVideoSample(context.Background(), db, 7,
		videoCatalogLifecycleInput(profile.ID, assets[0].ID, []int64{assets[2].ID, assets[3].ID}, model.VideoSampleStatusPublished))
	require.NoError(t, err)

	_, err = UpdateVideoSample(context.Background(), db, created.ID, 7,
		videoCatalogLifecycleInput(profile.ID, assets[1].ID, []int64{assets[4].ID, assets[5].ID}, model.VideoSampleStatusDraft))
	require.ErrorIs(t, err, ErrVideoModelNeedsSample)
	var persisted model.KKAIVideoSample
	require.NoError(t, db.First(&persisted, created.ID).Error)
	require.Equal(t, model.VideoSampleStatusPublished, persisted.Status)
	require.Equal(t, assets[0].ID, persisted.VideoAssetID)
	for _, index := range []int{0, 2, 3} {
		require.NoError(t, db.First(&assets[index], assets[index].ID).Error)
		require.Equal(t, model.VideoAssetStateReady, assets[index].State)
		require.Equal(t, model.VideoAssetScopeCatalog, assets[index].Scope)
	}
	for _, index := range []int{1, 4, 5} {
		require.NoError(t, db.First(&assets[index], assets[index].ID).Error)
		require.Equal(t, model.VideoAssetScopeUser, assets[index].Scope)
	}
	var deleteEvents int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicDelete).Count(&deleteEvents).Error)
	require.Zero(t, deleteEvents)
}

func TestDeleteLastPublishedVideoSampleRollsBackAssetLifecycle(t *testing.T) {
	db, profile, assets := seedVideoCatalogLifecycle(t)
	created, err := CreateVideoSample(context.Background(), db, 7,
		videoCatalogLifecycleInput(profile.ID, assets[0].ID, []int64{assets[2].ID, assets[3].ID}, model.VideoSampleStatusPublished))
	require.NoError(t, err)

	err = DeleteVideoSample(context.Background(), db, created.ID)
	require.ErrorIs(t, err, ErrVideoModelNeedsSample)
	var persisted model.KKAIVideoSample
	require.NoError(t, db.First(&persisted, created.ID).Error)
	require.Equal(t, model.VideoSampleStatusPublished, persisted.Status)
	for _, index := range []int{0, 2, 3} {
		require.NoError(t, db.First(&assets[index], assets[index].ID).Error)
		require.Equal(t, model.VideoAssetStateReady, assets[index].State)
		require.Equal(t, model.VideoAssetScopeCatalog, assets[index].Scope)
	}
	var deleteEvents int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicDelete).Count(&deleteEvents).Error)
	require.Zero(t, deleteEvents)
}

func TestVideoSampleRejectsDeletingAndDeletedAssets(t *testing.T) {
	for _, state := range []string{model.VideoAssetStateDeleting, model.VideoAssetStateDeleted} {
		t.Run(state, func(t *testing.T) {
			db, profile, assets := seedVideoCatalogLifecycle(t)
			require.NoError(t, db.Model(&assets[0]).Update("state", state).Error)

			_, err := CreateVideoSample(context.Background(), db, 7,
				videoCatalogLifecycleInput(profile.ID, assets[0].ID, []int64{assets[2].ID, assets[3].ID}, model.VideoSampleStatusDraft))
			require.ErrorIs(t, err, ErrVideoSampleNotPublishable)
			var samples int64
			require.NoError(t, db.Model(&model.KKAIVideoSample{}).Count(&samples).Error)
			require.Zero(t, samples)
		})
	}
}

func TestUpdateVideoSampleRejectsDeletingAndDeletedAssets(t *testing.T) {
	for _, state := range []string{model.VideoAssetStateDeleting, model.VideoAssetStateDeleted} {
		t.Run(state, func(t *testing.T) {
			db, profile, assets := seedVideoCatalogLifecycle(t)
			created, err := CreateVideoSample(context.Background(), db, 7,
				videoCatalogLifecycleInput(profile.ID, assets[0].ID, []int64{assets[2].ID, assets[3].ID}, model.VideoSampleStatusDraft))
			require.NoError(t, err)
			require.NoError(t, db.Model(&assets[1]).Update("state", state).Error)

			_, err = UpdateVideoSample(context.Background(), db, created.ID, 7,
				videoCatalogLifecycleInput(profile.ID, assets[1].ID, []int64{assets[4].ID, assets[5].ID}, model.VideoSampleStatusDraft))
			require.ErrorIs(t, err, ErrVideoSampleNotPublishable)
			var persisted model.KKAIVideoSample
			require.NoError(t, db.First(&persisted, created.ID).Error)
			require.Equal(t, assets[0].ID, persisted.VideoAssetID)
			for _, index := range []int{0, 2, 3} {
				require.NoError(t, db.First(&assets[index], assets[index].ID).Error)
				require.Equal(t, model.VideoAssetStateReady, assets[index].State)
			}
			var deleteEvents int64
			require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicDelete).Count(&deleteEvents).Error)
			require.Zero(t, deleteEvents)
		})
	}
}
