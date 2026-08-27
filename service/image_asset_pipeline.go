package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"

	"gorm.io/gorm"
)

const ImageAssetThumbnailTopic = "image.asset.thumbnail.v1"

var ErrInvalidImageAssetPipeline = errors.New("invalid image asset pipeline")

type ImageAssetPipeline struct {
	db        *gorm.DB
	store     ImageAssetStore
	fetcher   ImageArchiveSourceFetcher
	maxBytes  int64
	maxPixels int64
}

type ImageArchiveResult struct {
	Assets []model.KKAIImageAsset
	Ready  int
	Failed int
}

type imageThumbnailPayload struct {
	AssetID int64 `json:"asset_id"`
}

func NewImageAssetPipeline(
	db *gorm.DB,
	store ImageAssetStore,
	fetcher ImageArchiveSourceFetcher,
	maxBytes int64,
	maxPixels int64,
) (*ImageAssetPipeline, error) {
	if db == nil || store == nil || fetcher == nil || maxBytes <= 0 || maxPixels <= 0 {
		return nil, ErrInvalidImageAssetPipeline
	}
	return &ImageAssetPipeline{db: db, store: store, fetcher: fetcher, maxBytes: maxBytes, maxPixels: maxPixels}, nil
}

func NewRuntimeImageAssetPipeline(ctx context.Context, db *gorm.DB) (*ImageAssetPipeline, error) {
	store, err := ImageStudioR2AssetStore(ctx)
	if err != nil {
		return nil, err
	}
	tempDir := ImageStudioTempDirectory()
	settings := image_studio_setting.Get()
	return NewImageAssetPipeline(
		db, store, NewHTTPImageArchiveFetcher(tempDir), settings.MaxOutputBytes, settings.MaxPixels,
	)
}

func ImageStudioTempDirectory() string {
	tempDir := strings.TrimSpace(os.Getenv(videoStudioTempDirectoryEnvironment))
	if tempDir == "" {
		return os.TempDir()
	}
	return tempDir
}

func (pipeline *ImageAssetPipeline) ArchiveGeneration(
	ctx context.Context,
	generation model.KKAIImageGeneration,
	results []ImageRelayResult,
) (*ImageArchiveResult, error) {
	if pipeline == nil || pipeline.db == nil || pipeline.store == nil || pipeline.fetcher == nil ||
		generation.ID <= 0 || generation.UserID <= 0 || generation.Status != model.ImageGenerationStatusSubmitting ||
		len(results) == 0 || len(results) > generation.RequestedCount {
		return nil, ErrInvalidImageAssetPipeline
	}
	archiveResult := &ImageArchiveResult{Assets: make([]model.KKAIImageAsset, 0, len(results))}
	for position := range results {
		asset, err := pipeline.archiveResult(ctx, generation, position, results[position])
		results[position].Base64 = ""
		results[position].URL = ""
		results[position].RevisedPrompt = ""
		if err != nil {
			return archiveResult, err
		}
		archiveResult.Assets = append(archiveResult.Assets, asset)
		if asset.State == model.ImageAssetStateReady {
			archiveResult.Ready++
		} else {
			archiveResult.Failed++
		}
	}
	return archiveResult, nil
}

