package kkaimigrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalletPaymentExpandPreservesHistoryAndRequiresCanonicalReceipt(t *testing.T) {
	db := newMigrationTestDB(t)
	options := prepareRequestTicketExpandFixture(t, db)
	ctx := context.Background()
	_, err := ApplyRequestTicketExpand(ctx, db, options)
	require.NoError(t, err)
	_, err = ApplyInvoiceExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	// The canonical tenant fixture intentionally projects only tenant columns;
	// add the existing core application columns used by the lifecycle indexes.
	for _, ddl := range []string{
		"ALTER TABLE top_ups ADD COLUMN user_id INTEGER NULL",
		"ALTER TABLE top_ups ADD COLUMN trade_no VARCHAR(255) NULL",
		"ALTER TABLE top_ups ADD COLUMN status VARCHAR(32) NULL",
		"ALTER TABLE top_ups ADD COLUMN create_time INTEGER NULL",
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	var before []AppliedMigration
	require.NoError(t, db.Order("version").Find(&before).Error)
	require.NoError(t, db.Exec("INSERT INTO top_ups (id,user_id,trade_no,status,create_time,tenant_id,paid_minor,currency) VALUES (400,7,'historical-pending','pending',1,0,100,'CNY')").Error)
	plan, err := ApplyWalletPaymentExpand(ctx, db, Options{DryRun: true})
	require.NoError(t, err)
	require.Len(t, plan.Pending, 1)
	assert.EqualValues(t, WalletPaymentSchemaVersion, plan.Pending[0].Version)
	item := migrationSet()[WalletPaymentSchemaVersion-1]
	require.NoError(t, executeMigrationSchema(db, DialectSQLite, item))
	require.ErrorIs(t, CheckWalletPaymentSchema(ctx, db), ErrSchemaNotReady, "tables alone cannot enable financial lifecycle")
	_, err = ApplyWalletPaymentExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, CheckWalletPaymentSchema(ctx, db))
	require.NoError(t, CheckInvoiceSchema(ctx, db))
	require.NoError(t, CheckRequestTicketSchema(ctx, db))
	var after []AppliedMigration
	require.NoError(t, db.Order("version").Find(&after).Error)
	require.Len(t, after, len(before)+1)
	assert.Equal(t, before, after[:len(before)])
	var old struct {
		Status                 string
		WalletLifecycleVersion *int
		ClosedAt               *int64
	}
	require.NoError(t, db.Table("top_ups").Where("id = ?", 400).First(&old).Error)
	assert.Equal(t, "pending", old.Status)
	assert.Nil(t, old.WalletLifecycleVersion)
	assert.Nil(t, old.ClosedAt)
	_, err = ApplyWalletPaymentExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, db.Exec("DROP INDEX idx_wallet_topup_request").Error)
	require.ErrorIs(t, CheckWalletPaymentSchema(ctx, db), ErrSchemaNotReady, "idempotency uniqueness belongs to the financial schema contract")
}
func TestWalletPaymentExpandDialectCatalogAndIndexBoundary(t *testing.T) {
	require.NoError(t, validateMigrationCatalog(migrationSet()))
	for _, dialect := range []string{DialectSQLite, DialectMySQL, DialectPostgres} {
		t.Run(dialect, func(t *testing.T) {
			statements := walletPaymentSchemaStatements[dialect]
			require.NotEmpty(t, statements)
			tables := map[string]struct{}{}
			for _, statement := range statements {
				require.NoError(t, validateExpandSQL(dialect, statement, tables))
				if statement.Operation == migrationOperationCreateTable {
					tokens, err := expandSQLTokens(dialect, statement.SQL)
					require.NoError(t, err)
					tables[createTableName(tokens)] = struct{}{}
				}
			}
			err := validateExpandSQL(dialect, migrationStatement{Operation: migrationOperationCreateWalletTopUpIndex, SQL: "CREATE UNIQUE INDEX wallet_bad ON users (id)"}, tables)
			require.Error(t, err)
		})
	}
}
