package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrKKAIOutboxInvalidConfiguration = errors.New("invalid KKAI outbox configuration")
	ErrKKAIOutboxLockLost             = errors.New("KKAI outbox lock lost")

	outboxBearerPattern      = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]{8,}`)
	outboxSKPattern          = regexp.MustCompile(`(?i)\bsk-[a-z0-9][a-z0-9._-]{6,}`)
	outboxSecretFieldPattern = regexp.MustCompile(`(?i)\b(authorization|api[_-]?key|token|secret|credential)\s*[:=]\s*[^\s,;]+`)
)

type KKAIOutboxHandler func(context.Context, model.KKAIOutboxEvent) error

type permanentKKAIOutboxError struct {
	err error
}

func (e permanentKKAIOutboxError) Error() string {
	return e.err.Error()
}

func (e permanentKKAIOutboxError) Unwrap() error {
	return e.err
}

func PermanentKKAIOutboxError(err error) error {
	if err == nil {
		return nil
	}
	return permanentKKAIOutboxError{err: err}
}

type KKAIOutboxProcessor struct {
	db          *gorm.DB
	workerID    string
	handlersMu  sync.RWMutex
	handlers    map[string]KKAIOutboxHandler
	now         func() time.Time
	lockTimeout time.Duration
	baseRetry   time.Duration
	maxRetry    time.Duration
	maxAttempts int
}

type KKAIOutboxBatchResult struct {
	Claimed   int
	Delivered int
	Retried   int
	Dead      int
}

func NewKKAIOutboxProcessor(db *gorm.DB, workerID string) *KKAIOutboxProcessor {
	return &KKAIOutboxProcessor{
		db:          db,
		workerID:    strings.TrimSpace(workerID),
		handlers:    make(map[string]KKAIOutboxHandler),
		now:         time.Now,
		lockTimeout: 2 * time.Minute,
		baseRetry:   5 * time.Second,
		maxRetry:    time.Hour,
		maxAttempts: 12,
	}
}

func (p *KKAIOutboxProcessor) Register(topic string, handler KKAIOutboxHandler) error {
	if p == nil || strings.TrimSpace(topic) == "" || handler == nil {
		return ErrKKAIOutboxInvalidConfiguration
	}
	p.handlersMu.Lock()
	defer p.handlersMu.Unlock()
	p.handlers[strings.TrimSpace(topic)] = handler
	return nil
}

func (p *KKAIOutboxProcessor) ProcessBatch(ctx context.Context, limit int) (*KKAIOutboxBatchResult, error) {
	if p == nil || p.db == nil || p.workerID == "" || limit <= 0 || p.maxAttempts <= 0 ||
		p.lockTimeout <= 0 || p.baseRetry <= 0 || p.maxRetry < p.baseRetry {
		return nil, ErrKKAIOutboxInvalidConfiguration
	}
	now := p.now()
	topics := p.registeredTopics()
	if len(topics) == 0 {
		return &KKAIOutboxBatchResult{}, nil
	}
	events, err := p.claim(ctx, limit, now, topics)
	if err != nil {
		return nil, err
	}
	result := &KKAIOutboxBatchResult{Claimed: len(events)}
	for _, event := range events {
		p.handlersMu.RLock()
		handler, ok := p.handlers[event.Topic]
		p.handlersMu.RUnlock()
		if !ok {
			err = fmt.Errorf("no KKAI outbox handler for topic %s", event.Topic)
		} else {
			err = handler(ctx, event)
		}
		if err == nil {
			finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			finalizeErr := p.markDelivered(finalizeCtx, event.ID, now.Unix())
			cancel()
			if finalizeErr != nil {
				return nil, finalizeErr
			}
			result.Delivered++
			continue
		}
		finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		dead, finalizeErr := p.markFailed(finalizeCtx, event, err, now)
		cancel()
		if finalizeErr != nil {
			return nil, finalizeErr
		}
		if dead {
			result.Dead++
		} else {
			result.Retried++
		}
	}
	return result, nil
}

func (p *KKAIOutboxProcessor) registeredTopics() []string {
	p.handlersMu.RLock()
	defer p.handlersMu.RUnlock()
	topics := make([]string, 0, len(p.handlers))
	for topic := range p.handlers {
		topics = append(topics, topic)
	}
	return topics
}

func (p *KKAIOutboxProcessor) claim(ctx context.Context, limit int, now time.Time, topics []string) ([]model.KKAIOutboxEvent, error) {
	events := make([]model.KKAIOutboxEvent, 0, limit)
	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		staleBefore := now.Add(-p.lockTimeout).Unix()
		query := tx.Where(
			"topic IN ? AND status = ? AND available_at <= ? AND (locked_at = 0 OR locked_at <= ?)",
			topics,
			model.KKAIOutboxStatusPending,
			now.Unix(),
			staleBefore,
		).Order("id ASC").Limit(limit)
		if tx.Dialector.Name() != "sqlite" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		var candidates []model.KKAIOutboxEvent
		if err := query.Find(&candidates).Error; err != nil {
			return err
		}
		for _, candidate := range candidates {
			update := tx.Model(&model.KKAIOutboxEvent{}).
				Where("id = ? AND status = ? AND (locked_at = 0 OR locked_at <= ?)", candidate.ID, model.KKAIOutboxStatusPending, staleBefore).
				Updates(map[string]any{"locked_at": now.Unix(), "locked_by": p.workerID})
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected == 0 {
				continue
			}
			candidate.LockedAt = now.Unix()
			candidate.LockedBy = p.workerID
			events = append(events, candidate)
		}
		return nil
	})
	return events, err
}

func (p *KKAIOutboxProcessor) markDelivered(ctx context.Context, id int64, deliveredAt int64) error {
	update := p.db.WithContext(ctx).Model(&model.KKAIOutboxEvent{}).
		Where("id = ? AND locked_by = ?", id, p.workerID).
		Updates(map[string]any{
			"status":       model.KKAIOutboxStatusDelivered,
			"locked_at":    0,
			"locked_by":    "",
			"last_error":   "",
			"delivered_at": deliveredAt,
		})
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected == 0 {
		return ErrKKAIOutboxLockLost
	}
	return nil
}

func (p *KKAIOutboxProcessor) markFailed(ctx context.Context, event model.KKAIOutboxEvent, handlerErr error, now time.Time) (bool, error) {
	attempts := event.Attempts + 1
	var permanent permanentKKAIOutboxError
	dead := errors.As(handlerErr, &permanent) || attempts >= p.maxAttempts
	status := model.KKAIOutboxStatusPending
	availableAt := now.Add(p.retryDelay(attempts)).Unix()
	if dead {
		status = model.KKAIOutboxStatusDead
		availableAt = now.Unix()
	}
	update := p.db.WithContext(ctx).Model(&model.KKAIOutboxEvent{}).
		Where("id = ? AND locked_by = ?", event.ID, p.workerID).
		Updates(map[string]any{
			"status":       status,
			"attempts":     attempts,
			"available_at": availableAt,
			"locked_at":    0,
			"locked_by":    "",
			"last_error":   sanitizeKKAIOutboxError(handlerErr),
		})
	if update.Error != nil {
		return false, update.Error
	}
	if update.RowsAffected == 0 {
		return false, ErrKKAIOutboxLockLost
	}
	return dead, nil
}

func (p *KKAIOutboxProcessor) retryDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return p.baseRetry
	}
	delay := p.baseRetry
	for i := 1; i < attempt; i++ {
		if delay >= p.maxRetry/2 {
			return p.maxRetry
		}
		delay *= 2
	}
	if delay > p.maxRetry {
		return p.maxRetry
	}
	return delay
}

func sanitizeKKAIOutboxError(err error) string {
	if err == nil {
		return ""
	}
	message := outboxBearerPattern.ReplaceAllString(err.Error(), "[redacted]")
	message = outboxSKPattern.ReplaceAllString(message, "[redacted]")
	message = outboxSecretFieldPattern.ReplaceAllString(message, "$1=[redacted]")
	if len(message) > 1024 {
		message = message[:1024]
	}
	return message
}
