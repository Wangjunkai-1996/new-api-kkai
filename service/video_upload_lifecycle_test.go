package service

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
)

type blockingDeleteVideoAssetStore struct {
	*memoryVideoAssetStore
	deleteStarted chan struct{}
	releaseDelete chan struct{}
	startOnce     sync.Once
}

func (store *blockingDeleteVideoAssetStore) Delete(ctx context.Context, keys []string) error {
	store.startOnce.Do(func() { close(store.deleteStarted) })
	select {
	case <-store.releaseDelete:
	case <-ctx.Done():
		return ctx.Err()
	}
	return store.memoryVideoAssetStore.Delete(ctx, keys)
}

type blockingCompleteMultipartVideoAssetStore struct {
	*multipartVideoAssetStore
	completeStarted chan struct{}
	releaseComplete chan struct{}
	startOnce       sync.Once
}

func (store *blockingCompleteMultipartVideoAssetStore) CompleteMultipartUpload(
	ctx context.Context,
	key string,
	_ string,
	parts []VideoAssetCompletedPart,
) error {
	store.completeCalls++
	store.startOnce.Do(func() { close(store.completeStarted) })
	select {
	case <-store.releaseComplete:
	case <-ctx.Done():
		return ctx.Err()
	}
	var size int64
	for _, part := range store.parts {
		size += part.SizeBytes
	}
	if len(parts) != len(store.parts) {
		return ErrInvalidVideoAssetUpload
	}
	return store.Put(ctx, key, store.uploadContentType, bytes.NewReader(make([]byte, size)), size)
}

func TestSingleVideoUploadAbortClaimsStateBeforeDeletingObject(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := &blockingDeleteVideoAssetStore{
		memoryVideoAssetStore: newMemoryVideoAssetStore(),
		deleteStarted:         make(chan struct{}),
		releaseDelete:         make(chan struct{}),
	}
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png", SizeBytes: 5,
	})
	require.NoError(t, err)
	var reserved model.KKAIVideoAsset
	require.NoError(t, db.First(&reserved, upload.Asset.ID).Error)

	type abortResult struct {
		asset *VideoAssetView
		err   error
	}
	abortDone := make(chan abortResult, 1)
	go func() {
		asset, abortErr := AbortVideoAssetUpload(context.Background(), db, store, 7, false, upload.Asset.ID)
		abortDone <- abortResult{asset: asset, err: abortErr}
	}()
	<-store.deleteStarted

	var duringAbort model.KKAIVideoAsset
	require.NoError(t, db.First(&duringAbort, upload.Asset.ID).Error)
	require.NoError(t, store.Put(context.Background(), reserved.ObjectKey, "image/png", bytes.NewReader([]byte("image")), 5))
	completed, completeErr := CompleteVideoAssetUpload(
		context.Background(), db, store, 7, false, upload.Asset.ID, VideoAssetCompleteRequest{},
	)
	close(store.releaseDelete)
	aborted := <-abortDone

	require.Equal(t, model.VideoAssetStateDeleting, duringAbort.State)
	require.Nil(t, completed)
	require.ErrorIs(t, completeErr, ErrInvalidVideoAssetUpload)
	require.NoError(t, aborted.err)
	require.Equal(t, model.VideoAssetStateDeleting, aborted.asset.State)
}

func TestSingleVideoUploadAbortRemovesPutThatArrivesAfterAbort(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png", SizeBytes: 5,
	})
	require.NoError(t, err)

	aborted, err := AbortVideoAssetUpload(context.Background(), db, store, 7, false, upload.Asset.ID)
	require.NoError(t, err)
	require.Equal(t, model.VideoAssetStateDeleting, aborted.State)

	var asset model.KKAIVideoAsset
	require.NoError(t, db.First(&asset, upload.Asset.ID).Error)
	require.NoError(t, store.Put(context.Background(), asset.ObjectKey, "image/png", bytes.NewReader([]byte("image")), 5))
	require.NoError(t, db.Model(&asset).Update("upload_expires_at", time.Now().Add(-time.Minute).Unix()).Error)

	expired, err := ExpireVideoAssetUploads(context.Background(), db, store, 20)
	require.NoError(t, err)
	require.Equal(t, 1, expired)
	require.NoError(t, db.First(&asset, upload.Asset.ID).Error)
	require.Equal(t, model.VideoAssetStateDeleted, asset.State)
	require.NotContains(t, store.objects, asset.ObjectKey)
}

