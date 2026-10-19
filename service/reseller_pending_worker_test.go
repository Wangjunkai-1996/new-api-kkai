package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestResellerFinanceRecoveryWorkersRemainRegisteredWhenRoutingIsDisabled(t *testing.T) {
	t.Setenv("TENANT_ROUTING_ENABLED", "false")
	registry := NewBackgroundJobRegistry()
	require.NoError(t, RegisterResellerFinanceBackgroundJobs(registry))
	assert.Len(t, registry.Descriptors(), 2)
	t.Setenv("TENANT_ROUTING_ENABLED", "true")
	activeRegistry := NewBackgroundJobRegistry()
	require.NoError(t, RegisterResellerFinanceBackgroundJobs(activeRegistry))
	descriptors := activeRegistry.Descriptors()
	require.Len(t, descriptors, 2)
	for _, job := range descriptors {
		assert.True(t, job.WritesData)
		assert.True(t, job.RequiresLeaderLease)
	}
}

func TestResellerPendingWorkerContinuesAfterAnotherTenantFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:reseller-worker-%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	require.NoError(t, db.AutoMigrate(&model.ResellerWallet{}, &model.ResellerLedgerEntry{}))
	// Tenant 1 has a corrupted summary; tenant 2 still needs its due release.
	require.NoError(t, db.Create(&model.ResellerWallet{TenantID: 1, PendingMinor: 0, Version: 1}).Error)
	require.NoError(t, db.Create(&model.ResellerWallet{TenantID: 2, PendingMinor: 50, Version: 1}).Error)
	for _, tenantID := range []int64{1, 2} {
		require.NoError(t, db.Create(&model.ResellerLedgerEntry{TenantID: tenantID, OperationID: fmt.Sprintf("settlement:%d", tenantID), Kind: model.ResellerLedgerKindSettlement, AmountMinor: 50, PendingDelta: 50, AvailableAt: time.Now().Unix() - 1}).Error)
	}
	require.ErrorIs(t, RunResellerPendingRelease(context.Background()), model.ErrResellerWalletInvariant)
	var wallet model.ResellerWallet
	require.NoError(t, db.Where("tenant_id = ?", 2).First(&wallet).Error)
	assert.Zero(t, wallet.PendingMinor)
	assert.Equal(t, int64(50), wallet.AvailableMinor)
	// A failed tenant and a repeated pass cannot duplicate the successful release.
	require.ErrorIs(t, RunResellerPendingRelease(context.Background()), model.ErrResellerWalletInvariant)
	var count int64
	require.NoError(t, db.Model(&model.ResellerLedgerEntry{}).Where("tenant_id = ? AND kind = ?", 2, model.ResellerLedgerKindPendingRelease).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}
