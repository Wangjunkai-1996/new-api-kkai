package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newOutboxTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:kkai-outbox-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.KKAIOutboxEvent{}))
	return db
}

func seedOutboxEvent(t *testing.T, db *gorm.DB, topic string, availableAt int64) model.KKAIOutboxEvent {
	return seedOutboxEventForAggregate(t, db, topic, "aggregate", availableAt)
}

func seedOutboxEventForAggregate(
	t *testing.T,
	db *gorm.DB,
	topic string,
	aggregateID string,
	availableAt int64,
) model.KKAIOutboxEvent {
	t.Helper()
	event := model.KKAIOutboxEvent{
		EventKey:    fmt.Sprintf("event-%d", time.Now().UnixNano()),
		Topic:       topic,
		AggregateID: aggregateID,
		Payload:     `{"safe":true}`,
		Status:      model.KKAIOutboxStatusPending,
		AvailableAt: availableAt,
		LastError:   "",
		CreatedAt:   availableAt,
	}
	require.NoError(t, db.Create(&event).Error)
	return event
}

func TestKKAIOutboxProcessorDeliversClaimedEvent(t *testing.T) {
	db := newOutboxTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	event := seedOutboxEvent(t, db, "test.success", now.Unix())
	processor := NewKKAIOutboxProcessor(db, "worker-a")
	processor.now = func() time.Time { return now }
	var calls atomic.Int32
	require.NoError(t, processor.Register("test.success", func(ctx context.Context, claimed model.KKAIOutboxEvent) error {
		calls.Add(1)
		require.Equal(t, event.ID, claimed.ID)
		return nil
	}))

	result, err := processor.ProcessBatch(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.Claimed)
	require.Equal(t, 1, result.Delivered)
	require.EqualValues(t, 1, calls.Load())

	require.NoError(t, db.First(&event, event.ID).Error)
	require.Equal(t, model.KKAIOutboxStatusDelivered, event.Status)
	require.Equal(t, now.Unix(), event.DeliveredAt)
	require.Empty(t, event.LockedBy)
}

func TestKKAIOutboxProcessorRetriesThenMarksDead(t *testing.T) {
	db := newOutboxTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	event := seedOutboxEvent(t, db, "test.failure", now.Unix())
	processor := NewKKAIOutboxProcessor(db, "worker-a")
	processor.now = func() time.Time { return now }
	processor.baseRetry = time.Second
	processor.maxAttempts = 2
	require.NoError(t, processor.Register("test.failure", func(context.Context, model.KKAIOutboxEvent) error {
		return errors.New("Bearer abcdefghijkl sk-secretvalue")
	}))

	first, err := processor.ProcessBatch(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, first.Retried)
	require.NoError(t, db.First(&event, event.ID).Error)
	require.Equal(t, 1, event.Attempts)
	require.Equal(t, now.Add(time.Second).Unix(), event.AvailableAt)
	require.NotContains(t, event.LastError, "abcdefgh")
	require.NotContains(t, event.LastError, "secretvalue")

	now = now.Add(time.Second)
	second, err := processor.ProcessBatch(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, second.Dead)
	require.NoError(t, db.First(&event, event.ID).Error)
	require.Equal(t, model.KKAIOutboxStatusDead, event.Status)
	require.Equal(t, 2, event.Attempts)
}

func TestKKAIOutboxProcessorLeavesUnregisteredTopicsPending(t *testing.T) {
	db := newOutboxTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	event := seedOutboxEvent(t, db, "future.topic", now.Unix())
	processor := NewKKAIOutboxProcessor(db, "worker-a")
	processor.now = func() time.Time { return now }
	require.NoError(t, processor.Register("known.topic", func(context.Context, model.KKAIOutboxEvent) error {
		return nil
	}))

	result, err := processor.ProcessBatch(context.Background(), 10)
	require.NoError(t, err)
	require.Zero(t, result.Claimed)
	require.NoError(t, db.First(&event, event.ID).Error)
	require.Equal(t, model.KKAIOutboxStatusPending, event.Status)
	require.Zero(t, event.Attempts)
	require.Empty(t, event.LockedBy)
}

