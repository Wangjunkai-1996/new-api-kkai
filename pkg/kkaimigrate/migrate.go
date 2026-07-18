package kkaimigrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	DialectSQLite   = "sqlite"
	DialectMySQL    = "mysql"
	DialectPostgres = "postgres"

	// CurrentVersion and CompatibleVersion remain for callers outside the delivery
	// path. New delivery code must use the explicit runtime/target names.
	CurrentVersion    int64 = MigrationTargetVersion
	CompatibleVersion int64 = RuntimeMaxVersion // May exceed CurrentVersion in a rollback-compatible image.

	RiskSchemaVersion           int64 = 1
	LedgerSchemaVersion         int64 = 2
	JobLeaseSchemaVersion       int64 = 3
	OutboxEventKeySchemaVersion int64 = 4
)

var (
	ErrUnsupportedDialect = errors.New("unsupported KKAI migration dialect")
	ErrChecksumMismatch   = errors.New("KKAI migration checksum mismatch")
	ErrFutureMigration    = errors.New("database contains an unknown KKAI migration")
	ErrSchemaNotReady     = errors.New("KKAI schema is not ready")
	ErrUnsafeMigration    = errors.New("KKAI migration catalog is not safe for automatic execution")
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
	Version               int64
	Name                  string
	Kind                  string
	ImplementationID      string
	ChecksumVersion       int
	Statements            map[string][]migrationStatement
	Indexes               []indexSpec
	LegacyImportSpec      string
	LegacyImportID        string
	UpstreamSchemaVersion int
	ImportLegacy          func(*gorm.DB) error
}

type migrationStatement struct {
	Operation string
	SQL       string
}

func Apply(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	return applyThroughVersion(ctx, db, options, MigrationTargetVersion, RuntimeMaxVersion)
}

func applyThroughVersion(ctx context.Context, db *gorm.DB, options Options, currentVersion int64, compatibleVersion int64) (*Result, error) {
	return applyThroughMigrationSet(ctx, db, options, currentVersion, compatibleVersion, migrationSet())
}

