package kkaimigrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	DialectSQLite   = "sqlite"
	DialectMySQL    = "mysql"
	DialectPostgres = "postgres"

	CurrentVersion      int64 = 2
	RiskSchemaVersion   int64 = 1
	LedgerSchemaVersion int64 = 2
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
	}
}

var riskSchemaStatements = map[string][]string{
	DialectSQLite: {
		`CREATE TABLE IF NOT EXISTS kkai_policy_incidents (
id INTEGER PRIMARY KEY AUTOINCREMENT,
event_id VARCHAR(128) NOT NULL UNIQUE,
input_sha256 CHAR(64) NOT NULL,
source VARCHAR(32) NOT NULL,
occurred_at BIGINT NOT NULL,
request_id VARCHAR(64) NOT NULL DEFAULT '',
user_id INTEGER NOT NULL DEFAULT 0,
token_id INTEGER NOT NULL DEFAULT 0,
channel_id INTEGER NOT NULL DEFAULT 0,
model_name VARCHAR(128) NOT NULL DEFAULT '',
rule_version VARCHAR(64) NOT NULL DEFAULT '',
evidence_sha256 CHAR(64) NOT NULL,
token_fingerprint VARCHAR(80) NOT NULL DEFAULT '',
upstream_key_fingerprint VARCHAR(80) NOT NULL DEFAULT '',
decision VARCHAR(32) NOT NULL,
metadata TEXT NOT NULL,
action_taken VARCHAR(255) NOT NULL DEFAULT '',
action_result VARCHAR(255) NOT NULL DEFAULT '',
token_disabled BOOLEAN NOT NULL DEFAULT FALSE,
user_disabled BOOLEAN NOT NULL DEFAULT FALSE,
user_disable_skipped BOOLEAN NOT NULL DEFAULT FALSE,
channel_disabled BOOLEAN NOT NULL DEFAULT FALSE,
created_at BIGINT NOT NULL,
updated_at BIGINT NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS kkai_outbox (
id INTEGER PRIMARY KEY AUTOINCREMENT,
event_key VARCHAR(192) NOT NULL UNIQUE,
topic VARCHAR(128) NOT NULL,
aggregate_id VARCHAR(128) NOT NULL DEFAULT '',
payload TEXT NOT NULL,
status VARCHAR(16) NOT NULL,
attempts INTEGER NOT NULL DEFAULT 0,
available_at BIGINT NOT NULL,
locked_at BIGINT NOT NULL DEFAULT 0,
locked_by VARCHAR(128) NOT NULL DEFAULT '',
last_error TEXT NOT NULL,
created_at BIGINT NOT NULL,
delivered_at BIGINT NOT NULL DEFAULT 0
)`,
	},
	DialectMySQL: {
		`CREATE TABLE IF NOT EXISTS kkai_policy_incidents (
id BIGINT AUTO_INCREMENT PRIMARY KEY,
event_id VARCHAR(128) NOT NULL UNIQUE,
input_sha256 CHAR(64) NOT NULL,
source VARCHAR(32) NOT NULL,
occurred_at BIGINT NOT NULL,
request_id VARCHAR(64) NOT NULL DEFAULT '',
user_id INT NOT NULL DEFAULT 0,
token_id INT NOT NULL DEFAULT 0,
channel_id INT NOT NULL DEFAULT 0,
model_name VARCHAR(128) NOT NULL DEFAULT '',
rule_version VARCHAR(64) NOT NULL DEFAULT '',
evidence_sha256 CHAR(64) NOT NULL,
token_fingerprint VARCHAR(80) NOT NULL DEFAULT '',
upstream_key_fingerprint VARCHAR(80) NOT NULL DEFAULT '',
decision VARCHAR(32) NOT NULL,
metadata TEXT NOT NULL,
action_taken VARCHAR(255) NOT NULL DEFAULT '',
action_result VARCHAR(255) NOT NULL DEFAULT '',
token_disabled BOOLEAN NOT NULL DEFAULT FALSE,
user_disabled BOOLEAN NOT NULL DEFAULT FALSE,
user_disable_skipped BOOLEAN NOT NULL DEFAULT FALSE,
channel_disabled BOOLEAN NOT NULL DEFAULT FALSE,
created_at BIGINT NOT NULL,
updated_at BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS kkai_outbox (
id BIGINT AUTO_INCREMENT PRIMARY KEY,
event_key VARCHAR(192) NOT NULL UNIQUE,
topic VARCHAR(128) NOT NULL,
aggregate_id VARCHAR(128) NOT NULL DEFAULT '',
payload TEXT NOT NULL,
status VARCHAR(16) NOT NULL,
attempts INT NOT NULL DEFAULT 0,
available_at BIGINT NOT NULL,
locked_at BIGINT NOT NULL DEFAULT 0,
locked_by VARCHAR(128) NOT NULL DEFAULT '',
last_error TEXT NOT NULL,
created_at BIGINT NOT NULL,
delivered_at BIGINT NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	},
	DialectPostgres: {
		`CREATE TABLE IF NOT EXISTS kkai_policy_incidents (
id BIGSERIAL PRIMARY KEY,
event_id VARCHAR(128) NOT NULL UNIQUE,
input_sha256 CHAR(64) NOT NULL,
source VARCHAR(32) NOT NULL,
occurred_at BIGINT NOT NULL,
request_id VARCHAR(64) NOT NULL DEFAULT '',
user_id INTEGER NOT NULL DEFAULT 0,
token_id INTEGER NOT NULL DEFAULT 0,
channel_id INTEGER NOT NULL DEFAULT 0,
model_name VARCHAR(128) NOT NULL DEFAULT '',
rule_version VARCHAR(64) NOT NULL DEFAULT '',
evidence_sha256 CHAR(64) NOT NULL,
token_fingerprint VARCHAR(80) NOT NULL DEFAULT '',
upstream_key_fingerprint VARCHAR(80) NOT NULL DEFAULT '',
decision VARCHAR(32) NOT NULL,
metadata TEXT NOT NULL,
action_taken VARCHAR(255) NOT NULL DEFAULT '',
action_result VARCHAR(255) NOT NULL DEFAULT '',
token_disabled BOOLEAN NOT NULL DEFAULT FALSE,
user_disabled BOOLEAN NOT NULL DEFAULT FALSE,
user_disable_skipped BOOLEAN NOT NULL DEFAULT FALSE,
channel_disabled BOOLEAN NOT NULL DEFAULT FALSE,
created_at BIGINT NOT NULL,
updated_at BIGINT NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS kkai_outbox (
id BIGSERIAL PRIMARY KEY,
event_key VARCHAR(192) NOT NULL UNIQUE,
topic VARCHAR(128) NOT NULL,
aggregate_id VARCHAR(128) NOT NULL DEFAULT '',
payload TEXT NOT NULL,
status VARCHAR(16) NOT NULL,
attempts INTEGER NOT NULL DEFAULT 0,
available_at BIGINT NOT NULL,
locked_at BIGINT NOT NULL DEFAULT 0,
locked_by VARCHAR(128) NOT NULL DEFAULT '',
last_error TEXT NOT NULL,
created_at BIGINT NOT NULL,
delivered_at BIGINT NOT NULL DEFAULT 0
)`,
	},
}

var riskIndexes = []indexSpec{
	{Name: "idx_kkai_policy_incidents_occurred", Table: "kkai_policy_incidents", Columns: []string{"occurred_at"}},
	{Name: "idx_kkai_policy_incidents_request", Table: "kkai_policy_incidents", Columns: []string{"request_id"}},
	{Name: "idx_kkai_policy_incidents_user", Table: "kkai_policy_incidents", Columns: []string{"user_id"}},
	{Name: "idx_kkai_policy_incidents_token", Table: "kkai_policy_incidents", Columns: []string{"token_id"}},
	{Name: "idx_kkai_policy_incidents_channel", Table: "kkai_policy_incidents", Columns: []string{"channel_id"}},
	{Name: "idx_kkai_outbox_delivery", Table: "kkai_outbox", Columns: []string{"status", "available_at", "locked_at"}},
	{Name: "idx_kkai_outbox_topic", Table: "kkai_outbox", Columns: []string{"topic"}},
}

var ledgerSchemaStatements = map[string][]string{
	DialectSQLite: {
		`CREATE TABLE IF NOT EXISTS kkai_internal_balance_adjustments (
id INTEGER PRIMARY KEY AUTOINCREMENT,
operation_id VARCHAR(128) NOT NULL UNIQUE,
user_id INTEGER NOT NULL,
delta BIGINT NOT NULL,
reason VARCHAR(64) NOT NULL,
metadata TEXT NOT NULL,
payload_sha256 CHAR(64) NOT NULL,
original_operation_id VARCHAR(128) UNIQUE,
balance_before BIGINT NOT NULL,
balance_after BIGINT NOT NULL,
created_at BIGINT NOT NULL
)`,
	},
	DialectMySQL: {
		`CREATE TABLE IF NOT EXISTS kkai_internal_balance_adjustments (
id BIGINT AUTO_INCREMENT PRIMARY KEY,
operation_id VARCHAR(128) NOT NULL UNIQUE,
user_id INT NOT NULL,
delta BIGINT NOT NULL,
reason VARCHAR(64) NOT NULL,
metadata TEXT NOT NULL,
payload_sha256 CHAR(64) NOT NULL,
original_operation_id VARCHAR(128) UNIQUE,
balance_before BIGINT NOT NULL,
balance_after BIGINT NOT NULL,
created_at BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	},
	DialectPostgres: {
		`CREATE TABLE IF NOT EXISTS kkai_internal_balance_adjustments (
id BIGSERIAL PRIMARY KEY,
operation_id VARCHAR(128) NOT NULL UNIQUE,
user_id INTEGER NOT NULL,
delta BIGINT NOT NULL,
reason VARCHAR(64) NOT NULL,
metadata TEXT NOT NULL,
payload_sha256 CHAR(64) NOT NULL,
original_operation_id VARCHAR(128) UNIQUE,
balance_before BIGINT NOT NULL,
balance_after BIGINT NOT NULL,
created_at BIGINT NOT NULL
)`,
	},
}

var ledgerIndexes = []indexSpec{
	{Name: "idx_kkai_balance_user", Table: "kkai_internal_balance_adjustments", Columns: []string{"user_id"}},
	{Name: "idx_kkai_balance_created", Table: "kkai_internal_balance_adjustments", Columns: []string{"created_at"}},
}

type legacyPolicyIncident struct {
	ID                     int64  `gorm:"column:id"`
	RequestID              string `gorm:"column:request_id"`
	UserID                 int    `gorm:"column:user_id"`
	TokenID                int    `gorm:"column:token_id"`
	ModelName              string `gorm:"column:model_name"`
	ChannelID              int    `gorm:"column:channel_id"`
	UpstreamKeyFingerprint string `gorm:"column:upstream_key_fingerprint"`
	EvidenceLevel          string `gorm:"column:evidence_level"`
	Causality              string `gorm:"column:causality"`
	ActionTaken            string `gorm:"column:action_taken"`
	ActionResult           string `gorm:"column:action_result"`
	Metadata               string `gorm:"column:metadata"`
	CreatedAt              int64  `gorm:"column:created_at"`
}

func importLegacyPolicyIncidents(db *gorm.DB) error {
	if !db.Migrator().HasTable("policy_incident_events") {
		return nil
	}
	var rows []legacyPolicyIncident
	if err := db.Table("policy_incident_events").Order("id ASC").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		metadata := normalizedLegacyJSON(row.Metadata)
		canonical, _ := json.Marshal(row)
		evidence := sha256Hex([]byte(fmt.Sprintf("legacy-policy:%d:%s", row.ID, row.RequestID)))
		incident := model.KKAIPolicyIncident{
			EventID:                fmt.Sprintf("legacy-policy-incident:%d", row.ID),
			InputSHA256:            sha256Hex(canonical),
			Source:                 "legacy_import",
			OccurredAt:             row.CreatedAt,
			RequestID:              row.RequestID,
			UserID:                 row.UserID,
			TokenID:                row.TokenID,
			ChannelID:              row.ChannelID,
			ModelName:              row.ModelName,
			RuleVersion:            "legacy",
			EvidenceSHA256:         evidence,
			UpstreamKeyFingerprint: row.UpstreamKeyFingerprint,
			Decision:               "legacy_record",
			Metadata:               metadata,
			ActionTaken:            row.ActionTaken,
			ActionResult:           row.ActionResult,
			TokenDisabled:          strings.Contains(row.ActionTaken, "token_disabled"),
			UserDisabled:           strings.Contains(row.ActionTaken, "user_disabled"),
			UserDisableSkipped:     strings.Contains(row.ActionTaken, "user_disable_skipped"),
			ChannelDisabled:        strings.Contains(row.ActionTaken, "upstream_isolated"),
			CreatedAt:              row.CreatedAt,
			UpdatedAt:              row.CreatedAt,
		}
		if incident.OccurredAt <= 0 {
			incident.OccurredAt = 1
			incident.CreatedAt = 1
			incident.UpdatedAt = 1
		}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&incident).Error; err != nil {
			return err
		}
	}
	return nil
}

