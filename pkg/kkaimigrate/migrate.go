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

	RiskSchemaVersion                int64 = 1
	LedgerSchemaVersion              int64 = 2
	JobLeaseSchemaVersion            int64 = 3
	OutboxEventKeySchemaVersion      int64 = 4
	VideoStudioSchemaVersion         int64 = 5
	VideoSampleCategorySchemaVersion int64 = 6
	ImageStudioSchemaVersion         int64 = 7
	AuthenticationSchemaVersion      int64 = 8
)

var (
	ErrUnsupportedDialect = errors.New("unsupported KKAI migration dialect")
	ErrChecksumMismatch   = errors.New("KKAI migration checksum mismatch")
	ErrFutureMigration    = errors.New("database contains an unknown KKAI migration")
	ErrMigrationHole      = errors.New("KKAI migration ledger contains a missing prefix")
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
	Version          int64
	Name             string
	Kind             string
	ImplementationID string
	ChecksumVersion  int
	Statements       map[string][]migrationStatement
	Indexes          []indexSpec
	LegacyImportSpec string
	LegacyImportID   string
	ImportLegacy     func(*gorm.DB) error
	BackfillSpec     string
	BackfillID       string
	Backfill         func(*gorm.DB) error
	ApplyDialects    []string
	LegacyDialects   []string
}

type migrationStatement struct {
	Operation string
	SQL       string
}

func Apply(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	if db == nil {
		return nil, ErrSchemaNotReady
	}
	dialect, err := dialectName(db)
	if err != nil {
		return nil, err
	}
	contract, err := ContractForDialect(dialect)
	if err != nil {
		return nil, err
	}
	return applyThroughVersion(ctx, db, options, contract.MigrationTargetVersion, contract.RuntimeMaxVersion)
}

// ApplyOutboxEventKeyCompatibility applies the cross-dialect v4 maintenance.
// SQLite records the migration as a no-op while MySQL and PostgreSQL normalize
// kkai_outbox.event_key to the same 191-character shape.
func ApplyOutboxEventKeyCompatibility(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	if db == nil {
		return nil, ErrSchemaNotReady
	}
	if err := checkThroughVersion(ctx, db, JobLeaseSchemaVersion, JobLeaseSchemaVersion, MaxCompatibleVersion); err != nil {
		return nil, fmt.Errorf("KKAI maintenance target %d requires runtime schema %d: %w", OutboxEventKeySchemaVersion, JobLeaseSchemaVersion, err)
	}
	return applyThroughVersion(ctx, db, options, OutboxEventKeySchemaVersion, MaxCompatibleVersion)
}

// ApplyMySQL57Compatibility is kept for existing operators and automation.
func ApplyMySQL57Compatibility(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	if db == nil {
		return nil, ErrSchemaNotReady
	}
	dialect, err := dialectName(db)
	if err != nil {
		return nil, err
	}
	if dialect != DialectMySQL {
		return nil, fmt.Errorf("%w: MySQL 5.7 compatibility maintenance requires mysql", ErrUnsupportedDialect)
	}
	return ApplyOutboxEventKeyCompatibility(ctx, db, options)
}

// ApplyVideoStudioExpand applies the additive v5 Video Studio schema after v4.
// Operators invoke this separately from application delivery after both
// rollback slots can understand v5.
func ApplyVideoStudioExpand(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	if db == nil {
		return nil, ErrSchemaNotReady
	}
	if err := checkThroughVersion(ctx, db, OutboxEventKeySchemaVersion, OutboxEventKeySchemaVersion, MaxCompatibleVersion); err != nil {
		return nil, fmt.Errorf("KKAI maintenance target %d requires validated bridge schema %d: %w", VideoStudioSchemaVersion, OutboxEventKeySchemaVersion, err)
	}
	return applyThroughVersion(ctx, db, options, VideoStudioSchemaVersion, MaxCompatibleVersion)
}

// ApplyVideoSampleCategoryExpand adds the nullable v6 category column after
// the complete v5 Video Studio schema has been validated.
func ApplyVideoSampleCategoryExpand(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	if db == nil {
		return nil, ErrSchemaNotReady
	}
	if err := checkThroughVersion(ctx, db, VideoStudioSchemaVersion, VideoStudioSchemaVersion, MaxCompatibleVersion); err != nil {
		return nil, fmt.Errorf(
			"KKAI maintenance target %d requires validated Video Studio schema %d: %w",
			VideoSampleCategorySchemaVersion, VideoStudioSchemaVersion, err,
		)
	}
	return applyThroughVersion(ctx, db, options, VideoSampleCategorySchemaVersion, MaxCompatibleVersion)
}

