package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type imageUploadTestStore struct {
	*imagePipelineStore
	payload     []byte
	parts       []VideoAssetUploadedPart
	created     int
	completed   int
	reads       int
	aborted     int
	completeErr error
	afterGet    func()
}

func (store *imageUploadTestStore) CreateMultipartUpload(context.Context, string, string) (string, error) {
	store.created++
	return fmt.Sprintf("upload-%d", store.created), nil
}

func (store *imageUploadTestStore) PresignImageUploadPart(_ context.Context, _, _ string, _ int32, length int64, _ time.Duration) (VideoAssetSignedRequest, error) {
	return VideoAssetSignedRequest{URL: "https://storage.example/part", Method: "PUT", Headers: map[string]string{"expected-length": fmt.Sprint(length)}}, nil
}

func (store *imageUploadTestStore) ListUploadedParts(context.Context, string, string) ([]VideoAssetUploadedPart, error) {
	return append([]VideoAssetUploadedPart(nil), store.parts...), nil
}

func (store *imageUploadTestStore) CompleteMultipartUpload(_ context.Context, key, _ string, _ []VideoAssetCompletedPart) error {
	store.completed++
	store.objects[key] = append([]byte(nil), store.payload...)
	return store.completeErr
}

func (store *imageUploadTestStore) AbortMultipartUpload(context.Context, string, string) error {
	store.aborted++
	return nil
}
func (store *imageUploadTestStore) AbortImageUploadSessions(context.Context, string) error {
	store.aborted++
	return nil
}

func (store *imageUploadTestStore) Head(_ context.Context, key string) (VideoAssetObjectMetadata, error) {
	body, ok := store.objects[key]
	if !ok {
		return VideoAssetObjectMetadata{}, ErrVideoAssetObjectNotFound
	}
	return VideoAssetObjectMetadata{ContentLength: int64(len(body)), ContentType: "image/png", ETag: "stored-object"}, nil
}

func (store *imageUploadTestStore) Get(_ context.Context, key string) (ImageAssetObject, error) {
	store.reads++
	body, ok := store.objects[key]
	if !ok {
		return ImageAssetObject{}, ErrVideoAssetObjectNotFound
	}
	if store.afterGet != nil {
		store.afterGet()
	}
	return ImageAssetObject{Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), ContentType: "image/png", ETag: "stored-object"}, nil
}

func newImageUploadTest(t *testing.T) (*gorm.DB, *imageUploadTestStore, context.Context) {
	t.Helper()
	db := newImagePipelineTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.KKAIJobLease{}, &model.KKAIImageReferenceUpload{}, &model.KKAIImageGenerationReference{}))
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = connection.Close() })
	lease := model.KKAIJobLease{LeaseName: "image-upload-test-leader", Holder: "image-upload-test", LeaseUntil: time.Now().Add(time.Hour).Unix(), Fence: 1}
	require.NoError(t, db.Create(&lease).Error)
	payload := imageArchiveTestPNG(t, 2, 1)
	store := &imageUploadTestStore{imagePipelineStore: &imagePipelineStore{objects: map[string][]byte{}}, payload: payload,
		parts: []VideoAssetUploadedPart{{PartNumber: 1, SizeBytes: int64(len(payload)), ETag: "PartETag"}},
	}
	return db, store, WithImageWorkerLease(context.Background(), lease)
}

func createImageUploadTestReservation(t *testing.T, db *gorm.DB, store *imageUploadTestStore) *ImageUploadReservation {
	t.Helper()
	digest := sha256.Sum256(store.payload)
	reservation, err := CreateImageReferenceUpload(context.Background(), db, store, 7, "upload-attempt-1", ImageUploadRequest{MIMEType: "image/png", SizeBytes: int64(len(store.payload)), SHA256: fmt.Sprintf("%x", digest)})
	require.NoError(t, err)
	return reservation
}

