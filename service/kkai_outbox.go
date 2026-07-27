package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
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

type kkaiOutboxDeadLetterHandler func(
	context.Context,
	*gorm.DB,
	model.KKAIOutboxEvent,
	error,
	time.Time,
) error

type permanentKKAIOutboxError struct {
	err error
}

type deferredKKAIOutboxError struct {
	availableAt time.Time
	err         error
}

func (e deferredKKAIOutboxError) Error() string {
	if e.err == nil {
		return "KKAI outbox delivery deferred"
	}
	return e.err.Error()
}

func (e deferredKKAIOutboxError) Unwrap() error {
	return e.err
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

func DeferKKAIOutboxUntil(availableAt time.Time, reason error) error {
	return deferredKKAIOutboxError{availableAt: availableAt, err: reason}
}

func EnqueueKKAIOutboxEvent(ctx context.Context, db *gorm.DB, eventKey string, topic string, aggregateID string, availableAt time.Time, payload any) error {
	eventKey = strings.TrimSpace(eventKey)
	topic = strings.TrimSpace(topic)
	aggregateID = strings.TrimSpace(aggregateID)
	if db == nil || eventKey == "" || len(eventKey) > 191 || topic == "" || len(topic) > 128 || len(aggregateID) > 128 {
		return ErrKKAIOutboxInvalidConfiguration
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode KKAI outbox payload: %w", err)
	}
	if availableAt.IsZero() {
		availableAt = time.Now()
	}
	event := model.KKAIOutboxEvent{
		EventKey:    eventKey,
		Topic:       topic,
		AggregateID: aggregateID,
		Payload:     string(encoded),
		Status:      model.KKAIOutboxStatusPending,
		AvailableAt: availableAt.Unix(),
		CreatedAt:   time.Now().Unix(),
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&event).Error
}

type KKAIOutboxProcessor struct {
	db                 *gorm.DB
	workerID           string
	handlersMu         sync.RWMutex
	handlers           map[string]KKAIOutboxHandler
	deadLetterHandlers map[string]kkaiOutboxDeadLetterHandler
	now                func() time.Time
	lockTimeout        time.Duration
	heartbeatInterval  time.Duration
	baseRetry          time.Duration
	maxRetry           time.Duration
	maxAttempts        int
}

type KKAIOutboxBatchResult struct {
	Claimed   int
	Delivered int
	Deferred  int
	Retried   int
	Dead      int
}

const (
	kkaiOutboxClaimUnstarted uint32 = iota
	kkaiOutboxClaimStarted
	kkaiOutboxClaimFinished
)

type kkaiOutboxClaimReleaseResult struct {
	released int
	err      error
}

type kkaiOutboxCancellationWatch struct {
	stop     chan struct{}
	done     chan kkaiOutboxClaimReleaseResult
	stopOnce sync.Once
	result   kkaiOutboxClaimReleaseResult
}

func (watch *kkaiOutboxCancellationWatch) finish() kkaiOutboxClaimReleaseResult {
	watch.stopOnce.Do(func() {
		close(watch.stop)
		watch.result = <-watch.done
	})
	return watch.result
}

func NewKKAIOutboxProcessor(db *gorm.DB, workerID string) *KKAIOutboxProcessor {
	return &KKAIOutboxProcessor{
		db:                 db,
		workerID:           strings.TrimSpace(workerID),
		handlers:           make(map[string]KKAIOutboxHandler),
		deadLetterHandlers: make(map[string]kkaiOutboxDeadLetterHandler),
		now:                time.Now,
		lockTimeout:        2 * time.Minute,
		heartbeatInterval:  10 * time.Second,
		baseRetry:          5 * time.Second,
		maxRetry:           time.Hour,
		maxAttempts:        12,
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

func (p *KKAIOutboxProcessor) registerDeadLetter(topic string, handler kkaiOutboxDeadLetterHandler) error {
	if p == nil || strings.TrimSpace(topic) == "" || handler == nil {
		return ErrKKAIOutboxInvalidConfiguration
	}
	p.handlersMu.Lock()
	defer p.handlersMu.Unlock()
	p.deadLetterHandlers[strings.TrimSpace(topic)] = handler
	return nil
}

func (p *KKAIOutboxProcessor) ProcessBatch(ctx context.Context, limit int) (*KKAIOutboxBatchResult, error) {
	if p == nil || p.db == nil || p.workerID == "" || limit <= 0 || p.maxAttempts <= 0 ||
		p.lockTimeout <= 0 || p.heartbeatInterval <= 0 || p.heartbeatInterval > 10*time.Second ||
		p.heartbeatInterval >= p.lockTimeout || p.baseRetry <= 0 || p.maxRetry < p.baseRetry {
		return nil, ErrKKAIOutboxInvalidConfiguration
	}
	topics := p.registeredTopics()
	if len(topics) == 0 {
		return &KKAIOutboxBatchResult{}, nil
	}
	result := &KKAIOutboxBatchResult{}
	for result.Claimed < limit {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}
		// Handler execution is serial, so fence only the event this worker can start now.
		events, err := p.claim(ctx, 1, p.now(), topics)
		if err != nil {
			return result, err
		}
		if len(events) == 0 {
			return result, nil
		}
		result.Claimed++
		eventResult, err := p.processClaimedEvent(ctx, events[0])
		result.Delivered += eventResult.Delivered
		result.Deferred += eventResult.Deferred
		result.Retried += eventResult.Retried
		result.Dead += eventResult.Dead
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func (p *KKAIOutboxProcessor) processClaimedEvent(
	ctx context.Context,
	event model.KKAIOutboxEvent,
) (result KKAIOutboxBatchResult, processErr error) {
	lease := p.startKKAIOutboxLease(context.WithoutCancel(ctx), event)
	var claimState atomic.Uint32
	cancellationWatch := p.startKKAIOutboxCancellationWatch(ctx, event, lease, &claimState)
	defer func() {
		releaseResult := cancellationWatch.finish()
		result.Deferred += releaseResult.released
		processErr = errors.Join(processErr, releaseResult.err)
		_ = lease.stop()
	}()
	p.handlersMu.RLock()
	handler, ok := p.handlers[event.Topic]
	deadLetterHandler := p.deadLetterHandlers[event.Topic]
	p.handlersMu.RUnlock()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, ctxErr
	}
	if !claimState.CompareAndSwap(kkaiOutboxClaimUnstarted, kkaiOutboxClaimStarted) {
		return result, ctx.Err()
	}
	var handlerErr error
	if !ok {
		handlerErr = fmt.Errorf("no KKAI outbox handler for topic %s", event.Topic)
	} else {
		handlerCtx, cancelHandler := context.WithCancel(ctx)
		stopLeaseCancellation := context.AfterFunc(lease.context(), cancelHandler)
		handlerErr = callKKAIOutboxHandler(handlerCtx, handler, event)
		stopLeaseCancellation()
		cancelHandler()
	}
	claimState.Store(kkaiOutboxClaimFinished)
	if ctx.Err() != nil {
		cancellationWatch.finish()
	}
	if leaseErr := lease.stop(); leaseErr != nil {
		return result, leaseErr
	}
	finalizedAt := p.now()
	if handlerErr == nil {
		finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		finalizeErr := p.markDelivered(finalizeCtx, event, finalizedAt.Unix())
		cancel()
		if finalizeErr != nil {
			return result, finalizeErr
		}
		result.Delivered++
		return result, nil
	}
	var deferred deferredKKAIOutboxError
	if errors.As(handlerErr, &deferred) {
		finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		finalizeErr := p.markDeferred(finalizeCtx, event, deferred, finalizedAt)
		cancel()
		if finalizeErr != nil {
			return result, finalizeErr
		}
		result.Deferred++
		return result, nil
	}
	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	dead, finalizeErr := p.markFailed(finalizeCtx, event, handlerErr, finalizedAt, deadLetterHandler)
	cancel()
	if finalizeErr != nil {
		return result, finalizeErr
	}
	if dead {
		result.Dead++
	} else {
		result.Retried++
	}
	return result, nil
}

func callKKAIOutboxHandler(
	ctx context.Context,
	handler KKAIOutboxHandler,
	event model.KKAIOutboxEvent,
) (handlerErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			handlerErr = fmt.Errorf("KKAI outbox handler panic: %v", recovered)
		}
	}()
	return handler(ctx, event)
}

func (p *KKAIOutboxProcessor) startKKAIOutboxCancellationWatch(
	ctx context.Context,
	event model.KKAIOutboxEvent,
	lease *kkaiOutboxLease,
	claimState *atomic.Uint32,
) *kkaiOutboxCancellationWatch {
	watch := &kkaiOutboxCancellationWatch{
		stop: make(chan struct{}),
		done: make(chan kkaiOutboxClaimReleaseResult, 1),
	}
	go func() {
		select {
		case <-ctx.Done():
		case <-watch.stop:
			if ctx.Err() == nil {
				watch.done <- kkaiOutboxClaimReleaseResult{}
				return
			}
		}
		if !claimState.CompareAndSwap(kkaiOutboxClaimUnstarted, kkaiOutboxClaimFinished) {
			watch.done <- kkaiOutboxClaimReleaseResult{}
			return
		}
		released, err := p.releaseUnstartedClaim(ctx, event, lease)
		watch.done <- kkaiOutboxClaimReleaseResult{released: released, err: err}
	}()
	return watch
}

func (p *KKAIOutboxProcessor) releaseUnstartedClaim(
	ctx context.Context,
	event model.KKAIOutboxEvent,
	lease *kkaiOutboxLease,
) (int, error) {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := lease.stop(); err != nil {
		return 0, err
	}
	update := p.db.WithContext(releaseCtx).Model(&model.KKAIOutboxEvent{}).
		Where("id = ? AND status = ? AND locked_by = ?", event.ID, model.KKAIOutboxStatusPending, event.LockedBy).
		Updates(map[string]any{
			"available_at": p.now().Add(p.baseRetry).Unix(),
			"locked_at":    0,
			"locked_by":    "",
		})
	if update.Error != nil {
		return 0, fmt.Errorf("release unstarted KKAI outbox event %d: %w", event.ID, update.Error)
	}
	if update.RowsAffected == 0 {
		return 0, fmt.Errorf("%w: event %d fence %s", ErrKKAIOutboxLockLost, event.ID, event.LockedBy)
	}
	return 1, nil
}

func (p *KKAIOutboxProcessor) markDeferred(ctx context.Context, event model.KKAIOutboxEvent, deferred deferredKKAIOutboxError, now time.Time) error {
	// Deferral means the aggregate is not ready yet, so preserve the failure budget.
	availableAt := deferred.availableAt
	if !availableAt.After(now) {
		availableAt = now.Add(p.baseRetry)
	}
	update := p.db.WithContext(ctx).Model(&model.KKAIOutboxEvent{}).
		Where("id = ? AND status = ? AND locked_by = ?", event.ID, model.KKAIOutboxStatusPending, event.LockedBy).
		Updates(map[string]any{
			"available_at": availableAt.Unix(),
			"locked_at":    0,
			"locked_by":    "",
			"last_error":   kkaiOutboxFailureAudit(event, deferred),
		})
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected == 0 {
		return ErrKKAIOutboxLockLost
	}
	return nil
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
	claimEvents := func(tx *gorm.DB) error {
		staleBefore := now.Add(-p.lockTimeout).Unix()
		query := newKKAIOutboxClaimQuery(tx, limit, now.Unix(), staleBefore, topics)
		var candidates []model.KKAIOutboxEvent
		if err := query.Find(&candidates).Error; err != nil {
			return err
		}
		for _, candidate := range candidates {
			fence := p.newKKAIOutboxFence()
			update := tx.Model(&model.KKAIOutboxEvent{}).
				Where("id = ? AND status = ? AND (locked_at = 0 OR locked_at <= ?)", candidate.ID, model.KKAIOutboxStatusPending, staleBefore).
				Updates(map[string]any{"locked_at": now.Unix(), "locked_by": fence})
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected == 0 {
				continue
			}
			candidate.LockedAt = now.Unix()
			candidate.LockedBy = fence
			events = append(events, candidate)
		}
		return nil
	}
	if p.db.Dialector.Name() == "sqlite" {
		// SQLite has no row-level FOR UPDATE. A read transaction upgraded to a write lock can
		// deadlock with lease heartbeats; the fenced conditional updates provide the CAS here.
		return events, claimEvents(p.db.WithContext(ctx))
	}
	err := p.db.WithContext(ctx).Transaction(claimEvents)
	return events, err
}

func newKKAIOutboxClaimQuery(
	db *gorm.DB,
	limit int,
	now int64,
	staleBefore int64,
	topics []string,
) *gorm.DB {
	query := db.Model(&model.KKAIOutboxEvent{}).
		Where(
			"topic IN ? AND status = ? AND available_at <= ? AND (locked_at = 0 OR locked_at <= ?)",
			topics,
			model.KKAIOutboxStatusPending,
			now,
			staleBefore,
		).
		Where(
			`(
				kkai_outbox.aggregate_id = ?
				OR NOT EXISTS (
					SELECT 1
					FROM kkai_outbox AS predecessor
					WHERE predecessor.topic = kkai_outbox.topic
						AND predecessor.aggregate_id = kkai_outbox.aggregate_id
						AND predecessor.status = ?
						AND predecessor.id < kkai_outbox.id
				)
			)`,
			"",
			model.KKAIOutboxStatusPending,
		).
		Order("id ASC").
		Limit(limit)
	return lockKKAIOutboxClaim(query)
}

func lockKKAIOutboxClaim(query *gorm.DB) *gorm.DB {
	switch query.Dialector.Name() {
	case "postgres":
		return query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
	case "mysql":
		return query.Clauses(clause.Locking{Strength: "UPDATE"})
	default:
		return query
	}
}

func (p *KKAIOutboxProcessor) markDelivered(ctx context.Context, event model.KKAIOutboxEvent, deliveredAt int64) error {
	update := p.db.WithContext(ctx).Model(&model.KKAIOutboxEvent{}).
		Where("id = ? AND status = ? AND locked_by = ?", event.ID, model.KKAIOutboxStatusPending, event.LockedBy).
		Updates(map[string]any{
			"status":       model.KKAIOutboxStatusDelivered,
			"locked_at":    0,
			"locked_by":    "",
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

func (p *KKAIOutboxProcessor) markFailed(
	ctx context.Context,
	event model.KKAIOutboxEvent,
	handlerErr error,
	now time.Time,
	deadLetterHandler kkaiOutboxDeadLetterHandler,
) (bool, error) {
	attempts := event.Attempts + 1
	var permanent permanentKKAIOutboxError
	dead := errors.As(handlerErr, &permanent) || attempts >= p.maxAttempts
	status := model.KKAIOutboxStatusPending
	availableAt := now.Add(p.retryDelay(attempts)).Unix()
	if dead {
		status = model.KKAIOutboxStatusDead
		availableAt = now.Unix()
	}
	updates := map[string]any{
		"status":       status,
		"attempts":     attempts,
		"available_at": availableAt,
		"locked_at":    0,
		"locked_by":    "",
		"last_error":   kkaiOutboxFailureAudit(event, handlerErr),
	}
	finalize := func(tx *gorm.DB) error {
		update := tx.Model(&model.KKAIOutboxEvent{}).
			Where("id = ? AND status = ? AND locked_by = ?", event.ID, model.KKAIOutboxStatusPending, event.LockedBy).
			Updates(updates)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected == 0 {
			return ErrKKAIOutboxLockLost
		}
		if dead && deadLetterHandler != nil {
			deadEvent := event
			deadEvent.Status = model.KKAIOutboxStatusDead
			deadEvent.Attempts = attempts
			deadEvent.AvailableAt = availableAt
			deadEvent.LockedAt = 0
			deadEvent.LockedBy = ""
			deadEvent.LastError = updates["last_error"].(string)
			return deadLetterHandler(ctx, tx, deadEvent, handlerErr, now)
		}
		return nil
	}
	if dead && deadLetterHandler != nil {
		if err := p.db.WithContext(ctx).Transaction(finalize); err != nil {
			return false, err
		}
	} else if err := finalize(p.db.WithContext(ctx)); err != nil {
		return false, err
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
