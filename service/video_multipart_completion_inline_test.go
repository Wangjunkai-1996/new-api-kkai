package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type referenceImageMultipartStore struct {
	*multipartVideoAssetStore
	payload []byte
}

func (store *referenceImageMultipartStore) CompleteMultipartUpload(
	ctx context.Context,
	key string,
	_ string,
	parts []VideoAssetCompletedPart,
) error {
	store.completeCalls++
	if len(parts) != len(store.parts) {
		return ErrInvalidVideoAssetUpload
	}
	return store.memoryVideoAssetStore.Put(
		ctx, key, store.uploadContentType, bytes.NewReader(store.payload), int64(len(store.payload)),
	)
}

type transientGetVideoAssetStore struct {
	*memoryVideoAssetStore
	getErr         error
	getCalls       int
	getContentType string
}

func (store *transientGetVideoAssetStore) Get(ctx context.Context, key string) (VideoAssetObject, error) {
	store.getCalls++
	if store.getErr != nil {
		return VideoAssetObject{}, store.getErr
	}
	object, err := store.memoryVideoAssetStore.Get(ctx, key)
	if err == nil && store.getContentType != "" {
		object.ContentType = store.getContentType
	}
	return object, err
}

func TestCompleteVideoReferenceImageUploadMarksReadyBeforeOutboxDelivery(t *testing.T) {
	canvas := image.NewRGBA(image.Rect(0, 0, 3, 2))
	var jpegPayload bytes.Buffer
	require.NoError(t, jpeg.Encode(&jpegPayload, canvas, nil))
	var pngPayload bytes.Buffer
	require.NoError(t, png.Encode(&pngPayload, canvas))
	webpPayload, err := base64.StdEncoding.DecodeString(
		"UklGRnAAAABXRUJQVlA4WAoAAAAQAAAAAgAAAQAAQUxQSAcAAAAAKTcoKDcpAFZQOCBCAAAAEAIAnQEqAwACAAIANCWoAnS6AAMaGs9NAAD+yFXGKCRWm/YgwgSv4Sv3f/Jt6DH3/+Tf/0mzwLxwPv/8m/5xgAAA",
	)
	require.NoError(t, err)

	tests := []struct {
		name     string
		mimeType string
		filename string
		payload  []byte
		codec    string
	}{
		{name: "jpeg", mimeType: "image/jpeg", filename: "reference.jpg", payload: jpegPayload.Bytes(), codec: "mjpeg"},
		{name: "png", mimeType: "image/png", filename: "reference.png", payload: pngPayload.Bytes(), codec: "png"},
		{name: "webp", mimeType: "image/webp", filename: "reference.webp", payload: webpPayload, codec: "webp"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newVideoPipelineTestDB(t)
			store := newMemoryVideoAssetStore()
			upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
				Purpose: model.VideoAssetKindReference, Filename: test.filename,
				MIMEType: test.mimeType, SizeBytes: int64(len(test.payload)),
			})
			require.NoError(t, err)
			var reserved model.KKAIVideoAsset
			require.NoError(t, db.First(&reserved, upload.Asset.ID).Error)
			require.NoError(t, store.Put(
				context.Background(), reserved.ObjectKey, test.mimeType, bytes.NewReader(test.payload), int64(len(test.payload)),
			))

			completed, err := CompleteVideoAssetUpload(
				context.Background(), db, store, 7, false, upload.Asset.ID, VideoAssetCompleteRequest{},
			)
			require.NoError(t, err)
			require.Equal(t, model.VideoAssetStateReady, completed.State)
			require.Equal(t, test.mimeType, completed.MIMEType)
			require.Equal(t, test.codec, completed.Codec)
			require.Equal(t, 3, completed.Width)
			require.Equal(t, 2, completed.Height)
			require.NotEmpty(t, completed.ContentURL)

			var events int64
			require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicInspect).Count(&events).Error)
			require.Zero(t, events)

			repeated, err := CompleteVideoAssetUpload(
				context.Background(), db, store, 7, false, upload.Asset.ID, VideoAssetCompleteRequest{},
			)
			require.NoError(t, err)
			require.Equal(t, completed, repeated)
		})
	}
}

