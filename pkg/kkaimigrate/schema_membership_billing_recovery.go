package kkaimigrate

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

const MembershipBillingRecoverySchemaVersion int64 = 21

var membershipBillingRecoverySchemaStatements = map[string][]migrationStatement{
	DialectSQLite:   membershipBillingRecoveryStatements("INTEGER", ""),
	DialectMySQL:    membershipBillingRecoveryStatements("BIGINT", " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 ROW_FORMAT=DYNAMIC"),
	DialectPostgres: membershipBillingRecoveryStatements("BIGINT", ""),
}

// v21 creates only recovery sidecars. It neither changes reservations nor
// manufactures billing evidence from an existing request's age.
func membershipBillingRecoveryStatements(integer, suffix string) []migrationStatement {
	return []migrationStatement{
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS kkai_membership_billing_recovery (
request_id VARCHAR(128) NOT NULL PRIMARY KEY,
state VARCHAR(16) NOT NULL,
evidence_kind VARCHAR(24) NOT NULL,
evidence_payload TEXT NOT NULL,
evidence_hash VARCHAR(64) NOT NULL,
receipt_key VARCHAR(191) NULL,
next_attempt_at ` + integer + ` NOT NULL,
attempt_count ` + integer + ` NOT NULL,
last_checked_at ` + integer + ` NOT NULL,
last_error_code VARCHAR(64) NOT NULL,
revision ` + integer + ` NOT NULL,
created_at ` + integer + ` NOT NULL,
updated_at ` + integer + ` NOT NULL,
resolved_at ` + integer + ` NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS kkai_membership_billing_scan_progress (
lane VARCHAR(32) NOT NULL PRIMARY KEY,
cursor_at ` + integer + ` NOT NULL,
cursor_request_id VARCHAR(128) NOT NULL,
sweep_upper_at ` + integer + ` NOT NULL,
sweep_upper_request_id VARCHAR(128) NOT NULL,
revision ` + integer + ` NOT NULL,
updated_at ` + integer + ` NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_membership_recovery_due ON kkai_membership_billing_recovery (state, next_attempt_at, request_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX ux_membership_recovery_receipt ON kkai_membership_billing_recovery (receipt_key)`},
	}
}

// Frozen projections keep this migration independent of later live models.
type membershipBillingRecoveryV21 struct {
	RequestID       string  `gorm:"primaryKey;type:varchar(128);not null"`
	State           string  `gorm:"type:varchar(16);not null"`
	EvidenceKind    string  `gorm:"type:varchar(24);not null"`
	EvidencePayload string  `gorm:"type:text;not null"`
	EvidenceHash    string  `gorm:"type:varchar(64);not null"`
	ReceiptKey      *string `gorm:"type:varchar(191)"`
	NextAttemptAt   int64   `gorm:"not null"`
	AttemptCount    int64   `gorm:"not null"`
	LastCheckedAt   int64   `gorm:"not null"`
	LastErrorCode   string  `gorm:"type:varchar(64);not null"`
	Revision        int64   `gorm:"not null"`
	CreatedAt       int64   `gorm:"not null"`
	UpdatedAt       int64   `gorm:"not null"`
	ResolvedAt      int64   `gorm:"not null"`
}

func (membershipBillingRecoveryV21) TableName() string { return "kkai_membership_billing_recovery" }

type membershipBillingScanProgressV21 struct {
	Lane                string `gorm:"primaryKey;type:varchar(32);not null"`
	CursorAt            int64  `gorm:"not null"`
	CursorRequestID     string `gorm:"type:varchar(128);not null"`
	SweepUpperAt        int64  `gorm:"not null"`
	SweepUpperRequestID string `gorm:"type:varchar(128);not null"`
	Revision            int64  `gorm:"not null"`
	UpdatedAt           int64  `gorm:"not null"`
}

func (membershipBillingScanProgressV21) TableName() string {
	return "kkai_membership_billing_scan_progress"
}

func validateMembershipBillingRecoverySchema(db *gorm.DB, complete bool) error {
	for _, step := range []tenantOnlineStep{
		{table: "kkai_membership_billing_recovery", model: &membershipBillingRecoveryV21{}},
		{table: "kkai_membership_billing_scan_progress", model: &membershipBillingScanProgressV21{}},
	} {
		if !complete && !db.Migrator().HasTable(step.table) {
			continue
		}
		if err := step.validate(db); err != nil {
			return err
		}
		columns, err := db.Migrator().ColumnTypes(step.table)
		if err != nil {
			return err
		}
		primary := "request_id"
		if step.table == "kkai_membership_billing_scan_progress" {
			primary = "lane"
		}
		for _, column := range columns {
			if column.Name() == primary {
				if nullable, known := column.Nullable(); !known || nullable {
					return fmt.Errorf("%w: %s has a nullable primary key", ErrSchemaNotReady, step.table)
				}
			}
			if db.Dialector.Name() != DialectPostgres {
				isPrimary, known := column.PrimaryKey()
				if !known || isPrimary != (column.Name() == primary) {
					return fmt.Errorf("%w: %s has an invalid primary key", ErrSchemaNotReady, step.table)
				}
			}
		}
	}
	for _, index := range []walletPaymentIndexRequirement{
		{Table: "kkai_membership_billing_recovery", Name: "idx_membership_recovery_due", Columns: []string{"state", "next_attempt_at", "request_id"}},
		{Table: "kkai_membership_billing_recovery", Name: "ux_membership_recovery_receipt", Columns: []string{"receipt_key"}, Unique: true},
	} {
		if !complete && !db.Migrator().HasIndex(index.Table, index.Name) {
			continue
		}
		if err := validateTenantOnlineIndex(db, index.Table, index.Name, index.Columns, index.Unique); err != nil {
			return err
		}
	}
	return nil
}

func MembershipBillingRecoveryPlanChecksum() string {
	return migrationChecksum(migrationSet()[MembershipBillingRecoverySchemaVersion-1])
}

// MembershipBillingRecoverySchemaObjectsPresent detects even a partial expansion;
// callers must not mistake it for canonical runtime readiness.
func MembershipBillingRecoverySchemaObjectsPresent(db *gorm.DB) (bool, error) {
	if db == nil {
		return false, ErrSchemaNotReady
	}
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return false, err
	}
	for _, table := range tables {
		if table == "kkai_membership_billing_recovery" || table == "kkai_membership_billing_scan_progress" {
			return true, nil
		}
	}
	return false, nil
}

type MembershipBillingRecoveryExpandPrecheck struct {
	Schema         string `json:"schema"`
	CurrentVersion int64  `json:"current_version"`
	TargetVersion  int64  `json:"target_version"`
	Safe           bool   `json:"safe"`
}

func PrecheckMembershipBillingRecoveryExpand(ctx context.Context, db *gorm.DB) (*MembershipBillingRecoveryExpandPrecheck, error) {
	if ctx == nil || db == nil {
		return nil, ErrSchemaNotReady
	}
	state, err := loadValidatedState(ctx, db, MembershipBillingRecoverySchemaVersion)
	if err != nil {
		return nil, err
	}
	if state.currentVersion != MembershipPaymentSchemaVersion && state.currentVersion != MembershipBillingRecoverySchemaVersion {
		return nil, fmt.Errorf("%w: membership billing recovery expansion requires canonical schema 20 or 21", ErrSchemaNotReady)
	}
	if err := validateMembershipBillingRecoverySchema(db.WithContext(ctx), false); err != nil {
		return nil, err
	}
	return &MembershipBillingRecoveryExpandPrecheck{Schema: "kkai_membership_billing_recovery_expand_v1", CurrentVersion: state.currentVersion, TargetVersion: MembershipBillingRecoverySchemaVersion, Safe: true}, nil
}

func CheckMembershipBillingRecoverySchema(ctx context.Context, db *gorm.DB) error {
	return checkThroughVersion(ctx, db, MembershipBillingRecoverySchemaVersion, MembershipBillingRecoverySchemaVersion, MembershipBillingRecoverySchemaVersion)
}

// CheckMembershipBillingRecoveryRuntimeSchema is read-only and requires canonical
// receipts plus both sidecars' columns, primary keys and reconciliation indexes.
func CheckMembershipBillingRecoveryRuntimeSchema(ctx context.Context, db *gorm.DB) error {
	version, err := WalletPaymentLedgerVersion(ctx, db)
	if err != nil {
		return err
	}
	if version < MembershipBillingRecoverySchemaVersion {
		return ErrSchemaNotReady
	}
	if err := validateMembershipBillingRecoverySchema(db.WithContext(ctx), true); err != nil {
		return err
	}
	return ctx.Err()
}

// ApplyMembershipBillingRecoveryExpand is for isolated offline migration work.
// Production adoption needs a separate controller that verifies every runtime's
// v21 compatibility; no such controller is supplied by this migration.
func ApplyMembershipBillingRecoveryExpand(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	if !options.DryRun && !options.Offline {
		return nil, fmt.Errorf("%w: membership billing recovery production controller is not implemented; use isolated offline mode only", ErrUnsafeMigration)
	}
	result := &Result{}
	err := withTenantOnlineLock(ctx, db, options.Offline, func(locked *gorm.DB) error {
		precheck, err := PrecheckMembershipBillingRecoveryExpand(ctx, locked)
		if err != nil {
			return err
		}
		item := migrationSet()[MembershipBillingRecoverySchemaVersion-1]
		entry := planItems([]migration{item})[0]
		if precheck.CurrentVersion == MembershipBillingRecoverySchemaVersion {
			return locked.Where("version = ?", MembershipBillingRecoverySchemaVersion).Find(&result.Applied).Error
		}
		if options.DryRun {
			result.Pending = []AppliedMigration{entry}
			return nil
		}
		started := time.Now()
		apply := func(tx *gorm.DB) error {
			if options.AuthorizeMutation != nil {
				if err := options.AuthorizeMutation(ctx, tx); err != nil {
					return err
				}
			}
			if err := executeMigrationSchema(tx, locked.Dialector.Name(), item); err != nil {
				return err
			}
			if err := validateMembershipBillingRecoverySchema(tx, true); err != nil {
				return err
			}
			return importLegacyAndRecord(tx, locked.Dialector.Name(), item, entry.Checksum, started)
		}
		if locked.Dialector.Name() == DialectMySQL {
			err = apply(locked)
		} else {
			err = locked.Transaction(apply)
		}
		if err != nil {
			return err
		}
		return locked.Where("version = ?", MembershipBillingRecoverySchemaVersion).Find(&result.Applied).Error
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
