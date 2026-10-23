package kkaimigrate

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestInvoiceExpandPreservesCanonicalHistoryAndRequiresReceipt(t *testing.T) {
	db := newMigrationTestDB(t)
	options := prepareRequestTicketExpandFixture(t, db)
	ctx := context.Background()
	_, err := ApplyRequestTicketExpand(ctx, db, options)
	require.NoError(t, err)
	var before []AppliedMigration
	require.NoError(t, db.Order("version").Find(&before).Error)
	require.ErrorIs(t, CheckInvoiceSchema(ctx, db), ErrSchemaNotReady)
	plan, err := ApplyInvoiceExpand(ctx, db, Options{DryRun: true})
	require.NoError(t, err)
	require.Len(t, plan.Pending, 1)
	assert.EqualValues(t, InvoiceSchemaVersion, plan.Pending[0].Version)
	assert.False(t, db.Migrator().HasTable("invoice_requests"))
	denied := errors.New("missing controller proof")
	_, err = ApplyInvoiceExpand(ctx, db, Options{AuthorizeMutation: func(context.Context, *gorm.DB) error { return denied }})
	require.ErrorIs(t, err, denied)
	assert.False(t, db.Migrator().HasTable("invoice_requests"))
	item := migrationSet()[InvoiceSchemaVersion-1]
	require.NoError(t, executeMigrationSchema(db, DialectSQLite, item))
	assert.ErrorIs(t, CheckInvoiceSchema(ctx, db), ErrSchemaNotReady, "manual DDL alone cannot enable financial APIs")
	_, err = ApplyInvoiceExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, CheckInvoiceSchema(ctx, db))
	require.NoError(t, CheckRequestTicketSchema(ctx, db))
	require.NoError(t, CheckResellerApplicationSchema(ctx, db))
	require.NoError(t, CheckTenantOnlineSchema(ctx, db))
	if RuntimeMaxVersion >= InvoiceSchemaVersion {
		require.NoError(t, CheckRequired(ctx, db))
	}
	var after []AppliedMigration
	require.NoError(t, db.Order("version").Find(&after).Error)
	require.Len(t, after, len(before)+1)
	assert.Equal(t, before, after[:len(before)])
	_, err = ApplyInvoiceExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, db.Exec("DROP INDEX idx_invoice_claim").Error)
	require.NoError(t, db.Exec("CREATE INDEX idx_invoice_claim ON invoice_order_claims(tenant_id, canonical_payment_key)").Error)
	assert.ErrorIs(t, CheckInvoiceSchema(ctx, db), ErrSchemaNotReady, "claim uniqueness is part of the schema contract")
}

func TestInvoiceExpandRollsBackReceiptFailure(t *testing.T) {
	db := newMigrationTestDB(t)
	options := prepareRequestTicketExpandFixture(t, db)
	_, err := ApplyRequestTicketExpand(context.Background(), db, options)
	require.NoError(t, err)
	failure := errors.New("receipt failed")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("invoice:receipt-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "kkai_schema_migrations" {
			tx.AddError(failure)
		}
	}))
	_, err = ApplyInvoiceExpand(context.Background(), db, Options{Offline: true})
	require.ErrorIs(t, err, failure)
	assert.False(t, db.Migrator().HasTable("invoice_requests"))
}

func TestInvoiceExpandDatabaseCompatibility(t *testing.T) {
	for _, dialect := range []string{DialectMySQL, DialectPostgres} {
		t.Run(dialect, func(t *testing.T) {
			db := newImageAsyncDatabaseFixture(t, dialect)
			options := prepareRequestTicketExpandFixture(t, db)
			ctx := context.Background()
			_, err := ApplyRequestTicketExpand(ctx, db, options)
			require.NoError(t, err)
			options.Offline = true
			_, err = ApplyInvoiceExpand(ctx, db, options)
			require.NoError(t, err)
			require.NoError(t, CheckInvoiceSchema(ctx, db))
			claim := invoiceorderclaimV17{TenantID: 0, CanonicalPaymentKey: "canonical-receipt", RequestID: 1, CreatedAt: 1}
			require.NoError(t, db.Create(&claim).Error)
			duplicate := invoiceorderclaimV17{TenantID: 0, CanonicalPaymentKey: "canonical-receipt", RequestID: 2, CreatedAt: 2}
			assert.Error(t, db.Create(&duplicate).Error, "one real payment cannot be claimed by two applications")
		})
	}
}
