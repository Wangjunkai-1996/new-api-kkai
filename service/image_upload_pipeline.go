package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"
	"gorm.io/gorm"
)

const (
	imageUploadValidationTimeout = 2 * time.Minute
	imageUploadCleanupMargin     = 5 * time.Minute
)

type ImageUploadPipeline struct {
	db    *gorm.DB
	store ImageUploadStore
}

func NewImageUploadPipeline(db *gorm.DB, store ImageAssetStore) (*ImageUploadPipeline, error) {
	multipartStore, ok := store.(ImageUploadStore)
	if db == nil || !ok {
		return nil, ErrImageUploadUnavailable
	}
	return &ImageUploadPipeline{db: db, store: multipartStore}, nil
}

// Register this on its own processor; ProcessBatch executes validation serially
// and the admission limit prevents an unbounded validation backlog.
func (pipeline *ImageUploadPipeline) Register(processor *KKAIOutboxProcessor) error {
	if pipeline == nil || processor == nil {
		return ErrImageUploadUnavailable
	}
	return processor.Register(ImageUploadValidateTopic, pipeline.validate)
}

func (pipeline *ImageUploadPipeline) validate(ctx context.Context, event model.KKAIOutboxEvent) (processErr error) {
	var payload imageUploadPayload
	if err := common.UnmarshalJsonStr(event.Payload, &payload); err != nil || payload.AssetID <= 0 {
		return PermanentKKAIOutboxError(ErrInvalidImageUpload)
	}
	fence, err := ImageWorkerFence(ctx, event)
	if err != nil {
		return err
	}
	var record imageUploadRecord
	err = pipeline.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := model.LockImageStudioCapacityTx(tx); err != nil {
			return err
		}
		if err := model.RequireImageWorkerFenceTx(tx, fence); err != nil {
			return err
		}
		if err := tx.First(&record.upload, "asset_id = ?", payload.AssetID).Error; err != nil {
			return err
		}
		var err error
		record, err = loadImageUpload(ctx, tx, record.upload.OwnerUserID, payload.AssetID)
		return err
	})
	if err != nil {
		return err
	}
	if record.asset.State != model.ImageAssetStateValidating {
		return nil
	}
	defer func() {
		// Storage operations are safe to retry. Bound retries and publish failure
		// while this event is still fenced, before the generic outbox marks dead.
		if processErr != nil && event.Attempts >= 3 && ctx.Err() == nil &&
			!errors.Is(processErr, model.ErrImageExecutionFenceLost) && !errors.Is(processErr, ErrKKAIOutboxLockLost) {
			processErr = pipeline.finishValidation(ctx, fence, record, nil, "image_validation_unavailable")
		}
	}()
	if record.upload.UploadExpiresAt <= time.Now().Unix() {
		return pipeline.finishValidation(ctx, fence, record, nil, "upload_expired")
	}
	var manifest []VideoAssetCompletedPart
	if err := common.UnmarshalJsonStr(record.upload.UploadManifest, &manifest); err != nil {
		return pipeline.finishValidation(ctx, fence, record, nil, "invalid_upload_manifest")
	}
	settings := image_studio_setting.Get()
	if record.asset.SizeBytes <= 0 || record.asset.SizeBytes > settings.MaxReferenceBytes {
		return pipeline.finishValidation(ctx, fence, record, nil, "image_size_limit")
	}
	readCtx, cancel := context.WithTimeout(ctx, imageUploadValidationTimeout)
	defer cancel()
	metadata, headErr := pipeline.store.Head(readCtx, record.asset.ObjectKey)
	if errors.Is(headErr, ErrVideoAssetObjectNotFound) {
		parts, err := pipeline.store.ListUploadedParts(readCtx, record.asset.ObjectKey, record.upload.UploadID)
		if err != nil {
			return err
		}
		if err := validateUploadedVideoParts(model.KKAIVideoAsset{SizeBytes: record.asset.SizeBytes, UploadPartSize: record.upload.UploadPartSize}, parts, true); err != nil || len(parts) != len(manifest) {
			return pipeline.finishValidation(ctx, fence, record, nil, "invalid_upload_parts")
		}
		for i, part := range parts {
			if part.PartNumber != manifest[i].PartNumber || strings.Trim(strings.TrimSpace(part.ETag), "\"") != strings.Trim(manifest[i].ETag, "\"") {
				return pipeline.finishValidation(ctx, fence, record, nil, "upload_parts_changed")
			}
		}
		completeErr := pipeline.store.CompleteMultipartUpload(readCtx, record.asset.ObjectKey, record.upload.UploadID, manifest)
		metadata, headErr = pipeline.store.Head(readCtx, record.asset.ObjectKey)
		if headErr != nil && completeErr != nil {
			return completeErr
		}
	}
	if headErr != nil {
		return headErr
	}
	if metadata.ContentLength != record.asset.SizeBytes || normalizedVideoObjectContentType(metadata.ContentType) != record.asset.MIMEType {
		return pipeline.finishValidation(ctx, fence, record, nil, "image_content_mismatch")
	}
	object, err := pipeline.store.Get(readCtx, record.asset.ObjectKey)
	if err != nil {
		return err
	}
	if object.Body == nil {
		return ErrImageUploadUnavailable
	}
	defer object.Body.Close()
	if object.ContentLength != record.asset.SizeBytes || normalizedVideoObjectContentType(object.ContentType) != record.asset.MIMEType ||
		(metadata.ETag != "" && object.ETag != "" && metadata.ETag != object.ETag) {
		return pipeline.finishValidation(ctx, fence, record, nil, "image_content_mismatch")
	}
	archive, err := NewHTTPImageArchiveFetcher(ImageStudioTempDirectory()).Ingest(object.Body, record.asset.MIMEType, record.asset.SizeBytes, settings.MaxPixels)
	if err != nil {
		if readCtx.Err() != nil {
			return readCtx.Err()
		}
		if errors.Is(err, ErrImageTemporaryStorageUnavailable) {
			return err
		}
		return pipeline.finishValidation(ctx, fence, record, nil, "image_validation_failed")
	}
	defer archive.Remove()
	if archive.SizeBytes != record.asset.SizeBytes || archive.SHA256 != record.asset.SHA256 {
		return pipeline.finishValidation(ctx, fence, record, nil, "image_content_mismatch")
	}
	return pipeline.finishValidation(ctx, fence, record, archive, "")
}

