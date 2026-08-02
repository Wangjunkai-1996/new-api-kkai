package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"

	"gorm.io/gorm"
)

const ImageAssetDeleteTopic = "image.asset.delete.v1"

type imageAssetDeletePayload struct {
	AssetID            int64  `json:"asset_id"`
	ObjectKey          string `json:"object_key"`
	ThumbnailObjectKey string `json:"thumbnail_object_key,omitempty"`
}

type imageThumbnailProcessor interface {
	CreateImageThumbnail(context.Context, string, string, int64) error
}

type ImageAssetOutboxPipeline struct {
	db      *gorm.DB
	store   ImageAssetStore
	media   imageThumbnailProcessor
	tempDir string
}

func NewImageAssetOutboxPipeline(
	db *gorm.DB,
	store ImageAssetStore,
	media imageThumbnailProcessor,
	tempDir string,
) (*ImageAssetOutboxPipeline, error) {
	tempDir = strings.TrimSpace(tempDir)
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	info, err := os.Stat(tempDir)
	if db == nil || store == nil || media == nil || err != nil || !info.IsDir() {
		return nil, ErrInvalidImageAssetPipeline
	}
	return &ImageAssetOutboxPipeline{db: db, store: store, media: media, tempDir: tempDir}, nil
}

func (pipeline *ImageAssetOutboxPipeline) Register(processor *KKAIOutboxProcessor) error {
	if pipeline == nil || processor == nil {
		return ErrInvalidImageAssetPipeline
	}
	if err := processor.Register(ImageAssetThumbnailTopic, pipeline.HandleThumbnail); err != nil {
		return err
	}
	if err := processor.Register(ImageAssetDeleteTopic, pipeline.HandleDelete); err != nil {
		return err
	}
	if err := processor.registerDeadLetter(ImageAssetThumbnailTopic, pipeline.handleDeadLetter); err != nil {
		return err
	}
	return processor.registerDeadLetter(ImageAssetDeleteTopic, pipeline.handleDeadLetter)
}

