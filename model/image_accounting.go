package model

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	KKAIOutboxTopicImageAccounting = "image.accounting.v1"
	imageAccountingEventKeyPrefix  = "image-accounting:"
)

var ErrImageAccountingNotReady = errors.New("image accounting is not ready")

type ImageGenerationAccountingPayload struct {
	GenerationID      int64                  `json:"generation_id"`
	TargetQuota       int                    `json:"target_quota"`
	CountStatistics   bool                   `json:"count_statistics"`
	Username          string                 `json:"username"`
	UpstreamRequestID string                 `json:"upstream_request_id,omitempty"`
	ClientIP          string                 `json:"client_ip,omitempty"`
	NodeName          string                 `json:"node_name,omitempty"`
	LogParams         RecordConsumeLogParams `json:"log_params"`
}

func PrepareImageGenerationAccounting(
	ctx context.Context,
	db *gorm.DB,
	generationID int64,
	payload ImageGenerationAccountingPayload,
) error {
	if db == nil || generationID <= 0 || payload.TargetQuota < 0 ||
		payload.LogParams.Quota != payload.TargetQuota || payload.LogParams.ChannelId <= 0 {
		return ErrImageBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload.GenerationID = generationID
	encoded, err := common.Marshal(payload)
	if err != nil {
		return err
	}
	eventKey := imageAccountingEventKey(generationID)
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		generation, err := lockImageGenerationBillingRow(tx, generationID)
		if err != nil {
			return err
		}
		if generation.Status != ImageGenerationStatusSubmitting ||
			(generation.BillingState != ImageGenerationBillingStateReserved &&
				generation.BillingState != ImageGenerationBillingStateProcessing) ||
			payload.TargetQuota > generation.ReservedQuota ||
			payload.LogParams.TokenId != generation.TokenID ||
			payload.LogParams.ModelName != generation.Model {
			return ErrImageBillingStateConflict
		}
		now := time.Now().Unix()
		event := KKAIOutboxEvent{
			EventKey: eventKey, Topic: KKAIOutboxTopicImageAccounting,
			AggregateID: strconv.FormatInt(generationID, 10), Payload: string(encoded),
			Status: KKAIOutboxStatusPending, AvailableAt: now, CreatedAt: now,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&event).Error; err != nil {
			return err
		}
		var stored KKAIOutboxEvent
		if err := tx.Where("event_key = ? AND topic = ?", eventKey, KKAIOutboxTopicImageAccounting).
			First(&stored).Error; err != nil {
			return err
		}
		if stored.Payload != string(encoded) || stored.AggregateID != event.AggregateID {
			return ErrImageBillingStateConflict
		}
		return nil
	})
}

