package kkaimigrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceRuntimeSchemaChecksInvoiceContractWithoutCrossDomainAudit(t *testing.T) {
	db := newMigrationTestDB(t)
	ctx := context.Background()
	options := prepareRequestTicketExpandFixture(t, db)
	_, err := ApplyRequestTicketExpand(ctx, db, options)
	require.NoError(t, err)
	require.ErrorIs(t, CheckInvoiceRuntimeSchema(ctx, db), ErrSchemaNotReady)

	// Physical tables alone must not enable invoice transactions.
	require.NoError(t, executeMigrationSchema(db, DialectSQLite, migrationSet()[InvoiceSchemaVersion-1]))
	require.ErrorIs(t, CheckInvoiceRuntimeSchema(ctx, db), ErrSchemaNotReady)
	_, err = ApplyInvoiceExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, CheckInvoiceRuntimeSchema(ctx, db))

	require.NoError(t, db.Exec("DROP INDEX idx_invoice_claim").Error)
	require.NoError(t, db.Exec("CREATE INDEX idx_invoice_claim ON invoice_order_claims(tenant_id, canonical_payment_key)").Error)
	assert.ErrorIs(t, CheckInvoiceRuntimeSchema(ctx, db), ErrSchemaNotReady, "payment claims require uniqueness")
	require.NoError(t, db.Exec("DROP INDEX idx_invoice_claim").Error)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX idx_invoice_claim ON invoice_order_claims(tenant_id, canonical_payment_key)").Error)
	require.NoError(t, CheckInvoiceRuntimeSchema(ctx, db))

	require.NoError(t, db.Exec("DROP TABLE request_tickets").Error)
	require.NoError(t, CheckInvoiceRuntimeSchema(ctx, db), "unrelated table damage must not block invoices")
	assert.ErrorIs(t, CheckInvoiceSchema(ctx, db), ErrSchemaNotReady, "maintenance audit still checks other domains")

	require.NoError(t, db.Exec("DROP INDEX idx_invoice_payment_receipts_receipt_key").Error)
	require.NoError(t, db.Exec("ALTER TABLE invoice_payment_receipts DROP COLUMN receipt_key").Error)
	assert.ErrorIs(t, CheckInvoiceRuntimeSchema(ctx, db), ErrSchemaNotReady, "missing invoice receipt columns must fail closed")
}
