package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/kkaimigrate"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newLeaderLeaseTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:kkai-leader-lease-"+time.Now().Format("150405.000000000")+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	_, err = kkaimigrate.Apply(context.Background(), db, kkaimigrate.Options{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	return db
}

func TestLeaderLeaseAllowsSingleHolderAndCleanTakeover(t *testing.T) {
	db := newLeaderLeaseTestDB(t)
	now := time.Unix(1_720_000_000, 0)
	store := NewKKAILeaderLeaseStore(db, "background-writer")
	store.now = func(context.Context) (int64, error) { return now.Unix(), nil }
	ctx := context.Background()

	acquired, err := store.TryAcquire(ctx, "blue", 30*time.Second)
	require.NoError(t, err)
	require.True(t, acquired)
	acquired, err = store.TryAcquire(ctx, "green", 30*time.Second)
	require.NoError(t, err)
	require.False(t, acquired)

	now = now.Add(20 * time.Second)
	renewed, err := store.Renew(ctx, "blue", 30*time.Second)
	require.NoError(t, err)
	require.True(t, renewed)

	now = now.Add(31 * time.Second)
	acquired, err = store.TryAcquire(ctx, "green", 30*time.Second)
	require.NoError(t, err)
	require.True(t, acquired)
	renewed, err = store.Renew(ctx, "blue", 30*time.Second)
	require.NoError(t, err)
	require.False(t, renewed)
	require.NoError(t, store.Release(ctx, "blue"))

	lease, err := store.Current(ctx)
	require.NoError(t, err)
	require.Equal(t, "green", lease.Holder)
	require.GreaterOrEqual(t, lease.Fence, int64(2))
}

func TestLeaderLeaseConcurrentAcquisitionHasOneWinner(t *testing.T) {
	db := newLeaderLeaseTestDB(t)
	store := NewKKAILeaderLeaseStore(db, "background-writer")
	store.now = func(context.Context) (int64, error) { return 1_720_000_000, nil }
	const contenders = 12
	var winners atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, contenders)

	for index := 0; index < contenders; index++ {
		wg.Add(1)
		go func(holder string) {
			defer wg.Done()
			acquired, err := store.TryAcquire(context.Background(), holder, time.Minute)
			if err == nil && acquired {
				winners.Add(1)
			}
			errs <- err
		}(string(rune('a' + index)))
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, winners.Load())
}

func TestLeaderLeaseUsesDatabaseClock(t *testing.T) {
	db := newLeaderLeaseTestDB(t)
	store := NewKKAILeaderLeaseStore(db, "background-writer")

	acquired, err := store.TryAcquire(context.Background(), "blue", 30*time.Second)
	require.NoError(t, err)
	require.True(t, acquired)

	lease, err := store.Current(context.Background())
	require.NoError(t, err)
	databaseNow, err := store.databaseUnixTime(context.Background())
	require.NoError(t, err)
	require.GreaterOrEqual(t, lease.LeaseUntil, databaseNow+29)
	require.LessOrEqual(t, lease.LeaseUntil, databaseNow+30)
}