func claimImageUploadTestEvent(t *testing.T, db *gorm.DB) model.KKAIOutboxEvent {
	t.Helper()
	var event model.KKAIOutboxEvent
	require.NoError(t, db.Where("topic = ?", ImageUploadValidateTopic).First(&event).Error)
	event.LockedAt, event.LockedBy = time.Now().Unix(), "upload-event-fence"
	require.NoError(t, db.Save(&event).Error)
	return event
}

func TestImageUploadCreateReplayAndOwnership(t *testing.T) {
	db, store, _ := newImageUploadTest(t)
	reservation := createImageUploadTestReservation(t, db, store)
	replay := createImageUploadTestReservation(t, db, store)
	assert.Equal(t, reservation.Asset.ID, replay.Asset.ID)
	assert.Equal(t, 1, store.created)
	_, err := CreateImageReferenceUpload(context.Background(), db, store, 7, "upload-attempt-1", ImageUploadRequest{MIMEType: "image/png", SizeBytes: int64(len(store.payload)), SHA256: strings.Repeat("b", 64)})
	assert.ErrorIs(t, err, ErrImageUploadConflict)
	_, err = GetImageReferenceUpload(context.Background(), db, 8, reservation.Asset.ID)
	assert.ErrorIs(t, err, ErrImageAssetNotFound)
	_, err = SignImageReferenceUploadPart(context.Background(), db, store, 8, reservation.Asset.ID, 1)
	assert.ErrorIs(t, err, ErrImageAssetNotFound)
	_, err = AbortImageReferenceUpload(context.Background(), db, 8, reservation.Asset.ID)
	assert.ErrorIs(t, err, ErrImageAssetNotFound)
	signed, err := SignImageReferenceUploadPart(context.Background(), db, store, 7, reservation.Asset.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprint(len(store.payload)), signed.Headers["expected-length"])
	_, err = SignImageReferenceUploadPart(context.Background(), db, store, 7, reservation.Asset.ID, 2)
	assert.ErrorIs(t, err, ErrInvalidImageUpload)
}

func TestImageUploadCompleteFreezesManifestAndDoesNotReadObject(t *testing.T) {
	db, store, _ := newImageUploadTest(t)
	reservation := createImageUploadTestReservation(t, db, store)
	parts := []VideoAssetCompletedPart{{PartNumber: 1, ETag: `"PartETag"`}}
	view, err := CompleteImageReferenceUpload(context.Background(), db, 7, reservation.Asset.ID, parts)
	require.NoError(t, err)
	assert.Equal(t, model.ImageAssetStateValidating, view.State)
	assert.Zero(t, store.reads)
	assert.Zero(t, store.completed)
	replay := createImageUploadTestReservation(t, db, store)
	assert.Equal(t, reservation.Asset.ID, replay.Asset.ID)
	assert.Equal(t, model.ImageAssetStateValidating, replay.Asset.State)
	assert.Equal(t, 1, store.created)
	_, err = CompleteImageReferenceUpload(context.Background(), db, 7, reservation.Asset.ID, []VideoAssetCompletedPart{{PartNumber: 1, ETag: "PartETag"}})
	require.NoError(t, err)
	var count int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	_, err = CompleteImageReferenceUpload(context.Background(), db, 7, reservation.Asset.ID, []VideoAssetCompletedPart{{PartNumber: 1, ETag: "different"}})
	assert.ErrorIs(t, err, ErrImageUploadConflict)
	_, err = SignImageReferenceUploadPart(context.Background(), db, store, 7, reservation.Asset.ID, 1)
	assert.ErrorIs(t, err, ErrImageUploadConflict)
}