func TestKKAIOutboxProcessorMarksPermanentFailureDeadImmediately(t *testing.T) {
	db := newOutboxTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	event := seedOutboxEvent(t, db, "test.permanent", now.Unix())
	processor := NewKKAIOutboxProcessor(db, "worker-a")
	processor.now = func() time.Time { return now }
	require.NoError(t, processor.Register("test.permanent", func(context.Context, model.KKAIOutboxEvent) error {
		return PermanentKKAIOutboxError(errors.New("payload conflict"))
	}))

	result, err := processor.ProcessBatch(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.Dead)
	require.NoError(t, db.First(&event, event.ID).Error)
	require.Equal(t, model.KKAIOutboxStatusDead, event.Status)
	require.Equal(t, 1, event.Attempts)
	require.Contains(t, event.LastError, "payload conflict")
}

func TestKKAIOutboxProcessorDoesNotDoubleClaimLiveLock(t *testing.T) {
	db := newOutboxTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	seedOutboxEvent(t, db, "test.block", now.Unix())
	first := NewKKAIOutboxProcessor(db, "worker-a")
	second := NewKKAIOutboxProcessor(db, "worker-b")
	first.now = func() time.Time { return now }
	second.now = func() time.Time { return now }
	started := make(chan struct{})
	release := make(chan struct{})
	require.NoError(t, first.Register("test.block", func(context.Context, model.KKAIOutboxEvent) error {
		close(started)
		<-release
		return nil
	}))
	require.NoError(t, second.Register("test.block", func(context.Context, model.KKAIOutboxEvent) error {
		return errors.New("second worker must not receive live lock")
	}))

	firstDone := make(chan error, 1)
	go func() {
		_, err := first.ProcessBatch(context.Background(), 1)
		firstDone <- err
	}()
	<-started
	secondResult, err := second.ProcessBatch(context.Background(), 1)
	require.NoError(t, err)
	require.Zero(t, secondResult.Claimed)
	close(release)
	require.NoError(t, <-firstDone)
}

func TestKKAIOutboxProcessorSlowTopicDoesNotReserveLaterIndependentTopic(t *testing.T) {
	db := newOutboxTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	slowEvent := seedOutboxEvent(t, db, "test.slow", now.Unix())
	fastEvent := seedOutboxEvent(t, db, "test.fast", now.Unix())

	first := NewKKAIOutboxProcessor(db, "worker-a")
	second := NewKKAIOutboxProcessor(db, "worker-b")
	for _, processor := range []*KKAIOutboxProcessor{first, second} {
		processor.now = func() time.Time { return now }
	}

	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-releaseSlow:
		default:
			close(releaseSlow)
		}
	})
	require.NoError(t, first.Register("test.slow", func(context.Context, model.KKAIOutboxEvent) error {
		close(slowStarted)
		<-releaseSlow
		return nil
	}))
	require.NoError(t, first.Register("test.fast", func(context.Context, model.KKAIOutboxEvent) error {
		return nil
	}))
	require.NoError(t, second.Register("test.slow", func(context.Context, model.KKAIOutboxEvent) error {
		return errors.New("second worker must not execute the fenced slow event")
	}))
	var secondHandledID atomic.Int64
	require.NoError(t, second.Register("test.fast", func(_ context.Context, event model.KKAIOutboxEvent) error {
		secondHandledID.Store(event.ID)
		return nil
	}))

	type batchOutcome struct {
		result *KKAIOutboxBatchResult
		err    error
	}
	firstDone := make(chan batchOutcome, 1)
	go func() {
		result, err := first.ProcessBatch(context.Background(), 2)
		firstDone <- batchOutcome{result: result, err: err}
	}()
	<-slowStarted

	secondCtx, cancelSecond := context.WithTimeout(context.Background(), time.Second)
	defer cancelSecond()
	secondResult, err := second.ProcessBatch(secondCtx, 1)
	require.NoError(t, err)
	require.Equal(t, 1, secondResult.Claimed)
	require.Equal(t, 1, secondResult.Delivered)
	require.Equal(t, fastEvent.ID, secondHandledID.Load())

	close(releaseSlow)
	firstResult := <-firstDone
	require.NoError(t, firstResult.err)
	require.Equal(t, 1, firstResult.result.Claimed)
	require.Equal(t, 1, firstResult.result.Delivered)
	require.NoError(t, db.First(&slowEvent, slowEvent.ID).Error)
	require.Equal(t, model.KKAIOutboxStatusDelivered, slowEvent.Status)
	require.NoError(t, db.First(&fastEvent, fastEvent.ID).Error)
	require.Equal(t, model.KKAIOutboxStatusDelivered, fastEvent.Status)
}

