package kkaimigrate

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

const MembershipPaymentSchemaVersion int64 = 20

var membershipPaymentSchemaStatements = map[string][]migrationStatement{
	DialectSQLite:   membershipPaymentStatements("INTEGER PRIMARY KEY AUTOINCREMENT", "INTEGER", ""),
	DialectMySQL:    membershipPaymentStatements("BIGINT AUTO_INCREMENT PRIMARY KEY", "BIGINT", " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 ROW_FORMAT=DYNAMIC"),
	DialectPostgres: membershipPaymentStatements("BIGSERIAL PRIMARY KEY", "BIGINT", ""),
}

// v20 only creates sidecar tables. It neither modifies legacy membership rows
// nor infers payment, credentials, or fulfillment from an order's age.
func membershipPaymentStatements(identity, integer, suffix string) []migrationStatement {
	return []migrationStatement{
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS kkai_membership_payment_checks (
order_id ` + integer + ` PRIMARY KEY,
trade_no VARCHAR(128) NOT NULL,
user_id ` + integer + ` NOT NULL,
merchant_id VARCHAR(128) NOT NULL,
encrypted_snapshot TEXT NOT NULL,
status VARCHAR(24) NOT NULL,
next_check_at ` + integer + ` NOT NULL,
claim_until ` + integer + ` NOT NULL,
last_checked_at ` + integer + ` NOT NULL,
callback_received_at ` + integer + ` NOT NULL,
created_at ` + integer + ` NOT NULL,
updated_at ` + integer + ` NOT NULL,
claim_token VARCHAR(64) NOT NULL,
attempts ` + integer + ` NOT NULL,
last_source VARCHAR(32) NOT NULL,
last_error VARCHAR(512) NOT NULL,
close_reason VARCHAR(32) NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS kkai_membership_payment_evidence (
id ` + identity + `,
order_id ` + integer + ` NOT NULL,
receipt_key VARCHAR(191) NOT NULL,
receipt_id VARCHAR(128) NOT NULL,
merchant_id VARCHAR(128) NOT NULL,
amount_minor ` + integer + ` NOT NULL,
currency VARCHAR(8) NOT NULL,
provider_paid_at ` + integer + ` NOT NULL,
verified_at ` + integer + ` NOT NULL,
retry_at ` + integer + ` NOT NULL,
created_at ` + integer + ` NOT NULL,
updated_at ` + integer + ` NOT NULL,
source VARCHAR(32) NOT NULL,
status VARCHAR(24) NOT NULL,
attempts ` + integer + ` NOT NULL,
last_error VARCHAR(512) NOT NULL,
validation_error VARCHAR(512) NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX ux_membership_payment_check_trade ON kkai_membership_payment_checks (trade_no)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_membership_payment_check_user ON kkai_membership_payment_checks (user_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_membership_payment_check_due ON kkai_membership_payment_checks (status, next_check_at, claim_until)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_membership_payment_evidence_order ON kkai_membership_payment_evidence (order_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX ux_membership_payment_evidence_receipt ON kkai_membership_payment_evidence (receipt_key)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_membership_payment_evidence_retry ON kkai_membership_payment_evidence (status, retry_at)`},
	}
}

// Frozen v20 projections must not acquire fields from later live models.
type membershipPaymentCheckV20 struct {
	OrderID            int64  `gorm:"primaryKey;autoIncrement:false"`
	TradeNo            string `gorm:"type:varchar(128);not null"`
	UserID             int    `gorm:"not null"`
	MerchantID         string `gorm:"type:varchar(128);not null"`
	EncryptedSnapshot  string `gorm:"type:text;not null"`
	Status             string `gorm:"type:varchar(24);not null"`
	NextCheckAt        int64  `gorm:"not null"`
	ClaimUntil         int64  `gorm:"not null"`
	LastCheckedAt      int64  `gorm:"not null"`
	CallbackReceivedAt int64  `gorm:"not null"`
	CreatedAt          int64  `gorm:"not null"`
	UpdatedAt          int64  `gorm:"not null"`
	ClaimToken         string `gorm:"type:varchar(64);not null"`
	Attempts           int    `gorm:"not null"`
	LastSource         string `gorm:"type:varchar(32);not null"`
	LastError          string `gorm:"type:varchar(512);not null"`
	CloseReason        string `gorm:"type:varchar(32);not null"`
}

func (membershipPaymentCheckV20) TableName() string { return "kkai_membership_payment_checks" }

type membershipPaymentEvidenceV20 struct {
	ID              int64  `gorm:"primaryKey"`
	OrderID         int64  `gorm:"not null"`
	ReceiptKey      string `gorm:"type:varchar(191);not null"`
	ReceiptID       string `gorm:"type:varchar(128);not null"`
	MerchantID      string `gorm:"type:varchar(128);not null"`
	AmountMinor     int64  `gorm:"not null"`
	Currency        string `gorm:"type:varchar(8);not null"`
	ProviderPaidAt  int64  `gorm:"not null"`
	VerifiedAt      int64  `gorm:"not null"`
	RetryAt         int64  `gorm:"not null"`
	CreatedAt       int64  `gorm:"not null"`
	UpdatedAt       int64  `gorm:"not null"`
	Source          string `gorm:"type:varchar(32);not null"`
	Status          string `gorm:"type:varchar(24);not null"`
	Attempts        int    `gorm:"not null"`
	LastError       string `gorm:"type:varchar(512);not null"`
	ValidationError string `gorm:"type:varchar(512);not null"`
}

func (membershipPaymentEvidenceV20) TableName() string { return "kkai_membership_payment_evidence" }

var membershipPaymentIndexes = []walletPaymentIndexRequirement{
	{Table: "kkai_membership_payment_checks", Name: "ux_membership_payment_check_trade", Columns: []string{"trade_no"}, Unique: true},
	{Table: "kkai_membership_payment_checks", Name: "idx_membership_payment_check_user", Columns: []string{"user_id"}},
	{Table: "kkai_membership_payment_checks", Name: "idx_membership_payment_check_due", Columns: []string{"status", "next_check_at", "claim_until"}},
	{Table: "kkai_membership_payment_evidence", Name: "idx_membership_payment_evidence_order", Columns: []string{"order_id"}},
	{Table: "kkai_membership_payment_evidence", Name: "ux_membership_payment_evidence_receipt", Columns: []string{"receipt_key"}, Unique: true},
	{Table: "kkai_membership_payment_evidence", Name: "idx_membership_payment_evidence_retry", Columns: []string{"status", "retry_at"}},
}

func validateMembershipPaymentSchema(db *gorm.DB, complete bool) error {
	for _, step := range []tenantOnlineStep{
		{table: "kkai_membership_payment_checks", model: &membershipPaymentCheckV20{}},
		{table: "kkai_membership_payment_evidence", model: &membershipPaymentEvidenceV20{}},
	} {
		if !complete && !db.Migrator().HasTable(step.table) {
			continue
		}
		if err := step.validate(db); err != nil {
			return err
		}
		if db.Dialector.Name() != DialectPostgres {
			columns, err := db.Migrator().ColumnTypes(step.table)
			if err != nil {
				return err
			}
			primary := "id"
			if step.table == "kkai_membership_payment_checks" {
				primary = "order_id"
			}
			for _, column := range columns {
				isPrimary, known := column.PrimaryKey()
				if !known || isPrimary != (column.Name() == primary) {
					return fmt.Errorf("%w: %s has an invalid primary key", ErrSchemaNotReady, step.table)
				}
			}
		}
	}
	for _, index := range membershipPaymentIndexes {
		if !complete && !db.Migrator().HasIndex(index.Table, index.Name) {
			continue
		}
		if err := validateTenantOnlineIndex(db, index.Table, index.Name, index.Columns, index.Unique); err != nil {
			return err
		}
	}
	return nil
}

func MembershipPaymentPlanChecksum() string {
	return migrationChecksum(migrationSet()[MembershipPaymentSchemaVersion-1])
}

// MembershipPaymentSchemaObjectsPresent detects partial unreceipted expansion;
// it is not a runtime readiness check.
func MembershipPaymentSchemaObjectsPresent(db *gorm.DB) (bool, error) {
	if db == nil {
		return false, ErrSchemaNotReady
	}
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return false, err
	}
	for _, table := range tables {
		if table == "kkai_membership_payment_checks" || table == "kkai_membership_payment_evidence" {
			return true, nil
		}
	}
	return false, nil
}

type MembershipPaymentExpandPrecheck struct {
	Schema         string `json:"schema"`
	CurrentVersion int64  `json:"current_version"`
	TargetVersion  int64  `json:"target_version"`
	Safe           bool   `json:"safe"`
}

func PrecheckMembershipPaymentExpand(ctx context.Context, db *gorm.DB) (*MembershipPaymentExpandPrecheck, error) {
	if ctx == nil || db == nil {
		return nil, ErrSchemaNotReady
	}
	state, err := loadValidatedState(ctx, db, MembershipPaymentSchemaVersion)
	if err != nil {
		return nil, err
	}
	if state.currentVersion != ShopSchemaVersion && state.currentVersion != MembershipPaymentSchemaVersion {
		return nil, fmt.Errorf("%w: membership payment expansion requires canonical schema 19 or 20", ErrSchemaNotReady)
	}
	if err := validateMembershipPaymentSchema(db.WithContext(ctx), false); err != nil {
		return nil, err
	}
	return &MembershipPaymentExpandPrecheck{Schema: "kkai_membership_payment_expand_v1", CurrentVersion: state.currentVersion, TargetVersion: MembershipPaymentSchemaVersion, Safe: true}, nil
}

// CheckMembershipPaymentSchema is the complete read-only maintenance audit.
func CheckMembershipPaymentSchema(ctx context.Context, db *gorm.DB) error {
	return checkThroughVersion(ctx, db, MembershipPaymentSchemaVersion, MembershipPaymentSchemaVersion, KnownCatalogVersion)
}

// CheckMembershipPaymentRuntimeSchema requires canonical receipts and exact
// sidecar columns, primary keys, uniqueness, and bounded-scan indexes. It does
// not scan unrelated tenant/business data or mutate the schema.
func CheckMembershipPaymentRuntimeSchema(ctx context.Context, db *gorm.DB) error {
	version, err := WalletPaymentLedgerVersion(ctx, db)
	if err != nil {
		return err
	}
	if version < MembershipPaymentSchemaVersion {
		return ErrSchemaNotReady
	}
	if err := validateMembershipPaymentSchema(db.WithContext(ctx), true); err != nil {
		return err
	}
	return ctx.Err()
}

// ApplyMembershipPaymentExpand is never called by application startup.
// Production controller adoption and migration authorization are separate work.
func ApplyMembershipPaymentExpand(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	if !options.DryRun && !options.Offline && options.AuthorizeMutation == nil {
		return nil, fmt.Errorf("%w: membership payment expansion requires explicit controller authorization or isolated offline mode", ErrUnsafeMigration)
	}
	result := &Result{}
	err := withTenantOnlineLock(ctx, db, options.Offline, func(locked *gorm.DB) error {
		precheck, err := PrecheckMembershipPaymentExpand(ctx, locked)
		if err != nil {
			return err
		}
		item := migrationSet()[MembershipPaymentSchemaVersion-1]
		entry := planItems([]migration{item})[0]
		if precheck.CurrentVersion == MembershipPaymentSchemaVersion {
			return locked.Where("version = ?", MembershipPaymentSchemaVersion).Find(&result.Applied).Error
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
			if err := validateMembershipPaymentSchema(tx, true); err != nil {
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
		return locked.Where("version = ?", MembershipPaymentSchemaVersion).Find(&result.Applied).Error
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
