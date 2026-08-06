package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"

	"gorm.io/gorm"
)

func completeVideoAssetUpload(
	ctx context.Context,
	db *gorm.DB,
	store VideoAssetStore,
	userID int,
	isAdmin bool,
	assetID int64,
	request VideoAssetCompleteRequest,
) (*VideoAssetView, error) {
	if db == nil || store == nil || userID <= 0 || assetID <= 0 {
		return nil, ErrInvalidVideoAssetUpload
	}
	asset, err := loadOwnedVideoAssetUpload(ctx, db, userID, isAdmin, assetID)
	if err != nil {
		return nil, err
	}
	if videoAssetUploadCompleted(asset.State) {
		view := videoAssetView(*asset)
		return &view, nil
	}
	if recovered, completed, recoverErr := recoverCompletedVideoAssetUpload(ctx, db, store, *asset); recoverErr != nil {
		return nil, recoverErr
	} else if completed {
		return recovered, nil
	}
	if asset.State != model.VideoAssetStatePendingUpload {
		return nil, ErrInvalidVideoAssetUpload
	}
	if asset.UploadExpiresAt <= time.Now().Unix() {
		return nil, expireVideoAssetUpload(ctx, db, store, asset)
	}

	var metadata VideoAssetObjectMetadata
	switch videoAssetUploadMode(*asset) {
	case VideoUploadModeSingle:
		if len(request.Parts) != 0 {
			return nil, ErrInvalidVideoAssetUpload
		}
		metadata, err = verifyCompletedVideoAssetObject(ctx, store, *asset)
	case VideoUploadModeMultipart:
		metadata, err = completeMultipartVideoAssetUpload(ctx, store, *asset, request)
	default:
		err = ErrInvalidVideoAssetUpload
	}
	if err != nil {
		return nil, err
	}
	return finalizeVideoAssetUpload(ctx, db, store, *asset, metadata)
}

func completeMultipartVideoAssetUpload(
	ctx context.Context,
	store VideoAssetStore,
	asset model.KKAIVideoAsset,
	request VideoAssetCompleteRequest,
) (VideoAssetObjectMetadata, error) {
	multipartStore, ok := store.(VideoMultipartAssetStore)
	if !ok || asset.MultipartUploadID == "" || asset.UploadPartSize != videoMultipartPartSize {
		return VideoAssetObjectMetadata{}, ErrVideoMultipartUnavailable
	}
	if err := validateCompletedVideoParts(asset, request.Parts); err != nil {
		return VideoAssetObjectMetadata{}, err
	}
	uploaded, err := multipartStore.ListUploadedParts(ctx, asset.ObjectKey, asset.MultipartUploadID)
	if err != nil {
		if errors.Is(err, ErrVideoMultipartUploadNotFound) {
			metadata, verifyErr := verifyCompletedVideoAssetObject(ctx, store, asset)
			if verifyErr == nil {
				return metadata, nil
			}
		}
		return VideoAssetObjectMetadata{}, fmt.Errorf("list video multipart parts before completion: %w", err)
	}
	if err := validateUploadedVideoParts(asset, uploaded, true); err != nil {
		return VideoAssetObjectMetadata{}, err
	}
	sort.Slice(uploaded, func(left int, right int) bool { return uploaded[left].PartNumber < uploaded[right].PartNumber })
	serverParts := make([]VideoAssetCompletedPart, 0, len(uploaded))
	for index, part := range uploaded {
		clientETag, _ := normalizeVideoMultipartETag(request.Parts[index].ETag)
		serverETag, valid := normalizeVideoMultipartETag(part.ETag)
		if !valid || clientETag != serverETag {
			return VideoAssetObjectMetadata{}, ErrInvalidVideoAssetUpload
		}
		serverParts = append(serverParts, VideoAssetCompletedPart{PartNumber: part.PartNumber, ETag: part.ETag})
	}
	completeErr := multipartStore.CompleteMultipartUpload(ctx, asset.ObjectKey, asset.MultipartUploadID, serverParts)
	metadata, verifyErr := verifyCompletedVideoAssetObject(ctx, store, asset)
	if verifyErr == nil {
		return metadata, nil
	}
	if completeErr != nil {
		return VideoAssetObjectMetadata{}, fmt.Errorf("complete video multipart upload: %w", completeErr)
	}
	return VideoAssetObjectMetadata{}, verifyErr
}

