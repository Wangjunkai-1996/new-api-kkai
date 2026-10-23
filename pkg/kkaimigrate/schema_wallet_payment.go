package kkaimigrate

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

const WalletPaymentSchemaVersion int64 = 18

var walletPaymentSchemaStatements = map[string][]migrationStatement{
	DialectSQLite:   walletPaymentStatements("INTEGER PRIMARY KEY AUTOINCREMENT", "INTEGER", "", "\""),
	DialectMySQL:    walletPaymentStatements("BIGINT AUTO_INCREMENT PRIMARY KEY", "BIGINT", " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 ROW_FORMAT=DYNAMIC", "`"),
	DialectPostgres: walletPaymentStatements("BIGSERIAL PRIMARY KEY", "BIGINT", "", "\""),
}

// v18 is additive and immutable. All legacy rows retain NULL lifecycle fields;
// no migration infers a provider payment/closure from an order's age.
// Quote check_* identifiers so SQLite introspection does not mistake them for CHECK constraints.
func walletPaymentStatements(identity, integer, suffix, quote string) []migrationStatement {
	return []migrationStatement{
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN wallet_lifecycle_version ` + integer + ` NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN currency_exponent ` + integer + ` NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN credited_quota ` + integer + ` NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN credit_display_amount VARCHAR(64) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN credit_display_unit VARCHAR(32) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN pricing_mode VARCHAR(32) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN pricing_snapshot TEXT NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN pricing_notice_code VARCHAR(64) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN payable_minor ` + integer + ` NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN request_key_hash VARCHAR(64) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN request_hash VARCHAR(64) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN provider_account_ref VARCHAR(128) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN provider_environment VARCHAR(32) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN provider_credential_version VARCHAR(128) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN create_state VARCHAR(32) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN create_claim_token VARCHAR(64) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN create_claim_until ` + integer + ` NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN provider_session_id VARCHAR(255) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN provider_order_id VARCHAR(255) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN checkout_payload TEXT NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN provider_expires_at ` + integer + ` NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN pay_deadline_at ` + integer + ` NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN deadline_source VARCHAR(32) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN local_ended_at ` + integer + ` NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN local_end_reason VARCHAR(32) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN close_requested_at ` + integer + ` NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN close_reason VARCHAR(32) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN closed_at ` + integer + ` NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN close_evidence_ref VARCHAR(255) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN provider_payment_state VARCHAR(32) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN last_checked_at ` + integer + ` NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN next_check_at ` + integer + ` NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN ` + quote + `check_attempts` + quote + ` ` + integer + ` NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN ` + quote + `check_claim_token` + quote + ` VARCHAR(64) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN ` + quote + `check_claim_until` + quote + ` ` + integer + ` NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN reason_code VARCHAR(64) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN review_owner VARCHAR(128) NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN review_started_at ` + integer + ` NULL`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE top_ups ADD COLUMN version ` + integer + ` NULL`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS wallet_topup_flows (
id ` + identity + `,
tenant_id ` + integer + ` NOT NULL,
user_id ` + integer + ` NOT NULL,
flow_id VARCHAR(64) NOT NULL,
revision ` + integer + ` NOT NULL,
top_up_id ` + integer + `,
last_acknowledged_flow_id VARCHAR(64) NOT NULL,
last_acknowledged_trade_no VARCHAR(255) NOT NULL,
updated_at ` + integer + ` NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS wallet_payment_receipts (
id ` + identity + `,
receipt_key VARCHAR(64) NOT NULL,
provider VARCHAR(50) NOT NULL,
provider_account_ref VARCHAR(128) NOT NULL,
provider_environment VARCHAR(32) NOT NULL,
transaction_id VARCHAR(255) NOT NULL,
top_up_id ` + integer + `,
amount_minor ` + integer + `,
currency VARCHAR(8) NOT NULL,
currency_exponent ` + integer + `,
paid_at ` + integer + `,
received_at ` + integer + `,
evidence_source VARCHAR(32),
evidence_ref VARCHAR(255),
verification_status VARCHAR(32),
pricing_verified BOOLEAN,
settlement_kind VARCHAR(32),
processing_status VARCHAR(32),
exception_reason VARCHAR(64),
review_owner VARCHAR(128),
next_retry_at ` + integer + `
)` + suffix},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS wallet_payment_events (
id ` + identity + `,
actor_id ` + integer + `,
source VARCHAR(32),
evidence_ref VARCHAR(255),
receipt_id ` + integer + `,
evidence_hash VARCHAR(64),
top_up_id ` + integer + ` NOT NULL,
kind VARCHAR(32) NOT NULL,
status VARCHAR(32) NOT NULL,
reason_code VARCHAR(64) NOT NULL,
created_at ` + integer + ` NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS wallet_topup_runs (
id VARCHAR(64) PRIMARY KEY,
request_hash VARCHAR(64),
report_id VARCHAR(64),
scope_json TEXT NOT NULL,
scope_digest VARCHAR(64) NOT NULL,
max_order_id ` + integer + `,
cursor ` + integer + `,
control_version ` + integer + `,
expires_at ` + integer + `,
desired_state VARCHAR(32),
status VARCHAR(32),
owner VARCHAR(128),
last_task_id VARCHAR(64),
authorization_ref VARCHAR(255),
idempotency_key_hash VARCHAR(64),
scanned_count ` + integer + `,
credited_count ` + integer + `,
closed_count ` + integer + `,
review_count ` + integer + `,
error_count ` + integer + `,
created_at ` + integer + `,
updated_at ` + integer + `
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_wallet_flow_owner ON wallet_topup_flows (tenant_id, user_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_wallet_payment_receipts_receipt_key ON wallet_payment_receipts (receipt_key)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_wallet_payment_receipts_top_up_id ON wallet_payment_receipts (top_up_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_wallet_payment_receipts_processing_status ON wallet_payment_receipts (processing_status)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_wallet_payment_receipts_next_retry_at ON wallet_payment_receipts (next_retry_at)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_wallet_payment_event ON wallet_payment_events (top_up_id, created_at)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_wallet_topup_runs_report_id ON wallet_topup_runs (report_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_wallet_topup_runs_status ON wallet_topup_runs (status)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_wallet_topup_runs_idempotency_key_hash ON wallet_topup_runs (idempotency_key_hash)`},
		{Operation: migrationOperationCreateWalletTopUpIndex, SQL: `CREATE UNIQUE INDEX idx_wallet_topup_request ON top_ups (tenant_id, user_id, request_key_hash)`},
		{Operation: migrationOperationCreateWalletTopUpIndex, SQL: `CREATE INDEX idx_wallet_topup_due ON top_ups (next_check_at)`},
		{Operation: migrationOperationCreateWalletTopUpIndex, SQL: `CREATE INDEX idx_wallet_topup_owner ON top_ups (tenant_id, user_id, create_time, id)`},
	}
}