// ApplyImageStudioExpand creates only the additive Image Studio tables after
// the complete v6 Video Studio schema has been validated.
func ApplyImageStudioExpand(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	if db == nil {
		return nil, ErrSchemaNotReady
	}
	if err := checkThroughVersion(ctx, db, VideoSampleCategorySchemaVersion, VideoSampleCategorySchemaVersion, MaxCompatibleVersion); err != nil {
		return nil, fmt.Errorf(
			"KKAI maintenance target %d requires validated Video Studio schema %d: %w",
			ImageStudioSchemaVersion, VideoSampleCategorySchemaVersion, err,
		)
	}
	return applyThroughVersion(ctx, db, options, ImageStudioSchemaVersion, MaxCompatibleVersion)
}

// ApplyAuthenticationExpand adds the stateless dashboard authentication
// control plane after the complete v7 Image Studio schema has been validated.
func ApplyAuthenticationExpand(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	if db == nil {
		return nil, ErrSchemaNotReady
	}
	if err := checkThroughVersion(ctx, db, ImageStudioSchemaVersion, ImageStudioSchemaVersion, MaxCompatibleVersion); err != nil {
		return nil, fmt.Errorf(
			"KKAI maintenance target %d requires validated Image Studio schema %d: %w",
			AuthenticationSchemaVersion, ImageStudioSchemaVersion, err,
		)
	}
	return applyThroughVersion(ctx, db, options, AuthenticationSchemaVersion, MaxCompatibleVersion)
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
				result = &Result{Pending: planItems(planItemsForDialectFromSet(dialect, currentVersion, migrations))}
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
		if err := validateAppliedAgainstMigrationSet(applied, dialect, compatibleVersion, migrations); err != nil {
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
			if !item.appliesTo(dialect) {
				continue
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
	if db == nil {
		return ErrSchemaNotReady
	}
	dialect, err := dialectName(db)
	if err != nil {
		return err
	}
	contract, err := ContractForDialect(dialect)
	if err != nil {
		return err
	}
	return checkThroughVersion(ctx, db, minimumVersion, contract.MigrationTargetVersion, contract.RuntimeMaxVersion)
}

func CheckRequired(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return ErrSchemaNotReady
	}
	dialect, err := dialectName(db)
	if err != nil {
		return err
	}
	contract, err := ContractForDialect(dialect)
	if err != nil {
		return err
	}
	return checkThroughVersion(ctx, db, contract.RuntimeMinVersion, contract.MigrationTargetVersion, contract.RuntimeMaxVersion)
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
	dialect, err := dialectName(db)
	if err != nil {
		return err
	}
	state, err := loadValidatedStateFromMigrationSet(ctx, db, dialect, runtimeMaxVersion, migrations)
	if err != nil {
		return err
	}
	if state.currentVersion < minimumVersion {
		return fmt.Errorf("%w: current version %d is below required version %d", ErrSchemaNotReady, state.currentVersion, minimumVersion)
	}
	return nil
}

func applyMigration(db *gorm.DB, dialect string, item migration, checksum string, started time.Time) error {
	if dialect == DialectMySQL {
		for _, statement := range item.Statements[dialect] {
			if err := executeMigrationStatement(db, dialect, statement); err != nil {
				return err
			}
		}
		for _, index := range item.Indexes {
			if err := ensureIndex(db, index); err != nil {
				return err
			}
		}
		return db.Transaction(func(tx *gorm.DB) error {
			return importLegacyAndRecord(tx, dialect, item, checksum, started)
		})
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, statement := range item.Statements[dialect] {
			if err := executeMigrationStatement(tx, dialect, statement); err != nil {
				return err
			}
		}
		for _, index := range item.Indexes {
			if err := ensureIndex(tx, index); err != nil {
				return err
			}
		}
		return importLegacyAndRecord(tx, dialect, item, checksum, started)
	})
}

func executeMigrationStatement(db *gorm.DB, dialect string, statement migrationStatement) error {
	if statement.Operation == migrationOperationCreateIndex {
		table, index, err := migrationCreateIndexIdentifiers(dialect, statement.SQL)
		if err != nil {
			return err
		}
		if db.Migrator().HasIndex(table, index) {
			return nil
		}
	}
	if statement.Operation == migrationOperationAddNullableColumn {
		table, column, err := migrationAddNullableColumnIdentifiers(dialect, statement.SQL)
		if err != nil {
			return err
		}
		exists, err := migrationColumnExists(db, table, column)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}
	}
	return db.Exec(statement.SQL).Error
}

func migrationColumnExists(db *gorm.DB, table string, column string) (bool, error) {
	columnTypes, err := db.Migrator().ColumnTypes(table)
	if err != nil {
		return false, fmt.Errorf("inspect migration table %s: %w", table, err)
	}
	for _, columnType := range columnTypes {
		if strings.EqualFold(columnType.Name(), column) {
			return true, nil
		}
	}
	return false, nil
}