func TestImageUploadValidationAndLostCompleteResponseRecovery(t *testing.T) {
	for _, lostResponse := range []bool{false, true} {
		t.Run(fmt.Sprint(lostResponse), func(t *testing.T) {
			db, store, ctx := newImageUploadTest(t)
			if lostResponse {
				store.completeErr = errors.New("complete response lost")
			}
			reservation := createImageUploadTestReservation(t, db, store)
			_, err := CompleteImageReferenceUpload(ctx, db, 7, reservation.Asset.ID, []VideoAssetCompletedPart{{PartNumber: 1, ETag: "PartETag"}})
			require.NoError(t, err)
			pipeline, err := NewImageUploadPipeline(db, store)
			require.NoError(t, err)
			event := claimImageUploadTestEvent(t, db)
			require.NoError(t, pipeline.validate(ctx, event))
			require.NoError(t, pipeline.validate(ctx, event))
			view, err := GetImageReferenceUpload(ctx, db, 7, reservation.Asset.ID)
			require.NoError(t, err)
			assert.Equal(t, model.ImageAssetStateReady, view.Asset.State)
			assert.Equal(t, 2, view.Asset.Width)
			assert.Equal(t, 1, view.Asset.Height)
			assert.Equal(t, reservation.Asset.SHA256, view.Asset.SHA256)
			assert.Equal(t, 1, store.completed)
			assert.Equal(t, 1, store.reads)
		})
	}
}

func TestImageUploadValidatorCannotPublishChangedContentOrReviveAbortedAsset(t *testing.T) {
	for _, scenario := range []string{"wrong-hash", "abort", "leader-lost"} {
		t.Run(scenario, func(t *testing.T) {
			db, store, ctx := newImageUploadTest(t)
			reservation := createImageUploadTestReservation(t, db, store)
			_, err := CompleteImageReferenceUpload(ctx, db, 7, reservation.Asset.ID, []VideoAssetCompletedPart{{PartNumber: 1, ETag: "PartETag"}})
			require.NoError(t, err)
			switch scenario {
			case "wrong-hash":
				require.NoError(t, db.Model(&model.KKAIImageAsset{}).Where("id = ?", reservation.Asset.ID).Update("sha256", strings.Repeat("b", 64)).Error)
			case "abort":
				store.afterGet = func() {
					_, abortErr := AbortImageReferenceUpload(ctx, db, 7, reservation.Asset.ID)
					require.NoError(t, abortErr)
				}
			case "leader-lost":
				store.afterGet = func() {
					require.NoError(t, db.Model(&model.KKAIJobLease{}).Where("lease_name = ?", "image-upload-test-leader").Update("fence", 2).Error)
				}
			}
			pipeline, err := NewImageUploadPipeline(db, store)
			require.NoError(t, err)
			err = pipeline.validate(ctx, claimImageUploadTestEvent(t, db))
			if scenario == "leader-lost" {
				require.ErrorIs(t, err, model.ErrImageExecutionFenceLost)
			} else {
				require.NoError(t, err)
			}
			view, err := GetImageReferenceUpload(ctx, db, 7, reservation.Asset.ID)
			require.NoError(t, err)
			assert.NotEqual(t, model.ImageAssetStateReady, view.Asset.State)
			if scenario == "wrong-hash" {
				assert.Equal(t, "image_content_mismatch", view.Asset.FailureReason)
			}
			if scenario == "abort" {
				assert.Equal(t, model.ImageAssetStateDeleting, view.Asset.State)
			}
		})
	}
}