func TestExpiredSingleVideoUploadTombstoneRemovesPutCompletingAfterCleanup(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png", SizeBytes: 5,
	})
	require.NoError(t, err)
	var asset model.KKAIVideoAsset
	require.NoError(t, db.First(&asset, upload.Asset.ID).Error)
	require.NoError(t, db.Model(&asset).Update("upload_expires_at", time.Now().Add(-time.Minute).Unix()).Error)

	expired, err := ExpireVideoAssetUploads(context.Background(), db, store, 20)
	require.NoError(t, err)
	require.Equal(t, 1, expired)
	require.NoError(t, db.First(&asset, upload.Asset.ID).Error)
	require.Equal(t, model.VideoAssetStateDeleted, asset.State)
	require.Greater(t, asset.UploadExpiresAt, time.Now().Unix())

	require.NoError(t, store.Put(context.Background(), asset.ObjectKey, "image/png", bytes.NewReader([]byte("image")), 5))
	require.NoError(t, db.Model(&asset).Update("upload_expires_at", time.Now().Add(-time.Minute).Unix()).Error)
	expired, err = ExpireVideoAssetUploads(context.Background(), db, store, 20)
	require.NoError(t, err)
	require.Equal(t, 1, expired)
	require.NotContains(t, store.objects, asset.ObjectKey)
	require.NoError(t, db.First(&asset, upload.Asset.ID).Error)
	require.Equal(t, model.VideoAssetStateDeleted, asset.State)
	require.Greater(t, asset.UploadExpiresAt, time.Now().Unix())
}

func TestExpireVideoAssetUploadsPrioritizesActiveUploadsOverTombstones(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	tombstoneUpload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "old.png", MIMEType: "image/png", SizeBytes: 3,
	})
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Where("id = ?", tombstoneUpload.Asset.ID).
		Update("upload_expires_at", time.Now().Add(-time.Hour).Unix()).Error)
	expired, err := ExpireVideoAssetUploads(context.Background(), db, store, 1)
	require.NoError(t, err)
	require.Equal(t, 1, expired)

	activeUpload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "active.png", MIMEType: "image/png", SizeBytes: 6,
	})
	require.NoError(t, err)
	var tombstone model.KKAIVideoAsset
	var active model.KKAIVideoAsset
	require.NoError(t, db.First(&tombstone, tombstoneUpload.Asset.ID).Error)
	require.NoError(t, db.First(&active, activeUpload.Asset.ID).Error)
	require.NoError(t, store.Put(context.Background(), tombstone.ObjectKey, "image/png", bytes.NewReader([]byte("old")), 3))
	require.NoError(t, db.Model(&tombstone).Update("upload_expires_at", time.Now().Add(-2*time.Hour).Unix()).Error)
	require.NoError(t, db.Model(&active).Update("upload_expires_at", time.Now().Add(-time.Minute).Unix()).Error)

	expired, err = ExpireVideoAssetUploads(context.Background(), db, store, 1)
	require.NoError(t, err)
	require.Equal(t, 1, expired)
	require.Contains(t, store.objects, tombstone.ObjectKey)
	require.NoError(t, db.First(&active, active.ID).Error)
	require.Equal(t, model.VideoAssetStateDeleted, active.State)
}

