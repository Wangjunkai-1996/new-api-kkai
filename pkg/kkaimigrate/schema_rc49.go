package kkaimigrate

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

const RC49SchemaVersion int64 = 22

// v22 adds only authorization metadata and dashboard counters. Existing tokens
// remain denied; creation APIs explicitly opt new tokens into quota queries.
var rc49SchemaStatements = map[string][]migrationStatement{
	DialectSQLite: {}, // No rc49 apply/runtime support; catalog preserves historical dialect entries.
	DialectMySQL:  {},
	DialectPostgres: {
		{Operation: migrationOperationRC49Columns, SQL: `ALTER TABLE tokens ADD COLUMN IF NOT EXISTS allow_user_quota_query BOOLEAN NOT NULL DEFAULT FALSE`},
		{Operation: migrationOperationRC49Columns, SQL: `ALTER TABLE quota_data ADD COLUMN IF NOT EXISTS cache_read_tokens BIGINT NOT NULL DEFAULT 0`},
		{Operation: migrationOperationRC49Columns, SQL: `ALTER TABLE quota_data ADD COLUMN IF NOT EXISTS cache_creation_tokens BIGINT NOT NULL DEFAULT 0`},
	},
}

func validateRC49Schema(db *gorm.DB, complete bool) error {
	if db == nil || db.Dialector.Name() != DialectPostgres {
		return ErrUnsupportedDialect
	}
	for _, requirement := range []struct{ table, name, kind, defaultValue string }{
		{"tokens", "allow_user_quota_query", "boolean", "false"},
		{"quota_data", "cache_read_tokens", "bigint", "0"},
		{"quota_data", "cache_creation_tokens", "bigint", "0"},
	} {
		if !db.Migrator().HasTable(requirement.table) {
			return fmt.Errorf("%w: missing %s", ErrSchemaNotReady, requirement.table)
		}
		var columns []struct {
			Kind         string
			NotNull      bool
			DefaultValue string
		}
		err := db.Raw(`SELECT format_type(a.atttypid, a.atttypmod) AS kind, a.attnotnull AS not_null,
 COALESCE(pg_get_expr(d.adbin, d.adrelid), '') AS default_value
 FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
 WHERE a.attrelid=to_regclass(?) AND a.attname=? AND NOT a.attisdropped`, requirement.table, requirement.name).Scan(&columns).Error
		if err != nil {
			return err
		}
		if len(columns) == 0 && !complete {
			continue
		}
		if len(columns) != 1 || columns[0].Kind != requirement.kind || !columns[0].NotNull || strings.Trim(columns[0].DefaultValue, "()'\"") != requirement.defaultValue {
			return fmt.Errorf("%w: invalid %s.%s rc49 column contract", ErrSchemaNotReady, requirement.table, requirement.name)
		}
	}
	return nil
}

// ApplyRC49Expand is a separately authorized PostgreSQL operation. Runtime
// startup does not call it; old binaries limited to v21 cannot accept its ledger.
func ApplyRC49Expand(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	if ctx == nil || db == nil {
		return nil, ErrSchemaNotReady
	}
	if db.Dialector.Name() != DialectPostgres {
		return nil, ErrUnsupportedDialect
	}
	if !options.DryRun && !options.Offline && options.AuthorizeMutation == nil {
		return nil, ErrUnsafeMigration
	}
	result := &Result{}
	err := withTenantOnlineLock(ctx, db, options.Offline, func(locked *gorm.DB) error {
		state, err := loadValidatedState(ctx, locked, RC49SchemaVersion)
		if err != nil {
			return err
		}
		if state.currentVersion != MembershipBillingRecoverySchemaVersion && state.currentVersion != RC49SchemaVersion {
			return fmt.Errorf("%w: rc49 requires canonical schema 21 or 22", ErrSchemaNotReady)
		}
		if err := validateRC49Schema(locked, state.currentVersion == RC49SchemaVersion); err != nil {
			return err
		}
		if !options.DryRun && options.AuthorizeMutation != nil {
			if err := options.AuthorizeMutation(ctx, locked); err != nil {
				return err
			}
		}
		item := migrationSet()[RC49SchemaVersion-1]
		entry := planItems([]migration{item})[0]
		if state.currentVersion == RC49SchemaVersion {
			return locked.Where("version = ?", RC49SchemaVersion).Find(&result.Applied).Error
		}
		if options.DryRun {
			result.Pending = []AppliedMigration{entry}
			return nil
		}
		started := time.Now()
		if err := locked.Transaction(func(tx *gorm.DB) error {
			if options.AuthorizeMutation != nil {
				if err := options.AuthorizeMutation(ctx, tx); err != nil {
					return err
				}
			}
			if err := executeMigrationSchema(tx, DialectPostgres, item); err != nil {
				return err
			}
			if err := validateRC49Schema(tx, true); err != nil {
				return err
			}
			return importLegacyAndRecord(tx, DialectPostgres, item, entry.Checksum, started)
		}); err != nil {
			return err
		}
		return locked.Where("version = ?", RC49SchemaVersion).Find(&result.Applied).Error
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

const rc49AuditIndexSQL = `CREATE INDEX IF NOT EXISTS idx_audit_time_event ON audit_logs (created_at, event_id)`

type rc49LogSchemaRecord struct {
	ID        int    `gorm:"primaryKey;autoIncrement:false"`
	Checksum  string `gorm:"type:varchar(64);not null"`
	AppliedAt int64  `gorm:"not null"`
}

func (rc49LogSchemaRecord) TableName() string { return "kkai_rc49_log_schema" }
func rc49LogSchemaChecksum() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(rc49AuditIndexSQL)))
}