func (pipeline *ImageUploadPipeline) finishValidation(ctx context.Context, fence model.ImageExecutionFence, record imageUploadRecord, archive *FetchedImageArchive, reason string) error {
	return pipeline.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := model.LockImageStudioCapacityTx(tx); err != nil {
			return err
		}
		if err := model.RequireImageWorkerFenceTx(tx, fence); err != nil {
			return err
		}
		now := time.Now()
		updates := map[string]any{"state": model.ImageAssetStateFailed, "failure_reason": reason, "updated_at": now.Unix()}
		cleanupAt := now.Unix()
		if archive != nil {
			updates["state"], updates["failure_reason"] = model.ImageAssetStateReady, ""
			updates["width"], updates["height"] = archive.Width, archive.Height
			cleanupAt = now.Add(imageUploadRetention).Unix()
		}
		result := tx.Model(&model.KKAIImageAsset{}).Where("id = ? AND state = ?", record.asset.ID, model.ImageAssetStateValidating).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		// Abort/GC wins permanently; a late validator never resurrects it.
		if result.RowsAffected == 0 {
			return nil
		}
		return tx.Model(&model.KKAIImageReferenceUpload{}).Where("asset_id = ?", record.asset.ID).Updates(map[string]any{"cleanup_at": cleanupAt, "updated_at": now.Unix()}).Error
	})
}