type walletTopUpV18 struct {
	WalletLifecycleVersion    int
	CurrencyExponent          int
	CreditedQuota             int64
	CreditDisplayAmount       string `gorm:"type:varchar(64)"`
	CreditDisplayUnit         string `gorm:"type:varchar(32)"`
	PricingMode               string `gorm:"type:varchar(32)"`
	PricingSnapshot           string `gorm:"type:text"`
	PricingNoticeCode         string `gorm:"type:varchar(64)"`
	PayableMinor              *int64
	RequestKeyHash            *string `gorm:"type:varchar(64)"`
	RequestHash               string  `gorm:"type:varchar(64)"`
	ProviderAccountRef        string  `gorm:"type:varchar(128)"`
	ProviderEnvironment       string  `gorm:"type:varchar(32)"`
	ProviderCredentialVersion string  `gorm:"type:varchar(128)"`
	CreateState               string  `gorm:"type:varchar(32)"`
	CreateClaimToken          string  `gorm:"type:varchar(64)"`
	CreateClaimUntil          int64
	ProviderSessionID         string `gorm:"type:varchar(255)"`
	ProviderOrderID           string `gorm:"type:varchar(255)"`
	CheckoutPayload           string `gorm:"type:text"`
	ProviderExpiresAt         int64
	PayDeadlineAt             int64
	DeadlineSource            string `gorm:"type:varchar(32)"`
	LocalEndedAt              int64
	LocalEndReason            string `gorm:"type:varchar(32)"`
	CloseRequestedAt          int64
	CloseReason               string `gorm:"type:varchar(32)"`
	ClosedAt                  int64
	CloseEvidenceRef          string `gorm:"type:varchar(255)"`
	ProviderPaymentState      string `gorm:"type:varchar(32)"`
	LastCheckedAt             int64
	NextCheckAt               int64
	CheckAttempts             int
	CheckClaimToken           string `gorm:"type:varchar(64)"`
	CheckClaimUntil           int64
	ReasonCode                string `gorm:"type:varchar(64)"`
	ReviewOwner               string `gorm:"type:varchar(128)"`
	ReviewStartedAt           int64
	Version                   int64
}

