package kkaimigrate

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestShopHTTPFixtureSchema prepares an empty disposable local database for the
// opt-in router/browser harness. Its owner removes the whole container afterward.
func TestShopHTTPFixtureSchema(t *testing.T) {
	raw := os.Getenv("KKAI_SHOP_HTTP_TEST_DSN")
	if raw == "" {
		t.Skip("local HTTP harness DSN is not set")
	}
	target := os.Getenv("KKAI_SHOP_HTTP_FIXTURE_SCHEMA")
	require.Contains(t, []string{"", "18", "19"}, target)
	config, err := pgx.ParseConfig(raw)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", config.Host)
	require.True(t, strings.HasPrefix(config.Database, "kkai_test_shop_http_"))
	require.Empty(t, config.Fallbacks)
	db, err := gorm.Open(postgres.Open(raw), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	var count int64
	require.NoError(t, db.Raw("SELECT count(*) FROM information_schema.tables WHERE table_schema='public'").Scan(&count).Error)
	require.Zero(t, count, "the fixture only accepts an empty disposable database")
	previous := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
	t.Cleanup(func() { common.SetMainDatabaseType(previous) })
	for _, statement := range []string{
		"CREATE TABLE users (id BIGINT PRIMARY KEY, telegram_id TEXT, quota BIGINT NOT NULL DEFAULT 0, deleted_at TIMESTAMP NULL)",
		"CREATE TABLE tokens (id BIGINT PRIMARY KEY)",
		"CREATE TABLE passkey_credentials (id BIGINT PRIMARY KEY)",
		"CREATE TABLE options (\"key\" VARCHAR(255) PRIMARY KEY, value TEXT)",
		"CREATE TABLE midjourneys (id BIGINT PRIMARY KEY, mj_id TEXT, channel_id BIGINT, quota BIGINT, status TEXT)",
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	ctx := context.Background()
	options := prepareRequestTicketExpandFixture(t, db)
	_, err = ApplyRequestTicketExpand(ctx, db, options)
	require.NoError(t, err)
	_, err = ApplyInvoiceExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	for _, statement := range []string{"ALTER TABLE top_ups ADD COLUMN user_id BIGINT NULL", "ALTER TABLE top_ups ADD COLUMN create_time BIGINT NULL"} {
		require.NoError(t, db.Exec(statement).Error)
	}
	_, err = ApplyWalletPaymentExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, CheckWalletPaymentSchema(ctx, db))
	if target == "18" {
		return // The root-proof rehearsal must execute schema19 through the Linux CLI.
	}
	_, err = ApplyShopExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, CheckShopSchema(ctx, db))
}
