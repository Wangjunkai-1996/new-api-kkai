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

func TestMembershipBillingRecoveryTargetPrecheckAndDryRunAreReadOnly(t *testing.T) {
	db := newInvoiceCommandFixture(t)
	ctx := context.Background()
	_, err := kkaimigrate.ApplyInvoiceExpand(ctx, db, kkaimigrate.Options{Offline: true})
	require.NoError(t, err)
	for _, ddl := range []string{"ALTER TABLE top_ups ADD COLUMN user_id BIGINT NULL", "ALTER TABLE top_ups ADD COLUMN create_time BIGINT NULL"} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	_, err = kkaimigrate.ApplyWalletPaymentExpand(ctx, db, kkaimigrate.Options{Offline: true})
	require.NoError(t, err)
	_, err = kkaimigrate.ApplyShopExpand(ctx, db, kkaimigrate.Options{Offline: true})
	require.NoError(t, err)
	_, err = kkaimigrate.ApplyMembershipPaymentExpand(ctx, db, kkaimigrate.Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	var output bytes.Buffer
	require.NoError(t, runMigrationPrecheck(ctx, db, kkaimigrate.MembershipBillingRecoverySchemaVersion, &output))
	var precheck kkaimigrate.MembershipBillingRecoveryExpandPrecheck
	require.NoError(t, common.Unmarshal(output.Bytes(), &precheck))
	assert.EqualValues(t, 20, precheck.CurrentVersion)
	assert.EqualValues(t, 21, precheck.TargetVersion)
	assert.True(t, precheck.Safe)
	planned, err := applyMigrationTarget(ctx, db, kkaimigrate.MembershipBillingRecoverySchemaVersion, kkaimigrate.Options{DryRun: true})
	require.NoError(t, err)
	require.Len(t, planned.Pending, 1)
	assert.Equal(t, kkaimigrate.MembershipBillingRecoveryPlanChecksum(), planned.Pending[0].Checksum)
	present, err := kkaimigrate.MembershipBillingRecoverySchemaObjectsPresent(db)
	require.NoError(t, err)
	assert.False(t, present)
	assert.ErrorIs(t, kkaimigrate.CheckMembershipBillingRecoveryRuntimeSchema(ctx, db), kkaimigrate.ErrSchemaNotReady)
}