func finalizeVideoAssetUpload(
	ctx context.Context,
	db *gorm.DB,
	store VideoAssetStore,
	asset model.KKAIVideoAsset,
	metadata VideoAssetObjectMetadata,
) (*VideoAssetView, error) {
	var imageMetadata *VideoMediaMetadata
	var imageInspectionErr error
	if isVideoReferenceImage(asset) {
		inspected, err := inspectUploadedVideoReferenceImage(ctx, store, asset, metadata)
		if err != nil {
			if !errors.Is(err, ErrVideoMediaInvalid) {
				return nil, err
			}
			imageInspectionErr = err
		} else {
			imageMetadata = &inspected
		}
	}
	now := time.Now().Unix()
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{
			"state": model.VideoAssetStateUploaded, "size_bytes": metadata.ContentLength,
			"sha256":              strings.ToLower(strings.TrimSpace(metadata.SHA256)),
			"multipart_upload_id": "", "upload_part_size": 0, "upload_expires_at": 0, "updated_at": now,
		}
		if imageMetadata != nil {
			updates["state"] = model.VideoAssetStateReady
			updates["mime_type"] = imageMetadata.MIMEType
			updates["width"] = imageMetadata.Width
			updates["height"] = imageMetadata.Height
			updates["duration_seconds"] = imageMetadata.DurationSeconds
			updates["codec"] = imageMetadata.Codec
			updates["failure_reason"] = ""
		} else if imageInspectionErr != nil {
			updates["state"] = model.VideoAssetStateFailed
			updates["failure_reason"] = videoAssetFailureReason(imageInspectionErr)
			updates["archive_source_url"] = ""
		}
		update := tx.Model(&model.KKAIVideoAsset{}).
			Where("id = ? AND state = ?", asset.ID, model.VideoAssetStatePendingUpload).
			Updates(updates)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected == 0 {
			return nil
		}
		if isVideoReferenceImage(asset) {
			return nil
		}
		return EnqueueVideoOutboxEvent(ctx, tx,
			fmt.Sprintf("video:asset:%d:inspect:v1", asset.ID), VideoOutboxTopicInspect,
			strconv.FormatInt(asset.ID, 10), VideoAssetEventPayload{AssetID: asset.ID},
		)
	})
	if err != nil {
		return nil, fmt.Errorf("complete video asset upload: %w", err)
	}
	if err := db.WithContext(ctx).First(&asset, "id = ?", asset.ID).Error; err != nil {
		return nil, err
	}
	if !videoAssetUploadCompleted(asset.State) {
		return nil, ErrInvalidVideoAssetUpload
	}
	view := videoAssetView(asset)
	return &view, nil
}

func isVideoReferenceImage(asset model.KKAIVideoAsset) bool {
	return asset.Kind == model.VideoAssetKindReference &&
		isSupportedReferenceMIME(normalizedVideoObjectContentType(asset.MIMEType))
}