type legacyBalanceAdjustment struct {
	OperationID         string  `gorm:"column:operation_id"`
	UserID              int     `gorm:"column:user_id"`
	Delta               int64   `gorm:"column:delta"`
	Reason              string  `gorm:"column:reason"`
	Metadata            string  `gorm:"column:metadata"`
	PayloadSHA256       string  `gorm:"column:payload_sha256"`
	OriginalOperationID *string `gorm:"column:original_operation_id"`
	BalanceBefore       int64   `gorm:"column:balance_before"`
	BalanceAfter        int64   `gorm:"column:balance_after"`
	CreatedAt           int64   `gorm:"column:created_at"`
}

func importLegacyBalanceAdjustments(db *gorm.DB) error {
	if !db.Migrator().HasTable("internal_balance_adjustments") {
		return nil
	}
	var rows []legacyBalanceAdjustment
	if err := db.Table("internal_balance_adjustments").Order("id ASC").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		adjustment := model.KKAIInternalBalanceAdjustment{
			OperationID:         row.OperationID,
			UserID:              row.UserID,
			Delta:               row.Delta,
			Reason:              row.Reason,
			Metadata:            normalizedLegacyJSON(row.Metadata),
			PayloadSHA256:       row.PayloadSHA256,
			OriginalOperationID: row.OriginalOperationID,
			BalanceBefore:       row.BalanceBefore,
			BalanceAfter:        row.BalanceAfter,
			CreatedAt:           row.CreatedAt,
		}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&adjustment).Error; err != nil {
			return err
		}
	}
	return nil
}

func normalizedLegacyJSON(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 2048 {
		return "{}"
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return "{}"
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
