package kkaimigrate

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The shared fixture creates and removes a unique database/schema. These
// opt-in DSNs additionally require a dedicated database on the local machine.
func TestShopRealDatabaseSchemaContract(t *testing.T) {
	for _, dialect := range []string{DialectMySQL, DialectPostgres} {
		t.Run(dialect, func(t *testing.T) {
			// The immutable v18 bootstrap currently uses the MySQL reserved
			// word cursor without quoting it. Keep its reproduction explicit;
			// the independent v19 MySQL DDL contract is exercised below.
			if dialect == DialectMySQL && os.Getenv("KKAI_SHOP_MYSQL_BOOTSTRAP_TEST") != "1" {
				t.Skip("v18 MySQL bootstrap is blocked by unquoted cursor; set KKAI_SHOP_MYSQL_BOOTSTRAP_TEST=1 to reproduce")
			}
			db := newShopSchemaDatabaseFixture(t, dialect)
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
			require.NoError(t, CheckWalletPaymentSchema(ctx, db))
			require.ErrorIs(t, CheckShopSchema(ctx, db), ErrSchemaNotReady)
			require.NoError(t, db.Table("users").Where("id = ?", 101).Update("quota", 12345).Error)
			require.NoError(t, db.Table("top_ups").Create(map[string]any{"id": 400, "user_id": 101, "create_time": 1, "tenant_id": 0, "paid_minor": 123, "currency": "CNY", "wallet_lifecycle_version": 1, "version": 2}).Error)
			var before []AppliedMigration
			require.NoError(t, db.Order("version").Find(&before).Error)
			require.Equal(t, WalletPaymentSchemaVersion, before[len(before)-1].Version)

			_, err = ApplyShopExpand(ctx, db, Options{})
			require.ErrorIs(t, err, ErrUnsafeMigration)
			plan, err := ApplyShopExpand(ctx, db, Options{DryRun: true})
			require.NoError(t, err)
			require.Len(t, plan.Pending, 1)
			assert.Equal(t, ShopSchemaVersion, plan.Pending[0].Version)
			assert.False(t, db.Migrator().HasTable("shop_orders"))
			result, err := ApplyShopExpand(ctx, db, Options{Offline: true})
			require.NoError(t, err)
			require.Len(t, result.Applied, 1)
			assert.Equal(t, ShopPlanChecksum(), result.Applied[0].Checksum)
			require.NoError(t, CheckShopSchema(ctx, db))
			require.NoError(t, CheckWalletPaymentSchema(ctx, db))
			require.NoError(t, CheckInvoiceSchema(ctx, db))
			require.NoError(t, CheckRequestTicketSchema(ctx, db))
			require.NoError(t, CheckTenantOnlineSchema(ctx, db))

			for _, value := range model.ShopSchemaModels() {
				statement := &gorm.Statement{DB: db}
				require.NoError(t, statement.Parse(value))
				require.NoError(t, (tenantOnlineStep{table: statement.Schema.Table, model: value}).validate(db))
				columns, err := db.Migrator().ColumnTypes(value)
				require.NoError(t, err)
				assert.Len(t, columns, len(statement.Schema.DBNames), statement.Schema.Table)
			}
			settings, err := model.GetShopSettings(db)
			require.NoError(t, err)
			assert.False(t, settings.SalesEnabled, "installing schema must not enable sales")
			category := model.ShopCategory{ShopRecord: model.ShopRecord{PublicID: "sc_replay", Version: 1}, Name: "保留商品分类"}
			require.NoError(t, db.Create(&category).Error)
			_, err = ApplyShopExpand(ctx, db, Options{Offline: true})
			require.NoError(t, err)
			var preserved model.ShopCategory
			require.NoError(t, db.First(&preserved, category.ID).Error)
			assert.Equal(t, category, preserved)
			var after []AppliedMigration
			require.NoError(t, db.Order("version").Find(&after).Error)
			require.Len(t, after, len(before)+1)
			assert.Equal(t, before, after[:len(before)])
			var quota int64
			require.NoError(t, db.Table("users").Where("id = ?", 101).Pluck("quota", &quota).Error)
			assert.EqualValues(t, 12345, quota)
			var topUp struct{ PaidMinor, WalletLifecycleVersion, Version int64 }
			require.NoError(t, db.Table("top_ups").Where("id = ?", 400).Take(&topUp).Error)
			assert.EqualValues(t, 123, topUp.PaidMinor)
			assert.EqualValues(t, 1, topUp.WalletLifecycleVersion)
			assert.EqualValues(t, 2, topUp.Version)
			require.NoError(t, db.Table("top_ups").Where("id = ? AND version = ?", 400, 2).Update("version", 3).Error)
			require.NoError(t, db.Table("top_ups").Where("id = ?", 400).Take(&topUp).Error)
			assert.EqualValues(t, 3, topUp.Version, "existing wallet rows remain writable")

			receipt := result.Applied[0]
			require.NoError(t, db.Where("version = ?", ShopSchemaVersion).Delete(&AppliedMigration{}).Error)
			require.ErrorIs(t, CheckShopSchema(ctx, db), ErrSchemaNotReady, "physical tables cannot replace the canonical receipt")
			require.NoError(t, db.Create(&receipt).Error)
			require.NoError(t, CheckShopSchema(ctx, db))
			require.NoError(t, db.Model(&AppliedMigration{}).Where("version = ?", ShopSchemaVersion).Update("checksum", strings.Repeat("0", 64)).Error)
			require.Error(t, CheckShopSchema(ctx, db), "a mismatched receipt must fail closed")
			require.NoError(t, db.Model(&AppliedMigration{}).Where("version = ?", ShopSchemaVersion).Update("checksum", receipt.Checksum).Error)
			require.NoError(t, CheckShopSchema(ctx, db))
			require.NoError(t, db.Migrator().DropIndex(&model.ShopStockUnit{}, "idx_shop_stock_units_reserved_order_id"))
			require.ErrorIs(t, CheckShopSchema(ctx, db), ErrSchemaNotReady, "canonical receipt cannot conceal lost stock uniqueness")
		})
	}
}