func (walletTopUpV18) TableName() string { return "top_ups" }

type walletTopUpFlowV18 struct {
	ID                      int64  `gorm:"primaryKey"`
	TenantID                int64  `gorm:"not null"`
	UserID                  int    `gorm:"not null"`
	FlowID                  string `gorm:"type:varchar(64);not null"`
	Revision                int64  `gorm:"not null"`
	TopUpID                 *int
	LastAcknowledgedFlowID  string `gorm:"type:varchar(64);not null"`
	LastAcknowledgedTradeNo string `gorm:"type:varchar(255);not null"`
	UpdatedAt               int64  `gorm:"not null;autoCreateTime:false;autoUpdateTime:false"`
}

func (walletTopUpFlowV18) TableName() string { return "wallet_topup_flows" }

type walletPaymentReceiptV18 struct {
	ID                  int64  `gorm:"primaryKey"`
	ReceiptKey          string `gorm:"type:varchar(64);not null"`
	Provider            string `gorm:"type:varchar(50);not null"`
	ProviderAccountRef  string `gorm:"type:varchar(128);not null"`
	ProviderEnvironment string `gorm:"type:varchar(32);not null"`
	TransactionID       string `gorm:"type:varchar(255);not null"`
	TopUpID             *int
	AmountMinor         int64
	Currency            string `gorm:"type:varchar(8);not null"`
	CurrencyExponent    int
	PaidAt              int64
	ReceivedAt          int64
	EvidenceSource      string `gorm:"type:varchar(32)"`
	EvidenceRef         string `gorm:"type:varchar(255)"`
	VerificationStatus  string `gorm:"type:varchar(32)"`
	PricingVerified     bool   `gorm:"type:boolean"`
	SettlementKind      string `gorm:"type:varchar(32)"`
	ProcessingStatus    string `gorm:"type:varchar(32)"`
	ExceptionReason     string `gorm:"type:varchar(64)"`
	ReviewOwner         string `gorm:"type:varchar(128)"`
	NextRetryAt         int64
}

func (walletPaymentReceiptV18) TableName() string { return "wallet_payment_receipts" }

type walletPaymentEventV18 struct {
	ID           int64 `gorm:"primaryKey"`
	ActorID      int
	Source       string `gorm:"type:varchar(32)"`
	EvidenceRef  string `gorm:"type:varchar(255)"`
	ReceiptID    *int64
	EvidenceHash string `gorm:"type:varchar(64)"`
	TopUpID      int    `gorm:"not null"`
	Kind         string `gorm:"type:varchar(32);not null"`
	Status       string `gorm:"type:varchar(32);not null"`
	ReasonCode   string `gorm:"type:varchar(64);not null"`
	CreatedAt    int64  `gorm:"not null;autoCreateTime:false;autoUpdateTime:false"`
}

func (walletPaymentEventV18) TableName() string { return "wallet_payment_events" }

