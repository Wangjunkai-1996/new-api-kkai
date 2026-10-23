package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWalletPaymentSchemaCanceledCallerCannotPolluteReadiness(t *testing.T) {
	checks := 0
	gate := walletPaymentSchemaGate{
		now: time.Now,
		checkSchema: func(context.Context, *gorm.DB) error {
			checks++
			return nil
		},
	}
	db := &gorm.DB{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, gate.check(ctx, db), context.Canceled)
	assert.Zero(t, checks)
	require.NoError(t, gate.check(context.Background(), db))
	assert.Equal(t, 1, checks)
}

func TestWalletPaymentSchemaRunningInspectionSurvivesCallerCancellation(t *testing.T) {
	db := &gorm.DB{}
	started := make(chan context.Context, 1)
	release, result := make(chan struct{}), make(chan error, 1)
	var checks atomic.Int32
	gate := walletPaymentSchemaGate{
		now: time.Now,
		checkSchema: func(ctx context.Context, _ *gorm.DB) error {
			checks.Add(1)
			started <- ctx
			<-release
			return ctx.Err()
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { result <- gate.check(ctx, db) }()
	inspection := <-started
	callerDeadline, ok := ctx.Deadline()
	require.True(t, ok)
	inspectionDeadline, ok := inspection.Deadline()
	require.True(t, ok)
	assert.True(t, inspectionDeadline.After(callerDeadline), "the shared inspection must have its own complete-audit budget")
	cancel()
	assert.NoError(t, inspection.Err(), "closing one request cannot cancel the shared audit")
	close(release)
	require.NoError(t, <-result)
	require.NoError(t, gate.check(context.Background(), db))
	assert.EqualValues(t, 1, checks.Load())
}

func TestWalletPaymentSchemaFailureRecoversAfterShortBackoff(t *testing.T) {
	for name, failure := range map[string]error{
		"database_failure":   errors.New("database temporarily unavailable"),
		"inspection_timeout": context.DeadlineExceeded,
		"schema_not_ready":   kkaimigrate.ErrSchemaNotReady,
	} {
		t.Run(name, func(t *testing.T) {
			db := &gorm.DB{}
			now, checks := time.Unix(100, 0), 0
			gate := walletPaymentSchemaGate{
				now: func() time.Time { return now },
				checkSchema: func(context.Context, *gorm.DB) error {
					checks++
					if checks == 1 {
						return failure
					}
					return nil
				},
			}
			require.ErrorIs(t, gate.check(context.Background(), db), failure)
			now = now.Add(time.Second)
			require.ErrorIs(t, gate.check(context.Background(), db), failure)
			assert.Equal(t, 1, checks, "failed probes still coalesce during the short outage backoff")
			now = now.Add(5 * time.Second)
			require.NoError(t, gate.check(context.Background(), db), "a transient failure must not disable all wallets for thirty seconds")
			assert.Equal(t, 2, checks)
			now = now.Add(10 * time.Second)
			require.NoError(t, gate.check(context.Background(), db))
			assert.Equal(t, 2, checks, "success retains the normal shared cache lifetime")
		})
	}
}
