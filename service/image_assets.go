package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"

	"gorm.io/gorm"
)

var (
	ErrImageAssetNotFound     = errors.New("image asset not found")
	ErrImageAssetAccessDenied = errors.New("image asset access denied")
)

func GetAuthorizedImageAsset(
	ctx context.Context,
	db *gorm.DB,
	userID int,
	isAdmin bool,
	assetID int64,
) (*model.KKAIImageAsset, error) {
	if db == nil || userID <= 0 || assetID <= 0 {
		return nil, ErrImageAssetNotFound
	}
	var asset model.KKAIImageAsset
	if err := db.WithContext(ctx).First(&asset, "id = ? AND deleted_at = 0", assetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrImageAssetNotFound
		}
		return nil, fmt.Errorf("get image asset: %w", err)
	}
	if asset.Scope == model.ImageAssetScopeUser {
		if asset.OwnerUserID != userID && !isAdmin {
			return nil, ErrImageAssetAccessDenied
		}
		if asset.GenerationID == nil || asset.Kind != model.ImageAssetKindOutput {
			return nil, ErrImageAssetAccessDenied
		}
		var generation model.KKAIImageGeneration
		if err := db.WithContext(ctx).Select("status", "billing_state").First(
			&generation, "id = ? AND deleted_at = 0", *asset.GenerationID,
		).Error; err != nil {
			return nil, ErrImageAssetNotFound
		}
		if generation.BillingState != model.ImageGenerationBillingStateSettled ||
			(generation.Status != model.ImageGenerationStatusSucceeded &&
				generation.Status != model.ImageGenerationStatusPartial) {
			return nil, ErrImageAssetNotFound
		}
		return &asset, nil
	}
	if asset.Scope != model.ImageAssetScopeCatalog {
		return nil, ErrImageAssetAccessDenied
	}
	if isAdmin {
		return &asset, nil
	}
	var published int64
	if err := db.WithContext(ctx).Model(&model.KKAIImageSample{}).
		Joins("JOIN kkai_image_model_profiles ON kkai_image_model_profiles.id = kkai_image_samples.model_profile_id").
		Where(
			"kkai_image_samples.image_asset_id = ? AND kkai_image_samples.status = ? AND "+
				"kkai_image_model_profiles.enabled = ? AND "+
				"kkai_image_samples.model_version = kkai_image_model_profiles.specification_version",
			asset.ID, model.ImageSampleStatusPublished, true,
		).Count(&published).Error; err != nil {
		return nil, fmt.Errorf("check published image catalog asset: %w", err)
	}
	if published == 0 {
		return nil, ErrImageAssetAccessDenied
	}
	return &asset, nil
}

func SignAuthorizedImageAsset(
	ctx context.Context,
	db *gorm.DB,
	store ImageAssetStore,
	userID int,
	isAdmin bool,
	assetID int64,
	thumbnail bool,
	attachment bool,
) (string, error) {
	if store == nil {
		return "", ErrImageAssetNotFound
	}
	asset, err := GetAuthorizedImageAsset(ctx, db, userID, isAdmin, assetID)
	if err != nil {
		return "", err
	}
	if asset.State != model.ImageAssetStateReady {
		return "", ErrImageAssetNotFound
	}
	key := asset.ObjectKey
	filename := asset.OriginalFilename
	if filename == "" {
		filename = "image"
	}
	if thumbnail {
		if asset.ThumbnailState != model.ImageThumbnailStateReady || asset.ThumbnailObjectKey == "" {
			return "", ErrImageAssetNotFound
		}
		key = asset.ThumbnailObjectKey
		filename = "thumbnail.jpg"
	}
	settings := image_studio_setting.Get()
	return store.PresignDownload(
		ctx, key, filename, attachment, time.Duration(settings.SignedURLSeconds)*time.Second,
	)
}

func imageAssetContentPath(assetID int64, thumbnail bool) string {
	path := "/api/image-studio/assets/" + strconv.FormatInt(assetID, 10) + "/content"
	if thumbnail {
		return path + "?variant=thumbnail"
	}
	return path
}

func imageAssetDownloadPath(assetID int64) string {
	return "/api/image-studio/assets/" + strconv.FormatInt(assetID, 10) + "/download"
}