type walletTopUpRunV18 struct {
	ID                 string `gorm:"type:varchar(64);primaryKey"`
	RequestHash        string `gorm:"type:varchar(64)"`
	ReportID           string `gorm:"type:varchar(64)"`
	ScopeJSON          string `gorm:"type:text;not null"`
	ScopeDigest        string `gorm:"type:varchar(64);not null"`
	MaxOrderID         int64
	Cursor             int64
	ControlVersion     int64
	ExpiresAt          int64
	DesiredState       string  `gorm:"type:varchar(32)"`
	Status             string  `gorm:"type:varchar(32)"`
	Owner              string  `gorm:"type:varchar(128)"`
	LastTaskID         string  `gorm:"type:varchar(64)"`
	AuthorizationRef   string  `gorm:"type:varchar(255)"`
	IdempotencyKeyHash *string `gorm:"type:varchar(64)"`
	ScannedCount       int64
	CreditedCount      int64
	ClosedCount        int64
	ReviewCount        int64
	ErrorCount         int64
	CreatedAt          int64 `gorm:";autoCreateTime:false;autoUpdateTime:false"`
	UpdatedAt          int64 `gorm:";autoCreateTime:false;autoUpdateTime:false"`
}

func (walletTopUpRunV18) TableName() string { return "wallet_topup_runs" }

type walletPaymentIndexRequirement struct {
	Table, Name string
	Columns     []string
	Unique      bool
}

var walletPaymentIndexes = []walletPaymentIndexRequirement{
	{Table: "wallet_topup_flows", Name: "idx_wallet_flow_owner", Columns: []string{"tenant_id", "user_id"}, Unique: true},
	{Table: "wallet_payment_receipts", Name: "idx_wallet_payment_receipts_receipt_key", Columns: []string{"receipt_key"}, Unique: true},
	{Table: "wallet_payment_receipts", Name: "idx_wallet_payment_receipts_top_up_id", Columns: []string{"top_up_id"}, Unique: false},
	{Table: "wallet_payment_receipts", Name: "idx_wallet_payment_receipts_processing_status", Columns: []string{"processing_status"}, Unique: false},
	{Table: "wallet_payment_receipts", Name: "idx_wallet_payment_receipts_next_retry_at", Columns: []string{"next_retry_at"}, Unique: false},
	{Table: "wallet_payment_events", Name: "idx_wallet_payment_event", Columns: []string{"top_up_id", "created_at"}, Unique: false},
	{Table: "wallet_topup_runs", Name: "idx_wallet_topup_runs_report_id", Columns: []string{"report_id"}, Unique: false},
	{Table: "wallet_topup_runs", Name: "idx_wallet_topup_runs_status", Columns: []string{"status"}, Unique: false},
	{Table: "wallet_topup_runs", Name: "idx_wallet_topup_runs_idempotency_key_hash", Columns: []string{"idempotency_key_hash"}, Unique: true},
	{Table: "top_ups", Name: "idx_wallet_topup_request", Columns: []string{"tenant_id", "user_id", "request_key_hash"}, Unique: true},
	{Table: "top_ups", Name: "idx_wallet_topup_due", Columns: []string{"next_check_at"}, Unique: false},
	{Table: "top_ups", Name: "idx_wallet_topup_owner", Columns: []string{"tenant_id", "user_id", "create_time", "id"}, Unique: false},
}

func validateWalletPaymentSchema(db *gorm.DB, complete bool) error {
	// Validate individual added columns, preserving the unrelated legacy shape.
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&walletTopUpV18{}); err != nil {
		return err
	}
	for _, field := range stmt.Schema.Fields {
		if !complete && !db.Migrator().HasColumn("top_ups", field.DBName) {
			continue
		}
		step := tenantOnlineStep{table: "top_ups", model: &walletTopUpV18{}, field: field.Name}
		if err := step.validate(db); err != nil {
			return err
		}
	}
	for _, step := range []tenantOnlineStep{
		{table: "wallet_topup_flows", model: &walletTopUpFlowV18{}},
		{table: "wallet_payment_receipts", model: &walletPaymentReceiptV18{}},
		{table: "wallet_payment_events", model: &walletPaymentEventV18{}},
		{table: "wallet_topup_runs", model: &walletTopUpRunV18{}},
	} {
		if !complete && !db.Migrator().HasTable(step.table) {
			continue
		}
		if err := step.validate(db); err != nil {
			return err
		}
	}
	for _, index := range walletPaymentIndexes {
		if !complete && !db.Migrator().HasIndex(index.Table, index.Name) {
			continue
		}
		if err := validateTenantOnlineIndex(db, index.Table, index.Name, index.Columns, index.Unique); err != nil {
			return err
		}
	}
	return nil
}
func WalletPaymentPlanChecksum() string {
	return migrationChecksum(migrationSet()[WalletPaymentSchemaVersion-1])
}

