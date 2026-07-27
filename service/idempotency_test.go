package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
)

func TestVideoIdempotencyReservationConflictsAndReplaysBoundTask(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.KKAIIdempotencyKey{}))
	request := IdempotencyReservationRequest{
		UserID: 7, Operation: model.VideoIdempotencyOperationSubmit, Key: "same-key",
		RequestHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	first, err := ReserveIdempotencyKey(context.Background(), db, request)
	require.NoError(t, err)
	require.True(t, first.Created)
	_, err = ReserveIdempotencyKey(context.Background(), db, request)
	require.ErrorIs(t, err, ErrIdempotencyInProgress)

	conflict := request
	conflict.RequestHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	_, err = ReserveIdempotencyKey(context.Background(), db, conflict)
	require.ErrorIs(t, err, ErrIdempotencyConflict)

	require.NoError(t, BindIdempotencyResource(
		context.Background(), db, first.Record.ID, model.VideoIdempotencyResourceTask, "task_123",
	))
	replay, err := ReserveIdempotencyKey(context.Background(), db, request)
	require.NoError(t, err)
	require.False(t, replay.Created)
	require.Equal(t, "task_123", replay.Record.ResourceID)
}

func TestReleaseUnboundVideoIdempotencyReservationAllowsRetry(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.KKAIIdempotencyKey{}))
	request := IdempotencyReservationRequest{
		UserID: 7, Operation: model.VideoIdempotencyOperationSubmit, Key: "retry-key",
		RequestHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	first, err := ReserveIdempotencyKey(context.Background(), db, request)
	require.NoError(t, err)
	require.NoError(t, ReleaseUnboundIdempotencyReservation(context.Background(), db, first.Record.ID))

	retry, err := ReserveIdempotencyKey(context.Background(), db, request)
	require.NoError(t, err)
	require.True(t, retry.Created)
}

func TestCleanupExpiredIdempotencyKeysIsBoundedAndPreservesActiveReservations(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.KKAIIdempotencyKey{}))
	now := time.Unix(2_000_000_000, 0)
	records := []model.KKAIIdempotencyKey{
		{UserID: 7, Operation: "video.submit", Key: "expired-1", RequestHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ResourceType: "task", ResourceID: "task-1", CreatedAt: now.Add(-time.Hour).Unix(), ExpiresAt: now.Add(-3 * time.Minute).Unix()},
		{UserID: 7, Operation: "video.submit", Key: "expired-2", RequestHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ResourceType: "task", ResourceID: "task-2", CreatedAt: now.Add(-time.Hour).Unix(), ExpiresAt: now.Add(-2 * time.Minute).Unix()},
		{UserID: 7, Operation: "video.submit", Key: "expired-3", RequestHash: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", ResourceType: "task", ResourceID: "task-3", CreatedAt: now.Add(-time.Hour).Unix(), ExpiresAt: now.Add(-time.Minute).Unix()},
		{UserID: 7, Operation: "video.submit", Key: "valid", RequestHash: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", ResourceType: "task", ResourceID: "task-4", CreatedAt: now.Add(-time.Hour).Unix(), ExpiresAt: now.Add(time.Minute).Unix()},
		{UserID: 7, Operation: "video.submit", Key: "abandoned", RequestHash: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", CreatedAt: now.Add(-time.Hour).Unix(), ExpiresAt: now.Add(-time.Minute).Unix()},
		{UserID: 7, Operation: "video.submit", Key: "in-progress", RequestHash: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", CreatedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(time.Minute).Unix()},
	}
	require.NoError(t, db.Create(&records).Error)

	deleted, err := CleanupExpiredIdempotencyKeys(context.Background(), db, now, 2)
	require.NoError(t, err)
	require.Equal(t, 2, deleted)
	var expired int64
	require.NoError(t, db.Model(&model.KKAIIdempotencyKey{}).
		Where("expires_at <= ?", now.Unix()).Count(&expired).Error)
	require.EqualValues(t, 2, expired)

	deleted, err = CleanupExpiredIdempotencyKeys(context.Background(), db, now, 2)
	require.NoError(t, err)
	require.Equal(t, 2, deleted)
	deleted, err = CleanupExpiredIdempotencyKeys(context.Background(), db, now, 2)
	require.NoError(t, err)
	require.Zero(t, deleted)

	var survivors []model.KKAIIdempotencyKey
	require.NoError(t, db.Order("id ASC").Find(&survivors).Error)
	require.Len(t, survivors, 2)
	require.Equal(t, "valid", survivors[0].Key)
	require.Equal(t, "in-progress", survivors[1].Key)
}
