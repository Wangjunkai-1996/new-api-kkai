package model

import (
	"fmt"
	"strings"
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
func openAuthCacheTestPostgres(dsn string) (*gorm.DB, func(), error) {
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
