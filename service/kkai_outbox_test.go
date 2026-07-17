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
	t.Helper()
	event := model.KKAIOutboxEvent{
		EventKey:    fmt.Sprintf("event-%d", time.Now().UnixNano()),
		Topic:       topic,
		AggregateID: "aggregate",
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
