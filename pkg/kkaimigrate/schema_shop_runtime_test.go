package kkaimigrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestShopRuntimeSchemaRequiresOnlyShopObjectsAndCanonicalReceipt(t *testing.T) {
	for _, dialect := range []string{DialectSQLite, DialectMySQL, DialectPostgres} {
		t.Run(dialect, func(t *testing.T) {
			var db *gorm.DB
			if dialect == DialectSQLite {
				db = newMigrationTestDB(t)
			} else {
				db = newShopSchemaDatabaseFixture(t, dialect)
			}
			ctx := context.Background()
			require.NoError(t, db.Migrator().CreateTable(&AppliedMigration{}))
			for _, item := range planItemsForDialect(dialect, ShopSchemaVersion) {
				require.NoError(t, db.Create(&AppliedMigration{
					Version: item.Version, Name: item.Name, Checksum: storedMigrationChecksum(item),
				}).Error)
			}
			require.NoError(t, executeMigrationSchema(db, dialect, migrationSet()[ShopSchemaVersion-1]))
			require.NoError(t, CheckShopRuntimeSchema(ctx, db))

			require.NoError(t, db.Where("version = ?", ShopSchemaVersion).Delete(&AppliedMigration{}).Error)
			assert.ErrorIs(t, CheckShopRuntimeSchema(ctx, db), ErrSchemaNotReady)
			require.NoError(t, db.Create(&AppliedMigration{
				Version: ShopSchemaVersion, Name: migrationSet()[ShopSchemaVersion-1].Name,
				Checksum: storedMigrationChecksum(migrationSet()[ShopSchemaVersion-1]),
			}).Error)
			require.NoError(t, db.Model(&AppliedMigration{}).Where("version = ?", ShopSchemaVersion).Update("checksum", "invalid").Error)
			assert.ErrorIs(t, CheckShopRuntimeSchema(ctx, db), ErrChecksumMismatch)
			require.NoError(t, db.Model(&AppliedMigration{}).Where("version = ?", ShopSchemaVersion).Update("checksum", storedMigrationChecksum(migrationSet()[ShopSchemaVersion-1])).Error)

			require.NoError(t, db.Migrator().DropTable("users"))
			require.NoError(t, CheckShopRuntimeSchema(ctx, db), "unrelated physical schema must not gate shop requests")
			require.NoError(t, db.Migrator().DropIndex("shop_categories", "idx_shop_category_sort"))
			require.NoError(t, CheckShopRuntimeSchema(ctx, db), "ordinary query indexes belong to the full maintenance audit")

			require.NoError(t, db.Migrator().DropIndex("shop_stock_units", "idx_shop_stock_units_reserved_order_id"))
			assert.ErrorIs(t, CheckShopRuntimeSchema(ctx, db), ErrSchemaNotReady)
			require.NoError(t, db.Exec("CREATE INDEX idx_shop_stock_units_reserved_order_id ON shop_stock_units (reserved_order_id)").Error)
			assert.ErrorIs(t, CheckShopRuntimeSchema(ctx, db), ErrSchemaNotReady, "a non-unique replacement cannot protect stock reservation")
			require.NoError(t, db.Migrator().DropIndex("shop_stock_units", "idx_shop_stock_units_reserved_order_id"))
			require.NoError(t, db.Exec("CREATE UNIQUE INDEX idx_shop_stock_units_reserved_order_id ON shop_stock_units (reserved_order_id)").Error)
			require.NoError(t, CheckShopRuntimeSchema(ctx, db))

			require.NoError(t, db.Exec("ALTER TABLE shop_settings DROP COLUMN policy_version").Error)
			assert.ErrorIs(t, CheckShopRuntimeSchema(ctx, db), ErrSchemaNotReady)
		})
	}
}