func applyThroughMigrationSet(ctx context.Context, db *gorm.DB, options Options, currentVersion int64, compatibleVersion int64, migrations []migration) (*Result, error) {
	if db == nil {
		return nil, ErrSchemaNotReady
	}
	if err := validateMigrationCatalog(migrations); err != nil {
		return nil, err
	}
	if currentVersion <= 0 || currentVersion > compatibleVersion || compatibleVersion > latestKnownVersionFor(migrations) {
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
				result = &Result{Pending: planThroughMigrationSet(currentVersion, migrations)}
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
		if err := validateAppliedAgainstMigrationSet(applied, compatibleVersion, migrations); err != nil {
			return err
		}
		// Migration v1 is immutable. Precreate the 191-byte-safe shape so a fresh
		// MySQL 5.7 instance can apply v1 even with the legacy 767-byte index limit.
		if dialect == DialectMySQL && !options.DryRun {
			if _, riskSchemaApplied := applied[RiskSchemaVersion]; !riskSchemaApplied {
				if err := ensureMySQL57OutboxBootstrap(lockedDB.WithContext(ctx)); err != nil {
					return err
				}
			}
		}

		result = &Result{}
		for _, item := range migrations {
			if item.Version > currentVersion {
				break
			}
			checksum := storedMigrationChecksum(item)
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

// Check verifies that the database has every migration through minimumVersion
// and that its migration history is within this runtime's compatibility range.
func Check(ctx context.Context, db *gorm.DB, minimumVersion int64) error {
	return checkThroughVersion(ctx, db, minimumVersion, RuntimeMinVersion, RuntimeMaxVersion)
}

func checkThroughVersion(ctx context.Context, db *gorm.DB, minimumVersion int64, runtimeMinVersion int64, runtimeMaxVersion int64) error {
	return checkThroughMigrationSet(ctx, db, minimumVersion, runtimeMinVersion, runtimeMaxVersion, migrationSet())
}

func checkThroughMigrationSet(ctx context.Context, db *gorm.DB, minimumVersion int64, runtimeMinVersion int64, runtimeMaxVersion int64, migrations []migration) error {
	if db == nil || runtimeMinVersion <= 0 || minimumVersion < runtimeMinVersion ||
		minimumVersion > runtimeMaxVersion || runtimeMinVersion > runtimeMaxVersion ||
		runtimeMaxVersion > latestKnownVersionFor(migrations) {
		return ErrSchemaNotReady
	}
	if err := validateMigrationCatalog(migrations); err != nil {
		return err
	}
	if !db.Migrator().HasTable((AppliedMigration{}).TableName()) {
		return ErrSchemaNotReady
	}
	applied, err := loadApplied(db.WithContext(ctx))
	if err != nil {
		return err
	}
	if err := validateAppliedAgainstMigrationSet(applied, runtimeMaxVersion, migrations); err != nil {
		return err
	}
	for _, item := range migrations {
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
	return planThroughVersion(MigrationTargetVersion)
}

func planThroughVersion(currentVersion int64) []AppliedMigration {
	return planThroughMigrationSet(currentVersion, migrationSet())
}

func planThroughMigrationSet(currentVersion int64, migrations []migration) []AppliedMigration {
	result := make([]AppliedMigration, 0, currentVersion)
	for _, item := range migrations {
		if item.Version > currentVersion {
			break
		}
		result = append(result, AppliedMigration{
			Version:  item.Version,
			Name:     item.Name,
			Checksum: storedMigrationChecksum(item),
		})
	}
	return result
}

func contractPlanThroughVersion(currentVersion int64) []AppliedMigration {
	result := make([]AppliedMigration, 0, currentVersion)
	for _, item := range migrationSet() {
		if item.Version > currentVersion {
			break
		}
		result = append(result, AppliedMigration{
			Version:  item.Version,
			Name:     item.Name,
			Checksum: migrationContractChecksum(item),
		})
	}
	return result
}

func applyMigration(db *gorm.DB, dialect string, item migration, checksum string, started time.Time) error {
	if dialect == DialectMySQL {
		for _, statement := range item.Statements[dialect] {
			if err := db.Exec(statement.SQL).Error; err != nil {
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
			if err := tx.Exec(statement.SQL).Error; err != nil {
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

func validateApplied(applied map[int64]AppliedMigration, compatibleVersion int64) error {
	return validateAppliedAgainstMigrationSet(applied, compatibleVersion, migrationSet())
}

func validateAppliedAgainstMigrationSet(applied map[int64]AppliedMigration, compatibleVersion int64, migrations []migration) error {
	known := make(map[int64]migration)
	for _, item := range migrations {
		known[item.Version] = item
	}
	for version, stored := range applied {
		item, ok := known[version]
		if !ok || version > compatibleVersion {
			return fmt.Errorf("%w: version %d", ErrFutureMigration, version)
		}
		if stored.Name != item.Name || stored.Checksum != storedMigrationChecksum(item) {
			return fmt.Errorf("%w: version %d", ErrChecksumMismatch, version)
		}
	}
	return nil
}

func latestKnownVersion() int64 {
	return latestKnownVersionFor(migrationSet())
}

func latestKnownVersionFor(migrations []migration) int64 {
	if len(migrations) == 0 {
		return 0
	}
	return migrations[len(migrations)-1].Version
}

func storedMigrationChecksum(item migration) string {
	if item.ChecksumVersion == migrationChecksumSchemaLegacy {
		return legacyMigrationChecksum(item)
	}
	return migrationContractChecksum(item)
}

func legacyMigrationChecksum(item migration) string {
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
			fmt.Fprintf(hash, "%s\n", strings.TrimSpace(statement.SQL))
		}
	}
	for _, index := range item.Indexes {
		fmt.Fprintf(hash, "index=%s:%s:%s\n", index.Name, index.Table, strings.Join(index.Columns, ","))
	}
	fmt.Fprintf(hash, "legacy=%s\n", item.LegacyImportSpec)
	return hex.EncodeToString(hash.Sum(nil))
}

func migrationContractChecksum(item migration) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "checksum_schema=%d\n", migrationChecksumSchemaCurrent)
	fmt.Fprintf(hash, "version=%d\nname=%s\nkind=%s\n", item.Version, item.Name, item.Kind)
	fmt.Fprintf(hash, "implementation_id=%s\nstored_checksum_schema=%d\n", item.ImplementationID, item.ChecksumVersion)
	dialects := make([]string, 0, len(item.Statements))
	for dialect := range item.Statements {
		dialects = append(dialects, dialect)
	}
	sort.Strings(dialects)
	for _, dialect := range dialects {
		fmt.Fprintf(hash, "dialect=%s\n", dialect)
		for _, statement := range item.Statements[dialect] {
			fmt.Fprintf(hash, "operation=%s\nsql=%s\n", statement.Operation, strings.TrimSpace(statement.SQL))
		}
	}
	for _, index := range item.Indexes {
		fmt.Fprintf(hash, "index=%s:%s:%s\n", index.Name, index.Table, strings.Join(index.Columns, ","))
	}
	fmt.Fprintf(hash, "legacy_import_id=%s\nlegacy=%s\n", item.LegacyImportID, item.LegacyImportSpec)
	upstreamBeforeDigest, upstreamAfterDigest := upstreamSchemaDigestsForVersion(item.UpstreamSchemaVersion)
	upstreamImplementationID := upstreamSchemaImplementationIDForVersion(item.UpstreamSchemaVersion)
	fmt.Fprintf(
		hash,
		"upstream_schema_version=%d\nupstream_schema_implementation_id=%s\nupstream_schema_before_digest=%s\nupstream_schema_after_digest=%s\n",
		item.UpstreamSchemaVersion,
		upstreamImplementationID,
		upstreamBeforeDigest,
		upstreamAfterDigest,
	)
	return hex.EncodeToString(hash.Sum(nil))
}

func migrationKindForRange(runtimeMinVersion, targetVersion int64, migrations []migration) (string, error) {
	if err := validateMigrationCatalog(migrations); err != nil {
		return "", err
	}
	if targetVersion == runtimeMinVersion {
		return MigrationKindNone, nil
	}
	if targetVersion < runtimeMinVersion {
		return "", ErrSchemaNotReady
	}
	found := false
	kind := MigrationKindExpand
	for _, item := range migrations {
		if item.Version <= runtimeMinVersion || item.Version > targetVersion {
			continue
		}
		found = true
		if item.Kind == MigrationKindContract {
			kind = MigrationKindContract
		}
	}
	if !found {
		return "", ErrSchemaNotReady
	}
	return kind, nil
}