func TestKKAIOutboxProcessorPreservesAggregateOrderAcrossDeferredPredecessor(t *testing.T) {
	db := newOutboxTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	predecessor := seedOutboxEventForAggregate(
		t,
		db,
		"test.aggregate-order",
		"aggregate-a",
		now.Add(time.Hour).Unix(),
	)
	later := seedOutboxEventForAggregate(t, db, "test.aggregate-order", "aggregate-a", now.Unix())

	processor := NewKKAIOutboxProcessor(db, "worker-a")
	processor.now = func() time.Time { return now }
	handled := make([]int64, 0, 2)
	require.NoError(t, processor.Register("test.aggregate-order", func(_ context.Context, event model.KKAIOutboxEvent) error {
		handled = append(handled, event.ID)
		return nil
	}))

	blocked, err := processor.ProcessBatch(context.Background(), 2)
	require.NoError(t, err)
	require.Zero(t, blocked.Claimed)
	require.Zero(t, blocked.Delivered)
	require.Empty(t, handled)
	require.NoError(t, db.First(&later, later.ID).Error)
	require.Equal(t, model.KKAIOutboxStatusPending, later.Status)
	require.Empty(t, later.LockedBy)

	now = now.Add(time.Hour)
	delivered, err := processor.ProcessBatch(context.Background(), 2)
	require.NoError(t, err)
	require.Equal(t, 2, delivered.Delivered)
	require.Equal(t, []int64{predecessor.ID, later.ID}, handled)
}

func TestKKAIOutboxProcessorDoesNotSerializeUnrelatedAggregates(t *testing.T) {
	testCases := []struct {
		name                 string
		predecessorAggregate string
		laterAggregate       string
	}{
		{name: "different aggregate", predecessorAggregate: "aggregate-a", laterAggregate: "aggregate-b"},
		{name: "empty aggregate", predecessorAggregate: "", laterAggregate: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := newOutboxTestDB(t)
			now := time.Unix(1_720_000_000, 0)
			predecessor := seedOutboxEventForAggregate(
				t,
				db,
				"test.aggregate-independent",
				testCase.predecessorAggregate,
				now.Add(time.Hour).Unix(),
			)
			later := seedOutboxEventForAggregate(
				t,
				db,
				"test.aggregate-independent",
				testCase.laterAggregate,
				now.Unix(),
			)

			processor := NewKKAIOutboxProcessor(db, "worker-a")
			processor.now = func() time.Time { return now }
			var handledID int64
			require.NoError(t, processor.Register("test.aggregate-independent", func(_ context.Context, event model.KKAIOutboxEvent) error {
				handledID = event.ID
				return nil
			}))

			result, err := processor.ProcessBatch(context.Background(), 1)
			require.NoError(t, err)
			require.Equal(t, 1, result.Delivered)
			require.Equal(t, later.ID, handledID)
			require.NoError(t, db.First(&predecessor, predecessor.ID).Error)
			require.Equal(t, model.KKAIOutboxStatusPending, predecessor.Status)
		})
	}
}

