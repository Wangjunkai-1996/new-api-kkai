package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

const videoAbortedUploadCleanupInterval = 24 * time.Hour

func AbortVideoAssetUpload(
	ctx context.Context,
	db *gorm.DB,
	store VideoAssetStore,
	userID int,
	isAdmin bool,
	assetID int64,
) (*VideoAssetView, error) {
	asset, err := loadOwnedVideoAssetUpload(ctx, db, userID, isAdmin, assetID)
	if err != nil {
		return nil, err
	}
	if asset.State == model.VideoAssetStateDeleted {
		view := videoAssetView(*asset)
		return &view, nil
	}
	if asset.State != model.VideoAssetStatePendingUpload && !videoAssetUploadAbortInProgress(*asset) {
		return nil, ErrVideoAssetUploadCompleted
	}
	completed, err := abortOwnedVideoAssetUpload(ctx, db, store, asset)
	if err != nil {
		return nil, err
	}
	if completed {
		return nil, ErrVideoAssetUploadCompleted
	}
	if err := db.WithContext(ctx).First(asset, "id = ?", asset.ID).Error; err != nil {
		return nil, err
	}
	view := videoAssetView(*asset)
	return &view, nil
}

func ExpireVideoAssetUploads(ctx context.Context, db *gorm.DB, store VideoAssetStore, limit int) (int, error) {
	if db == nil || store == nil || limit <= 0 {
		return 0, ErrInvalidVideoAssetUpload
	}
	if limit > 500 {
		limit = 500
	}
	now := time.Now().Unix()
	assets := make([]model.KKAIVideoAsset, 0, limit)
	if err := db.WithContext(ctx).
		Where("state IN ? AND upload_expires_at > 0 AND upload_expires_at <= ?",
			[]string{model.VideoAssetStatePendingUpload, model.VideoAssetStateDeleting}, now).
		Order("upload_expires_at ASC").Order("id ASC").Limit(limit).Find(&assets).Error; err != nil {
		return 0, fmt.Errorf("list expired video uploads: %w", err)
	}
	if remaining := limit - len(assets); remaining > 0 {
		selectedIDs := make([]int64, 0, len(assets))
		for _, asset := range assets {
			selectedIDs = append(selectedIDs, asset.ID)
		}
		query := db.WithContext(ctx).
			Where("state = ? AND upload_expires_at > 0 AND upload_expires_at <= ?", model.VideoAssetStateDeleted, now)
		if len(selectedIDs) > 0 {
			query = query.Where("id NOT IN ?", selectedIDs)
		}
		var tombstones []model.KKAIVideoAsset
		if err := query.Order("upload_expires_at ASC").Order("id ASC").Limit(remaining).Find(&tombstones).Error; err != nil {
			return 0, fmt.Errorf("list expired video upload tombstones: %w", err)
		}
		assets = append(assets, tombstones...)
	}
	expired := 0
	errorsByAsset := make([]error, 0)
	for index := range assets {
		completed, err := abortOwnedVideoAssetUpload(ctx, db, store, &assets[index])
		if err != nil {
			errorsByAsset = append(errorsByAsset, fmt.Errorf("expire video upload %d: %w", assets[index].ID, err))
			continue
		}
		if completed {
			continue
		}
		expired++
	}
	return expired, errors.Join(errorsByAsset...)
}

func loadOwnedVideoAssetUpload(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	isAdmin bool,
	assetID int64,
) (*model.KKAIVideoAsset, error) {
	if db == nil || userID <= 0 || assetID <= 0 {
		return nil, ErrVideoAssetNotFound
	}
	var asset model.KKAIVideoAsset
	if err := db.WithContext(ctx).First(&asset, "id = ?", assetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVideoAssetNotFound
		}
		return nil, err
	}
	if asset.OwnerUserID != userID || (asset.Scope == model.VideoAssetScopeCatalog && !isAdmin) {
		return nil, ErrVideoAssetAccessDenied
	}
	return &asset, nil
}

