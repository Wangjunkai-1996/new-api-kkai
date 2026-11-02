package kkaimigrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMembershipPaymentPostgresAtomicFailureAndReplay(t *testing.T) {
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

	// PostgreSQL index names are schema-wide. Force a failure after both
	// membership tables have been created inside the migration transaction.
	require.NoError(t, db.Exec("CREATE INDEX ux_membership_payment_check_trade ON users (id)").Error)
	_, err = ApplyMembershipPaymentExpand(ctx, db, Options{AuthorizeMutation: func(context.Context, *gorm.DB) error { return nil }})
	require.Error(t, err)
	assert.False(t, db.Migrator().HasTable("kkai_membership_payment_checks"))
	assert.False(t, db.Migrator().HasTable("kkai_membership_payment_evidence"))
	var receipts int64
	require.NoError(t, db.Model(&AppliedMigration{}).Where("version = ?", MembershipPaymentSchemaVersion).Count(&receipts).Error)
	assert.Zero(t, receipts)
	require.NoError(t, CheckShopSchema(ctx, db))

	require.NoError(t, db.Exec("DROP INDEX ux_membership_payment_check_trade").Error)
	result, err := ApplyMembershipPaymentExpand(ctx, db, Options{AuthorizeMutation: func(context.Context, *gorm.DB) error { return nil }})
	require.NoError(t, err)
	require.Len(t, result.Applied, 1)
	assert.Equal(t, MembershipPaymentPlanChecksum(), result.Applied[0].Checksum)
	require.NoError(t, CheckMembershipPaymentSchema(ctx, db))
	_, err = ApplyMembershipPaymentExpand(ctx, db, Options{AuthorizeMutation: func(context.Context, *gorm.DB) error { return nil }})
	require.NoError(t, err)
	require.NoError(t, db.Model(&AppliedMigration{}).Where("version = ?", MembershipPaymentSchemaVersion).Count(&receipts).Error)
	assert.EqualValues(t, 1, receipts)
}