func TestShopMySQLSchemaDDLContract(t *testing.T) {
	db := newShopSchemaDatabaseFixture(t, DialectMySQL)
	require.NoError(t, executeMigrationSchema(db, DialectMySQL, migrationSet()[ShopSchemaVersion-1]))
	require.NoError(t, validateShopSchema(db, true))
	for _, value := range model.ShopSchemaModels() {
		statement := &gorm.Statement{DB: db}
		require.NoError(t, statement.Parse(value))
		require.NoError(t, (tenantOnlineStep{table: statement.Schema.Table, model: value}).validate(db))
		columns, err := db.Migrator().ColumnTypes(value)
		require.NoError(t, err)
		assert.Len(t, columns, len(statement.Schema.DBNames), statement.Schema.Table)
	}
	first := model.ShopStockUnit{ShopRecord: model.ShopRecord{PublicID: "su_first", Version: 1}, IdentityFingerprint: "one"}
	second := model.ShopStockUnit{ShopRecord: model.ShopRecord{PublicID: "su_second", Version: 1}, IdentityFingerprint: "two"}
	require.NoError(t, db.Create(&first).Error)
	require.NoError(t, db.Create(&second).Error)
	require.NoError(t, db.Model(&first).Update("reserved_order_id", 10).Error)
	err := db.Model(&second).Update("reserved_order_id", 10).Error
	var duplicate *mysqldriver.MySQLError
	require.ErrorAs(t, err, &duplicate)
	assert.EqualValues(t, 1062, duplicate.Number)
	require.NoError(t, executeMigrationSchema(db, DialectMySQL, migrationSet()[ShopSchemaVersion-1]))
	var preserved model.ShopStockUnit
	require.NoError(t, db.First(&preserved, first.ID).Error)
	require.NotNil(t, preserved.ReservedOrderID)
	assert.EqualValues(t, 10, *preserved.ReservedOrderID)
	require.NoError(t, db.Migrator().DropIndex(&model.ShopStockUnit{}, "idx_shop_stock_units_reserved_order_id"))
	require.Error(t, validateShopSchema(db, true))
}

func newShopSchemaDatabaseFixture(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	variable := "KKAI_SHOP_" + strings.ToUpper(dialect) + "_TEST_DSN"
	dsn := os.Getenv(variable)
	if dsn == "" {
		t.Skip(variable + " is not set; isolated real database acceptance was not run")
	}
	if dialect == DialectMySQL {
		config, err := mysqldriver.ParseDSN(dsn)
		require.NoError(t, err)
		require.Equal(t, "tcp", config.Net)
		host, _, err := net.SplitHostPort(config.Addr)
		require.NoError(t, err)
		require.Contains(t, []string{"127.0.0.1", "::1", "localhost"}, host)
		require.True(t, strings.HasPrefix(config.DBName, "kkai_test_"))
	} else {
		config, err := pgx.ParseConfig(dsn)
		require.NoError(t, err)
		require.Contains(t, []string{"127.0.0.1", "::1", "localhost"}, config.Host)
		for _, fallback := range config.Fallbacks {
			require.Contains(t, []string{"127.0.0.1", "::1", "localhost"}, fallback.Host)
		}
		require.True(t, strings.HasPrefix(config.Database, "kkai_test_"))
	}
	t.Setenv("KKAI_IMAGE_ASYNC_"+strings.ToUpper(dialect)+"_TEST_DSN", dsn)
	return newImageAsyncDatabaseFixture(t, dialect)
}
