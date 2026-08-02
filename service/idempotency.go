package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalidIdempotencyRequest  = errors.New("invalid idempotency request")
	ErrIdempotencyConflict        = errors.New("idempotency key was already used for a different request")
	ErrIdempotencyInProgress      = errors.New("idempotent operation is still in progress")
	ErrIdempotencyBindingConflict = errors.New("idempotency key is already bound to another resource")
)

const DefaultIdempotencyTTL = 24 * time.Hour

type IdempotencyReservationRequest struct {
	UserID      int
	Operation   string
	Key         string
	RequestHash string
	TTL         time.Duration
}

type IdempotencyReservation struct {
	Record  model.KKAIIdempotencyKey
	Created bool
}

func ReserveIdempotencyKey(ctx context.Context, db *gorm.DB, request IdempotencyReservationRequest) (*IdempotencyReservation, error) {
	request.Operation = strings.TrimSpace(request.Operation)
	request.Key = strings.TrimSpace(request.Key)
	request.RequestHash = strings.ToLower(strings.TrimSpace(request.RequestHash))
	if request.TTL == 0 {
		request.TTL = DefaultIdempotencyTTL
	}
	if db == nil || request.UserID <= 0 || request.Operation == "" || len(request.Operation) > 64 ||
		request.Key == "" || len(request.Key) > 128 || len(request.RequestHash) != 64 || request.TTL <= 0 {
		return nil, ErrInvalidIdempotencyRequest
	}
	now := time.Now().Unix()
	database := db.WithContext(ctx)
	if err := database.Where(map[string]any{
		"user_id": request.UserID, "operation": request.Operation, "key": request.Key,
	}).Where("expires_at <= ?", now).Delete(&model.KKAIIdempotencyKey{}).Error; err != nil {
		return nil, fmt.Errorf("reclaim expired idempotency key: %w", err)
	}

	candidate := model.KKAIIdempotencyKey{
		UserID:      request.UserID,
		Operation:   request.Operation,
		Key:         request.Key,
		RequestHash: request.RequestHash,
		CreatedAt:   now,
		ExpiresAt:   time.Unix(now, 0).Add(request.TTL).Unix(),
	}
	created := database.Clauses(clause.OnConflict{DoNothing: true}).Create(&candidate)
	if created.Error != nil {
		return nil, fmt.Errorf("reserve idempotency key: %w", created.Error)
	}

	var record model.KKAIIdempotencyKey
	if err := database.Where(map[string]any{
		"user_id": request.UserID, "operation": request.Operation, "key": request.Key,
	}).First(&record).Error; err != nil {
		return nil, fmt.Errorf("load idempotency reservation: %w", err)
	}
	if record.RequestHash != request.RequestHash {
		return nil, ErrIdempotencyConflict
	}
	wasCreated := created.RowsAffected == 1
	if !wasCreated && record.ResourceID == "" {
		return nil, ErrIdempotencyInProgress
	}
	return &IdempotencyReservation{Record: record, Created: wasCreated}, nil
}

func BindIdempotencyResource(ctx context.Context, db *gorm.DB, reservationID int64, resourceType string, resourceID string) error {
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if db == nil || reservationID <= 0 || resourceType == "" || len(resourceType) > 32 || resourceID == "" || len(resourceID) > 128 {
		return ErrInvalidIdempotencyRequest
	}
	update := db.WithContext(ctx).Model(&model.KKAIIdempotencyKey{}).
		Where("id = ? AND (resource_id = '' OR (resource_type = ? AND resource_id = ?))", reservationID, resourceType, resourceID).
		Updates(map[string]any{"resource_type": resourceType, "resource_id": resourceID})
	if update.Error != nil {
		return fmt.Errorf("bind idempotency resource: %w", update.Error)
	}
	if update.RowsAffected == 1 {
		return nil
	}
	var record model.KKAIIdempotencyKey
	if err := db.WithContext(ctx).First(&record, "id = ?", reservationID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidIdempotencyRequest
		}
		return fmt.Errorf("load idempotency binding: %w", err)
	}
	if record.ResourceType == resourceType && record.ResourceID == resourceID {
		return nil
	}
	return ErrIdempotencyBindingConflict
}

func ReleaseUnboundIdempotencyReservation(ctx context.Context, db *gorm.DB, reservationID int64) error {
	if db == nil || reservationID <= 0 {
		return ErrInvalidIdempotencyRequest
	}
	result := db.WithContext(ctx).Where("id = ? AND resource_id = ''", reservationID).Delete(&model.KKAIIdempotencyKey{})
	if result.Error != nil {
		return fmt.Errorf("release idempotency reservation: %w", result.Error)
	}
	return nil
}

func CleanupExpiredIdempotencyKeys(ctx context.Context, db *gorm.DB, expiredBefore time.Time, limit int) (int, error) {
	return cleanupExpiredIdempotencyKeysForOperation(ctx, db, "", expiredBefore, limit)
}

func cleanupExpiredIdempotencyKeysForOperation(
	ctx context.Context,
	db *gorm.DB,
	operation string,
	expiredBefore time.Time,
	limit int,
) (int, error) {
	if db == nil || expiredBefore.IsZero() || limit <= 0 {
		return 0, ErrInvalidIdempotencyRequest
	}
	if limit > 500 {
		limit = 500
	}
	cutoff := expiredBefore.Unix()
	ids := make([]int64, 0, limit)
	query := db.WithContext(ctx).Model(&model.KKAIIdempotencyKey{}).Where("expires_at <= ?", cutoff)
	operation = strings.TrimSpace(operation)
	if operation != "" {
		query = query.Where("operation = ?", operation)
	}
	if err := query.Order("expires_at ASC").Order("id ASC").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return 0, fmt.Errorf("list expired idempotency keys: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	deleteQuery := db.WithContext(ctx).Where("id IN ? AND expires_at <= ?", ids, cutoff)
	if operation != "" {
		deleteQuery = deleteQuery.Where("operation = ?", operation)
	}
	result := deleteQuery.Delete(&model.KKAIIdempotencyKey{})
	if result.Error != nil {
		return 0, fmt.Errorf("delete expired idempotency keys: %w", result.Error)
	}
	return int(result.RowsAffected), nil
}
