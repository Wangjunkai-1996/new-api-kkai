package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"

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
	return finalizeVideoAssetUpload(ctx, db, *asset, metadata)
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
	asset model.KKAIVideoAsset,
	metadata VideoAssetObjectMetadata,
) (*VideoAssetView, error) {
	now := time.Now().Unix()
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		update := tx.Model(&model.KKAIVideoAsset{}).
			Where("id = ? AND state = ?", asset.ID, model.VideoAssetStatePendingUpload).
			Updates(map[string]any{
				"state": model.VideoAssetStateUploaded, "size_bytes": metadata.ContentLength,
				"sha256":              strings.ToLower(strings.TrimSpace(metadata.SHA256)),
				"multipart_upload_id": "", "upload_part_size": 0, "upload_expires_at": 0, "updated_at": now,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected == 0 {
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
	view, err := finalizeVideoAssetUpload(ctx, db, asset, metadata)
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
