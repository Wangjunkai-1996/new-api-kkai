package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestResellerFinanceWorkersFreezeUntilFinalSchemaAtStartup(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.TempDir()+"/worker-schema.db"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	previousDB, previousType := model.DB, common.MainDatabaseType()
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetMainDatabaseType(previousType)
		assert.NoError(t, sqlDB.Close())
	})
	// Start from actual legacy tables and apply the public historical migration
	// entry points. Merely inserting a v14 ledger row must never enable a worker.
	for _, statement := range []string{
		"CREATE TABLE users(id INTEGER PRIMARY KEY,telegram_id TEXT,username VARCHAR(20),email VARCHAR(50),CONSTRAINT uni_users_username UNIQUE(username))",
		"CREATE TABLE tokens(id INTEGER PRIMARY KEY)",
		"CREATE TABLE passkey_credentials(id INTEGER PRIMARY KEY)",
		"CREATE TABLE options(key VARCHAR(255) PRIMARY KEY,value TEXT)",
		"CREATE TABLE midjourneys(id INTEGER PRIMARY KEY,mj_id TEXT,channel_id BIGINT,quota BIGINT,status TEXT)",
		"CREATE TABLE top_ups(id INTEGER PRIMARY KEY)",
		"CREATE TABLE user_oauth_bindings(id INTEGER PRIMARY KEY)",
		"CREATE TABLE logs(id INTEGER PRIMARY KEY)",
		"CREATE TABLE quota_data(id INTEGER PRIMARY KEY)",
		"CREATE TABLE tasks(id INTEGER PRIMARY KEY)",
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	ctx := context.Background()
	for _, apply := range []func(context.Context, *gorm.DB, kkaimigrate.Options) (*kkaimigrate.Result, error){
		kkaimigrate.ApplyLegacyBaseline, kkaimigrate.ApplyOutboxEventKeyCompatibility,
		kkaimigrate.ApplyVideoStudioExpand, kkaimigrate.ApplyVideoSampleCategoryExpand,
		kkaimigrate.ApplyImageStudioExpand, kkaimigrate.ApplyAuthenticationExpand,
		kkaimigrate.ApplyRC41ArchitectureExpand, kkaimigrate.ApplyImageAsyncExpand, kkaimigrate.ApplyMembershipExpand,
	} {
		_, err := apply(ctx, db, kkaimigrate.Options{})
		require.NoError(t, err)
	}
	legacy := NewBackgroundJobRegistry()
	require.NoError(t, RegisterResellerFinanceBackgroundJobs(legacy))
	assert.Empty(t, legacy.Descriptors())
	_, err = kkaimigrate.ApplyTenantOnlineExpansion(ctx, db, kkaimigrate.Options{})
	require.NoError(t, err)
	runtime := CaptureTenantRuntime(db)
	assert.Equal(t, "expanded", runtime.SchemaAtStartup)
	assert.False(t, runtime.ResellerReadyAtStartup)
	bridge := NewBackgroundJobRegistry()
	require.NoError(t, RegisterResellerFinanceBackgroundJobs(bridge))
	assert.Empty(t, bridge.Descriptors())
	_, err = kkaimigrate.FinalizeTenantOnlineSchema(ctx, db, kkaimigrate.Options{})
	require.NoError(t, err)
	require.NoError(t, kkaimigrate.CheckTenantOnlineSchema(ctx, db))
	assert.Equal(t, "expanded", runtime.SchemaAtStartup, "a running bridge cannot claim final-schema startup")
	finalRuntime := CaptureTenantRuntime(db)
	assert.Equal(t, "finalized", finalRuntime.SchemaAtStartup)
	assert.True(t, finalRuntime.ResellerReadyAtStartup)
	assert.Empty(t, bridge.Descriptors(), "the running bridge must remain frozen after the database advances")
	for _, routing := range []string{"false", "true"} {
		t.Setenv("TENANT_ROUTING_ENABLED", routing)
		restarted := NewBackgroundJobRegistry()
		require.NoError(t, RegisterResellerFinanceBackgroundJobs(restarted))
		descriptors := restarted.Descriptors()
		require.Len(t, descriptors, 2)
		for _, job := range descriptors {
			assert.True(t, job.WritesData)
			assert.True(t, job.RequiresLeaderLease)
		}
	}
	require.NoError(t, db.Model(&kkaimigrate.AppliedMigration{}).Where("version = ?", 14).Update("checksum", "forged").Error)
	invalid := NewBackgroundJobRegistry()
	require.NoError(t, RegisterResellerFinanceBackgroundJobs(invalid))
	assert.Empty(t, invalid.Descriptors())
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
