package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type multipartVideoAssetStore struct {
	*memoryVideoAssetStore
	createErr         error
	presignErr        error
	listErr           error
	completeErr       error
	abortErr          error
	uploadID          string
	parts             []VideoAssetUploadedPart
	completeCalls     int
	abortCalls        int
	headCalls         int
	presignCalls      int
	presignedLength   int64
	presignedExpiry   time.Duration
	objectKey         string
	uploadContentType string
}

func newMultipartVideoAssetStore() *multipartVideoAssetStore {
	return &multipartVideoAssetStore{
		memoryVideoAssetStore: newMemoryVideoAssetStore(),
		uploadID:              "r2-upload-1",
	}
}

func (store *multipartVideoAssetStore) CreateMultipartUpload(_ context.Context, key string, contentType string) (string, error) {
	if store.createErr != nil {
		return "", store.createErr
	}
	store.objectKey = key
	store.uploadContentType = contentType
	return store.uploadID, nil
}

func (store *multipartVideoAssetStore) PresignUploadPart(_ context.Context, _ string, _ string, _ int32, contentLength int64, expires time.Duration) (VideoAssetSignedRequest, error) {
	store.presignCalls++
	if store.presignErr != nil {
		return VideoAssetSignedRequest{}, store.presignErr
	}
	store.presignedLength = contentLength
	store.presignedExpiry = expires
	return VideoAssetSignedRequest{
		URL: "https://signed.example/part", Method: "PUT", Headers: map[string]string{"Content-Length": fmt.Sprint(contentLength)},
		ExpiresAt: time.Now().Add(expires).Unix(),
	}, nil
}

func (store *multipartVideoAssetStore) Head(ctx context.Context, key string) (VideoAssetObjectMetadata, error) {
	store.headCalls++
	return store.memoryVideoAssetStore.Head(ctx, key)
}

func (store *multipartVideoAssetStore) ListUploadedParts(context.Context, string, string) ([]VideoAssetUploadedPart, error) {
	if store.listErr != nil {
		return nil, store.listErr
	}
	return append([]VideoAssetUploadedPart(nil), store.parts...), nil
}

func (store *multipartVideoAssetStore) CompleteMultipartUpload(_ context.Context, key string, _ string, parts []VideoAssetCompletedPart) error {
	store.completeCalls++
	if store.completeErr != nil {
		return store.completeErr
	}
	var size int64
	for _, part := range store.parts {
		size += part.SizeBytes
	}
	store.objects[key] = make([]byte, size)
	store.memoryVideoAssetStore.contentType[key] = store.uploadContentType
	if len(store.parts) != len(parts) {
		panic("multipart completion received unexpected part count")
	}
	return nil
}

func (store *multipartVideoAssetStore) AbortMultipartUpload(context.Context, string, string) error {
	store.abortCalls++
	return store.abortErr
}

func TestMultipartVideoUploadCompletesIdempotently(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMultipartVideoAssetStore()
	size := videoMultipartPartSize + 1024
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png",
		SizeBytes: size, Multipart: true,
	})
	require.NoError(t, err)
	require.Equal(t, VideoUploadModeMultipart, upload.UploadMode)
	require.EqualValues(t, videoMultipartPartSize, upload.PartSize)
	store.parts = []VideoAssetUploadedPart{
		{PartNumber: 1, SizeBytes: videoMultipartPartSize, ETag: `"etag-1"`},
		{PartNumber: 2, SizeBytes: 1024, ETag: `"etag-2"`},
	}
	request := VideoAssetCompleteRequest{Parts: []VideoAssetCompletedPart{
		{PartNumber: 1, ETag: `"etag-1"`}, {PartNumber: 2, ETag: `"etag-2"`},
	}}

	first, err := CompleteVideoAssetUpload(context.Background(), db, store, 7, false, upload.Asset.ID, request)
	require.NoError(t, err)
	second, err := CompleteVideoAssetUpload(context.Background(), db, store, 7, false, upload.Asset.ID, request)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, model.VideoAssetStateUploaded, second.State)
	require.Equal(t, 1, store.completeCalls)

	var events int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicInspect).Count(&events).Error)
	require.EqualValues(t, 1, events)
}