func GetImageGenerationAccounting(
	ctx context.Context, db *gorm.DB, generationID int64,
) (*ImageGenerationAccountingPayload, error) {
	if db == nil || generationID <= 0 {
		return nil, ErrImageBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var event KKAIOutboxEvent
	if err := db.WithContext(ctx).Where(
		"event_key = ? AND topic = ?", imageAccountingEventKey(generationID), KKAIOutboxTopicImageAccounting,
	).First(&event).Error; err != nil {
		return nil, err
	}
	payload, err := decodeImageGenerationAccounting(event)
	if err != nil {
		return nil, err
	}
	return &payload, nil
}

func RecordImageGenerationAccountingLog(
	ctx context.Context, db *gorm.DB, payload ImageGenerationAccountingPayload,
) (bool, error) {
	if db == nil || payload.GenerationID <= 0 || payload.TargetQuota < 0 ||
		payload.LogParams.Quota != payload.TargetQuota {
		return false, ErrImageBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var generation KKAIImageGeneration
	if err := db.WithContext(ctx).First(&generation, payload.GenerationID).Error; err != nil {
		return false, err
	}
	if generation.Status == ImageGenerationStatusSubmitting ||
		generation.Status == ImageGenerationStatusRecovering {
		return false, ErrImageAccountingNotReady
	}
	if generation.BillingState == ImageGenerationBillingStateRefunded {
		return false, nil
	}
	if generation.BillingState != ImageGenerationBillingStateSettled ||
		generation.FinalQuota != payload.TargetQuota ||
		(generation.Status != ImageGenerationStatusSucceeded && generation.Status != ImageGenerationStatusPartial) {
		return false, ErrImageBillingStateConflict
	}

	logDB := LOG_DB
	if logDB == nil {
		return false, errors.New("log database is not configured")
	}
	requestID := generation.RequestID
	var existing int64
	if err := logDB.WithContext(ctx).Model(&Log{}).Where(
		"request_id = ? AND type = ?", requestID, LogTypeConsume,
	).Count(&existing).Error; err != nil {
		return false, err
	}
	if existing > 0 || !common.LogConsumeEnabled {
		return false, nil
	}
	createdAt := common.GetTimestamp()
	params := payload.LogParams
	log := &Log{
		UserId: generation.UserID, Username: payload.Username, CreatedAt: createdAt,
		Type: LogTypeConsume, Content: params.Content, PromptTokens: params.PromptTokens,
		CompletionTokens: params.CompletionTokens, TokenName: params.TokenName,
		ModelName: params.ModelName, Quota: params.Quota, ChannelId: params.ChannelId,
		TokenId: params.TokenId, UseTime: params.UseTimeSeconds, IsStream: params.IsStream,
		Group: params.Group, Ip: payload.ClientIP, RequestId: requestID,
		UpstreamRequestId: payload.UpstreamRequestID, Other: common.MapToJsonStr(params.Other),
	}
	if err := logDB.WithContext(ctx).Create(log).Error; err != nil {
		return false, err
	}
	if common.DataExportEnabled {
		nodeName := strings.TrimSpace(payload.NodeName)
		if nodeName == "" {
			nodeName = common.NodeName
		}
		LogQuotaData(QuotaDataLogParams{
			UserID: generation.UserID, Username: payload.Username, ModelName: params.ModelName,
			Quota: params.Quota, CreatedAt: createdAt,
			TokenUsed: params.PromptTokens + params.CompletionTokens,
			UseGroup:  params.Group, TokenID: params.TokenId, ChannelID: params.ChannelId,
			NodeName: nodeName,
		})
	}
	return true, nil
}

func imageGenerationAccountingInTransaction(
	tx *gorm.DB, generationID int64,
) (ImageGenerationAccountingPayload, error) {
	var event KKAIOutboxEvent
	if err := tx.Where(
		"event_key = ? AND topic = ?", imageAccountingEventKey(generationID), KKAIOutboxTopicImageAccounting,
	).First(&event).Error; err != nil {
		return ImageGenerationAccountingPayload{}, err
	}
	return decodeImageGenerationAccounting(event)
}

func decodeImageGenerationAccounting(event KKAIOutboxEvent) (ImageGenerationAccountingPayload, error) {
	payload := ImageGenerationAccountingPayload{}
	if event.Topic != KKAIOutboxTopicImageAccounting ||
		common.UnmarshalJsonStr(event.Payload, &payload) != nil || payload.GenerationID <= 0 ||
		payload.TargetQuota < 0 || payload.LogParams.Quota != payload.TargetQuota ||
		event.AggregateID != strconv.FormatInt(payload.GenerationID, 10) {
		return ImageGenerationAccountingPayload{}, fmt.Errorf("%w: invalid image accounting payload", ErrImageBillingInvalidRequest)
	}
	return payload, nil
}

func applyImageGenerationAccountingStatistics(
	tx *gorm.DB, generation *KKAIImageGeneration, payload ImageGenerationAccountingPayload,
) error {
	if !payload.CountStatistics {
		return nil
	}
	updatedUser := tx.Model(&User{}).Where("id = ?", generation.UserID).Updates(map[string]any{
		"used_quota":    gorm.Expr("used_quota + ?", payload.TargetQuota),
		"request_count": gorm.Expr("request_count + 1"),
	})
	if updatedUser.Error != nil {
		return updatedUser.Error
	}
	if updatedUser.RowsAffected != 1 {
		return ErrImageBillingStateConflict
	}
	if payload.TargetQuota == 0 {
		return nil
	}
	updatedChannel := tx.Model(&Channel{}).Where("id = ?", payload.LogParams.ChannelId).
		Update("used_quota", gorm.Expr("used_quota + ?", payload.TargetQuota))
	if updatedChannel.Error != nil {
		return updatedChannel.Error
	}
	if updatedChannel.RowsAffected != 1 {
		return ErrImageBillingStateConflict
	}
	return nil
}

func imageAccountingEventKey(generationID int64) string {
	return imageAccountingEventKeyPrefix + strconv.FormatInt(generationID, 10)
}
