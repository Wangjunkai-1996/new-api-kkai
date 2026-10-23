package service

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWalletPaymentSchemaGateRequiresReceiptAndRechecksDatabaseDrift(t *testing.T) {
	db := walletReceiptRecoveryDB(t)
	ctx := context.Background()
	var receipt kkaimigrate.AppliedMigration
	require.NoError(t, db.Where("version = ?", kkaimigrate.WalletPaymentSchemaVersion).First(&receipt).Error)
	require.NoError(t, db.Where("version = ?", receipt.Version).Delete(&kkaimigrate.AppliedMigration{}).Error)
	now := time.Unix(100, 0)
	checks := 0
	gate := walletPaymentSchemaGate{
		now: func() time.Time { return now },
		checkSchema: func(ctx context.Context, db *gorm.DB) error {
			checks++
			_, bounded := ctx.Deadline()
			assert.True(t, bounded)
			return kkaimigrate.CheckWalletPaymentSchema(ctx, db)
		},
	}
	require.Error(t, gate.check(ctx, db), "physical tables without the canonical receipt cannot enable payments")
	require.NoError(t, db.Create(&receipt).Error)
	require.Error(t, gate.check(ctx, db), "failed probes are cached as well")
	assert.Equal(t, 1, checks)
	now = now.Add(31 * time.Second)
	require.NoError(t, gate.check(ctx, db))
	assert.Equal(t, 2, checks)

	other, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "unexpanded.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := other.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.Error(t, gate.check(ctx, other), "switching databases must discard the previous ready result immediately")
	assert.False(t, other.Migrator().HasTable("wallet_topup_flows"), "readiness never installs schema")
	require.NoError(t, gate.check(ctx, db))
	assert.Equal(t, 4, checks)

	require.NoError(t, db.Exec("DROP INDEX idx_wallet_topup_request").Error)
	require.NoError(t, gate.check(ctx, db), "success is reused only within the bounded interval")
	require.Error(t, kkaimigrate.CheckWalletPaymentSchema(ctx, db), "the maintenance API always observes current physical drift")
	now = now.Add(31 * time.Second)
	require.Error(t, gate.check(ctx, db), "expired successful probes must fail closed on drift")
	assert.Equal(t, 5, checks)
}

func TestWalletPaymentSchemaGateCoalescesConcurrentProbes(t *testing.T) {
	db := &gorm.DB{}
	var checks atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	gate := walletPaymentSchemaGate{
		now: func() time.Time { return time.Unix(100, 0) },
		checkSchema: func(context.Context, *gorm.DB) error {
			checks.Add(1)
			close(started)
			<-release
			return nil
		},
	}
	results := make(chan error, 2)
	go func() { results <- gate.check(context.Background(), db) }()
	<-started
	go func() { results <- gate.check(context.Background(), db) }()
	close(release)
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	assert.EqualValues(t, 1, checks.Load())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, gate.check(ctx, db), context.Canceled)
	require.ErrorIs(t, gate.check(context.Background(), nil), kkaimigrate.ErrSchemaNotReady)
	assert.EqualValues(t, 1, checks.Load())
}
