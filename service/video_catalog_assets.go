package service

import (
	"context"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

func videoSampleAssetIDs(sample model.KKAIVideoSample) ([]int64, error) {
	if sample.VideoAssetID <= 0 {
		return nil, ErrVideoSampleDataCorrupt
	}
	referenceIDs, err := decodeVideoSampleReferenceAssetIDs(sample.ReferenceAssetIDs)
	if err != nil {
		return nil, fmt.Errorf("%w: decode sample %d references: %v", ErrVideoSampleDataCorrupt, sample.ID, err)
	}
	assetIDs := make([]int64, 0, len(referenceIDs)+1)
	assetIDs = append(assetIDs, sample.VideoAssetID)
	assetIDs = append(assetIDs, referenceIDs...)
	return assetIDs, nil
}

func videoCatalogAssetInUse(ctx context.Context, db *gorm.DB, assetID int64) (bool, error) {
	needle := "%" + strconv.FormatInt(assetID, 10) + "%"
	var samples []model.KKAIVideoSample
	if err := db.WithContext(ctx).
		Where("video_asset_id = ? OR reference_asset_ids LIKE ?", assetID, needle).
		Select("id, video_asset_id, reference_asset_ids").
		Find(&samples).Error; err != nil {
		return false, fmt.Errorf("audit video catalog asset usage: %w", err)
	}
	for _, sample := range samples {
		if sample.VideoAssetID == assetID {
			return true, nil
		}
		referenceIDs, err := decodeVideoSampleReferenceAssetIDs(sample.ReferenceAssetIDs)
		if err != nil {
			return false, fmt.Errorf("decode video sample %d references: %w", sample.ID, err)
		}
		for _, referenceID := range referenceIDs {
			if referenceID == assetID {
				return true, nil
			}
		}
	}
	return false, nil
}

func videoAssetInActiveTask(ctx context.Context, db *gorm.DB, assetID int64) (bool, error) {
	var references int64
	if err := db.WithContext(ctx).Model(&model.KKAIVideoTaskAsset{}).
		Joins("JOIN tasks AS video_asset_tasks ON video_asset_tasks.id = kkai_video_task_assets.task_id").
		Where("kkai_video_task_assets.asset_id = ? AND video_asset_tasks.status NOT IN ?", assetID,
			[]model.TaskStatus{model.TaskStatusSuccess, model.TaskStatusFailure}).
		Count(&references).Error; err != nil {
		return false, fmt.Errorf("audit active video task asset usage: %w", err)
	}
	return references > 0, nil
}

func videoAssetInUse(ctx context.Context, db *gorm.DB, assetID int64) (bool, error) {
	inUse, err := videoCatalogAssetInUse(ctx, db, assetID)
	if err != nil || inUse {
		return inUse, err
	}
	return videoAssetInActiveTask(ctx, db, assetID)
}

func queueUnusedVideoCatalogAssetsForDeletion(
	ctx context.Context,
	tx *gorm.DB,
	previousAssetIDs []int64,
	currentAssetIDs []int64,
) error {
	current := make(map[int64]struct{}, len(currentAssetIDs))
	for _, assetID := range currentAssetIDs {
		current[assetID] = struct{}{}
	}
	candidates := make([]int64, 0, len(previousAssetIDs))
	seen := make(map[int64]struct{}, len(previousAssetIDs))
	for _, assetID := range previousAssetIDs {
		if assetID <= 0 {
			continue
		}
		if _, retained := current[assetID]; retained {
			continue
		}
		if _, duplicate := seen[assetID]; duplicate {
			continue
		}
		seen[assetID] = struct{}{}
		candidates = append(candidates, assetID)
	}
	assets, err := lockVideoAssetRowsForUpdate(ctx, tx, candidates)
	if err != nil {
		return err
	}
	for index := range assets {
		asset := &assets[index]
		if asset.Scope != model.VideoAssetScopeCatalog || asset.DeletedAt != 0 ||
			(asset.Kind != model.VideoAssetKindSample && asset.Kind != model.VideoAssetKindReference) ||
			asset.State == model.VideoAssetStatePendingUpload || asset.State == model.VideoAssetStateDeleting ||
			asset.State == model.VideoAssetStateDeleted {
			continue
		}
		inUse, err := videoCatalogAssetInUse(ctx, tx, asset.ID)
		if err != nil {
			return err
		}
		if inUse {
			continue
		}
		if err := queueVideoAssetDeletion(ctx, tx, asset); err != nil {
			return err
		}
	}
	return nil
}
