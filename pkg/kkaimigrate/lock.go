package kkaimigrate

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"gorm.io/gorm"
)

var migrationMu sync.Mutex

func dialectName(db *gorm.DB) (string, error) {
	switch db.Dialector.Name() {
	case DialectSQLite:
		return DialectSQLite, nil
	case DialectMySQL:
		return DialectMySQL, nil
	case DialectPostgres:
		return DialectPostgres, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedDialect, db.Dialector.Name())
	}
}

func withMigrationLock(ctx context.Context, db *gorm.DB, dialect string, fn func(*gorm.DB) error) error {
	migrationMu.Lock()
	defer migrationMu.Unlock()

	switch dialect {
	case DialectPostgres, DialectMySQL:
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		conn, err := sqlDB.Conn(ctx)
		if err != nil {
			return err
		}
		defer conn.Close()
		lockedDB := db.Session(&gorm.Session{NewDB: true, Context: ctx})
		lockedDB.Statement.ConnPool = conn
		if dialect == DialectPostgres {
			if err := lockedDB.Exec("SELECT pg_advisory_lock(hashtext(?))", "kkai_schema_migrations").Error; err != nil {
				return err
			}
			defer lockedDB.WithContext(context.Background()).Exec("SELECT pg_advisory_unlock(hashtext(?))", "kkai_schema_migrations")
		} else {
			var acquired int
			if err := lockedDB.Raw("SELECT GET_LOCK(?, 30)", "kkai_schema_migrations").Scan(&acquired).Error; err != nil {
				return err
			}
			if acquired != 1 {
				return errors.New("failed to acquire KKAI migration lock")
			}
			defer lockedDB.WithContext(context.Background()).Exec("SELECT RELEASE_LOCK(?)", "kkai_schema_migrations")
		}
		return fn(lockedDB)
	}
	return fn(db.WithContext(ctx))
}

var migrationTableStatements = map[string]string{
	DialectSQLite: `CREATE TABLE IF NOT EXISTS kkai_schema_migrations (
version INTEGER PRIMARY KEY,
name VARCHAR(128) NOT NULL UNIQUE,
checksum CHAR(64) NOT NULL,
applied_at BIGINT NOT NULL,
execution_ms BIGINT NOT NULL
)`,
	DialectMySQL: `CREATE TABLE IF NOT EXISTS kkai_schema_migrations (
version BIGINT PRIMARY KEY,
name VARCHAR(128) NOT NULL UNIQUE,
checksum CHAR(64) NOT NULL,
applied_at BIGINT NOT NULL,
execution_ms BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	DialectPostgres: `CREATE TABLE IF NOT EXISTS kkai_schema_migrations (
version BIGINT PRIMARY KEY,
name VARCHAR(128) NOT NULL UNIQUE,
checksum CHAR(64) NOT NULL,
applied_at BIGINT NOT NULL,
execution_ms BIGINT NOT NULL
)`,
}