func TestMultipartVideoUploadRecoversWhenR2CompletedBeforeDatabase(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMultipartVideoAssetStore()
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png",
		SizeBytes: videoMultipartPartSize, Multipart: true,
	})
	require.NoError(t, err)
	store.objects[store.objectKey] = make([]byte, videoMultipartPartSize)
	store.memoryVideoAssetStore.contentType[store.objectKey] = store.uploadContentType
	store.listErr = ErrVideoMultipartUploadNotFound

	asset, err := CompleteVideoAssetUpload(context.Background(), db, store, 7, false, upload.Asset.ID, VideoAssetCompleteRequest{
		Parts: []VideoAssetCompletedPart{{PartNumber: 1, ETag: `"etag-1"`}},
	})
	require.NoError(t, err)
	require.Equal(t, model.VideoAssetStateUploaded, asset.State)
	require.Zero(t, store.completeCalls)
}

func TestMultipartVideoUploadRecoversWithoutPersistedClientETags(t *testing.T) {
	tests := []struct {
		name    string
		recover func(context.Context, *gorm.DB, *multipartVideoAssetStore, int64) error
	}{
		{
			name: "get upload",
			recover: func(ctx context.Context, db *gorm.DB, store *multipartVideoAssetStore, assetID int64) error {
				asset, err := GetVideoAssetUpload(ctx, db, store, 7, false, assetID)
				if err == nil {
					require.Equal(t, model.VideoAssetStateUploaded, asset.State)
				}
				return err
			},
		},
		{
			name: "list parts",
			recover: func(ctx context.Context, db *gorm.DB, store *multipartVideoAssetStore, assetID int64) error {
				parts, err := ListVideoAssetUploadParts(ctx, db, store, 7, false, assetID)
				require.Empty(t, parts)
				return err
			},
		},
		{
			name: "complete without etags",
			recover: func(ctx context.Context, db *gorm.DB, store *multipartVideoAssetStore, assetID int64) error {
				asset, err := CompleteVideoAssetUpload(ctx, db, store, 7, false, assetID, VideoAssetCompleteRequest{})
				if err == nil {
					require.Equal(t, model.VideoAssetStateUploaded, asset.State)
				}
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newVideoPipelineTestDB(t)
			store := newMultipartVideoAssetStore()
			upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
				Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png",
				SizeBytes: videoMultipartPartSize, Multipart: true,
			})
			require.NoError(t, err)
			store.objects[store.objectKey] = make([]byte, videoMultipartPartSize)
			store.contentType[store.objectKey] = store.uploadContentType
			store.listErr = ErrVideoMultipartUploadNotFound

			require.NoError(t, test.recover(context.Background(), db, store, upload.Asset.ID))
			require.Equal(t, 1, store.headCalls)
			var persisted model.KKAIVideoAsset
			require.NoError(t, db.First(&persisted, upload.Asset.ID).Error)
			require.Equal(t, model.VideoAssetStateUploaded, persisted.State)
			require.Empty(t, persisted.MultipartUploadID)
			var events int64
			require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicInspect).Count(&events).Error)
			require.EqualValues(t, 1, events)
		})
	}
}

