package relay

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The cache regressions run against an explicitly selected local disposable
// database. Each invocation owns only its random schema, never existing data.
func openRelayTestPostgres(dsn string) (*gorm.DB, func(), error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, nil, err
	}
	hosts := []string{config.Host}
	for _, fallback := range config.Fallbacks {
		hosts = append(hosts, fallback.Host)
	}
	for _, host := range hosts {
		if host != "127.0.0.1" && host != "::1" && host != "localhost" {
			return nil, nil, fmt.Errorf("auth cache tests require a loopback PostgreSQL host")
		}
	}
	if !strings.HasPrefix(config.Database, "kkai_test_") {
		return nil, nil, fmt.Errorf("auth cache tests require a kkai_test_ database")
	}
	config.ConnectTimeout = 5 * time.Second
	config.RuntimeParams["statement_timeout"] = "5000"
	config.RuntimeParams["lock_timeout"] = "4000"
	admin := stdlib.OpenDB(*config)
	schema := "auth_cache_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(`CREATE SCHEMA "` + schema + `"`); err != nil {
		_ = admin.Close()
		return nil, nil, err
	}
	config.RuntimeParams["search_path"] = schema
	sqlDB := stdlib.OpenDB(*config)
	cleanup := func() {
		_ = sqlDB.Close()
		if _, err := admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`); err != nil {
			panic("failed to clean auth cache test schema: " + err.Error())
		}
		_ = admin.Close()
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return db, cleanup, nil
}

func setupRC49TaskDB(t *testing.T) {
	t.Helper()
	dsn := os.Getenv("TEST_AUTH_CACHE_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires isolated TEST_AUTH_CACHE_POSTGRES_DSN")
	}
	db, cleanup, err := openRelayTestPostgres(dsn)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.KKAIOutboxEvent{}, &model.Channel{}, &model.User{}))
	oldDB, oldType, oldRedis := model.DB, common.MainDatabaseType(), common.RedisEnabled
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
	common.RedisEnabled = false
	t.Cleanup(func() { model.DB = oldDB; common.SetMainDatabaseType(oldType); common.RedisEnabled = oldRedis })
}
