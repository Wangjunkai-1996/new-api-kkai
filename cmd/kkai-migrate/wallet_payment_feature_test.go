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

func TestWalletPaymentTargetPrecheckAndDryRunAreReadOnly(t *testing.T) {
	db := newInvoiceCommandFixture(t)
	ctx := context.Background()
	_, err := kkaimigrate.ApplyInvoiceExpand(ctx, db, kkaimigrate.Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	var output bytes.Buffer
	require.NoError(t, runMigrationPrecheck(ctx, db, kkaimigrate.WalletPaymentSchemaVersion, &output))
	var precheck kkaimigrate.WalletPaymentExpandPrecheck
	require.NoError(t, common.Unmarshal(output.Bytes(), &precheck))
	assert.True(t, precheck.Safe)
	assert.EqualValues(t, 17, precheck.CurrentVersion)
	assert.EqualValues(t, 18, precheck.TargetVersion)
	planned, err := applyMigrationTarget(ctx, db, kkaimigrate.WalletPaymentSchemaVersion, kkaimigrate.Options{DryRun: true})
	require.NoError(t, err)
	require.Len(t, planned.Pending, 1)
	assert.EqualValues(t, 18, planned.Pending[0].Version)
	assert.False(t, db.Migrator().HasTable("wallet_topup_flows"))
	assert.False(t, db.Migrator().HasColumn("top_ups", "wallet_lifecycle_version"))
}