func ReconcileImageReferenceUploads(ctx context.Context, db *gorm.DB, assetStore ImageAssetStore, limit int) (int, error) {
	store, ok := assetStore.(ImageUploadStore)
	if db == nil || !ok || limit <= 0 || limit > 500 {
		return 0, ErrInvalidImageUpload
	}
	fence, err := ImageWorkerFence(ctx, model.KKAIOutboxEvent{})
	if err != nil {
		return 0, err
	}
	var uploads []model.KKAIImageReferenceUpload
	if err := db.WithContext(ctx).Where("cleanup_at > 0 AND cleanup_at <= ?", time.Now().Unix()).Order("cleanup_at ASC, asset_id ASC").Limit(limit).Find(&uploads).Error; err != nil {
		return 0, err
	}
	cleaned := 0
	for _, upload := range uploads {
		var record imageUploadRecord
		remove := false
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := model.LockImageStudioCapacityTx(tx); err != nil {
				return err
			}
			if err := model.RequireImageWorkerFenceTx(tx, fence); err != nil {
				return err
			}
			var err error
			record, err = loadImageUpload(ctx, tx, upload.OwnerUserID, upload.AssetID)
			if err != nil {
				return err
			}
			if record.upload.CleanupAt <= 0 || record.upload.CleanupAt > time.Now().Unix() {
				return nil
			}
			var refs int64
			if err := tx.Model(&model.KKAIImageGenerationReference{}).Where("asset_id = ?", upload.AssetID).Count(&refs).Error; err != nil {
				return err
			}
			if refs != 0 {
				return tx.Model(&model.KKAIImageReferenceUpload{}).Where("asset_id = ?", upload.AssetID).Update("cleanup_at", time.Now().Add(time.Hour).Unix()).Error
			}
			if record.asset.State != model.ImageAssetStateDeleted {
				reason := record.asset.FailureReason
				if reason == "" {
					reason = "upload_expired"
				}
				if err := tx.Model(&model.KKAIImageAsset{}).Where("id = ?", upload.AssetID).Updates(map[string]any{"state": model.ImageAssetStateDeleting, "failure_reason": reason, "updated_at": time.Now().Unix()}).Error; err != nil {
					return err
				}
			}
			remove = true
			return nil
		})
		if err != nil {
			return cleaned, err
		}
		if !remove {
			continue
		}
		cleanupCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err = store.AbortImageUploadSessions(cleanupCtx, record.asset.ObjectKey)
		if err == nil {
			err = store.Delete(cleanupCtx, []string{record.asset.ObjectKey})
		}
		cancel()
		if err != nil {
			return cleaned, fmt.Errorf("clean image reference upload %d: %w", upload.AssetID, err)
		}
		err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := model.LockImageStudioCapacityTx(tx); err != nil {
				return err
			}
			if err := model.RequireImageWorkerFenceTx(tx, fence); err != nil {
				return err
			}
			now := time.Now()
			result := tx.Model(&model.KKAIImageAsset{}).Where("id = ? AND state IN ?", upload.AssetID, []string{model.ImageAssetStateDeleting, model.ImageAssetStateDeleted}).Updates(map[string]any{"state": model.ImageAssetStateDeleted, "deleted_at": now.Unix(), "updated_at": now.Unix()})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrImageUploadConflict
			}
			// Presigns end by UploadExpiresAt. Give an already admitted Complete
			// its bounded storage timeout plus a completion/clock-skew margin,
			// then sweep once more for late objects. Finished tombstones must not
			// compete with new uploads for every future GC batch.
			cleanupAt := time.Unix(record.upload.UploadExpiresAt, 0).Add(imageUploadValidationTimeout + imageUploadCleanupMargin).Unix()
			if cleanupAt <= now.Unix() {
				cleanupAt = 0
			}
			return tx.Model(&model.KKAIImageReferenceUpload{}).Where("asset_id = ?", upload.AssetID).Updates(map[string]any{"cleanup_at": cleanupAt, "updated_at": now.Unix()}).Error
		})
		if err != nil {
			return cleaned, err
		}
		cleaned++
	}
	return cleaned, nil
}