func TestKKAIOutboxProcessorReclaimsStaleLock(t *testing.T) {
	db := newOutboxTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	event := seedOutboxEvent(t, db, "test.reclaim", now.Add(-time.Hour).Unix())
	require.NoError(t, db.Model(&event).Updates(map[string]any{
		"locked_at": now.Add(-10 * time.Minute).Unix(),
		"locked_by": "dead-worker",
	}).Error)
	processor := NewKKAIOutboxProcessor(db, "worker-a")
	processor.now = func() time.Time { return now }
	processor.lockTimeout = time.Minute
	require.NoError(t, processor.Register("test.reclaim", func(context.Context, model.KKAIOutboxEvent) error { return nil }))

	result, err := processor.ProcessBatch(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, result.Delivered)
}

func TestKKAIOutboxProcessorDeferredDeliveryDoesNotConsumeRetryBudget(t *testing.T) {
	db := newOutboxTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	event := seedOutboxEvent(t, db, "test.defer", now.Unix())
	processor := NewKKAIOutboxProcessor(db, "worker-a")
	processor.now = func() time.Time { return now }
	processor.maxAttempts = 2
	initialAttempts := processor.maxAttempts - 1
	require.NoError(t, db.Model(&event).Update("attempts", initialAttempts).Error)
	succeed := false
	require.NoError(t, processor.Register("test.defer", func(context.Context, model.KKAIOutboxEvent) error {
		if succeed {
			return nil
		}
		return DeferKKAIOutboxUntil(now.Add(30*time.Minute), errors.New("aggregate is still running"))
	}))

	for range processor.maxAttempts + 3 {
		deferredUntil := now.Add(30 * time.Minute)
		result, err := processor.ProcessBatch(context.Background(), 1)
		require.NoError(t, err)
		require.NoError(t, db.First(&event, event.ID).Error)
		require.Equal(t, initialAttempts, event.Attempts)
		require.Empty(t, event.LockedBy)
		require.Equal(t, 1, result.Deferred)
		require.Zero(t, result.Dead)
		require.Equal(t, model.KKAIOutboxStatusPending, event.Status)
		require.Equal(t, deferredUntil.Unix(), event.AvailableAt)
		now = deferredUntil
	}

	succeed = true
	result, err := processor.ProcessBatch(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, result.Delivered)
	require.NoError(t, db.First(&event, event.ID).Error)
	require.Equal(t, model.KKAIOutboxStatusDelivered, event.Status)
	require.Equal(t, initialAttempts, event.Attempts)
}