func TestCompleteMultipartVideoReferenceImageUploadMarksReadyInline(t *testing.T) {
	var payload bytes.Buffer
	require.NoError(t, png.Encode(&payload, image.NewRGBA(image.Rect(0, 0, 4, 3))))
	store := &referenceImageMultipartStore{multipartVideoAssetStore: newMultipartVideoAssetStore(), payload: payload.Bytes()}
	db := newVideoPipelineTestDB(t)
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png",
		SizeBytes: int64(payload.Len()), Multipart: true,
	})
	require.NoError(t, err)
	store.parts = []VideoAssetUploadedPart{{PartNumber: 1, SizeBytes: int64(payload.Len()), ETag: `"etag-1"`}}

	completed, err := CompleteVideoAssetUpload(context.Background(), db, store, 7, false, upload.Asset.ID, VideoAssetCompleteRequest{
		Parts: []VideoAssetCompletedPart{{PartNumber: 1, ETag: `"etag-1"`}},
	})
	require.NoError(t, err)
	require.Equal(t, model.VideoAssetStateReady, completed.State)
	require.Equal(t, 4, completed.Width)
	require.Equal(t, 3, completed.Height)
	require.Equal(t, 1, store.completeCalls)
	var events int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicInspect).Count(&events).Error)
	require.Zero(t, events)
}

func TestCompleteVideoReferenceImageUploadRetriesObjectReadBeforeFinalizing(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := &transientGetVideoAssetStore{memoryVideoAssetStore: newMemoryVideoAssetStore()}
	var payload bytes.Buffer
	require.NoError(t, png.Encode(&payload, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png", SizeBytes: int64(payload.Len()),
	})
	require.NoError(t, err)
	var reserved model.KKAIVideoAsset
	require.NoError(t, db.First(&reserved, upload.Asset.ID).Error)
	require.NoError(t, store.Put(
		context.Background(), reserved.ObjectKey, "image/png", bytes.NewReader(payload.Bytes()), int64(payload.Len()),
	))

	readErr := errors.New("temporary object read failure")
	store.getErr = readErr
	_, err = CompleteVideoAssetUpload(context.Background(), db, store, 7, false, upload.Asset.ID, VideoAssetCompleteRequest{})
	require.ErrorIs(t, err, readErr)
	require.NoError(t, db.First(&reserved, reserved.ID).Error)
	require.Equal(t, model.VideoAssetStatePendingUpload, reserved.State)
	var events int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Count(&events).Error)
	require.Zero(t, events)

	store.getErr = nil
	completed, err := CompleteVideoAssetUpload(
		context.Background(), db, store, 7, false, upload.Asset.ID, VideoAssetCompleteRequest{},
	)
	require.NoError(t, err)
	require.Equal(t, model.VideoAssetStateReady, completed.State)
	require.Equal(t, 2, store.getCalls)
}

func TestCompleteVideoReferenceImageUploadRejectsObjectMetadataDrift(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := &transientGetVideoAssetStore{memoryVideoAssetStore: newMemoryVideoAssetStore()}
	var payload bytes.Buffer
	require.NoError(t, png.Encode(&payload, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png", SizeBytes: int64(payload.Len()),
	})
	require.NoError(t, err)
	var reserved model.KKAIVideoAsset
	require.NoError(t, db.First(&reserved, upload.Asset.ID).Error)
	require.NoError(t, store.Put(
		context.Background(), reserved.ObjectKey, "image/png", bytes.NewReader(payload.Bytes()), int64(payload.Len()),
	))

	store.getContentType = "image/jpeg"
	_, err = CompleteVideoAssetUpload(context.Background(), db, store, 7, false, upload.Asset.ID, VideoAssetCompleteRequest{})
	require.ErrorIs(t, err, ErrInvalidVideoAssetUpload)
	require.NoError(t, db.First(&reserved, reserved.ID).Error)
	require.Equal(t, model.VideoAssetStatePendingUpload, reserved.State)
	var events int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Count(&events).Error)
	require.Zero(t, events)
}

