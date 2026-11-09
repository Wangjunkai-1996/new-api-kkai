package kkaimigrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMembershipBillingRecoveryPostgresAtomicFailureAndReplay(t *testing.T) {
	db := newShopSchemaDatabaseFixture(t, DialectPostgres)
	ctx := context.Background()
	options := prepareRequestTicketExpandFixture(t, db)
	_, err := ApplyRequestTicketExpand(ctx, db, options)
	require.NoError(t, err)
	_, err = ApplyInvoiceExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	for _, ddl := range []string{
		"ALTER TABLE top_ups ADD COLUMN user_id BIGINT NULL",
		"ALTER TABLE top_ups ADD COLUMN create_time BIGINT NULL",
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	_, err = ApplyWalletPaymentExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	_, err = ApplyShopExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, CheckShopSchema(ctx, db))

	_, err = ApplyMembershipPaymentExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, CheckMembershipPaymentSchema(ctx, db))

	// PostgreSQL index names are schema-wide. Force a failure after both
	// membership tables have been created inside the migration transaction.
	require.NoError(t, db.Exec("CREATE INDEX ux_membership_recovery_receipt ON users (id)").Error)
	_, err = ApplyMembershipBillingRecoveryExpand(ctx, db, Options{AuthorizeMutation: func(context.Context, *gorm.DB) error { return nil }})
	require.Error(t, err)
	assert.False(t, db.Migrator().HasTable("kkai_membership_billing_recovery"))
	assert.False(t, db.Migrator().HasTable("kkai_membership_billing_scan_progress"))
	var receipts int64
	require.NoError(t, db.Model(&AppliedMigration{}).Where("version = ?", MembershipBillingRecoverySchemaVersion).Count(&receipts).Error)
	assert.Zero(t, receipts)
	require.NoError(t, CheckMembershipPaymentSchema(ctx, db))

	require.NoError(t, db.Exec("DROP INDEX ux_membership_recovery_receipt").Error)
	result, err := ApplyMembershipBillingRecoveryExpand(ctx, db, Options{AuthorizeMutation: func(context.Context, *gorm.DB) error { return nil }})
	require.NoError(t, err)
	require.Len(t, result.Applied, 1)
	assert.Equal(t, MembershipBillingRecoveryPlanChecksum(), result.Applied[0].Checksum)
	require.NoError(t, CheckMembershipBillingRecoverySchema(ctx, db))
	_, err = ApplyMembershipBillingRecoveryExpand(ctx, db, Options{AuthorizeMutation: func(context.Context, *gorm.DB) error { return nil }})
	require.NoError(t, err)
	require.NoError(t, db.Model(&AppliedMigration{}).Where("version = ?", MembershipBillingRecoverySchemaVersion).Count(&receipts).Error)
	assert.EqualValues(t, 1, receipts)
}