func TestKKAIOutboxProcessorCancellationDoesNotClaimOrChargeUnstartedEvents(t *testing.T) {
	db := newOutboxTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	events := []model.KKAIOutboxEvent{
		seedOutboxEvent(t, db, "test.cancel-batch", now.Unix()),
		seedOutboxEvent(t, db, "test.cancel-batch", now.Unix()),
		seedOutboxEvent(t, db, "test.cancel-batch", now.Unix()),
	}
	const previousFailure = "real upstream failure"
	for index := 1; index < len(events); index++ {
		require.NoError(t, db.Model(&events[index]).Updates(map[string]any{
			"attempts": 1, "last_error": previousFailure,
		}).Error)
	}

	processor := NewKKAIOutboxProcessor(db, "worker-a")
	processor.now = func() time.Time { return now }
	processor.baseRetry = time.Second
	processor.maxAttempts = 2
	ctx, cancel := context.WithCancel(context.Background())
	calls := make(map[int64]int)
	require.NoError(t, processor.Register("test.cancel-batch", func(handlerCtx context.Context, event model.KKAIOutboxEvent) error {
		calls[event.ID]++
		if event.ID == events[0].ID {
			cancel()
		}
		return handlerCtx.Err()
	}))

	result, err := processor.ProcessBatch(ctx, len(events))
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, result.Claimed)
	require.Equal(t, 1, result.Retried)
	require.Zero(t, result.Deferred)
	require.Equal(t, map[int64]int{events[0].ID: 1}, calls)

	for index := range events {
		require.NoError(t, db.First(&events[index], events[index].ID).Error)
		require.Equal(t, model.KKAIOutboxStatusPending, events[index].Status)
		require.Empty(t, events[index].LockedBy)
		if index == 0 {
			require.Equal(t, now.Add(time.Second).Unix(), events[index].AvailableAt)
			require.Equal(t, 1, events[index].Attempts)
			continue
		}
		require.Equal(t, now.Unix(), events[index].AvailableAt)
		require.Equal(t, 1, events[index].Attempts)
		require.Equal(t, previousFailure, events[index].LastError)
	}

	now = now.Add(time.Second)
	restarted := NewKKAIOutboxProcessor(db, "worker-b")
	restarted.now = func() time.Time { return now }
	restarted.maxAttempts = 2
	require.NoError(t, restarted.Register("test.cancel-batch", func(context.Context, model.KKAIOutboxEvent) error {
		return nil
	}))

	restartResult, err := restarted.ProcessBatch(context.Background(), len(events))
	require.NoError(t, err)
	require.Equal(t, len(events), restartResult.Delivered)
	for index := range events {
		require.NoError(t, db.First(&events[index], events[index].ID).Error)
		require.Equal(t, model.KKAIOutboxStatusDelivered, events[index].Status)
		require.Equal(t, 1, events[index].Attempts)
	}
}

func TestKKAIOutboxProcessorCancellationKeepsStartedEventLeaseFenced(t *testing.T) {
	db := newOutboxTestDB(t)
	start := time.Unix(1_720_000_000, 0)
	firstEvent := seedOutboxEventForAggregate(t, db, "test.cancel-fence", "aggregate-a", start.Unix())
	secondEvent := seedOutboxEventForAggregate(t, db, "test.cancel-fence", "aggregate-b", start.Unix())
	var clock atomic.Int64
	clock.Store(start.Unix())

	first := NewKKAIOutboxProcessor(db, "worker-a")
	second := NewKKAIOutboxProcessor(db, "worker-b")
	for _, processor := range []*KKAIOutboxProcessor{first, second} {
		processor.now = func() time.Time { return time.Unix(clock.Load(), 0) }
		processor.lockTimeout = 30 * time.Second
		processor.heartbeatInterval = 5 * time.Millisecond
	}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	require.NoError(t, first.Register("test.cancel-fence", func(handlerCtx context.Context, _ model.KKAIOutboxEvent) error {
		close(started)
		<-handlerCtx.Done()
		close(canceled)
		<-release
		return handlerCtx.Err()
	}))
	var secondCalls atomic.Int64
	var secondHandledID atomic.Int64
	require.NoError(t, second.Register("test.cancel-fence", func(_ context.Context, event model.KKAIOutboxEvent) error {
		secondCalls.Add(1)
		secondHandledID.Store(event.ID)
		return nil
	}))

	firstDone := make(chan error, 1)
	go func() {
		_, err := first.ProcessBatch(ctx, 2)
		firstDone <- err
	}()
	<-started
	cancel()
	<-canceled
	require.NoError(t, db.First(&secondEvent, secondEvent.ID).Error)
	require.Empty(t, secondEvent.LockedBy)
	require.Equal(t, start.Unix(), secondEvent.AvailableAt)
	clock.Store(start.Add(31 * time.Second).Unix())
	require.Eventually(t, func() bool {
		var event model.KKAIOutboxEvent
		return db.First(&event, firstEvent.ID).Error == nil && event.LockedAt == clock.Load()
	}, time.Second, 5*time.Millisecond)

	secondResult, err := second.ProcessBatch(context.Background(), 2)
	require.NoError(t, err)
	require.Equal(t, 1, secondResult.Claimed)
	require.Equal(t, 1, secondResult.Delivered)
	require.EqualValues(t, 1, secondCalls.Load())
	require.Equal(t, secondEvent.ID, secondHandledID.Load())
	close(release)
	require.ErrorIs(t, <-firstDone, context.Canceled)

	require.NoError(t, db.First(&firstEvent, firstEvent.ID).Error)
	require.Equal(t, 1, firstEvent.Attempts)
	require.Empty(t, firstEvent.LockedBy)
	require.NoError(t, db.First(&secondEvent, secondEvent.ID).Error)
	require.Equal(t, model.KKAIOutboxStatusDelivered, secondEvent.Status)
	require.Zero(t, secondEvent.Attempts)
}

