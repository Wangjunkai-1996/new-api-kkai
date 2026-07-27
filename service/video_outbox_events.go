package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	VideoOutboxTopicInspect       = "video.asset.inspect.v1"
	VideoOutboxTopicArchive       = "video.asset.archive.v1"
	VideoOutboxTopicPoster        = "video.asset.poster.v1"
	VideoOutboxTopicDelete        = "video.asset.delete.v1"
	VideoOutboxTopicSamplePrepare = "video.sample.prepare.v1"
)

var (
	ErrInvalidVideoOutboxEvent    = errors.New("invalid video outbox event")
	ErrVideoOutboxEventNotFound   = errors.New("video outbox event not found")
	ErrVideoOutboxRedriveConflict = errors.New("video outbox event cannot be redriven from its current aggregate state")
)

type VideoAssetEventPayload struct {
	AssetID int64 `json:"asset_id"`
}

type VideoSamplePrepareEventPayload struct {
	SampleID int64 `json:"sample_id"`
}

func EnqueueVideoOutboxEvent(ctx context.Context, db *gorm.DB, eventKey string, topic string, aggregateID string, payload any) error {
	eventKey = strings.TrimSpace(eventKey)
	topic = strings.TrimSpace(topic)
	aggregateID = strings.TrimSpace(aggregateID)
	if db == nil || eventKey == "" || len(eventKey) > 191 || topic == "" || len(topic) > 128 || len(aggregateID) > 128 {
		return ErrInvalidVideoOutboxEvent
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode video outbox payload: %w", err)
	}
	now := time.Now().Unix()
	event := model.KKAIOutboxEvent{
		EventKey: eventKey, Topic: topic, AggregateID: aggregateID, Payload: string(encoded),
		Status: model.KKAIOutboxStatusPending, AvailableAt: now, CreatedAt: now,
	}
	result := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&event)
	if result.Error != nil {
		return fmt.Errorf("enqueue video outbox event: %w", result.Error)
	}
	return nil
}

func RedriveVideoOutboxDeadEvent(
	ctx context.Context,
	db *gorm.DB,
	eventID int64,
	redriveKey string,
	actor string,
	now time.Time,
) (*model.KKAIOutboxEvent, bool, error) {
	if db == nil || eventID <= 0 {
		return nil, false, ErrInvalidVideoOutboxEvent
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var event model.KKAIOutboxEvent
	if err := db.WithContext(ctx).Select("id, topic").First(&event, eventID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, ErrVideoOutboxEventNotFound
		}
		return nil, false, err
	}
	if !isVideoOutboxTopic(event.Topic) {
		return nil, false, ErrVideoOutboxEventNotFound
	}
	redriven, applied, err := redriveKKAIOutboxDeadEvent(
		ctx, db, eventID, redriveKey, actor, now, prepareVideoOutboxRedrive,
	)
	if errors.Is(err, ErrKKAIOutboxInvalidConfiguration) {
		return nil, false, ErrInvalidVideoOutboxEvent
	}
	return redriven, applied, err
}

func isVideoOutboxTopic(topic string) bool {
	switch topic {
	case VideoOutboxTopicInspect, VideoOutboxTopicArchive, VideoOutboxTopicPoster,
		VideoOutboxTopicDelete, VideoOutboxTopicSamplePrepare:
		return true
	default:
		return false
	}
}

func videoOutboxAssetRetryState(
	ctx context.Context,
	db *gorm.DB,
	event model.KKAIOutboxEvent,
) (int64, string, error) {
	if db == nil {
		return 0, "", ErrInvalidVideoOutboxEvent
	}
	switch event.Topic {
	case VideoOutboxTopicArchive, VideoOutboxTopicInspect, VideoOutboxTopicPoster:
		var payload VideoAssetEventPayload
		if common.UnmarshalJsonStr(event.Payload, &payload) != nil || payload.AssetID <= 0 {
			return 0, "", ErrInvalidVideoOutboxEvent
		}
		return payload.AssetID, model.VideoAssetStateProcessing, nil
	case VideoOutboxTopicDelete:
		var payload VideoAssetEventPayload
		if common.UnmarshalJsonStr(event.Payload, &payload) != nil || payload.AssetID <= 0 {
			return 0, "", ErrInvalidVideoOutboxEvent
		}
		return payload.AssetID, model.VideoAssetStateDeleting, nil
	case VideoOutboxTopicSamplePrepare:
		var payload VideoSamplePrepareEventPayload
		if common.UnmarshalJsonStr(event.Payload, &payload) != nil || payload.SampleID <= 0 {
			return 0, "", ErrInvalidVideoOutboxEvent
		}
		var sample model.KKAIVideoSample
		if err := db.WithContext(ctx).Select("video_asset_id").First(&sample, payload.SampleID).Error; err != nil {
			return 0, "", err
		}
		return sample.VideoAssetID, model.VideoAssetStateReady, nil
	default:
		return 0, "", ErrInvalidVideoOutboxEvent
	}
}

func prepareVideoOutboxRedrive(
	ctx context.Context,
	tx *gorm.DB,
	event model.KKAIOutboxEvent,
	now time.Time,
) error {
	assetID, retryState, err := videoOutboxAssetRetryState(ctx, tx, event)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrVideoOutboxEventNotFound
		}
		return err
	}
	var asset model.KKAIVideoAsset
	if err := lockVideoRowsForUpdate(tx.WithContext(ctx)).Select("id, state").First(&asset, assetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrVideoOutboxEventNotFound
		}
		return err
	}
	if asset.State != model.VideoAssetStateFailed {
		return ErrVideoOutboxRedriveConflict
	}
	update := tx.WithContext(ctx).Model(&model.KKAIVideoAsset{}).
		Where("id = ? AND state = ?", assetID, model.VideoAssetStateFailed).
		Updates(map[string]any{
			"state": retryState, "failure_reason": "", "updated_at": now.Unix(),
		})
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected == 0 {
		return ErrVideoOutboxRedriveConflict
	}
	return nil
}
