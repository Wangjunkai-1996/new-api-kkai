package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func openAuthCachePostgresTestDB(dsn string) (*gorm.DB, func() error, error) {
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
		return nil, nil, fmt.Errorf("auth cache tests require a dedicated kkai_test_ database")
	}
	config.ConnectTimeout = 5 * time.Second
	config.RuntimeParams["statement_timeout"] = "5000"
	config.RuntimeParams["lock_timeout"] = "5000"
	admin := stdlib.OpenDB(*config)
	schema := "auth_cache_service_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		_ = admin.Close()
		return nil, nil, err
	}
	var pool *sql.DB
	cleanup := func() error {
		var closeErr error
		if pool != nil {
			closeErr = pool.Close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, dropErr := admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE")
		return errors.Join(closeErr, dropErr, admin.Close())
	}
	dataConfig := config.Copy()
	dataConfig.RuntimeParams["search_path"] = schema
	pool = stdlib.OpenDB(*dataConfig)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{})
	if err != nil {
		return nil, nil, errors.Join(err, cleanup())
	}
	return db, cleanup, nil
}