func TestKKAIOutboxProcessorCancellationBeforeHandlerStartPreservesBudget(t *testing.T) {
	db := newOutboxTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	event := seedOutboxEvent(t, db, "test.cancel-before-start", now.Unix())
	processor := NewKKAIOutboxProcessor(db, "worker-a")
	processor.now = func() time.Time { return now }
	processor.baseRetry = time.Second
	processor.maxAttempts = 1
	var calls atomic.Int64
	require.NoError(t, processor.Register("test.cancel-before-start", func(context.Context, model.KKAIOutboxEvent) error {
		calls.Add(1)
		return context.Canceled
	}))

	claimQueryStarted := make(chan struct{})
	continueClaim := make(chan struct{})
	claimReleased := false
	var blockClaimQuery atomic.Bool
	blockClaimQuery.Store(true)
	callbackName := "test:block_outbox_after_claim_query"
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != (model.KKAIOutboxEvent{}).TableName() || !blockClaimQuery.CompareAndSwap(true, false) {
			return
		}
		close(claimQueryStarted)
		<-continueClaim
	}))
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(callbackName)
		if !claimReleased {
			close(continueClaim)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := processor.ProcessBatch(ctx, 1)
		done <- err
	}()
	<-claimQueryStarted
	processor.handlersMu.Lock()
	handlersLocked := true
	close(continueClaim)
	claimReleased = true
	t.Cleanup(func() {
		if handlersLocked {
			processor.handlersMu.Unlock()
		}
	})
	require.Eventually(t, func() bool {
		var claimed model.KKAIOutboxEvent
		return db.First(&claimed, event.ID).Error == nil && claimed.LockedBy != ""
	}, time.Second, 5*time.Millisecond)
	cancel()
	processor.handlersMu.Unlock()
	handlersLocked = false

	require.ErrorIs(t, <-done, context.Canceled)
	require.Zero(t, calls.Load())
	require.NoError(t, db.First(&event, event.ID).Error)
	require.Equal(t, model.KKAIOutboxStatusPending, event.Status)
	require.Zero(t, event.Attempts)
	require.Empty(t, event.LockedBy)
	require.Equal(t, now.Add(time.Second).Unix(), event.AvailableAt)
}