func TestMultipartVideoUploadRefreshRecoversAfterDatabaseFailure(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMultipartVideoAssetStore()
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png",
		SizeBytes: videoMultipartPartSize, Multipart: true,
	})
	require.NoError(t, err)
	store.parts = []VideoAssetUploadedPart{{PartNumber: 1, SizeBytes: videoMultipartPartSize, ETag: `"etag-1"`}}
	failFinalize := true
	callbackName := "test:fail_video_multipart_finalize"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if failFinalize && tx.Statement.Table == (model.KKAIVideoAsset{}).TableName() {
			failFinalize = false
			tx.AddError(errors.New("forced multipart database failure"))
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })

	_, err = CompleteVideoAssetUpload(context.Background(), db, store, 7, false, upload.Asset.ID, VideoAssetCompleteRequest{
		Parts: []VideoAssetCompletedPart{{PartNumber: 1, ETag: `"etag-1"`}},
	})
	require.Error(t, err)
	require.Equal(t, 1, store.completeCalls)
	store.listErr = ErrVideoMultipartUploadNotFound

	recovered, err := GetVideoAssetUpload(context.Background(), db, store, 7, false, upload.Asset.ID)
	require.NoError(t, err)
	require.Equal(t, model.VideoAssetStateUploaded, recovered.State)
	require.Equal(t, 1, store.completeCalls)
}

func TestMultipartVideoUploadCleanupPreservesCompletedObject(t *testing.T) {
	tests := []struct {
		name        string
		cleanup     func(context.Context, *gorm.DB, *multipartVideoAssetStore, int64) (*VideoAssetView, int, error)
		expectedErr error
	}{
		{
			name: "abort",
			cleanup: func(ctx context.Context, db *gorm.DB, store *multipartVideoAssetStore, assetID int64) (*VideoAssetView, int, error) {
				asset, err := AbortVideoAssetUpload(ctx, db, store, 7, false, assetID)
				return asset, 0, err
			},
			expectedErr: ErrVideoAssetUploadCompleted,
		},
		{
			name: "expiry",
			cleanup: func(ctx context.Context, db *gorm.DB, store *multipartVideoAssetStore, _ int64) (*VideoAssetView, int, error) {
				expired, err := ExpireVideoAssetUploads(ctx, db, store, 20)
				return nil, expired, err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newVideoPipelineTestDB(t)
			store := newMultipartVideoAssetStore()
			upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
				Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png",
				SizeBytes: videoMultipartPartSize, Multipart: true,
			})
			require.NoError(t, err)
			store.objects[store.objectKey] = make([]byte, videoMultipartPartSize)
			store.contentType[store.objectKey] = store.uploadContentType
			store.listErr = ErrVideoMultipartUploadNotFound
			require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Where("id = ?", upload.Asset.ID).
				Update("upload_expires_at", time.Now().Add(-time.Minute).Unix()).Error)

			asset, expired, cleanupErr := test.cleanup(context.Background(), db, store, upload.Asset.ID)
			if test.expectedErr != nil {
				require.ErrorIs(t, cleanupErr, test.expectedErr)
			} else {
				require.NoError(t, cleanupErr)
			}
			require.Nil(t, asset)
			require.Zero(t, expired)
			var persisted model.KKAIVideoAsset
			require.NoError(t, db.First(&persisted, upload.Asset.ID).Error)
			require.Equal(t, model.VideoAssetStateUploaded, persisted.State)
			require.Contains(t, store.objects, store.objectKey)
			require.Zero(t, store.deleteCount[store.objectKey])
			require.Zero(t, store.abortCalls)
			require.Equal(t, 1, store.headCalls)
		})
	}
}

func TestCompletedVideoAssetObjectValidatesKnownChecksum(t *testing.T) {
	store := newMemoryVideoAssetStore()
	store.objects["reference.png"] = []byte("image")
	store.contentType["reference.png"] = "image/png"
	store.sha256["reference.png"] = strings.Repeat("b", 64)
	asset := model.KKAIVideoAsset{
		ObjectKey: "reference.png", MIMEType: "image/png", SizeBytes: 5, SHA256: strings.Repeat("a", 64),
	}

	_, err := verifyCompletedVideoAssetObject(context.Background(), store, asset)
	require.ErrorIs(t, err, ErrInvalidVideoAssetUpload)
	store.sha256["reference.png"] = asset.SHA256
	_, err = verifyCompletedVideoAssetObject(context.Background(), store, asset)
	require.NoError(t, err)
}

