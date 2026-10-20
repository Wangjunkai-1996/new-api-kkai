package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKKAIOutboxConcurrentStartsNewEventsWhileEarlierEventsRun(t *testing.T) {
	db := newOutboxTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	const topic = "test.concurrent"
	now := time.Now()
	events := []model.KKAIOutboxEvent{
		seedOutboxEventForAggregate(t, db, topic, "first", now.Unix()),
		seedOutboxEventForAggregate(t, db, topic, "second", now.Unix()),
		seedOutboxEventForAggregate(t, db, topic, "third", now.Unix()),
	}
	processor := NewKKAIOutboxProcessor(db, "concurrent-worker")
	started := make(chan model.KKAIOutboxEvent, 4)
	release := make(chan struct{})
	var releaseOnce sync.Once
	finishHandlers := func() { releaseOnce.Do(func() { close(release) }) }
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { finishHandlers(); cancel() })
	require.NoError(t, processor.Register(topic, func(_ context.Context, event model.KKAIOutboxEvent) error {
		started <- event
		<-release
		return nil
	}))
	var inflight atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- processor.RunConcurrent(ctx, time.Millisecond, func(ctx context.Context) (context.Context, func(), error) {
			inflight.Add(1)
			return ctx, func() { inflight.Add(-1) }, nil
		})
	}()
	claimed := make(map[int64]bool)
	for range events {
		select {
		case event := <-started:
			assert.NotEmpty(t, event.LockedBy)
			claimed[event.ID] = true
		case <-time.After(5 * time.Second):
			t.Fatal("the first three events must run concurrently")
		}
	}
	// Polling must continue while every previously claimed handler is blocked.
	fourth := seedOutboxEventForAggregate(t, db, topic, "fourth", now.Unix())
	events = append(events, fourth)
	select {
	case event := <-started:
		assert.Equal(t, fourth.ID, event.ID)
		claimed[event.ID] = true
	case <-time.After(5 * time.Second):
		t.Fatal("a new event must not wait for an existing worker to finish")
	}
	assert.Len(t, claimed, 4)
	other := NewKKAIOutboxProcessor(db, "other-worker")
	require.NoError(t, other.Register(topic, func(context.Context, model.KKAIOutboxEvent) error {
		t.Error("live claims must not execute twice")
		return nil
	}))
	result, err := other.ProcessBatch(context.Background(), 4)
	require.NoError(t, err)
	assert.Zero(t, result.Claimed)
	finishHandlers()
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent processing did not join its handlers")
	}
	assert.Zero(t, inflight.Load())
	for _, event := range events {
		require.NoError(t, db.First(&event, event.ID).Error)
		assert.Equal(t, model.KKAIOutboxStatusDelivered, event.Status)
		assert.Empty(t, event.LockedBy)
	}
}

func TestKKAIOutboxConcurrentCancellationWaitsForClaimFinalization(t *testing.T) {
	db := newOutboxTestDB(t)
	event := seedOutboxEventForAggregate(t, db, "test.cancel", "one", time.Now().Unix())
	processor := NewKKAIOutboxProcessor(db, "concurrent-worker")
	started := make(chan struct{})
	cancelObserved := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	finishHandler := func() { releaseOnce.Do(func() { close(release) }) }
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { finishHandler(); cancel() })
	require.NoError(t, processor.Register(event.Topic, func(ctx context.Context, _ model.KKAIOutboxEvent) error {
		close(started)
		<-ctx.Done()
		close(cancelObserved)
		<-release
		return DeferKKAIOutboxUntil(time.Now().Add(time.Minute), context.Canceled)
	}))
	var inflight atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- processor.RunConcurrent(ctx, time.Millisecond, func(ctx context.Context) (context.Context, func(), error) {
			inflight.Add(1)
			return ctx, func() { inflight.Add(-1) }, nil
		})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("event did not start")
	}
	cancel()
	select {
	case <-cancelObserved:
	case <-time.After(5 * time.Second):
		t.Fatal("leader cancellation did not reach the claimed handler")
	}
	assert.GreaterOrEqual(t, inflight.Load(), int32(1))
	select {
	case <-done:
		t.Fatal("runner returned before claim finalization and drain release")
	default:
	}
	finishHandler()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled event was not joined")
	}
	assert.Zero(t, inflight.Load())
	require.NoError(t, db.First(&event, event.ID).Error)
	assert.Equal(t, model.KKAIOutboxStatusPending, event.Status)
	assert.Empty(t, event.LockedBy)
	assert.Zero(t, event.Attempts)
	assert.Greater(t, event.AvailableAt, time.Now().Unix())
}

func TestKKAIOutboxConcurrentDrainPausesClaims(t *testing.T) {
	db := newOutboxTestDB(t)
	event := seedOutboxEventForAggregate(t, db, "test.drain", "one", time.Now().Unix())
	processor := NewKKAIOutboxProcessor(db, "concurrent-worker")
	require.NoError(t, processor.Register(event.Topic, func(context.Context, model.KKAIOutboxEvent) error {
		t.Error("a draining worker must not execute a new event")
		return nil
	}))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	paused := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- processor.RunConcurrent(ctx, time.Millisecond, func(ctx context.Context) (context.Context, func(), error) {
			select {
			case paused <- struct{}{}:
			default:
			}
			return ctx, nil, nil
		})
	}()
	select {
	case <-paused:
	case <-time.After(5 * time.Second):
		t.Fatal("drain hook was not checked")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("draining runner did not stop")
	}
	require.NoError(t, db.First(&event, event.ID).Error)
	assert.Equal(t, model.KKAIOutboxStatusPending, event.Status)
	assert.Empty(t, event.LockedBy)
	assert.Zero(t, event.Attempts)
}