func requirePendingMultipartVideoUpload(
	ctx context.Context,
	db *gorm.DB,
	store VideoAssetStore,
	asset *model.KKAIVideoAsset,
) error {
	if asset == nil || asset.State != model.VideoAssetStatePendingUpload ||
		videoAssetUploadMode(*asset) != VideoUploadModeMultipart || asset.MultipartUploadID == "" ||
		asset.UploadPartSize != videoMultipartPartSize {
		return ErrInvalidVideoAssetUpload
	}
	if _, ok := store.(VideoMultipartAssetStore); !ok {
		return ErrVideoMultipartUnavailable
	}
	if asset.UploadExpiresAt <= time.Now().Unix() {
		return expireVideoAssetUpload(ctx, db, store, asset)
	}
	return nil
}

func expireVideoAssetUpload(ctx context.Context, db *gorm.DB, store VideoAssetStore, asset *model.KKAIVideoAsset) error {
	completed, err := abortOwnedVideoAssetUpload(ctx, db, store, asset)
	if err != nil {
		return err
	}
	if completed {
		return ErrVideoAssetUploadCompleted
	}
	return ErrVideoAssetUploadExpired
}

func abortOwnedVideoAssetUpload(
	ctx context.Context,
	db *gorm.DB,
	store VideoAssetStore,
	asset *model.KKAIVideoAsset,
) (bool, error) {
	if asset == nil || (asset.State != model.VideoAssetStatePendingUpload && !videoAssetUploadCleanupScheduled(*asset)) {
		return false, ErrInvalidVideoAssetUpload
	}
	if asset.State == model.VideoAssetStatePendingUpload {
		if _, completed, err := recoverCompletedVideoAssetUpload(ctx, db, store, *asset); err != nil {
			if !errors.Is(err, ErrInvalidVideoAssetUpload) {
				return false, err
			}
			completed, refreshErr := refreshContendedVideoUploadAbort(ctx, db, asset)
			if refreshErr != nil || completed || asset.State == model.VideoAssetStateDeleted {
				return completed, refreshErr
			}
		} else if completed {
			return true, nil
		}
	}
	if asset.State == model.VideoAssetStatePendingUpload {
		now := time.Now().Unix()
		update := db.WithContext(ctx).Model(&model.KKAIVideoAsset{}).
			Where("id = ? AND state = ?", asset.ID, model.VideoAssetStatePendingUpload).
			Updates(map[string]any{"state": model.VideoAssetStateDeleting, "updated_at": now})
		if update.Error != nil {
			return false, fmt.Errorf("claim video upload abort: %w", update.Error)
		}
		if update.RowsAffected != 1 {
			completed, err := refreshContendedVideoUploadAbort(ctx, db, asset)
			if err != nil || completed || asset.State == model.VideoAssetStateDeleted {
				return completed, err
			}
		} else {
			asset.State = model.VideoAssetStateDeleting
			asset.UpdatedAt = now
		}
	}

	switch videoAssetUploadMode(*asset) {
	case VideoUploadModeSingle:
		if err := store.Delete(ctx, []string{asset.ObjectKey}); err != nil {
			return false, fmt.Errorf("delete incomplete video upload: %w", err)
		}
	case VideoUploadModeMultipart:
		if asset.State != model.VideoAssetStateDeleted {
			multipartStore, ok := store.(VideoMultipartAssetStore)
			if !ok || asset.MultipartUploadID == "" {
				return false, ErrVideoMultipartUnavailable
			}
			err := multipartStore.AbortMultipartUpload(ctx, asset.ObjectKey, asset.MultipartUploadID)
			if err != nil && !errors.Is(err, ErrVideoMultipartUploadNotFound) {
				return false, fmt.Errorf("abort video multipart upload: %w", err)
			}
		}
		if err := store.Delete(ctx, []string{asset.ObjectKey}); err != nil {
			return false, fmt.Errorf("delete incomplete video multipart upload: %w", err)
		}
	default:
		return false, ErrInvalidVideoAssetUpload
	}
	now := time.Now()
	if asset.State == model.VideoAssetStateDeleted {
		// Keep the tombstone scheduled: a PUT or multipart completion admitted before expiry may finish after this pass.
		nextCleanupAt := now.Add(videoAbortedUploadCleanupInterval).Unix()
		update := db.WithContext(ctx).Model(&model.KKAIVideoAsset{}).
			Where("id = ? AND state = ? AND upload_expires_at = ?", asset.ID, model.VideoAssetStateDeleted, asset.UploadExpiresAt).
			Updates(map[string]any{"upload_expires_at": nextCleanupAt, "updated_at": now.Unix()})
		if update.Error != nil {
			return false, fmt.Errorf("reschedule aborted video upload cleanup: %w", update.Error)
		}
		if update.RowsAffected == 0 {
			_, err := refreshContendedVideoUploadAbort(ctx, db, asset)
			return false, err
		}
		asset.UploadExpiresAt = nextCleanupAt
		asset.UpdatedAt = now.Unix()
		return false, nil
	}
	if asset.UploadExpiresAt > now.Unix() {
		return false, nil
	}

	nextCleanupAt := now.Add(videoAbortedUploadCleanupInterval).Unix()
	update := db.WithContext(ctx).Model(&model.KKAIVideoAsset{}).
		Where("id = ? AND state = ?", asset.ID, model.VideoAssetStateDeleting).
		Updates(map[string]any{
			"state": model.VideoAssetStateDeleted, "multipart_upload_id": "", "upload_part_size": 0,
			"upload_expires_at": nextCleanupAt, "deleted_at": now.Unix(), "updated_at": now.Unix(),
		})
	if update.Error != nil {
		return false, fmt.Errorf("mark video upload aborted: %w", update.Error)
	}
	if update.RowsAffected != 1 {
		completed, err := refreshContendedVideoUploadAbort(ctx, db, asset)
		if err != nil || completed || asset.State == model.VideoAssetStateDeleted {
			return completed, err
		}
		return false, ErrInvalidVideoAssetUpload
	}
	asset.State = model.VideoAssetStateDeleted
	asset.MultipartUploadID = ""
	asset.UploadPartSize = 0
	asset.UploadExpiresAt = nextCleanupAt
	asset.DeletedAt = now.Unix()
	asset.UpdatedAt = now.Unix()
	return false, nil
}

