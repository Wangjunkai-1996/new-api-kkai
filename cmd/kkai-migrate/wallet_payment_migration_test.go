//go:build !kkai_bridge

package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalletPaymentCompatibilityCommandIsReadOnly(t *testing.T) {
	for _, operation := range []string{"plan", "status"} {
		require.NoError(t, validateWalletPaymentMigrationCommand(operation, "", true, false))
		assert.Error(t, validateWalletPaymentMigrationCommand(operation, walletPaymentControllerProofPath, true, false))
		assert.Error(t, validateWalletPaymentMigrationCommand(operation, "", false, false))
		assert.Error(t, validateWalletPaymentMigrationCommand(operation, "", true, true))
	}
	for _, path := range []string{"", walletPaymentControllerProofPath, "/tmp/proof.json", tenantControllerProofPath, invoiceControllerProofPath} {
		assert.Error(t, validateWalletPaymentMigrationCommand("apply", path, true, false))
		assert.Error(t, runWalletPaymentMigration(context.Background(), nil, "apply", path, &bytes.Buffer{}), "mutation must be rejected before a database can be touched")
	}
	assert.Error(t, validateWalletPaymentMigrationCommand("repair", "", true, false))
}

func TestWalletPaymentInspectionRejectsUnreceiptedObjects(t *testing.T) {
	db := newInvoiceCommandFixture(t)
	ctx := context.Background()
	_, err := kkaimigrate.ApplyInvoiceExpand(ctx, db, kkaimigrate.Options{Offline: true})
	require.NoError(t, err)
	for _, ddl := range []string{
		"ALTER TABLE top_ups ADD COLUMN user_id BIGINT NULL",
		"ALTER TABLE top_ups ADD COLUMN create_time BIGINT NULL",
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	var output bytes.Buffer
	require.NoError(t, runWalletPaymentMigration(ctx, db, "plan", "", &output))
	var status walletPaymentMigrationStatus
	require.NoError(t, common.Unmarshal(output.Bytes(), &status))
	assert.EqualValues(t, 17, status.CurrentVersion)
	assert.False(t, status.WalletSchemaPresent)
	assert.Equal(t, kkaimigrate.WalletPaymentPlanChecksum(), status.PlanChecksum)
	assert.Error(t, runWalletPaymentMigration(ctx, db, "apply", walletPaymentControllerProofPath, &output))
	require.NoError(t, db.Exec("PRAGMA query_only=OFF").Error)
	for _, ddl := range []string{
		"ALTER TABLE top_ups ADD COLUMN currency_exponent INTEGER NULL",
		"CREATE TABLE wallet_topup_runs(id VARCHAR(64) PRIMARY KEY)",
	} {
		tx := db.Begin()
		require.NoError(t, tx.Error)
		require.NoError(t, tx.Exec(ddl).Error)
		_, err = inspectWalletPaymentMigration(ctx, tx)
		assert.ErrorIs(t, err, kkaimigrate.ErrSchemaNotReady)
		require.NoError(t, tx.Rollback().Error)
	}
	_, err = inspectWalletPaymentMigration(ctx, db)
	require.NoError(t, err)
	_, err = kkaimigrate.ApplyWalletPaymentExpand(ctx, db, kkaimigrate.Options{Offline: true})
	require.NoError(t, err)
	var receipt kkaimigrate.AppliedMigration
	require.NoError(t, db.Where("version = ?", 18).Take(&receipt).Error)
	require.NoError(t, db.Where("version = ?", 18).Delete(&receipt).Error)
	_, err = inspectWalletPaymentMigration(ctx, db)
	assert.ErrorIs(t, err, kkaimigrate.ErrSchemaNotReady, "unreceipted expansion cannot be adopted by production apply")
	require.NoError(t, db.Create(&receipt).Error)
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	output.Reset()
	require.NoError(t, runWalletPaymentMigration(ctx, db, "status", "", &output))
	require.NoError(t, common.Unmarshal(output.Bytes(), &status))
	assert.EqualValues(t, 18, status.CurrentVersion)
	assert.True(t, status.WalletSchemaPresent)
	require.NoError(t, db.Exec("PRAGMA query_only=OFF").Error)
	require.NoError(t, db.Exec("DROP INDEX idx_wallet_topup_request").Error)
	_, err = inspectWalletPaymentMigration(ctx, db)
	assert.ErrorIs(t, err, kkaimigrate.ErrSchemaNotReady, "the canonical receipt cannot conceal lost payment idempotency")
}