func TestImageUploadCleanupProtectsReferencesAndFinishesAfterLateObjectSweep(t *testing.T) {
	db, store, ctx := newImageUploadTest(t)
	reservation := createImageUploadTestReservation(t, db, store)
	var asset model.KKAIImageAsset
	require.NoError(t, db.First(&asset, reservation.Asset.ID).Error)
	store.objects[asset.ObjectKey] = append([]byte(nil), store.payload...)
	require.NoError(t, db.Model(&model.KKAIImageAsset{}).Where("id = ?", asset.ID).Update("state", model.ImageAssetStateReady).Error)
	past := time.Now().Add(-time.Minute).Unix()
	require.NoError(t, db.Model(&model.KKAIImageReferenceUpload{}).Where("asset_id = ?", asset.ID).Update("cleanup_at", past).Error)
	require.NoError(t, db.Create(&model.KKAIImageGenerationReference{GenerationID: 99, AssetID: asset.ID, Position: 0}).Error)
	_, err := AbortImageReferenceUpload(ctx, db, 7, asset.ID)
	assert.ErrorIs(t, err, ErrImageUploadConflict)
	cleaned, err := ReconcileImageReferenceUploads(ctx, db, store, 10)
	require.NoError(t, err)
	assert.Zero(t, cleaned)
	assert.Contains(t, store.objects, asset.ObjectKey)
	require.NoError(t, db.Where("asset_id = ?", asset.ID).Delete(&model.KKAIImageGenerationReference{}).Error)
	_, err = AbortImageReferenceUpload(ctx, db, 7, asset.ID)
	require.NoError(t, err)
	cleaned, err = ReconcileImageReferenceUploads(ctx, db, store, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, cleaned)
	assert.Empty(t, store.objects)
	store.objects[asset.ObjectKey] = append([]byte(nil), store.payload...)
	cleaned, err = ReconcileImageReferenceUploads(ctx, db, store, 1)
	require.NoError(t, err)
	assert.Zero(t, cleaned, "the tail sweep waits for outstanding upload signatures to expire")
	assert.Contains(t, store.objects, asset.ObjectKey)

	// Move the persisted upload window into the past, including its bounded
	// in-flight storage allowance, without sleeping or relying on wall time.
	require.NoError(t, db.Model(&model.KKAIImageReferenceUpload{}).Where("asset_id = ?", asset.ID).Updates(map[string]any{
		"cleanup_at": past, "upload_expires_at": time.Now().Add(-time.Hour).Unix(),
	}).Error)
	cleaned, err = ReconcileImageReferenceUploads(ctx, db, store, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, cleaned)
	assert.Empty(t, store.objects)
	view, err := GetImageReferenceUpload(ctx, db, 7, asset.ID)
	require.NoError(t, err)
	assert.Equal(t, model.ImageAssetStateDeleted, view.Asset.State)
	var upload model.KKAIImageReferenceUpload
	require.NoError(t, db.First(&upload, "asset_id = ?", asset.ID).Error)
	assert.Zero(t, upload.CleanupAt)

	digest := sha256.Sum256(store.payload)
	next, err := CreateImageReferenceUpload(ctx, db, store, 7, "next-upload", ImageUploadRequest{
		MIMEType: "image/png", SizeBytes: int64(len(store.payload)), SHA256: fmt.Sprintf("%x", digest),
	})
	require.NoError(t, err)
	_, err = AbortImageReferenceUpload(ctx, db, 7, next.Asset.ID)
	require.NoError(t, err)
	cleaned, err = ReconcileImageReferenceUploads(ctx, db, store, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, cleaned)
	nextView, err := GetImageReferenceUpload(ctx, db, 7, next.Asset.ID)
	require.NoError(t, err)
	assert.Equal(t, model.ImageAssetStateDeleted, nextView.Asset.State, "finished tombstones must not consume the next GC batch")
}

func TestImageUploadCapacityRetainsFailedAssetsUntilStorageCleanup(t *testing.T) {
	db, store, ctx := newImageUploadTest(t)
	require.NoError(t, db.Create(&model.KKAIImageAsset{OwnerUserID: 7, Scope: model.ImageAssetScopeUser, Kind: model.ImageAssetKindReference, State: model.ImageAssetStateFailed,
		ObjectKey: "image-studio/users/7/old", SizeBytes: 2 * image_studio_setting.Get().MaxReferenceTotalBytes}).Error)
	digest := sha256.Sum256(store.payload)
	request := ImageUploadRequest{MIMEType: "image/png", SizeBytes: int64(len(store.payload)), SHA256: fmt.Sprintf("%x", digest)}
	_, err := CreateImageReferenceUpload(ctx, db, store, 7, "another-upload", request)
	assert.ErrorIs(t, err, ErrImageUploadCapacity)
	assert.Zero(t, store.created)
	require.NoError(t, db.Model(&model.KKAIImageAsset{}).Where("object_key = ?", "image-studio/users/7/old").Update("deleted_at", time.Now().Unix()).Error)
	_, err = CreateImageReferenceUpload(ctx, db, store, 7, "another-upload", request)
	require.NoError(t, err)
}
