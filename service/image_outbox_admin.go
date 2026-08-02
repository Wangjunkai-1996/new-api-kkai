package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

var (
	ErrInvalidImageOutboxEvent    = errors.New("invalid image outbox event")
	ErrImageOutboxEventNotFound   = errors.New("image outbox event not found")
	ErrImageOutboxRedriveConflict = errors.New("image outbox event cannot be redriven from its current asset state")
)

func RedriveImageOutboxDeadEvent(
	ctx context.Context,
	db *gorm.DB,
	eventID int64,
	redriveKey string,
	actor string,
	now time.Time,
) (*model.KKAIOutboxEvent, bool, error) {
	if db == nil || eventID <= 0 || strings.TrimSpace(redriveKey) == "" || now.IsZero() {
		return nil, false, ErrInvalidImageOutboxEvent
	}
	var event model.KKAIOutboxEvent
	if err := db.WithContext(ctx).Select("id", "topic").First(&event, eventID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, ErrImageOutboxEventNotFound
		}
		return nil, false, err
	}
	if event.Topic != ImageAssetThumbnailTopic && event.Topic != ImageAssetDeleteTopic {
		return nil, false, ErrImageOutboxEventNotFound
	}
	redriven, applied, err := redriveKKAIOutboxDeadEvent(
		ctx, db, eventID, redriveKey, actor, now, prepareImageOutboxRedrive,
	)
	if errors.Is(err, ErrKKAIOutboxInvalidConfiguration) {
		return nil, false, ErrInvalidImageOutboxEvent
	}
	return redriven, applied, err
}

func prepareImageOutboxRedrive(
	ctx context.Context,
	tx *gorm.DB,
	event model.KKAIOutboxEvent,
	now time.Time,
) error {
	var assetID int64
	switch event.Topic {
	case ImageAssetThumbnailTopic:
		parsed, err := imageAssetIDFromOutboxEvent(event, ImageAssetThumbnailTopic)
		if err != nil {
			return ErrInvalidImageOutboxEvent
		}
		assetID = parsed
		updated := tx.WithContext(ctx).Model(&model.KKAIImageAsset{}).
			Where(
				"id = ? AND deleted_at = 0 AND state = ? AND thumbnail_state = ?",
				assetID, model.ImageAssetStateReady, model.ImageThumbnailStateFailed,
			).
			Updates(map[string]any{
				"thumbnail_state": model.ImageThumbnailStatePending,
				"failure_reason":  "", "updated_at": now.Unix(),
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrImageOutboxRedriveConflict
		}
		return nil
	case ImageAssetDeleteTopic:
		var payload imageAssetDeletePayload
		if common.UnmarshalJsonStr(event.Payload, &payload) != nil || payload.AssetID <= 0 {
			return ErrInvalidImageOutboxEvent
		}
		assetID = payload.AssetID
		var count int64
		if err := tx.WithContext(ctx).Model(&model.KKAIImageAsset{}).
			Where("id = ? AND state = ? AND deleted_at > 0", assetID, model.ImageAssetStateDeleted).
			Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return ErrImageOutboxRedriveConflict
		}
		return nil
	default:
		return ErrInvalidImageOutboxEvent
	}
}
