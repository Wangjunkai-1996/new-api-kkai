package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupWalletRunSegment(t *testing.T) (*gorm.DB, *model.WalletTopUpRun) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous; _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.TopUp{}, &model.WalletTopUpRun{}, &model.SystemTaskLock{}))
	now := common.GetTimestamp()
	selection := model.WalletRunSelection{Scope: model.WalletRunScope{PaymentProvider: "stripe", AccountRef: "original-account", Environment: "test", CreatedBefore: now, TenantID: 0}, BatchSize: 1, QueriesPerSecond: 10}
	encoded, err := common.Marshal(selection)
	require.NoError(t, err)
	run := &model.WalletTopUpRun{ID: "historical-run", ScopeJSON: string(encoded), ScopeDigest: "frozen-scope", MaxOrderID: 1, ControlVersion: 1, Status: "queued", DesiredState: "running", Owner: "1", AuthorizationRef: "authorized-runbook-record"}
	require.NoError(t, db.Create(run).Error)
	require.NoError(t, db.Create(&model.SystemTaskLock{Type: WalletPaymentHistoryTaskType, TaskID: "task-a", LockedBy: "runner-a", LockedUntil: common.GetTimestamp() + 60}).Error)
	require.NoError(t, db.Create(&model.TopUp{Id: 1, UserId: 9, TradeNo: "historical-order", PaymentProvider: "stripe", ProviderAccountRef: "original-account", ProviderEnvironment: "test", CreateTime: now - 100, Status: "pending"}).Error)
	return db, run
}
func TestWalletHistoryPauseInvalidatesInflightCheckpointAndResumesOriginalOrder(t *testing.T) {
	db, run := setupWalletRunSegment(t)
	calls := 0
	err := runWalletHistorySegment(context.Background(), run, "task-a", "runner-a", func(context.Context, string) (string, error) {
		calls++
		current, e := model.GetWalletTopUpRun(run.ID)
		require.NoError(t, e)
		_, e = model.UpdateWalletTopUpRun(run.ID, current.ControlVersion, map[string]any{"desired_state": "paused", "status": "pausing"})
		require.NoError(t, e)
		return "credited", nil
	})
	require.NoError(t, err)
	paused, err := model.GetWalletTopUpRun(run.ID)
	require.NoError(t, err)
	assert.Equal(t, "paused", paused.Status)
	assert.Zero(t, paused.Cursor)
	assert.Zero(t, paused.ScannedCount)
	assert.Equal(t, 1, calls)
	resumed, err := model.UpdateWalletTopUpRun(run.ID, paused.ControlVersion, map[string]any{"desired_state": "running", "status": "queued"})
	require.NoError(t, err)
	require.NoError(t, runWalletHistorySegment(context.Background(), resumed, "task-a", "runner-a", func(_ context.Context, trade string) (string, error) {
		assert.Equal(t, "historical-order", trade)
		calls++
		return "credited", nil
	}))
	stored, err := model.GetWalletTopUpRun(run.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, stored.Cursor)
	assert.EqualValues(t, 1, stored.ScannedCount)
	assert.EqualValues(t, 1, stored.CreditedCount)
	assert.Equal(t, 2, calls)
	// A task segment reaching its budget is queued, not a completed business run.
	assert.Equal(t, "queued", stored.Status)
	require.NoError(t, db.Model(&model.TopUp{}).Where("id = 1").Update("status", "success").Error)
	require.NoError(t, runWalletHistorySegment(context.Background(), stored, "task-a", "runner-a", func(context.Context, string) (string, error) {
		t.Fatal("completed order must not be queried again")
		return "", nil
	}))
	stored, err = model.GetWalletTopUpRun(run.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", stored.Status)
}
func TestWalletHistoryLostLeaseCannotAdvanceOrStartAnotherQuery(t *testing.T) {
	db, run := setupWalletRunSegment(t)
	calls := 0
	require.ErrorIs(t, runWalletHistorySegment(context.Background(), run, "task-a", "runner-a", func(context.Context, string) (string, error) {
		calls++
		require.NoError(t, db.Model(&model.SystemTaskLock{}).Where("task_id = ?", "task-a").Update("locked_until", 0).Error)
		return "closed", nil
	}), model.ErrSystemTaskLockLost)
	stored, err := model.GetWalletTopUpRun(run.ID)
	require.NoError(t, err)
	assert.Zero(t, stored.Cursor)
	assert.Zero(t, stored.ScannedCount)
	assert.Equal(t, 1, calls)
	err = runWalletHistorySegment(context.Background(), stored, "task-a", "runner-a", func(context.Context, string) (string, error) {
		t.Fatal("expired lease must not start provider query")
		return "", nil
	})
	require.ErrorIs(t, err, model.ErrSystemTaskLockLost)
	pausing, err := model.UpdateWalletTopUpRun(stored.ID, stored.ControlVersion, map[string]any{"desired_state": "paused", "status": "pausing"})
	require.NoError(t, err)
	require.ErrorIs(t, finishWalletRunPause(pausing, "task-a", "runner-a"), model.ErrWalletOrderChanged)
	stored, err = model.GetWalletTopUpRun(run.ID)
	require.NoError(t, err)
	assert.Equal(t, "pausing", stored.Status, "the expired worker cannot acknowledge a pause")
}