// CheckRC49LogSchema checks the actual LOG_DB, whether shared or separate.
// No application startup path creates the index or its immutable receipt.
func CheckRC49LogSchema(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return ErrSchemaNotReady
	}
	if db.Dialector.Name() != DialectPostgres {
		return ErrUnsupportedDialect
	}
	db = db.WithContext(ctx)
	if db.Migrator().HasTable(&rc49OnlineReceipt{}) {
		receipts, err := loadRC49OnlineReceipts(db)
		if err != nil {
			return err
		}
		if receipts["audit_index"].State != "done" {
			return fmt.Errorf("%w: incomplete rc49 online log expansion", ErrSchemaNotReady)
		}
		return validateRC49AuditIndex(db)
	}
	if !db.Migrator().HasTable(&rc49LogSchemaRecord{}) {
		return fmt.Errorf("%w: missing rc49 log expansion receipt", ErrSchemaNotReady)
	}
	var records []rc49LogSchemaRecord
	if err := db.Find(&records).Error; err != nil {
		return err
	}
	if len(records) != 1 || records[0].ID != int(RC49SchemaVersion) || records[0].Checksum != rc49LogSchemaChecksum() {
		return ErrChecksumMismatch
	}
	return validateRC49AuditIndex(db)
}

func validateRC49AuditIndex(db *gorm.DB) error {
	var columns []string
	err := db.Raw(`SELECT a.attname FROM pg_index i JOIN pg_class idx ON idx.oid=i.indexrelid
 JOIN LATERAL unnest(i.indkey) WITH ORDINALITY key(attnum,position) ON true
 JOIN pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=key.attnum
 WHERE i.indrelid=to_regclass('audit_logs') AND idx.relname='idx_audit_time_event'
 AND i.indisvalid AND i.indisready AND NOT i.indisunique AND i.indpred IS NULL AND i.indexprs IS NULL
 ORDER BY key.position`).Scan(&columns).Error
	if err != nil {
		return err
	}
	if !equalRuntimeIndexColumns(columns, []string{"created_at", "event_id"}) {
		return fmt.Errorf("%w: invalid audit time/event index in LOG_DB", ErrSchemaNotReady)
	}
	return nil
}

// ApplyRC49LogExpand must receive LOG_DB explicitly. It never writes the main
// migration ledger, so a separate log database cannot be mistaken for ready.
func ApplyRC49LogExpand(ctx context.Context, db *gorm.DB, options Options) error {
	if ctx == nil || db == nil {
		return ErrSchemaNotReady
	}
	if db.Dialector.Name() != DialectPostgres {
		return ErrUnsupportedDialect
	}
	if !options.DryRun && !options.Offline && options.AuthorizeMutation == nil {
		return ErrUnsafeMigration
	}
	return withTenantOnlineLock(ctx, db, options.Offline, func(locked *gorm.DB) error {
		if !locked.Migrator().HasTable("audit_logs") {
			return fmt.Errorf("%w: missing audit_logs in LOG_DB", ErrSchemaNotReady)
		}
		if options.DryRun {
			return nil
		}
		if options.AuthorizeMutation != nil {
			if err := options.AuthorizeMutation(ctx, locked); err != nil {
				return err
			}
		}
		return locked.Transaction(func(tx *gorm.DB) error {
			if options.AuthorizeMutation != nil {
				if err := options.AuthorizeMutation(ctx, tx); err != nil {
					return err
				}
			}
			if err := tx.Exec(rc49AuditIndexSQL).Error; err != nil {
				return err
			}
			if err := validateRC49AuditIndex(tx); err != nil {
				return err
			}
			if !tx.Migrator().HasTable(&rc49LogSchemaRecord{}) {
				if err := tx.Migrator().CreateTable(&rc49LogSchemaRecord{}); err != nil {
					return err
				}
			}
			var count int64
			if err := tx.Model(&rc49LogSchemaRecord{}).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				if err := tx.Create(&rc49LogSchemaRecord{ID: int(RC49SchemaVersion), Checksum: rc49LogSchemaChecksum(), AppliedAt: time.Now().Unix()}).Error; err != nil {
					return err
				}
			}
			return CheckRC49LogSchema(ctx, tx)
		})
	})
}
