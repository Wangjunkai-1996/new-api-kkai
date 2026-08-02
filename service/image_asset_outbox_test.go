package service

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageAssetOutboxPipelineCreatesThumbnailAndMarksReady(t *testing.T) {
	db := newImageLibraryTestDB(t)
	store := newMemoryVideoAssetStore()
	store.objects["image/original"] = []byte("original-image")
	store.contentType["image/original"] = "image/png"
	now := time.Now().Unix()
	asset := model.KKAIImageAsset{
		OwnerUserID: 7, Scope: model.ImageAssetScopeUser, Kind: model.ImageAssetKindOutput,
		State: model.ImageAssetStateReady, ObjectKey: "image/original",
		ThumbnailState: model.ImageThumbnailStatePending, MIMEType: "image/png",
		SizeBytes: int64(len(store.objects["image/original"])), Width: 10, Height: 10,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)
	pipeline, err := NewImageAssetOutboxPipeline(db, store, staticVideoMediaProcessor{}, t.TempDir())
	require.NoError(t, err)
	payload, err := common.Marshal(imageThumbnailPayload{AssetID: asset.ID})
	require.NoError(t, err)

	require.NoError(t, pipeline.HandleThumbnail(context.Background(), model.KKAIOutboxEvent{
		Topic: ImageAssetThumbnailTopic, AggregateID: "1", Payload: string(payload),
	}))
	require.NoError(t, db.First(&asset, asset.ID).Error)
	assert.Equal(t, model.ImageThumbnailStateReady, asset.ThumbnailState)
	assert.Equal(t, "image/original.thumbnail.jpg", asset.ThumbnailObjectKey)
	assert.Equal(t, []byte("poster"), store.objects[asset.ThumbnailObjectKey])
}

func TestImageAssetDeleteOutboxRemovesOriginalAndThumbnail(t *testing.T) {
	db := newImageLibraryTestDB(t)
	store := newMemoryVideoAssetStore()
	store.objects["image/original"] = []byte("original")
	store.objects["image/thumbnail"] = []byte("thumbnail")
	store.objects["image/original.thumbnail.jpg"] = []byte("deterministic-thumbnail")
	pipeline, err := NewImageAssetOutboxPipeline(db, store, staticVideoMediaProcessor{}, t.TempDir())
	require.NoError(t, err)
	payload, err := common.Marshal(imageAssetDeletePayload{
		AssetID: 1, ObjectKey: "image/original", ThumbnailObjectKey: "image/thumbnail",
	})
	require.NoError(t, err)

	require.NoError(t, pipeline.HandleDelete(context.Background(), model.KKAIOutboxEvent{
		Topic: ImageAssetDeleteTopic, AggregateID: "1", Payload: string(payload),
	}))
	assert.NotContains(t, store.objects, "image/original")
	assert.NotContains(t, store.objects, "image/thumbnail")
	assert.NotContains(t, store.objects, "image/original.thumbnail.jpg")
}

func TestImageThumbnailRetryCleansObjectCreatedDuringConcurrentDeletion(t *testing.T) {
	db := newImageLibraryTestDB(t)
	memoryStore := newMemoryVideoAssetStore()
	memoryStore.objects["image/racy-original"] = []byte("original-image")
	memoryStore.contentType["image/racy-original"] = "image/png"
	now := time.Now().Unix()
	asset := model.KKAIImageAsset{
		OwnerUserID: 7, Scope: model.ImageAssetScopeUser, Kind: model.ImageAssetKindOutput,
		State: model.ImageAssetStateReady, ObjectKey: "image/racy-original",
		ThumbnailState: model.ImageThumbnailStatePending, MIMEType: "image/png",
		SizeBytes: int64(len(memoryStore.objects["image/racy-original"])), Width: 10, Height: 10,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&asset).Error)
	store := &racyImageThumbnailStore{memoryVideoAssetStore: memoryStore, failFirstDelete: true}
	store.afterThumbnailPut = func() {
		require.NoError(t, db.Model(&model.KKAIImageAsset{}).Where("id = ?", asset.ID).Updates(map[string]any{
			"state": model.ImageAssetStateDeleted, "deleted_at": time.Now().Unix(), "updated_at": time.Now().Unix(),
		}).Error)
	}
	pipeline, err := NewImageAssetOutboxPipeline(db, store, staticVideoMediaProcessor{}, t.TempDir())
	require.NoError(t, err)
	thumbnailPayload, err := common.Marshal(imageThumbnailPayload{AssetID: asset.ID})
	require.NoError(t, err)
	thumbnailEvent := model.KKAIOutboxEvent{
		Topic: ImageAssetThumbnailTopic, AggregateID: "1", Payload: string(thumbnailPayload),
	}

	require.Error(t, pipeline.HandleThumbnail(context.Background(), thumbnailEvent))
	require.Contains(t, memoryStore.objects, imageThumbnailObjectKey(asset.ObjectKey))
	store.afterThumbnailPut = nil
	require.NoError(t, pipeline.HandleThumbnail(context.Background(), thumbnailEvent))
	require.NotContains(t, memoryStore.objects, imageThumbnailObjectKey(asset.ObjectKey))
	deletePayload, err := common.Marshal(imageAssetDeletePayload{AssetID: asset.ID, ObjectKey: asset.ObjectKey})
	require.NoError(t, err)
	require.NoError(t, pipeline.HandleDelete(context.Background(), model.KKAIOutboxEvent{
		Topic: ImageAssetDeleteTopic, AggregateID: "1", Payload: string(deletePayload),
	}))
	require.NotContains(t, memoryStore.objects, asset.ObjectKey)
}

type racyImageThumbnailStore struct {
	*memoryVideoAssetStore
	afterThumbnailPut func()
	failFirstDelete   bool
}

func (store *racyImageThumbnailStore) Put(
	ctx context.Context, key string, contentType string, reader io.Reader, length int64,
) error {
	if err := store.memoryVideoAssetStore.Put(ctx, key, contentType, reader, length); err != nil {
		return err
	}
	if store.afterThumbnailPut != nil && key == "image/racy-original.thumbnail.jpg" {
		store.afterThumbnailPut()
	}
	return nil
}

func (store *racyImageThumbnailStore) Delete(ctx context.Context, keys []string) error {
	if store.failFirstDelete {
		store.failFirstDelete = false
		return errors.New("transient object delete failure")
	}
	return store.memoryVideoAssetStore.Delete(ctx, keys)
}