func (pipeline *ImageAssetOutboxPipeline) HandleThumbnail(ctx context.Context, event model.KKAIOutboxEvent) error {
	assetID, err := imageAssetIDFromOutboxEvent(event, ImageAssetThumbnailTopic)
	if err != nil {
		return PermanentKKAIOutboxError(err)
	}
	var asset model.KKAIImageAsset
	if err := pipeline.db.WithContext(ctx).First(&asset, "id = ?", assetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if asset.DeletedAt != 0 || asset.State == model.ImageAssetStateDeleted {
		if strings.TrimSpace(asset.ObjectKey) == "" {
			return nil
		}
		return pipeline.store.Delete(ctx, []string{imageThumbnailObjectKey(asset.ObjectKey)})
	}
	if asset.State != model.ImageAssetStateReady {
		return PermanentKKAIOutboxError(ErrInvalidImageAssetPipeline)
	}
	if asset.ThumbnailState == model.ImageThumbnailStateReady && asset.ThumbnailObjectKey != "" {
		return nil
	}
	settings := image_studio_setting.Get()
	object, err := pipeline.store.Get(ctx, asset.ObjectKey)
	if err != nil {
		return err
	}
	defer object.Body.Close()
	if object.ContentLength <= 0 || object.ContentLength > settings.MaxOutputBytes || object.ContentLength != asset.SizeBytes {
		return PermanentKKAIOutboxError(ErrInvalidImageAssetPipeline)
	}
	input, err := os.CreateTemp(pipeline.tempDir, "new-api-image-thumbnail-input-*")
	if err != nil {
		return ErrImageTemporaryStorageUnavailable
	}
	inputPath := input.Name()
	defer os.Remove(inputPath)
	written, copyErr := io.Copy(input, io.LimitReader(object.Body, settings.MaxOutputBytes+1))
	closeErr := input.Close()
	if copyErr != nil || closeErr != nil {
		return fmt.Errorf("copy image asset for thumbnail: %w", errors.Join(copyErr, closeErr))
	}
	if written != asset.SizeBytes {
		return PermanentKKAIOutboxError(ErrInvalidImageAssetPipeline)
	}
	output, err := os.CreateTemp(pipeline.tempDir, "new-api-image-thumbnail-*.jpg")
	if err != nil {
		return ErrImageTemporaryStorageUnavailable
	}
	outputPath := output.Name()
	_ = output.Close()
	defer os.Remove(outputPath)
	if err := pipeline.media.CreateImageThumbnail(ctx, inputPath, outputPath, imageThumbnailMaximumBytes); err != nil {
		if errors.Is(err, errImageThumbnailRejected) {
			return PermanentKKAIOutboxError(err)
		}
		return err
	}
	thumbnail, err := os.Open(outputPath)
	if err != nil {
		return err
	}
	thumbnailInfo, err := thumbnail.Stat()
	if err != nil {
		_ = thumbnail.Close()
		return err
	}
	if thumbnailInfo.Size() <= 0 || thumbnailInfo.Size() > imageThumbnailMaximumBytes {
		_ = thumbnail.Close()
		return PermanentKKAIOutboxError(ErrInvalidImageAssetPipeline)
	}
	thumbnailKey := imageThumbnailObjectKey(asset.ObjectKey)
	putErr := pipeline.store.Put(ctx, thumbnailKey, "image/jpeg", thumbnail, thumbnailInfo.Size())
	closeErr = thumbnail.Close()
	if putErr != nil || closeErr != nil {
		return errors.Join(putErr, closeErr)
	}
	updated := pipeline.db.WithContext(ctx).Model(&model.KKAIImageAsset{}).
		Where("id = ? AND deleted_at = 0 AND state = ? AND thumbnail_state = ?", asset.ID, model.ImageAssetStateReady, model.ImageThumbnailStatePending).
		Updates(map[string]any{
			"thumbnail_object_key": thumbnailKey, "thumbnail_state": model.ImageThumbnailStateReady,
			"updated_at": time.Now().Unix(),
		})
	if updated.Error != nil {
		deleteErr := pipeline.store.Delete(ctx, []string{thumbnailKey})
		return errors.Join(updated.Error, deleteErr)
	}
	if updated.RowsAffected != 1 {
		if err := pipeline.store.Delete(ctx, []string{thumbnailKey}); err != nil {
			return err
		}
	}
	return nil
}

func (pipeline *ImageAssetOutboxPipeline) HandleDelete(ctx context.Context, event model.KKAIOutboxEvent) error {
	if event.Topic != ImageAssetDeleteTopic {
		return PermanentKKAIOutboxError(ErrInvalidImageAssetPipeline)
	}
	var payload imageAssetDeletePayload
	if common.UnmarshalJsonStr(event.Payload, &payload) != nil || payload.AssetID <= 0 || strings.TrimSpace(payload.ObjectKey) == "" {
		return PermanentKKAIOutboxError(ErrInvalidImageAssetPipeline)
	}
	keys := []string{payload.ObjectKey, imageThumbnailObjectKey(payload.ObjectKey)}
	if strings.TrimSpace(payload.ThumbnailObjectKey) != "" {
		thumbnailKey := strings.TrimSpace(payload.ThumbnailObjectKey)
		if thumbnailKey != keys[1] {
			keys = append(keys, thumbnailKey)
		}
	}
	return pipeline.store.Delete(ctx, keys)
}

func imageThumbnailObjectKey(objectKey string) string {
	return strings.TrimSpace(objectKey) + ".thumbnail.jpg"
}

func (pipeline *ImageAssetOutboxPipeline) handleDeadLetter(
	ctx context.Context,
	tx *gorm.DB,
	event model.KKAIOutboxEvent,
	_ error,
	now time.Time,
) error {
	if event.Topic != ImageAssetThumbnailTopic {
		return nil
	}
	assetID, err := imageAssetIDFromOutboxEvent(event, ImageAssetThumbnailTopic)
	if err != nil {
		return nil
	}
	return tx.WithContext(ctx).Model(&model.KKAIImageAsset{}).
		Where("id = ? AND thumbnail_state = ?", assetID, model.ImageThumbnailStatePending).
		Updates(map[string]any{
			"thumbnail_state": model.ImageThumbnailStateFailed,
			"failure_reason":  "thumbnail_generation_failed", "updated_at": now.Unix(),
		}).Error
}

func imageAssetIDFromOutboxEvent(event model.KKAIOutboxEvent, expectedTopic string) (int64, error) {
	if event.Topic != expectedTopic {
		return 0, ErrInvalidImageAssetPipeline
	}
	var payload imageThumbnailPayload
	if common.UnmarshalJsonStr(event.Payload, &payload) != nil || payload.AssetID <= 0 {
		return 0, ErrInvalidImageAssetPipeline
	}
	if event.AggregateID != "" && event.AggregateID != strconv.FormatInt(payload.AssetID, 10) {
		return 0, ErrInvalidImageAssetPipeline
	}
	return payload.AssetID, nil
}