func TestExpireVideoAssetUploadsRotatesOldestTombstonesFirst(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	uploads := make([]*VideoAssetUpload, 0, 2)
	for _, filename := range []string{"first.png", "second.png"} {
		upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
			Purpose: model.VideoAssetKindReference, Filename: filename, MIMEType: "image/png", SizeBytes: 3,
		})
		require.NoError(t, err)
		require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Where("id = ?", upload.Asset.ID).
			Update("upload_expires_at", time.Now().Add(-time.Hour).Unix()).Error)
		expired, err := ExpireVideoAssetUploads(context.Background(), db, store, 1)
		require.NoError(t, err)
		require.Equal(t, 1, expired)
		uploads = append(uploads, upload)
	}

	var first model.KKAIVideoAsset
	var second model.KKAIVideoAsset
	require.NoError(t, db.First(&first, uploads[0].Asset.ID).Error)
	require.NoError(t, db.First(&second, uploads[1].Asset.ID).Error)
	require.NoError(t, store.Put(context.Background(), first.ObjectKey, "image/png", bytes.NewReader([]byte("one")), 3))
	require.NoError(t, store.Put(context.Background(), second.ObjectKey, "image/png", bytes.NewReader([]byte("two")), 3))
	require.NoError(t, db.Model(&first).Update("upload_expires_at", time.Now().Add(-time.Minute).Unix()).Error)
	require.NoError(t, db.Model(&second).Update("upload_expires_at", time.Now().Add(-2*time.Minute).Unix()).Error)

	expired, err := ExpireVideoAssetUploads(context.Background(), db, store, 1)
	require.NoError(t, err)
	require.Equal(t, 1, expired)
	require.Contains(t, store.objects, first.ObjectKey)
	require.NotContains(t, store.objects, second.ObjectKey)
}

func TestMultipartVideoUploadAbortCleansObjectCompletedAfterAbort(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := &blockingCompleteMultipartVideoAssetStore{
		multipartVideoAssetStore: newMultipartVideoAssetStore(),
		completeStarted:          make(chan struct{}),
		releaseComplete:          make(chan struct{}),
	}
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png",
		SizeBytes: videoMultipartPartSize, Multipart: true,
	})
	require.NoError(t, err)
	store.parts = []VideoAssetUploadedPart{{PartNumber: 1, SizeBytes: videoMultipartPartSize, ETag: `"etag-1"`}}

	type completeResult struct {
		asset *VideoAssetView
		err   error
	}
	completeDone := make(chan completeResult, 1)
	go func() {
		asset, completeErr := CompleteVideoAssetUpload(context.Background(), db, store, 7, false, upload.Asset.ID, VideoAssetCompleteRequest{
			Parts: []VideoAssetCompletedPart{{PartNumber: 1, ETag: `"etag-1"`}},
		})
		completeDone <- completeResult{asset: asset, err: completeErr}
	}()
	<-store.completeStarted

	var asset model.KKAIVideoAsset
	require.NoError(t, db.First(&asset, upload.Asset.ID).Error)
	require.NoError(t, db.Model(&asset).Update("upload_expires_at", time.Now().Add(-time.Minute).Unix()).Error)
	expired, err := ExpireVideoAssetUploads(context.Background(), db, store, 20)
	require.NoError(t, err)
	require.Equal(t, 1, expired)
	require.NoError(t, db.First(&asset, upload.Asset.ID).Error)
	require.Equal(t, model.VideoAssetStateDeleted, asset.State)
	require.Greater(t, asset.UploadExpiresAt, time.Now().Unix())

	close(store.releaseComplete)
	completed := <-completeDone
	require.Nil(t, completed.asset)
	require.ErrorIs(t, completed.err, ErrInvalidVideoAssetUpload)
	require.Contains(t, store.objects, asset.ObjectKey)
	require.NoError(t, db.Model(&asset).Update("upload_expires_at", time.Now().Add(-time.Minute).Unix()).Error)

	expired, err = ExpireVideoAssetUploads(context.Background(), db, store, 20)
	require.NoError(t, err)
	require.Equal(t, 1, expired)
	require.NoError(t, db.First(&asset, upload.Asset.ID).Error)
	require.Equal(t, model.VideoAssetStateDeleted, asset.State)
	require.NotContains(t, store.objects, asset.ObjectKey)
	require.Greater(t, asset.UploadExpiresAt, time.Now().Unix())
}
