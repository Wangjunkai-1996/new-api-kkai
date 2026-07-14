package kkaimigrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

const (
	DialectSQLite   = "sqlite"
	DialectMySQL    = "mysql"
	DialectPostgres = "postgres"

	CurrentVersion        int64 = 3
	RiskSchemaVersion     int64 = 1
	LedgerSchemaVersion   int64 = 2
	JobLeaseSchemaVersion int64 = 3
)

var (
	ErrUnsupportedDialect = errors.New("unsupported KKAI migration dialect")
	ErrChecksumMismatch   = errors.New("KKAI migration checksum mismatch")
	ErrFutureMigration    = errors.New("database contains an unknown KKAI migration")
	ErrSchemaNotReady     = errors.New("KKAI schema is not ready")

	migrationMu sync.Mutex
)

type AppliedMigration struct {
	Version     int64  `json:"version"`
	Name        string `json:"name"`
	Checksum    string `json:"checksum"`
	AppliedAt   int64  `json:"applied_at"`
	ExecutionMS int64  `json:"execution_ms"`
}

func (AppliedMigration) TableName() string {
	return "kkai_schema_migrations"
}

type Result struct {
	Applied []AppliedMigration
	Pending []AppliedMigration
}

type Options struct {
	DryRun bool
}

type indexSpec struct {
	Name    string
	Table   string
	Columns []string
}

type migration struct {
	Version          int64
	Name             string
	Statements       map[string][]string
	Indexes          []indexSpec
	LegacyImportSpec string
	ImportLegacy     func(*gorm.DB) error
}