func TestMultipartVideoUploadRejectsCrossUserOperations(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMultipartVideoAssetStore()
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png",
		SizeBytes: videoMultipartPartSize, Multipart: true,
	})
	require.NoError(t, err)

	_, err = SignVideoAssetUploadPart(context.Background(), db, store, 8, true, upload.Asset.ID, 1)
	require.ErrorIs(t, err, ErrVideoAssetAccessDenied)
	_, err = ListVideoAssetUploadParts(context.Background(), db, store, 8, false, upload.Asset.ID)
	require.ErrorIs(t, err, ErrVideoAssetAccessDenied)
	_, err = CompleteVideoAssetUpload(context.Background(), db, store, 8, false, upload.Asset.ID, VideoAssetCompleteRequest{})
	require.ErrorIs(t, err, ErrVideoAssetAccessDenied)
	_, err = AbortVideoAssetUpload(context.Background(), db, store, 8, false, upload.Asset.ID)
	require.ErrorIs(t, err, ErrVideoAssetAccessDenied)
}

func TestExpireVideoAssetUploadsAbortsAbandonedMultipartSessions(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMultipartVideoAssetStore()
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png",
		SizeBytes: videoMultipartPartSize, Multipart: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Where("id = ?", upload.Asset.ID).
		Update("upload_expires_at", time.Now().Add(-time.Minute).Unix()).Error)

	expired, err := ExpireVideoAssetUploads(context.Background(), db, store, 20)
	require.NoError(t, err)
	require.Equal(t, 1, expired)
	require.Equal(t, 1, store.abortCalls)
	var asset model.KKAIVideoAsset
	require.NoError(t, db.First(&asset, upload.Asset.ID).Error)
	require.Equal(t, model.VideoAssetStateDeleted, asset.State)
}

func TestMultipartVideoUploadValidatesPartsAgainstR2(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMultipartVideoAssetStore()
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png",
		SizeBytes: videoMultipartPartSize + 1024, Multipart: true,
	})
	require.NoError(t, err)
	store.parts = []VideoAssetUploadedPart{
		{PartNumber: 1, SizeBytes: videoMultipartPartSize, ETag: `"etag-1"`},
		{PartNumber: 2, SizeBytes: 1024, ETag: `"etag-2"`},
	}

	tests := []VideoAssetCompleteRequest{
		{Parts: []VideoAssetCompletedPart{{PartNumber: 2, ETag: `"etag-2"`}, {PartNumber: 1, ETag: `"etag-1"`}}},
		{Parts: []VideoAssetCompletedPart{{PartNumber: 1, ETag: `"wrong"`}, {PartNumber: 2, ETag: `"etag-2"`}}},
		{Parts: []VideoAssetCompletedPart{{PartNumber: 0, ETag: `"etag-1"`}, {PartNumber: 2, ETag: `"etag-2"`}}},
		{Parts: []VideoAssetCompletedPart{{PartNumber: 1, ETag: `"etag-1`}, {PartNumber: 2, ETag: `"etag-2"`}}},
	}
	for _, request := range tests {
		_, err := CompleteVideoAssetUpload(context.Background(), db, store, 7, false, upload.Asset.ID, request)
		require.ErrorIs(t, err, ErrInvalidVideoAssetUpload)
	}
	require.Zero(t, store.completeCalls)
}

func TestMultipartVideoUploadPartSignatureUsesExpectedSizeAndShortExpiry(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMultipartVideoAssetStore()
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png",
		SizeBytes: videoMultipartPartSize + 1024, Multipart: true,
	})
	require.NoError(t, err)

	signed, err := SignVideoAssetUploadPart(context.Background(), db, store, 7, false, upload.Asset.ID, 2)
	require.NoError(t, err)
	require.EqualValues(t, 1024, store.presignedLength)
	require.Positive(t, store.presignedExpiry)
	require.LessOrEqual(t, store.presignedExpiry, videoMultipartPartSignatureTTL)
	require.Greater(t, signed.ExpiresAt, time.Now().Unix())
}

