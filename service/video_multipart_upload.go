package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

const (
	videoMultipartPartSize         int64 = 8 << 20
	videoMultipartMaximumParts           = 10_000
	videoMultipartUploadTTL              = time.Hour
	videoMultipartPartSignatureTTL       = 15 * time.Minute
)

type VideoAssetCompleteRequest struct {
	Parts []VideoAssetCompletedPart `json:"parts,omitempty"`
}

func createMultipartVideoAssetUpload(
	ctx context.Context,
	db *gorm.DB,
	store VideoMultipartAssetStore,
	asset model.KKAIVideoAsset,
) (*VideoAssetUpload, error) {
	uploadID, err := store.CreateMultipartUpload(ctx, asset.ObjectKey, asset.MIMEType)
	if err != nil {
		discardVideoAssetUploadReservation(ctx, db, asset.ID)
		return nil, fmt.Errorf("create video multipart upload: %w", err)
	}
	update := db.WithContext(ctx).Model(&model.KKAIVideoAsset{}).
		Where("id = ? AND state = ? AND multipart_upload_id = ''", asset.ID, model.VideoAssetStatePendingUpload).
		Update("multipart_upload_id", uploadID)
	if update.Error != nil || update.RowsAffected != 1 {
		if abortErr := store.AbortMultipartUpload(ctx, asset.ObjectKey, uploadID); abortErr != nil && !errors.Is(abortErr, ErrVideoMultipartUploadNotFound) {
			common.SysError("abort unbound video multipart upload: " + abortErr.Error())
		}
		discardVideoAssetUploadReservation(ctx, db, asset.ID)
		if update.Error != nil {
			return nil, fmt.Errorf("bind video multipart upload: %w", update.Error)
		}
		return nil, ErrInvalidVideoAssetUpload
	}
	asset.MultipartUploadID = uploadID
	view := videoAssetView(asset)
	return &VideoAssetUpload{
		Asset: view, UploadMode: VideoUploadModeMultipart, PartSize: asset.UploadPartSize, ExpiresAt: asset.UploadExpiresAt,
	}, nil
}

func discardVideoAssetUploadReservation(ctx context.Context, db *gorm.DB, assetID int64) {
	if db == nil || assetID <= 0 {
		return
	}
	if err := db.WithContext(ctx).Unscoped().Delete(&model.KKAIVideoAsset{}, "id = ?", assetID).Error; err != nil {
		common.SysError("discard video asset upload reservation: " + err.Error())
	}
}

func SignVideoAssetUploadPart(
	ctx context.Context,
	db *gorm.DB,
	store VideoAssetStore,
	userID int,
	isAdmin bool,
	assetID int64,
	partNumber int32,
) (*VideoAssetSignedRequest, error) {
	asset, err := loadOwnedVideoAssetUpload(ctx, db, userID, isAdmin, assetID)
	if err != nil {
		return nil, err
	}
	if _, completed, recoverErr := recoverCompletedVideoAssetUpload(ctx, db, store, *asset); recoverErr != nil {
		return nil, recoverErr
	} else if completed {
		return nil, ErrVideoAssetUploadCompleted
	}
	if err := requirePendingMultipartVideoUpload(ctx, db, store, asset); err != nil {
		return nil, err
	}
	expectedParts := videoMultipartPartCount(asset.SizeBytes, asset.UploadPartSize)
	if partNumber <= 0 || int(partNumber) > expectedParts {
		return nil, ErrInvalidVideoAssetUpload
	}
	contentLength := videoMultipartPartLength(asset.SizeBytes, asset.UploadPartSize, int(partNumber))
	remaining := time.Until(time.Unix(asset.UploadExpiresAt, 0))
	expires := videoMultipartPartSignatureTTL
	if remaining < expires {
		expires = remaining
	}
	if expires <= 0 {
		return nil, expireVideoAssetUpload(ctx, db, store, asset)
	}
	multipartStore := store.(VideoMultipartAssetStore)
	signed, err := multipartStore.PresignUploadPart(
		ctx, asset.ObjectKey, asset.MultipartUploadID, partNumber, contentLength, expires,
	)
	if err != nil {
		return nil, fmt.Errorf("presign video multipart part: %w", err)
	}
	return &signed, nil
}

func ListVideoAssetUploadParts(
	ctx context.Context,
	db *gorm.DB,
	store VideoAssetStore,
	userID int,
	isAdmin bool,
	assetID int64,
) ([]VideoAssetUploadedPart, error) {
	asset, err := loadOwnedVideoAssetUpload(ctx, db, userID, isAdmin, assetID)
	if err != nil {
		return nil, err
	}
	if _, completed, recoverErr := recoverCompletedVideoAssetUpload(ctx, db, store, *asset); recoverErr != nil {
		return nil, recoverErr
	} else if completed {
		return []VideoAssetUploadedPart{}, nil
	}
	if err := requirePendingMultipartVideoUpload(ctx, db, store, asset); err != nil {
		return nil, err
	}
	parts, err := store.(VideoMultipartAssetStore).ListUploadedParts(ctx, asset.ObjectKey, asset.MultipartUploadID)
	if err != nil {
		return nil, fmt.Errorf("list video multipart parts: %w", err)
	}
	if err := validateUploadedVideoParts(*asset, parts, false); err != nil {
		return nil, err
	}
	sort.Slice(parts, func(left int, right int) bool { return parts[left].PartNumber < parts[right].PartNumber })
	return parts, nil
}