func migrationAddNullableColumnIdentifiers(dialect string, sql string) (string, string, error) {
	tokens, err := expandSQLTokens(dialect, sql)
	if err != nil {
		return "", "", err
	}
	if err := validateAddNullableColumn(tokens); err != nil {
		return "", "", err
	}
	columnIndex := 4
	if tokens[columnIndex] == "COLUMN" {
		columnIndex++
	}
	return migrationIdentifierValue(tokens[2]), migrationIdentifierValue(tokens[columnIndex]), nil
}

func migrationCreateIndexIdentifiers(dialect string, sql string) (string, string, error) {
	tokens, err := expandSQLTokens(dialect, sql)
	if err != nil {
		return "", "", err
	}
	position := 1
	if len(tokens) > position && tokens[position] == "UNIQUE" {
		position++
	}
	if len(tokens) <= position || tokens[position] != "INDEX" {
		return "", "", fmt.Errorf("migration create-index statement has no INDEX token")
	}
	position++
	if hasTokenPrefix(tokens[position:], "IF", "NOT", "EXISTS") {
		position += 3
	}
	if len(tokens) <= position+2 || tokens[position+1] != "ON" {
		return "", "", fmt.Errorf("migration create-index statement has no canonical target")
	}
	index := migrationIdentifierValue(tokens[position])
	table := migrationIdentifierValue(tokens[position+2])
	if index == "" || table == "" {
		return "", "", fmt.Errorf("migration create-index statement has invalid identifiers")
	}
	return table, index, nil
}

func migrationIdentifierValue(token string) string {
	token = strings.TrimPrefix(token, quotedSQLIdentifierTokenPrefix)
	if !isCanonicalSQLIdentifier(token) {
		return ""
	}
	return strings.ToLower(token)
}

func importLegacyAndRecord(tx *gorm.DB, dialect string, item migration, checksum string, started time.Time) error {
	if item.Version == VideoSampleCategorySchemaVersion {
		if err := validateVideoSampleCategoryColumn(tx, dialect); err != nil {
			return err
		}
	}
	if item.ImportLegacy != nil {
		if err := item.ImportLegacy(tx); err != nil {
			return err
		}
	}
	if item.Backfill != nil {
		if err := item.Backfill(tx); err != nil {
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

func validateApplied(applied map[int64]AppliedMigration, dialect string, compatibleVersion int64) error {
	return validateAppliedAgainstMigrationSet(applied, dialect, compatibleVersion, migrationSet())
}

func validateAppliedAgainstMigrationSet(applied map[int64]AppliedMigration, dialect string, compatibleVersion int64, migrations []migration) error {
	known := make(map[int64]migration)
	for _, item := range migrations {
		known[item.Version] = item
	}
	for version, stored := range applied {
		item, ok := known[version]
		if !ok || version > compatibleVersion {
			return fmt.Errorf("%w: version %d", ErrFutureMigration, version)
		}
		if !item.acceptsStoredDialect(dialect) {
			return fmt.Errorf("%w: version %d is not valid for %s", ErrFutureMigration, version, dialect)
		}
		if stored.Name != item.Name || stored.Checksum != storedMigrationChecksum(item) {
			return fmt.Errorf("%w: version %d", ErrChecksumMismatch, version)
		}
	}
	if len(applied) == 0 {
		return nil
	}
	maxApplied := int64(0)
	for version := range applied {
		if version > maxApplied {
			maxApplied = version
		}
	}
	for _, item := range migrations {
		if item.Version > maxApplied {
			break
		}
		if !item.appliesTo(dialect) {
			continue
		}
		if _, ok := applied[item.Version]; !ok {
			return fmt.Errorf("%w: missing version %d before version %d", ErrMigrationHole, item.Version, maxApplied)
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

func migrationChecksum(item migration) string {
	return storedMigrationChecksum(item)
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
	fmt.Fprintf(hash, "checksum_schema=%d\n", item.ChecksumVersion)
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
	if item.ChecksumVersion >= migrationChecksumSchemaBackfill {
		fmt.Fprintf(hash, "backfill_id=%s\nbackfill=%s\n", item.BackfillID, item.BackfillSpec)
	}
	applyDialects := append([]string(nil), item.ApplyDialects...)
	legacyDialects := append([]string(nil), item.LegacyDialects...)
	sort.Strings(applyDialects)
	sort.Strings(legacyDialects)
	fmt.Fprintf(hash, "apply_dialects=%s\nlegacy_dialects=%s\n", strings.Join(applyDialects, ","), strings.Join(legacyDialects, ","))
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