func (pipeline *ImageAssetPipeline) archiveResult(
	ctx context.Context,
	generation model.KKAIImageGeneration,
	position int,
	result ImageRelayResult,
) (model.KKAIImageAsset, error) {
	now := time.Now().Unix()
	objectKey := fmt.Sprintf("image-studio/users/%d/generations/%d/%03d-original", generation.UserID, generation.ID, position)
	asset := model.KKAIImageAsset{
		GenerationID: &generation.ID, OwnerUserID: generation.UserID,
		Scope: model.ImageAssetScopeUser, Kind: model.ImageAssetKindOutput,
		State: model.ImageAssetStateStaging, Position: position, ObjectKey: objectKey,
		ThumbnailState: model.ImageThumbnailStatePending, CreatedAt: now, UpdatedAt: now,
	}
	if err := pipeline.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireSubmittingImageGeneration(tx, generation.ID); err != nil {
			return err
		}
		return tx.Create(&asset).Error
	}); err != nil {
		return model.KKAIImageAsset{}, fmt.Errorf("create staging image asset: %w", err)
	}

	archive, err := pipeline.fetchResult(ctx, result)
	if err != nil {
		if markErr := pipeline.markAssetFailed(ctx, asset.ID, imageArchiveFailureReason(err)); markErr != nil {
			return model.KKAIImageAsset{}, markErr
		}
		asset.State = model.ImageAssetStateFailed
		asset.ThumbnailState = model.ImageThumbnailStateFailed
		asset.FailureReason = imageArchiveFailureReason(err)
		return asset, nil
	}
	defer archive.Remove()
	file, err := os.Open(archive.Path)
	if err != nil {
		if markErr := pipeline.markAssetFailed(ctx, asset.ID, "temporary_file_unavailable"); markErr != nil {
			return model.KKAIImageAsset{}, markErr
		}
		asset.State = model.ImageAssetStateFailed
		asset.ThumbnailState = model.ImageThumbnailStateFailed
		asset.FailureReason = "temporary_file_unavailable"
		return asset, nil
	}
	putCompensationKey := imageAssetPutCompensationEventKey(asset.ID)
	compensationAvailableAt := time.Now().Add(
		time.Duration(image_studio_setting.Get().SubmissionTimeoutSecs)*time.Second + time.Minute,
	)
	if err := EnqueueKKAIOutboxEvent(
		ctx,
		pipeline.db,
		putCompensationKey,
		ImageAssetDeleteTopic,
		strconv.FormatInt(asset.ID, 10),
		compensationAvailableAt,
		imageAssetDeletePayload{AssetID: asset.ID, ObjectKey: objectKey},
	); err != nil {
		_ = file.Close()
		return model.KKAIImageAsset{}, fmt.Errorf("prepare image object compensation: %w", err)
	}
	putErr := pipeline.store.Put(ctx, objectKey, archive.MIMEType, file, archive.SizeBytes)
	closeErr := file.Close()
	if putErr != nil || closeErr != nil {
		if markErr := pipeline.markAssetFailed(ctx, asset.ID, "object_store_write_failed"); markErr != nil {
			return model.KKAIImageAsset{}, markErr
		}
		asset.State = model.ImageAssetStateFailed
		asset.ThumbnailState = model.ImageThumbnailStateFailed
		asset.FailureReason = "object_store_write_failed"
		return asset, nil
	}
	filename := generatedImageAssetFilename(asset.ID, archive.MIMEType)

	err = pipeline.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireSubmittingImageGeneration(tx, generation.ID); err != nil {
			return err
		}
		updated := tx.Model(&model.KKAIImageAsset{}).
			Where("id = ? AND state = ?", asset.ID, model.ImageAssetStateStaging).
			Updates(map[string]any{
				"state": model.ImageAssetStateReady, "mime_type": archive.MIMEType,
				"original_filename": filename,
				"size_bytes":        archive.SizeBytes, "width": archive.Width, "height": archive.Height,
				"sha256": archive.SHA256, "failure_reason": "", "updated_at": time.Now().Unix(),
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrInvalidImageAssetPipeline
		}
		if err := EnqueueKKAIOutboxEvent(
			ctx, tx, "image-thumbnail:"+strconv.FormatInt(asset.ID, 10)+":v1",
			ImageAssetThumbnailTopic, strconv.FormatInt(asset.ID, 10), time.Now(),
			imageThumbnailPayload{AssetID: asset.ID},
		); err != nil {
			return err
		}
		deliveredAt := time.Now().Unix()
		completed := tx.Model(&model.KKAIOutboxEvent{}).Where(
			"event_key = ? AND topic = ? AND status = ?",
			putCompensationKey, ImageAssetDeleteTopic, model.KKAIOutboxStatusPending,
		).Updates(map[string]any{
			"status": model.KKAIOutboxStatusDelivered, "delivered_at": deliveredAt,
			"locked_at": 0, "locked_by": "", "last_error": "",
		})
		if completed.Error != nil {
			return completed.Error
		}
		if completed.RowsAffected != 1 {
			return ErrImageGenerationConflict
		}
		return nil
	})
	if err != nil {
		_ = pipeline.store.Delete(ctx, []string{objectKey})
		return model.KKAIImageAsset{}, fmt.Errorf("finalize image asset: %w", err)
	}
	asset.State = model.ImageAssetStateReady
	asset.OriginalFilename = filename
	asset.MIMEType = archive.MIMEType
	asset.SizeBytes = archive.SizeBytes
	asset.Width = archive.Width
	asset.Height = archive.Height
	asset.SHA256 = archive.SHA256
	return asset, nil
}

