package kkaimigrate

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWalletPaymentPostgresGuardAndReceiptAreAtomic(t *testing.T) {
	db := newImageAsyncDatabaseFixture(t, DialectPostgres)
	options := prepareRequestTicketExpandFixture(t, db)
	ctx := context.Background()
	_, err := ApplyRequestTicketExpand(ctx, db, options)
	require.NoError(t, err)
	_, err = ApplyInvoiceExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	for _, ddl := range []string{
		"ALTER TABLE top_ups ADD COLUMN user_id BIGINT NULL",
		"ALTER TABLE top_ups ADD COLUMN trade_no VARCHAR(255) NULL",
		"ALTER TABLE top_ups ADD COLUMN status VARCHAR(32) NULL",
		"ALTER TABLE top_ups ADD COLUMN create_time BIGINT NULL",
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	require.NoError(t, db.Exec("INSERT INTO top_ups (id,user_id,trade_no,status,create_time,tenant_id,paid_minor,currency) VALUES (400,7,'historical-pending','pending',1,0,100,'CNY')").Error)
	var before []AppliedMigration
	require.NoError(t, db.Order("version").Find(&before).Error)
	plan, err := ApplyWalletPaymentExpand(ctx, db, Options{DryRun: true})
	require.NoError(t, err)
	require.Len(t, plan.Pending, 1)
	assert.EqualValues(t, 18, plan.Pending[0].Version)
	assert.False(t, db.Migrator().HasColumn("top_ups", "wallet_lifecycle_version"))

	denied := errors.New("wallet payment controller proof rejected inside migration lock")
	_, err = ApplyWalletPaymentExpand(ctx, db, Options{AuthorizeMutation: func(ctx context.Context, locked *gorm.DB) error {
		var holdsAdvisoryLock bool
		require.NoError(t, locked.WithContext(ctx).Raw("SELECT EXISTS (SELECT 1 FROM pg_locks WHERE locktype = 'advisory' AND pid = pg_backend_pid() AND granted)").Scan(&holdsAdvisoryLock).Error)
		require.True(t, holdsAdvisoryLock)
		return denied
	}})
	require.ErrorIs(t, err, denied)
	assert.False(t, db.Migrator().HasColumn("top_ups", "wallet_lifecycle_version"))

	failure := errors.New("wallet payment canonical receipt write failed")
	ddlReached := false
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("wallet:receipt-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "kkai_schema_migrations" {
			ddlReached = tx.Migrator().HasTable("wallet_payment_receipts")
			tx.AddError(failure)
		}
	}))
	options.AuthorizeMutation = func(context.Context, *gorm.DB) error { return nil }
	_, err = ApplyWalletPaymentExpand(ctx, db, options)
	require.ErrorIs(t, err, failure)
	assert.True(t, ddlReached)
	require.NoError(t, db.Callback().Create().Remove("wallet:receipt-failure"))
	present, err := WalletPaymentSchemaObjectsPresent(db)
	require.NoError(t, err)
	assert.False(t, present, "a receipt failure must roll all PostgreSQL DDL back")
	precheck, err := PrecheckWalletPaymentExpand(ctx, db)
	require.NoError(t, err)
	assert.EqualValues(t, 17, precheck.CurrentVersion)

	result, err := ApplyWalletPaymentExpand(ctx, db, options)
	require.NoError(t, err)
	require.Len(t, result.Applied, 1)
	assert.Equal(t, WalletPaymentPlanChecksum(), result.Applied[0].Checksum)
	require.NoError(t, CheckWalletPaymentSchema(ctx, db))
	require.NoError(t, CheckInvoiceSchema(ctx, db))
	var after []AppliedMigration
	require.NoError(t, db.Order("version").Find(&after).Error)
	require.Len(t, after, len(before)+1)
	assert.Equal(t, before, after[:len(before)])
	var historical struct {
		Status                 string
		WalletLifecycleVersion *int
		ClosedAt               *int64
	}
	require.NoError(t, db.Table("top_ups").Where("id = ?", 400).First(&historical).Error)
	assert.Equal(t, "pending", historical.Status)
	assert.Nil(t, historical.WalletLifecycleVersion)
	assert.Nil(t, historical.ClosedAt)
	_, err = ApplyWalletPaymentExpand(ctx, db, options)
	require.NoError(t, err, "the completed canonical migration is idempotent")
}