func TestMultipartVideoUploadPartSigningDoesNotProbeCompletedObject(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMultipartVideoAssetStore()
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png",
		SizeBytes: 2*videoMultipartPartSize + 1024, Multipart: true,
	})
	require.NoError(t, err)

	for partNumber := int32(1); partNumber <= 3; partNumber++ {
		_, err := SignVideoAssetUploadPart(context.Background(), db, store, 7, false, upload.Asset.ID, partNumber)
		require.NoError(t, err)
	}
	require.Equal(t, 3, store.presignCalls)
	require.Zero(t, store.headCalls)
}

func TestMultipartVideoUploadExpiresAndAborts(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMultipartVideoAssetStore()
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png",
		SizeBytes: videoMultipartPartSize, Multipart: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Where("id = ?", upload.Asset.ID).Update("upload_expires_at", time.Now().Add(-time.Minute).Unix()).Error)

	_, err = SignVideoAssetUploadPart(context.Background(), db, store, 7, false, upload.Asset.ID, 1)
	require.ErrorIs(t, err, ErrVideoAssetUploadExpired)
	require.Equal(t, 1, store.abortCalls)
	var asset model.KKAIVideoAsset
	require.NoError(t, db.First(&asset, upload.Asset.ID).Error)
	require.Equal(t, model.VideoAssetStateDeleted, asset.State)
}

func TestMultipartVideoUploadAbortIsIdempotentAndFailsClosed(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMultipartVideoAssetStore()
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png",
		SizeBytes: videoMultipartPartSize, Multipart: true,
	})
	require.NoError(t, err)
	store.abortErr = errors.New("r2 unavailable")
	_, err = AbortVideoAssetUpload(context.Background(), db, store, 7, false, upload.Asset.ID)
	require.Error(t, err)
	var asset model.KKAIVideoAsset
	require.NoError(t, db.First(&asset, upload.Asset.ID).Error)
	require.Equal(t, model.VideoAssetStateDeleting, asset.State)

	store.abortErr = nil
	first, err := AbortVideoAssetUpload(context.Background(), db, store, 7, false, upload.Asset.ID)
	require.NoError(t, err)
	second, err := AbortVideoAssetUpload(context.Background(), db, store, 7, false, upload.Asset.ID)
	require.NoError(t, err)
	require.Equal(t, model.VideoAssetStateDeleting, first.State)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, 3, store.abortCalls)
}

func TestMultipartVideoUploadListsValidatedPartsForResume(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMultipartVideoAssetStore()
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png",
		SizeBytes: videoMultipartPartSize + 1024, Multipart: true,
	})
	require.NoError(t, err)
	store.parts = []VideoAssetUploadedPart{{PartNumber: 1, SizeBytes: videoMultipartPartSize, ETag: `"etag-1"`}}

	parts, err := ListVideoAssetUploadParts(context.Background(), db, store, 7, false, upload.Asset.ID)
	require.NoError(t, err)
	require.Equal(t, store.parts, parts)
}

func TestMultipartVideoUploadFailsClosedOnR2Errors(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMultipartVideoAssetStore()
	store.createErr = errors.New("r2 unavailable")
	_, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png",
		SizeBytes: videoMultipartPartSize, Multipart: true,
	})
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Count(&count).Error)
	require.Zero(t, count)

	store.createErr = nil
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png",
		SizeBytes: videoMultipartPartSize, Multipart: true,
	})
	require.NoError(t, err)
	store.parts = []VideoAssetUploadedPart{{PartNumber: 1, SizeBytes: videoMultipartPartSize, ETag: `"etag-1"`}}
	store.completeErr = errors.New("r2 unavailable")
	_, err = CompleteVideoAssetUpload(context.Background(), db, store, 7, false, upload.Asset.ID, VideoAssetCompleteRequest{
		Parts: []VideoAssetCompletedPart{{PartNumber: 1, ETag: `"etag-1"`}},
	})
	require.Error(t, err)
	var asset model.KKAIVideoAsset
	require.NoError(t, db.First(&asset, upload.Asset.ID).Error)
	require.Equal(t, model.VideoAssetStatePendingUpload, asset.State)
}
