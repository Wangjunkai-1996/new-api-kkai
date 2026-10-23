package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// No SQL is needed: readiness owns caching and passes the bounded context to
// the injected canonical-schema checker.
func invoiceSchemaTestDB() *gorm.DB {
	db := &gorm.DB{Config: &gorm.Config{}}
	db.Statement = &gorm.Statement{DB: db, Context: context.Background()}
	return db
}

func TestInvoiceSchemaCanceledCallerDoesNotPolluteReadiness(t *testing.T) {
	var state invoiceSchemaReadiness
	db := invoiceSchemaTestDB()
	now := time.Unix(100, 0)
	clock := func() time.Time { return now }
	checks := 0
	check := func(context.Context, *gorm.DB) error {
		checks++
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.False(t, state.check(ctx, db, check, clock))
	assert.Zero(t, checks)
	require.True(t, state.check(context.Background(), db, check, clock))
	assert.Equal(t, 1, checks)
	assert.False(t, state.check(ctx, db, check, clock), "a canceled caller cannot use cached success")
	assert.True(t, state.check(context.Background(), db, check, clock))
	assert.Equal(t, 1, checks)
}

func TestInvoiceSchemaRunningCheckSurvivesCallerCancellation(t *testing.T) {
	var state invoiceSchemaReadiness
	db := invoiceSchemaTestDB()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	observed := make(chan struct {
		ctx context.Context
		db  *gorm.DB
	}, 1)
	release := make(chan struct{})
	result := make(chan bool, 1)
	check := func(ctx context.Context, scoped *gorm.DB) error {
		observed <- struct {
			ctx context.Context
			db  *gorm.DB
		}{ctx: ctx, db: scoped}
		<-release
		return ctx.Err()
	}
	go func() { result <- state.check(ctx, db, check, time.Now) }()
	inspection := <-observed
	cancel()
	assert.NoError(t, inspection.ctx.Err())
	assert.Same(t, inspection.ctx, inspection.db.Statement.Context)
	_, bounded := inspection.ctx.Deadline()
	assert.True(t, bounded, "shared inspection retains its own deadline")
	close(release)
	require.True(t, <-result)
	assert.True(t, state.check(context.Background(), db, func(context.Context, *gorm.DB) error {
		t.Error("completed readiness should be reused")
		return nil
	}, time.Now))
}

func TestInvoiceSchemaConcurrentCallersShareOneInspection(t *testing.T) {
	var state invoiceSchemaReadiness
	db := invoiceSchemaTestDB()
	var checks atomic.Int32
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	results := make(chan bool, 2)
	check := func(context.Context, *gorm.DB) error {
		checks.Add(1)
		started <- struct{}{}
		<-release
		return nil
	}
	go func() { results <- state.check(context.Background(), db, check, time.Now) }()
	<-started
	followerStarted := make(chan struct{})
	go func() {
		close(followerStarted)
		results <- state.check(context.Background(), db, check, time.Now)
	}()
	<-followerStarted
	close(release)
	assert.True(t, <-results)
	assert.True(t, <-results)
	assert.EqualValues(t, 1, checks.Load())
}

func TestInvoiceSchemaFailureBackoffRecoversWithoutStaleSuccess(t *testing.T) {
	for _, failure := range []struct {
		name string
		err  error
	}{
		{name: "schema drift", err: fmt.Errorf("receipt: %w", kkaimigrate.ErrSchemaNotReady)},
		{name: "timeout", err: fmt.Errorf("inspection: %w", context.DeadlineExceeded)},
		{name: "unknown database error", err: fmt.Errorf("database inspection failed")},
	} {
		t.Run(failure.name, func(t *testing.T) {
			var state invoiceSchemaReadiness
			db := invoiceSchemaTestDB()
			now := time.Unix(100, 0)
			clock := func() time.Time { return now }
			checks := 0
			inspectionError := error(nil)
			check := func(context.Context, *gorm.DB) error {
				checks++
				return inspectionError
			}
			require.True(t, state.check(context.Background(), db, check, clock))
			inspectionError = failure.err
			now = now.Add(30 * time.Second)
			require.False(t, state.check(context.Background(), db, check, clock), "expired success must not survive a failed recheck")
			assert.Equal(t, 2, checks)
			inspectionError = nil
			now = now.Add(5*time.Second - time.Nanosecond)
			assert.False(t, state.check(context.Background(), db, check, clock))
			assert.Equal(t, 2, checks, "failed inspection is shared during backoff")
			now = now.Add(time.Nanosecond)
			assert.True(t, state.check(context.Background(), db, check, clock))
			assert.Equal(t, 3, checks)
		})
	}
}

func TestInvoiceSchemaReadinessDoesNotCrossDatabaseInstances(t *testing.T) {
	var state invoiceSchemaReadiness
	first, second := invoiceSchemaTestDB(), invoiceSchemaTestDB()
	checks := 0
	check := func(context.Context, *gorm.DB) error {
		checks++
		if checks == 1 {
			return nil
		}
		return kkaimigrate.ErrSchemaNotReady
	}
	require.True(t, state.check(context.Background(), first, check, time.Now))
	assert.False(t, state.check(context.Background(), second, check, time.Now))
	assert.Equal(t, 2, checks)
}

func TestInvoiceSchemaCacheLifetimeStartsWhenInspectionFinishes(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		ttl  time.Duration
	}{
		{name: "ready", ttl: 30 * time.Second},
		{name: "unavailable", err: context.DeadlineExceeded, ttl: 5 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			var state invoiceSchemaReadiness
			db := invoiceSchemaTestDB()
			now := time.Unix(100, 0)
			clock := func() time.Time { return now }
			checks := 0
			check := func(context.Context, *gorm.DB) error {
				checks++
				now = now.Add(10 * time.Second)
				return test.err
			}
			assert.Equal(t, test.err == nil, state.check(context.Background(), db, check, clock))
			now = now.Add(test.ttl - time.Nanosecond)
			assert.Equal(t, test.err == nil, state.check(context.Background(), db, check, clock))
			assert.Equal(t, 1, checks)
			now = now.Add(time.Nanosecond)
			assert.Equal(t, test.err == nil, state.check(context.Background(), db, check, clock))
			assert.Equal(t, 2, checks, "cache hits must not extend the expiration")
		})
	}
}