func refreshContendedVideoUploadAbort(
	ctx context.Context,
	db *gorm.DB,
	asset *model.KKAIVideoAsset,
) (bool, error) {
	var current model.KKAIVideoAsset
	if err := db.WithContext(ctx).First(&current, "id = ?", asset.ID).Error; err != nil {
		return false, err
	}
	*asset = current
	if videoAssetUploadCompleted(current.State) {
		return true, nil
	}
	if current.State == model.VideoAssetStateDeleted || videoAssetUploadCleanupScheduled(current) {
		return false, nil
	}
	return false, ErrInvalidVideoAssetUpload
}

func videoAssetUploadMode(asset model.KKAIVideoAsset) string {
	if asset.UploadMode == "" {
		return VideoUploadModeSingle
	}
	return asset.UploadMode
}

func videoAssetUploadCompleted(state string) bool {
	switch state {
	case model.VideoAssetStateUploaded, model.VideoAssetStateProcessing, model.VideoAssetStateReady, model.VideoAssetStateFailed:
		return true
	default:
		return false
	}
}

func videoAssetUploadAbortInProgress(asset model.KKAIVideoAsset) bool {
	return asset.State == model.VideoAssetStateDeleting && videoAssetUploadCleanupScheduled(asset)
}

func videoAssetUploadCleanupScheduled(asset model.KKAIVideoAsset) bool {
	if asset.UploadExpiresAt <= 0 || (asset.State != model.VideoAssetStateDeleting && asset.State != model.VideoAssetStateDeleted) {
		return false
	}
	switch videoAssetUploadMode(asset) {
	case VideoUploadModeSingle:
		return true
	case VideoUploadModeMultipart:
		return asset.State == model.VideoAssetStateDeleted ||
			(asset.MultipartUploadID != "" && asset.UploadPartSize == videoMultipartPartSize)
	default:
		return false
	}
}