func TestCompleteVideoReferenceImageUploadMarksInvalidContentFailed(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	payload := []byte("not-an-image")
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png", SizeBytes: int64(len(payload)),
	})
	require.NoError(t, err)
	var reserved model.KKAIVideoAsset
	require.NoError(t, db.First(&reserved, upload.Asset.ID).Error)
	require.NoError(t, store.Put(
		context.Background(), reserved.ObjectKey, "image/png", bytes.NewReader(payload), int64(len(payload)),
	))

	completed, err := CompleteVideoAssetUpload(
		context.Background(), db, store, 7, false, upload.Asset.ID, VideoAssetCompleteRequest{},
	)
	require.NoError(t, err)
	require.Equal(t, model.VideoAssetStateFailed, completed.State)
	require.Equal(t, "media processing failed", completed.FailureReason)
	var events int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Count(&events).Error)
	require.Zero(t, events)
}

func TestRecoverCompletedVideoReferenceImageUploadMarksReadyInline(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := &transientGetVideoAssetStore{memoryVideoAssetStore: newMemoryVideoAssetStore()}
	var payload bytes.Buffer
	require.NoError(t, png.Encode(&payload, image.NewRGBA(image.Rect(0, 0, 5, 4))))
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoAssetKindReference, Filename: "reference.png", MIMEType: "image/png", SizeBytes: int64(payload.Len()),
	})
	require.NoError(t, err)
	var reserved model.KKAIVideoAsset
	require.NoError(t, db.First(&reserved, upload.Asset.ID).Error)
	require.NoError(t, store.Put(
		context.Background(), reserved.ObjectKey, "image/png", bytes.NewReader(payload.Bytes()), int64(payload.Len()),
	))

	failFinalize := true
	callbackName := "test:fail_inline_reference_image_finalize"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if failFinalize && tx.Statement.Table == (model.KKAIVideoAsset{}).TableName() {
			failFinalize = false
			tx.AddError(errors.New("forced inline image database failure"))
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })

	_, err = CompleteVideoAssetUpload(context.Background(), db, store, 7, false, upload.Asset.ID, VideoAssetCompleteRequest{})
	require.Error(t, err)
	require.NoError(t, db.First(&reserved, reserved.ID).Error)
	require.Equal(t, model.VideoAssetStatePendingUpload, reserved.State)

	recovered, err := GetVideoAssetUpload(context.Background(), db, store, 7, false, upload.Asset.ID)
	require.NoError(t, err)
	require.Equal(t, model.VideoAssetStateReady, recovered.State)
	require.Equal(t, 5, recovered.Width)
	require.Equal(t, 4, recovered.Height)
	require.Equal(t, 2, store.getCalls)
	var events int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Count(&events).Error)
	require.Zero(t, events)
}

func TestCompleteReferenceVideoUploadRemainsAsynchronous(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	store := newMemoryVideoAssetStore()
	payload := []byte("video")
	upload, err := CreateVideoAssetUpload(context.Background(), db, store, 7, false, VideoAssetUploadRequest{
		Purpose: model.VideoUploadPurposeReferenceVideo, Filename: "reference.mp4", MIMEType: "video/mp4", SizeBytes: int64(len(payload)),
	})
	require.NoError(t, err)
	var reserved model.KKAIVideoAsset
	require.NoError(t, db.First(&reserved, upload.Asset.ID).Error)
	require.NoError(t, store.Put(
		context.Background(), reserved.ObjectKey, "video/mp4", bytes.NewReader(payload), int64(len(payload)),
	))

	completed, err := CompleteVideoAssetUpload(
		context.Background(), db, store, 7, false, upload.Asset.ID, VideoAssetCompleteRequest{},
	)
	require.NoError(t, err)
	require.Equal(t, model.VideoAssetStateUploaded, completed.State)
	var events int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicInspect).Count(&events).Error)
	require.EqualValues(t, 1, events)
}

var _ VideoAssetStore = (*transientGetVideoAssetStore)(nil)
var _ VideoMultipartAssetStore = (*referenceImageMultipartStore)(nil)
