package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrVideoAssetInUse = errors.New("video asset is still referenced")

const videoReferenceCleanupScanBatchSize = 100

func DeleteVideoReferenceAsset(ctx context.Context, db *gorm.DB, userID int, assetID int64) (*VideoAssetView, error) {
	if db == nil || userID <= 0 || assetID <= 0 {
		return nil, ErrVideoAssetNotFound
	}
	var asset model.KKAIVideoAsset
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockVideoRowsForUpdate(tx).First(&asset, "id = ?", assetID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrVideoAssetNotFound
			}
			return err
		}
		if asset.OwnerUserID != userID {
			return ErrVideoAssetAccessDenied
		}
		if asset.Scope != model.VideoAssetScopeUser || asset.Kind != model.VideoAssetKindReference || asset.DeletedAt != 0 {
			return ErrVideoAssetNotFound
		}
		if asset.State == model.VideoAssetStateDeleting {
			return nil
		}
		if asset.State == model.VideoAssetStatePendingUpload {
			return ErrVideoAssetUploadCompleted
		}
		inUse, err := videoAssetInUse(ctx, tx, asset.ID)
		if err != nil {
			return err
		}
		if inUse {
			return ErrVideoAssetInUse
		}
		return queueVideoAssetDeletion(ctx, tx, &asset)
	})
	if err != nil {
		return nil, err
	}
	view := videoAssetView(asset)
	return &view, nil
}

func CleanupAbandonedVideoReferenceAssets(ctx context.Context, db *gorm.DB, createdBefore time.Time, limit int) (int, error) {
	if db == nil || createdBefore.IsZero() || limit <= 0 {
		return 0, ErrInvalidVideoAssetUpload
	}
	if limit > 500 {
		limit = 500
	}
	cleaned := 0
	errorsByAsset := make([]error, 0)
	lastID := int64(0)
	for cleaned < limit {
		batchLimit := limit - cleaned
		if batchLimit < videoReferenceCleanupScanBatchSize {
			batchLimit = videoReferenceCleanupScanBatchSize
		}
		if batchLimit > 500 {
			batchLimit = 500
		}
		var assets []model.KKAIVideoAsset
		err := db.WithContext(ctx).
			Where("id > ? AND scope = ? AND kind IN ? AND deleted_at = 0 AND created_at <= ? AND state IN ?",
				lastID, model.VideoAssetScopeUser, []string{model.VideoAssetKindReference, model.VideoAssetKindSample}, createdBefore.Unix(),
				[]string{model.VideoAssetStateUploaded, model.VideoAssetStateProcessing, model.VideoAssetStateReady, model.VideoAssetStateFailed}).
			Where(`NOT EXISTS (
SELECT 1
FROM kkai_video_task_assets AS active_reference
JOIN tasks AS active_reference_task ON active_reference_task.id = active_reference.task_id
WHERE active_reference.asset_id = kkai_video_assets.id
  AND active_reference.role IN ?
  AND active_reference_task.status NOT IN ?
)`, []string{model.VideoTaskAssetRoleReference, model.VideoTaskAssetRoleReferenceVideo,
				model.VideoTaskAssetRoleFirstFrame, model.VideoTaskAssetRoleLastFrame},
				[]model.TaskStatus{model.TaskStatusSuccess, model.TaskStatusFailure}).
			Order("id ASC").Limit(batchLimit).Find(&assets).Error
		if err != nil {
			return cleaned, fmt.Errorf("list abandoned video references: %w", err)
		}
		if len(assets) == 0 {
			break
		}
		lastID = assets[len(assets)-1].ID
		for index := range assets {
			err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				var current model.KKAIVideoAsset
				if err := lockVideoRowsForUpdate(tx).First(&current, "id = ?", assets[index].ID).Error; err != nil {
					return err
				}
				if current.Scope != model.VideoAssetScopeUser ||
					(current.Kind != model.VideoAssetKindReference && current.Kind != model.VideoAssetKindSample) || current.DeletedAt != 0 ||
					current.CreatedAt > createdBefore.Unix() || current.State == model.VideoAssetStatePendingUpload ||
					current.State == model.VideoAssetStateDeleting || current.State == model.VideoAssetStateDeleted {
					return nil
				}
				inUse, err := videoAssetInUse(ctx, tx, current.ID)
				if err != nil {
					return err
				}
				if inUse {
					return nil
				}
				if err := queueVideoAssetDeletion(ctx, tx, &current); err != nil {
					return err
				}
				assets[index] = current
				return nil
			})
			if err != nil {
				errorsByAsset = append(errorsByAsset, fmt.Errorf("cleanup video reference %d: %w", assets[index].ID, err))
				continue
			}
			if assets[index].State == model.VideoAssetStateDeleting {
				cleaned++
				if cleaned == limit {
					break
				}
			}
		}
	}
	return cleaned, errors.Join(errorsByAsset...)
}

func queueVideoAssetDeletion(ctx context.Context, tx *gorm.DB, asset *model.KKAIVideoAsset) error {
	if asset == nil || tx == nil {
		return ErrVideoAssetNotFound
	}
	now := time.Now().Unix()
	update := tx.Model(&model.KKAIVideoAsset{}).
		Where("id = ? AND state NOT IN ?", asset.ID, []string{
			model.VideoAssetStatePendingUpload, model.VideoAssetStateDeleting, model.VideoAssetStateDeleted,
		}).Updates(map[string]any{"state": model.VideoAssetStateDeleting, "updated_at": now})
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected != 1 {
		return ErrVideoAssetInUse
	}
	if err := EnqueueVideoOutboxEvent(ctx, tx,
		fmt.Sprintf("video:asset:%d:delete:v2:%s", asset.ID, uuid.NewString()), VideoOutboxTopicDelete,
		strconv.FormatInt(asset.ID, 10), VideoAssetEventPayload{AssetID: asset.ID},
	); err != nil {
		return err
	}
	asset.State = model.VideoAssetStateDeleting
	asset.UpdatedAt = now
	return nil
}
