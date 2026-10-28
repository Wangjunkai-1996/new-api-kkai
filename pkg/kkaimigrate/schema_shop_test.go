package kkaimigrate

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestShopExpandRequiresCanonicalReceiptAndPreservesPriorContracts(t *testing.T) {
	db := newMigrationTestDB(t)
	ctx := context.Background()
	options := prepareRequestTicketExpandFixture(t, db)
	_, err := ApplyRequestTicketExpand(ctx, db, options)
	require.NoError(t, err)
	_, err = ApplyInvoiceExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	for _, ddl := range []string{"ALTER TABLE top_ups ADD COLUMN user_id INTEGER NULL", "ALTER TABLE top_ups ADD COLUMN create_time INTEGER NULL"} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	_, err = ApplyWalletPaymentExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	var before []AppliedMigration
	require.NoError(t, db.Order("version").Find(&before).Error)
	_, err = ApplyShopExpand(ctx, db, Options{})
	require.ErrorIs(t, err, ErrUnsafeMigration)
	plan, err := ApplyShopExpand(ctx, db, Options{DryRun: true})
	require.NoError(t, err)
	require.Len(t, plan.Pending, 1)
	assert.Equal(t, ShopSchemaVersion, plan.Pending[0].Version)
	assert.False(t, db.Migrator().HasTable("shop_orders"))
	require.NoError(t, executeMigrationSchema(db, DialectSQLite, migrationSet()[ShopSchemaVersion-1]))
	require.ErrorIs(t, CheckShopSchema(ctx, db), ErrSchemaNotReady, "tables without canonical receipt cannot enable commerce")
	_, err = ApplyShopExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, CheckShopSchema(ctx, db))
	require.NoError(t, CheckWalletPaymentSchema(ctx, db))
	require.NoError(t, CheckInvoiceSchema(ctx, db))
	require.NoError(t, CheckRequestTicketSchema(ctx, db))
	require.NoError(t, CheckTenantOnlineSchema(ctx, db))
	var after []AppliedMigration
	require.NoError(t, db.Order("version").Find(&after).Error)
	require.Len(t, after, len(before)+1)
	assert.Equal(t, before, after[:len(before)])
	// Unsold units may share NULL reservations; a real reservation is exclusive.
	for _, sql := range []string{
		"INSERT INTO shop_stock_units (public_id,version,created_at,updated_at,reserved_order_id) VALUES ('one',1,1,1,NULL)",
		"INSERT INTO shop_stock_units (public_id,version,created_at,updated_at,reserved_order_id) VALUES ('two',1,1,1,NULL)",
		"UPDATE shop_stock_units SET reserved_order_id=10 WHERE public_id='one'",
	} {
		require.NoError(t, db.Exec(sql).Error)
	}
	assert.Error(t, db.Exec("UPDATE shop_stock_units SET reserved_order_id=10 WHERE public_id='two'").Error)
	require.NoError(t, db.Exec("DROP INDEX idx_shop_stock_units_reserved_order_id").Error)
	assert.ErrorIs(t, CheckShopSchema(ctx, db), ErrSchemaNotReady)
}

func TestShopSchemaSupportsCurrentRuntimeModels(t *testing.T) {
	db := newMigrationTestDB(t)
	require.NoError(t, executeMigrationSchema(db, DialectSQLite, migrationSet()[ShopSchemaVersion-1]))
	// The application cannot silently gain a column, constraint or index absent
	// from its immutable migration. This validates the runtime against real DDL.
	for _, value := range model.ShopSchemaModels() {
		statement := &gorm.Statement{DB: db}
		require.NoError(t, statement.Parse(value))
		t.Run(statement.Schema.Table, func(t *testing.T) {
			require.NoError(t, (tenantOnlineStep{table: statement.Schema.Table, model: value}).validate(db))
			columns, err := db.Migrator().ColumnTypes(value)
			require.NoError(t, err)
			assert.Len(t, columns, len(statement.Schema.DBNames))
		})
	}
}

func TestShopExpandCatalogIsAdditiveAcrossDialects(t *testing.T) {
	require.NoError(t, validateMigrationCatalog(migrationSet()))
	for _, dialect := range []string{DialectSQLite, DialectMySQL, DialectPostgres} {
		t.Run(dialect, func(t *testing.T) {
			tables := map[string]struct{}{}
			for _, statement := range shopSchemaStatements[dialect] {
				require.NoError(t, validateExpandSQL(dialect, statement, tables))
				if statement.Operation == migrationOperationCreateTable {
					tokens, err := expandSQLTokens(dialect, statement.SQL)
					require.NoError(t, err)
					tables[createTableName(tokens)] = struct{}{}
				}
			}
			assert.Len(t, tables, 16)
		})
	}
}
