package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var kkaiOutboxRedriveKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

// RedriveKKAIOutboxDeadEvent reuses the original row so its ID, event key,
// aggregate, topic, and business payload remain the durable lineage.
func RedriveKKAIOutboxDeadEvent(ctx context.Context, db *gorm.DB, eventID int64, redriveKey string, actor string, now time.Time) (*model.KKAIOutboxEvent, bool, error) {
	return redriveKKAIOutboxDeadEvent(ctx, db, eventID, redriveKey, actor, now, nil)
}

func redriveKKAIOutboxDeadEvent(
	ctx context.Context,
	db *gorm.DB,
	eventID int64,
	redriveKey string,
	actor string,
	now time.Time,
	prepare func(context.Context, *gorm.DB, model.KKAIOutboxEvent, time.Time) error,
) (*model.KKAIOutboxEvent, bool, error) {
	redriveKey = strings.TrimSpace(redriveKey)
	actor = strings.TrimSpace(actor)
	if db == nil || eventID <= 0 || !kkaiOutboxRedriveKeyPattern.MatchString(redriveKey) || actor == "" || len(actor) > 128 {
		return nil, false, ErrKKAIOutboxInvalidConfiguration
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		now = time.Now()
	}

	var event model.KKAIOutboxEvent
	applied := false
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx
		if tx.Dialector.Name() != "sqlite" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&event, eventID).Error; err != nil {
			return err
		}
		marker := "redrive_key=" + redriveKey
		if event.Status != model.KKAIOutboxStatusDead || strings.HasPrefix(strings.TrimSpace(event.LastError), marker+" ") {
			return nil
		}
		if prepare != nil {
			if err := prepare(ctx, tx, event, now); err != nil {
				return err
			}
		}

		audit := sanitizeKKAIOutboxError(errors.New(fmt.Sprintf(
			"redrive_key=%s actor=%s at=%d source_event_id=%d previous_error=%s",
			redriveKey,
			actor,
			now.Unix(),
			event.ID,
			event.LastError,
		)))
		update := tx.Model(&model.KKAIOutboxEvent{}).
			Where("id = ? AND status = ? AND last_error = ?", event.ID, model.KKAIOutboxStatusDead, event.LastError).
			Updates(map[string]any{
				"status":       model.KKAIOutboxStatusPending,
				"attempts":     0,
				"available_at": now.Unix(),
				"locked_at":    0,
				"locked_by":    "",
				"last_error":   audit,
				"delivered_at": 0,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected == 0 {
			return ErrKKAIOutboxLockLost
		}
		applied = true
		event.Status = model.KKAIOutboxStatusPending
		event.Attempts = 0
		event.AvailableAt = now.Unix()
		event.LockedAt = 0
		event.LockedBy = ""
		event.LastError = audit
		event.DeliveredAt = 0
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &event, applied, nil
}

func kkaiOutboxFailureAudit(event model.KKAIOutboxEvent, deliveryErr error) string {
	current := strings.TrimSpace(event.LastError)
	next := sanitizeKKAIOutboxError(deliveryErr)
	if !strings.Contains(current, "redrive_key=") {
		return next
	}
	return sanitizeKKAIOutboxError(errors.New(current + " | delivery_error=" + next))
}