// WalletPaymentSchemaObjectsPresent detects even partial unreceipted expansion.
// Production controllers reject these objects; isolated offline recovery may
// still resume the immutable additive plan on databases with nontransactional DDL.
func WalletPaymentSchemaObjectsPresent(db *gorm.DB) (bool, error) {
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(&walletTopUpV18{}); err != nil {
		return false, err
	}
	columns, err := db.Migrator().ColumnTypes("top_ups")
	if err != nil {
		return false, err
	}
	for _, column := range columns {
		if _, exists := statement.Schema.FieldsByDBName[column.Name()]; exists {
			return true, nil
		}
	}
	for _, table := range []string{"wallet_topup_flows", "wallet_payment_receipts", "wallet_payment_events", "wallet_topup_runs"} {
		if db.Migrator().HasTable(table) {
			return true, nil
		}
	}
	return false, nil
}

type WalletPaymentExpandPrecheck struct {
	Schema         string `json:"schema"`
	CurrentVersion int64  `json:"current_version"`
	TargetVersion  int64  `json:"target_version"`
	Safe           bool   `json:"safe"`
}

func PrecheckWalletPaymentExpand(ctx context.Context, db *gorm.DB) (*WalletPaymentExpandPrecheck, error) {
	if db == nil {
		return nil, ErrSchemaNotReady
	}
	state, err := loadValidatedState(ctx, db, WalletPaymentSchemaVersion)
	if err != nil {
		return nil, err
	}
	if state.currentVersion != InvoiceSchemaVersion && state.currentVersion != WalletPaymentSchemaVersion {
		return nil, fmt.Errorf("%w: wallet payment expansion requires canonical schema 17 or 18", ErrSchemaNotReady)
	}
	if err := validateWalletPaymentSchema(db.WithContext(ctx), false); err != nil {
		return nil, err
	}
	return &WalletPaymentExpandPrecheck{Schema: "kkai_wallet_payment_expand_v1", CurrentVersion: state.currentVersion, TargetVersion: WalletPaymentSchemaVersion, Safe: true}, nil
}

// CheckWalletPaymentSchema requires canonical receipts, not merely tables.
func CheckWalletPaymentSchema(ctx context.Context, db *gorm.DB) error {
	return checkThroughVersion(ctx, db, WalletPaymentSchemaVersion, WalletPaymentSchemaVersion, WalletPaymentSchemaVersion)
}

// ApplyWalletPaymentExpand is deliberately never called from application
// startup. Production authorization and controller adoption are separate work.
func ApplyWalletPaymentExpand(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	if !options.DryRun && !options.Offline && options.AuthorizeMutation == nil {
		return nil, fmt.Errorf("%w: wallet payment expansion requires explicit controller authorization or isolated offline mode", ErrUnsafeMigration)
	}
	result := &Result{}
	err := withTenantOnlineLock(ctx, db, options.Offline, func(locked *gorm.DB) error {
		precheck, err := PrecheckWalletPaymentExpand(ctx, locked)
		if err != nil {
			return err
		}
		item := migrationSet()[WalletPaymentSchemaVersion-1]
		entry := planItems([]migration{item})[0]
		if precheck.CurrentVersion == WalletPaymentSchemaVersion {
			return locked.Where("version = ?", WalletPaymentSchemaVersion).Find(&result.Applied).Error
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
			if err := validateWalletPaymentSchema(tx, true); err != nil {
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
		return locked.Where("version = ?", WalletPaymentSchemaVersion).Find(&result.Applied).Error
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