func TestKKAIOutboxProcessorRenewsLeasePastThirtySeconds(t *testing.T) {
	db := newOutboxTestDB(t)
	start := time.Unix(1_720_000_000, 0)
	seedOutboxEvent(t, db, "test.long", start.Unix())
	var clock atomic.Int64
	clock.Store(start.Unix())

	first := NewKKAIOutboxProcessor(db, "worker-a")
	second := NewKKAIOutboxProcessor(db, "worker-b")
	for _, processor := range []*KKAIOutboxProcessor{first, second} {
		processor.now = func() time.Time { return time.Unix(clock.Load(), 0) }
		processor.lockTimeout = 30 * time.Second
		processor.heartbeatInterval = 5 * time.Millisecond
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var sideEffects atomic.Int64
	require.NoError(t, first.Register("test.long", func(context.Context, model.KKAIOutboxEvent) error {
		close(started)
		<-release
		sideEffects.Add(1)
		return nil
	}))
	require.NoError(t, second.Register("test.long", func(context.Context, model.KKAIOutboxEvent) error {
		sideEffects.Add(1)
		return nil
	}))

	firstDone := make(chan error, 1)
	go func() {
		_, err := first.ProcessBatch(context.Background(), 1)
		firstDone <- err
	}()
	<-started
	clock.Store(start.Add(31 * time.Second).Unix())
	require.Eventually(t, func() bool {
		var event model.KKAIOutboxEvent
		return db.First(&event).Error == nil && event.LockedAt == clock.Load()
	}, time.Second, 5*time.Millisecond)

	secondResult, err := second.ProcessBatch(context.Background(), 1)
	require.NoError(t, err)
	require.Zero(t, secondResult.Claimed)
	close(release)
	require.NoError(t, <-firstDone)
	require.EqualValues(t, 1, sideEffects.Load())
}

func TestKKAIOutboxProcessorCancelsHandlerWhenLeaseIsLost(t *testing.T) {
	db := newOutboxTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	event := seedOutboxEvent(t, db, "test.lease-loss", now.Unix())
	processor := NewKKAIOutboxProcessor(db, "worker-a")
	processor.now = func() time.Time { return now }
	processor.lockTimeout = 30 * time.Second
	processor.heartbeatInterval = 5 * time.Millisecond
	started := make(chan struct{})
	canceled := make(chan struct{})
	require.NoError(t, processor.Register("test.lease-loss", func(ctx context.Context, _ model.KKAIOutboxEvent) error {
		close(started)
		<-ctx.Done()
		close(canceled)
		return ctx.Err()
	}))

	done := make(chan error, 1)
	go func() {
		_, err := processor.ProcessBatch(context.Background(), 1)
		done <- err
	}()
	<-started
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("id = ?", event.ID).
		Updates(map[string]any{"locked_by": "worker-b:replacement-fence", "locked_at": now.Unix()}).Error)
	require.Eventually(t, func() bool {
		select {
		case <-canceled:
			return true
		default:
			return false
		}
	}, time.Second, 5*time.Millisecond)
	require.ErrorIs(t, <-done, ErrKKAIOutboxLockLost)
}

func TestKKAIOutboxDeadEventRedriveIsAuditedAndIdempotent(t *testing.T) {
	db := newOutboxTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	event := seedOutboxEvent(t, db, "test.redrive", now.Unix())
	require.NoError(t, db.Model(&event).Updates(map[string]any{
		"status":     model.KKAIOutboxStatusDead,
		"attempts":   12,
		"last_error": "archive failed",
	}).Error)

	redriven, applied, err := RedriveKKAIOutboxDeadEvent(context.Background(), db, event.ID, "redrive-1", "admin:42", now)
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, event.ID, redriven.ID)
	require.Equal(t, event.Payload, redriven.Payload)
	require.Equal(t, model.KKAIOutboxStatusPending, redriven.Status)
	require.Zero(t, redriven.Attempts)
	require.Contains(t, redriven.LastError, "redrive_key=redrive-1")
	require.Contains(t, redriven.LastError, "actor=admin:42")

	duplicate, applied, err := RedriveKKAIOutboxDeadEvent(context.Background(), db, event.ID, "redrive-1", "admin:42", now)
	require.NoError(t, err)
	require.False(t, applied)
	require.Equal(t, event.ID, duplicate.ID)

	var sideEffects atomic.Int64
	processor := NewKKAIOutboxProcessor(db, "worker-a")
	processor.now = func() time.Time { return now }
	require.NoError(t, processor.Register("test.redrive", func(context.Context, model.KKAIOutboxEvent) error {
		sideEffects.Add(1)
		return nil
	}))
	result, err := processor.ProcessBatch(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, result.Delivered)
	require.EqualValues(t, 1, sideEffects.Load())

	require.NoError(t, db.First(&event, event.ID).Error)
	require.Equal(t, model.KKAIOutboxStatusDelivered, event.Status)
	require.Contains(t, event.LastError, "redrive_key=redrive-1")
	_, applied, err = RedriveKKAIOutboxDeadEvent(context.Background(), db, event.ID, "redrive-1", "admin:42", now)
	require.NoError(t, err)
	require.False(t, applied)
}