func inspectUploadedVideoReferenceImage(
	ctx context.Context,
	store VideoAssetStore,
	asset model.KKAIVideoAsset,
	expected VideoAssetObjectMetadata,
) (VideoMediaMetadata, error) {
	if store == nil || !isVideoReferenceImage(asset) {
		return VideoMediaMetadata{}, ErrVideoMediaInvalid
	}

	object, err := store.Get(ctx, asset.ObjectKey)
	if err != nil {
		return VideoMediaMetadata{}, fmt.Errorf("read uploaded video reference image: %w", err)
	}
	if object.Body == nil {
		return VideoMediaMetadata{}, fmt.Errorf("read uploaded video reference image: %w", ErrVideoMediaProcessingFailed)
	}
	defer object.Body.Close()

	maxBytes := video_studio_setting.Get().MaxReferenceBytes
	contentType := normalizedVideoObjectContentType(object.ContentType)
	expectedContentType := normalizedVideoObjectContentType(expected.ContentType)
	actualETag := strings.Trim(strings.TrimSpace(object.ETag), `"`)
	expectedETag := strings.Trim(strings.TrimSpace(expected.ETag), `"`)
	if object.ContentLength != asset.SizeBytes || object.ContentLength != expected.ContentLength ||
		object.ContentLength <= 0 || object.ContentLength > maxBytes ||
		contentType != normalizedVideoObjectContentType(asset.MIMEType) || contentType != expectedContentType ||
		(actualETag != "" && expectedETag != "" && actualETag != expectedETag) {
		return VideoMediaMetadata{}, ErrInvalidVideoAssetUpload
	}

	reader := &io.LimitedReader{R: object.Body, N: maxBytes + 1}
	mediaMetadata, detected, inspectionErr := inspectRasterVideoMediaReader(reader)
	_, drainErr := io.Copy(io.Discard, reader)
	readBytes := maxBytes + 1 - reader.N
	if drainErr != nil || readBytes != object.ContentLength {
		return VideoMediaMetadata{}, fmt.Errorf("read uploaded video reference image: %w", ErrVideoMediaProcessingFailed)
	}
	if inspectionErr == nil && (!detected || !videoReferenceMediaCategoryMatches(asset.MIMEType, mediaMetadata.MIMEType)) {
		inspectionErr = ErrVideoMediaInvalid
	}
	if inspectionErr != nil {
		return VideoMediaMetadata{}, fmt.Errorf("inspect uploaded video reference image: %w", inspectionErr)
	}
	return mediaMetadata, nil
}

func recoverCompletedVideoAssetUpload(
	ctx context.Context,
	db *gorm.DB,
	store VideoAssetStore,
	asset model.KKAIVideoAsset,
) (*VideoAssetView, bool, error) {
	if asset.State != model.VideoAssetStatePendingUpload {
		return nil, false, nil
	}
	metadata, err := verifyCompletedVideoAssetObject(ctx, store, asset)
	if errors.Is(err, ErrVideoAssetObjectNotFound) || errors.Is(err, ErrInvalidVideoAssetUpload) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	view, err := finalizeVideoAssetUpload(ctx, db, store, asset, metadata)
	if err != nil {
		return nil, false, err
	}
	return view, true, nil
}

func verifyCompletedVideoAssetObject(
	ctx context.Context,
	store VideoAssetStore,
	asset model.KKAIVideoAsset,
) (VideoAssetObjectMetadata, error) {
	metadata, err := store.Head(ctx, asset.ObjectKey)
	if err != nil {
		return VideoAssetObjectMetadata{}, err
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(metadata.ContentType, ";")[0]))
	if metadata.ContentLength != asset.SizeBytes || contentType != asset.MIMEType {
		return VideoAssetObjectMetadata{}, ErrInvalidVideoAssetUpload
	}
	metadataSHA256 := strings.ToLower(strings.TrimSpace(metadata.SHA256))
	assetSHA256 := strings.ToLower(strings.TrimSpace(asset.SHA256))
	if metadataSHA256 != "" && !validSHA256Hex(metadataSHA256) {
		return VideoAssetObjectMetadata{}, ErrInvalidVideoAssetUpload
	}
	if assetSHA256 != "" && metadataSHA256 != assetSHA256 {
		return VideoAssetObjectMetadata{}, ErrInvalidVideoAssetUpload
	}
	return metadata, nil
}
