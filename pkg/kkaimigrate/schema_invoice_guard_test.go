package kkaimigrate

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestInvoicePostgresGuardAndReceiptAreAtomic(t *testing.T) {
	db := newImageAsyncDatabaseFixture(t, DialectPostgres)
	options := prepareRequestTicketExpandFixture(t, db)
	ctx := context.Background()
	_, err := ApplyRequestTicketExpand(ctx, db, options)
	require.NoError(t, err)
	var before []AppliedMigration
	require.NoError(t, db.Order("version").Find(&before).Error)
	precheck, err := PrecheckInvoiceExpand(ctx, db)
	require.NoError(t, err)
	require.EqualValues(t, 16, precheck.CurrentVersion)

	// A successful earlier preflight cannot authorize a source that changes
	// before the controller obtains its database lock.
	require.NoError(t, db.Exec("CREATE TABLE invoice_requests (id BIGINT PRIMARY KEY)").Error)
	called := false
	_, err = ApplyInvoiceExpand(ctx, db, Options{AuthorizeMutation: func(context.Context, *gorm.DB) error {
		called = true
		return nil
	}})
	require.ErrorIs(t, err, ErrSchemaNotReady)
	assert.False(t, called, "source drift must be rejected before controller authorization or DDL")
	require.NoError(t, db.Exec("DROP TABLE invoice_requests").Error)

	denied := errors.New("controller proof expired after preflight")
	_, err = ApplyInvoiceExpand(ctx, db, Options{AuthorizeMutation: func(ctx context.Context, locked *gorm.DB) error {
		called = true
		var holdsAdvisoryLock bool
		require.NoError(t, locked.WithContext(ctx).Raw("SELECT EXISTS (SELECT 1 FROM pg_locks WHERE locktype = 'advisory' AND pid = pg_backend_pid() AND granted)").Scan(&holdsAdvisoryLock).Error)
		require.True(t, holdsAdvisoryLock, "the proof must be revalidated inside the migration lock")
		fresh, err := PrecheckInvoiceExpand(ctx, locked)
		require.NoError(t, err)
		assert.Equal(t, precheck, fresh)
		return denied
	}})
	require.ErrorIs(t, err, denied)
	assert.True(t, called)
	precheck, err = PrecheckInvoiceExpand(ctx, db)
	require.NoError(t, err)
	assert.False(t, precheck.InvoicesPresent)
	assert.EqualValues(t, 16, precheck.CurrentVersion)

	// A receipt failure after table/index creation must roll the whole
	// PostgreSQL transaction back; a retry cannot inherit unreceipted objects.
	failure := errors.New("invoice canonical receipt write failed")
	ddlReached := false
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("invoice:guard-receipt-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "kkai_schema_migrations" {
			ddlReached = tx.Migrator().HasTable("invoice_requests")
			tx.AddError(failure)
		}
	}))
	options.AuthorizeMutation = func(context.Context, *gorm.DB) error { return nil }
	_, err = ApplyInvoiceExpand(ctx, db, options)
	require.ErrorIs(t, err, failure)
	assert.True(t, ddlReached)
	require.NoError(t, db.Callback().Create().Remove("invoice:guard-receipt-failure"))
	precheck, err = PrecheckInvoiceExpand(ctx, db)
	require.NoError(t, err)
	assert.False(t, precheck.InvoicesPresent)
	assert.EqualValues(t, 16, precheck.CurrentVersion)

	result, err := ApplyInvoiceExpand(ctx, db, options)
	require.NoError(t, err)
	require.Len(t, result.Applied, 1)
	assert.EqualValues(t, 17, result.Applied[0].Version)
	assert.Equal(t, InvoicePlanChecksum(), result.Applied[0].Checksum)
	require.NoError(t, CheckInvoiceSchema(ctx, db))
	var after []AppliedMigration
	require.NoError(t, db.Order("version").Find(&after).Error)
	require.Len(t, after, len(before)+1)
	assert.Equal(t, before, after[:len(before)])
}