func imageAssetPutCompensationEventKey(assetID int64) string {
	return "image-delete:" + strconv.FormatInt(assetID, 10) + ":put-compensation:v1"
}

func (pipeline *ImageAssetPipeline) fetchResult(ctx context.Context, result ImageRelayResult) (*FetchedImageArchive, error) {
	if strings.TrimSpace(result.URL) != "" && strings.TrimSpace(result.Base64) == "" {
		return pipeline.fetcher.FetchURL(ctx, result.URL, pipeline.maxBytes, pipeline.maxPixels)
	}
	if strings.TrimSpace(result.Base64) != "" && strings.TrimSpace(result.URL) == "" {
		return pipeline.fetcher.FetchBase64(result.Base64, pipeline.maxBytes, pipeline.maxPixels)
	}
	return nil, ErrInvalidImageRelayResponse
}

func (pipeline *ImageAssetPipeline) markAssetFailed(ctx context.Context, assetID int64, reason string) error {
	return pipeline.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var asset model.KKAIImageAsset
		if err := tx.Select("id", "generation_id").First(&asset, "id = ?", assetID).Error; err != nil {
			return fmt.Errorf("load staging image asset: %w", err)
		}
		if asset.GenerationID == nil {
			return ErrInvalidImageAssetPipeline
		}
		if err := requireSubmittingImageGeneration(tx, *asset.GenerationID); err != nil {
			return err
		}
		updated := tx.Model(&model.KKAIImageAsset{}).
			Where("id = ? AND state = ?", assetID, model.ImageAssetStateStaging).
			Updates(map[string]any{
				"state": model.ImageAssetStateFailed, "thumbnail_state": model.ImageThumbnailStateFailed,
				"failure_reason": reason, "updated_at": time.Now().Unix(),
			})
		if updated.Error != nil {
			return fmt.Errorf("mark image asset failed: %w", updated.Error)
		}
		if updated.RowsAffected != 1 {
			return ErrInvalidImageAssetPipeline
		}
		return nil
	})
}

func requireSubmittingImageGeneration(tx *gorm.DB, generationID int64) error {
	var generation model.KKAIImageGeneration
	if err := lockRowsForUpdate(tx).Select("id", "status").First(
		&generation, "id = ?", generationID,
	).Error; err != nil {
		return err
	}
	if generation.Status != model.ImageGenerationStatusSubmitting {
		return ErrImageGenerationConflict
	}
	return nil
}

func imageArchiveFailureReason(err error) string {
	switch {
	case errors.Is(err, ErrImageArchiveSourceRejected):
		return "source_rejected"
	case errors.Is(err, ErrImageArchiveResponseRejected):
		return "response_rejected"
	case errors.Is(err, ErrImageArchiveMIMERejected):
		return "mime_rejected"
	case errors.Is(err, ErrImageArchiveTooLarge):
		return "size_limit_exceeded"
	case errors.Is(err, ErrImageArchivePixelsExceeded):
		return "pixel_limit_exceeded"
	case errors.Is(err, ErrImageTemporaryStorageUnavailable):
		return "temporary_storage_unavailable"
	default:
		return "archive_failed"
	}
}