func Apply(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	if db == nil {
		return nil, ErrSchemaNotReady
	}
	dialect, err := dialectName(db)
	if err != nil {
		return nil, err
	}

	var result *Result
	err = withMigrationLock(ctx, db, dialect, func(lockedDB *gorm.DB) error {
		if !lockedDB.Migrator().HasTable((AppliedMigration{}).TableName()) {
			if options.DryRun {
				result = &Result{Pending: Plan()}
				return nil
			}
			if err := ensureMigrationTable(lockedDB, dialect); err != nil {
				return err
			}
		}
		applied, err := loadApplied(lockedDB)
		if err != nil {
			return err
		}
		if err := validateApplied(applied); err != nil {
			return err
		}

		result = &Result{}
		for _, item := range migrationSet() {
			checksum := migrationChecksum(item)
			if stored, ok := applied[item.Version]; ok {
				result.Applied = append(result.Applied, stored)
				continue
			}
			pending := AppliedMigration{Version: item.Version, Name: item.Name, Checksum: checksum}
			result.Pending = append(result.Pending, pending)
			if options.DryRun {
				continue
			}

			started := time.Now()
			if err := applyMigration(lockedDB.WithContext(ctx), dialect, item, checksum, started); err != nil {
				return fmt.Errorf("apply KKAI migration %d %s: %w", item.Version, item.Name, err)
			}
			pending.AppliedAt = started.Unix()
			pending.ExecutionMS = time.Since(started).Milliseconds()
			result.Applied = append(result.Applied, pending)
		}
		if !options.DryRun {
			result.Pending = nil
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func Check(ctx context.Context, db *gorm.DB, minimumVersion int64) error {
	if db == nil || minimumVersion <= 0 || minimumVersion > CurrentVersion {
		return ErrSchemaNotReady
	}
	if !db.Migrator().HasTable((AppliedMigration{}).TableName()) {
		return ErrSchemaNotReady
	}
	applied, err := loadApplied(db.WithContext(ctx))
	if err != nil {
		return err
	}
	if err := validateApplied(applied); err != nil {
		return err
	}
	for _, item := range migrationSet() {
		if item.Version > minimumVersion {
			break
		}
		if _, ok := applied[item.Version]; !ok {
			return fmt.Errorf("%w: missing version %d", ErrSchemaNotReady, item.Version)
		}
	}
	return nil
}

func Plan() []AppliedMigration {
	items := migrationSet()
	result := make([]AppliedMigration, 0, len(items))
	for _, item := range items {
		result = append(result, AppliedMigration{
			Version:  item.Version,
			Name:     item.Name,
			Checksum: migrationChecksum(item),
		})
	}
	return result
}

func applyMigration(db *gorm.DB, dialect string, item migration, checksum string, started time.Time) error {
	if dialect == DialectMySQL {
		for _, statement := range item.Statements[dialect] {
			if err := db.Exec(statement).Error; err != nil {
				return err
			}
		}
		for _, index := range item.Indexes {
			if err := ensureIndex(db, index); err != nil {
				return err
			}
		}
		return db.Transaction(func(tx *gorm.DB) error {
			return importLegacyAndRecord(tx, item, checksum, started)
		})
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, statement := range item.Statements[dialect] {
			if err := tx.Exec(statement).Error; err != nil {
				return err
			}
		}
		for _, index := range item.Indexes {
			if err := ensureIndex(tx, index); err != nil {
				return err
			}
		}
		return importLegacyAndRecord(tx, item, checksum, started)
	})
}

func importLegacyAndRecord(tx *gorm.DB, item migration, checksum string, started time.Time) error {
	if item.ImportLegacy != nil {
		if err := item.ImportLegacy(tx); err != nil {
			return err
		}
	}
	record := AppliedMigration{
		Version:     item.Version,
		Name:        item.Name,
		Checksum:    checksum,
		AppliedAt:   started.Unix(),
		ExecutionMS: time.Since(started).Milliseconds(),
	}
	return tx.Create(&record).Error
}

func ensureMigrationTable(db *gorm.DB, dialect string) error {
	statement, ok := migrationTableStatements[dialect]
	if !ok {
		return ErrUnsupportedDialect
	}
	return db.Exec(statement).Error
}

func ensureIndex(db *gorm.DB, index indexSpec) error {
	if db.Migrator().HasIndex(index.Table, index.Name) {
		return nil
	}
	statement := fmt.Sprintf(
		"CREATE INDEX %s ON %s (%s)",
		index.Name,
		index.Table,
		strings.Join(index.Columns, ", "),
	)
	return db.Exec(statement).Error
}

func loadApplied(db *gorm.DB) (map[int64]AppliedMigration, error) {
	var rows []AppliedMigration
	if err := db.Order("version ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[int64]AppliedMigration, len(rows))
	for _, row := range rows {
		result[row.Version] = row
	}
	return result, nil
}

func validateApplied(applied map[int64]AppliedMigration) error {
	known := make(map[int64]migration)
	for _, item := range migrationSet() {
		known[item.Version] = item
	}
	for version, stored := range applied {
		item, ok := known[version]
		if !ok {
			return fmt.Errorf("%w: version %d", ErrFutureMigration, version)
		}
		if stored.Name != item.Name || stored.Checksum != migrationChecksum(item) {
			return fmt.Errorf("%w: version %d", ErrChecksumMismatch, version)
		}
	}
	return nil
}

func migrationChecksum(item migration) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "version=%d\nname=%s\n", item.Version, item.Name)
	dialects := make([]string, 0, len(item.Statements))
	for dialect := range item.Statements {
		dialects = append(dialects, dialect)
	}
	sort.Strings(dialects)
	for _, dialect := range dialects {
		fmt.Fprintf(hash, "dialect=%s\n", dialect)
		for _, statement := range item.Statements[dialect] {
			fmt.Fprintf(hash, "%s\n", strings.TrimSpace(statement))
		}
	}
	for _, index := range item.Indexes {
		fmt.Fprintf(hash, "index=%s:%s:%s\n", index.Name, index.Table, strings.Join(index.Columns, ","))
	}
	fmt.Fprintf(hash, "legacy=%s\n", item.LegacyImportSpec)
	return hex.EncodeToString(hash.Sum(nil))
}

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

func migrationSet() []migration {
	return []migration{
		{
			Version:          1,
			Name:             "risk_incidents_and_outbox",
			Statements:       riskSchemaStatements,
			Indexes:          riskIndexes,
			LegacyImportSpec: "copy policy_incident_events by legacy id; omit token names and raw content; never replay actions",
			ImportLegacy:     importLegacyPolicyIncidents,
		},
		{
			Version:          2,
			Name:             "internal_balance_ledger",
			Statements:       ledgerSchemaStatements,
			Indexes:          ledgerIndexes,
			LegacyImportSpec: "copy internal_balance_adjustments by operation_id; preserve balances and reversal link; never reapply delta",
			ImportLegacy:     importLegacyBalanceAdjustments,
		},
		{
			Version:    3,
			Name:       "background_job_leases",
			Statements: jobLeaseSchemaStatements,
			Indexes:    jobLeaseIndexes,
		},
	}
}