func TestKKAIOutboxDeadEventRedriveKeyPrefixDoesNotCauseFalseReplay(t *testing.T) {
	db := newOutboxTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	event := seedOutboxEvent(t, db, "test.redrive-prefix", now.Unix())
	require.NoError(t, db.Model(&event).Updates(map[string]any{
		"status":     model.KKAIOutboxStatusDead,
		"attempts":   12,
		"last_error": "redrive_key=redrive-10 actor=admin:42 at=1719999999 source_event_id=1 | delivery_error=archive failed",
	}).Error)

	redriven, applied, err := RedriveKKAIOutboxDeadEvent(
		context.Background(), db, event.ID, "redrive-1", "admin:42", now,
	)
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, model.KKAIOutboxStatusPending, redriven.Status)
	require.Contains(t, redriven.LastError, "redrive_key=redrive-1 ")
}

func TestKKAIOutboxClaimQueryGuardsAggregateOrderAcrossDialects(t *testing.T) {
	dialectors := []struct {
		name      string
		dialector gorm.Dialector
	}{
		{
			name:      "sqlite",
			dialector: sqlite.Open("file:kkai-outbox-claim-dry-run?mode=memory&cache=shared"),
		},
		{
			name: "mysql-5.7",
			dialector: mysql.New(mysql.Config{
				DSN:                       "root@tcp(127.0.0.1:3306)/kkai_test_dry_run?charset=utf8mb4&parseTime=True&loc=Local",
				SkipInitializeWithVersion: true,
			}),
		},
		{
			name: "postgres",
			dialector: postgres.New(postgres.Config{
				DSN:                  "host=127.0.0.1 user=kkai dbname=kkai_test_dry_run port=5432 sslmode=disable",
				PreferSimpleProtocol: true,
			}),
		},
	}

	for _, dialect := range dialectors {
		t.Run(dialect.name, func(t *testing.T) {
			db, err := gorm.Open(dialect.dialector, &gorm.Config{
				DryRun:               true,
				DisableAutomaticPing: true,
			})
			require.NoError(t, err)

			var events []model.KKAIOutboxEvent
			statement := newKKAIOutboxClaimQuery(
				db,
				50,
				1_720_000_000,
				1_719_999_880,
				[]string{"test.aggregate-order"},
			).Find(&events).Statement.SQL.String()

			require.Contains(t, statement, "NOT EXISTS")
			require.Contains(t, statement, "kkai_outbox.aggregate_id =")
			require.Contains(t, statement, "FROM kkai_outbox AS predecessor")
			require.Contains(t, statement, "predecessor.topic = kkai_outbox.topic")
			require.Contains(t, statement, "predecessor.aggregate_id = kkai_outbox.aggregate_id")
			require.Contains(t, statement, "predecessor.status =")
			require.Contains(t, statement, "predecessor.id < kkai_outbox.id")
		})
	}
}

func TestKKAIOutboxMySQLClaimUsesMySQL57CompatibleLock(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "root@tcp(127.0.0.1:3306)/kkai_test_dry_run?charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	require.NoError(t, err)

	var events []model.KKAIOutboxEvent
	statement := lockKKAIOutboxClaim(db.Where("status = ?", model.KKAIOutboxStatusPending)).Find(&events).Statement.SQL.String()
	require.Contains(t, statement, "FOR UPDATE")
	require.NotContains(t, statement, "SKIP LOCKED")
}
