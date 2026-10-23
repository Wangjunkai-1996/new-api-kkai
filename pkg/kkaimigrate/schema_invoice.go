package kkaimigrate

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

const InvoiceSchemaVersion int64 = 17

// The v17 projections and DDL are immutable and deliberately independent of
// current business models. Adding fields later requires another migration.
var invoiceSchemaStatements = map[string][]migrationStatement{
	DialectSQLite:   invoiceStatements("INTEGER PRIMARY KEY AUTOINCREMENT", "INTEGER", ""),
	DialectMySQL:    invoiceStatements("BIGINT AUTO_INCREMENT PRIMARY KEY", "BIGINT", " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 ROW_FORMAT=DYNAMIC"),
	DialectPostgres: invoiceStatements("BIGSERIAL PRIMARY KEY", "BIGINT", ""),
}

func invoiceStatements(identity, integer, suffix string) []migrationStatement {
	return []migrationStatement{
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS invoice_requests (
id ` + identity + `,
public_id VARCHAR(64) NOT NULL,
tenant_id ` + integer + ` NOT NULL,
user_id ` + integer + ` NOT NULL,
status VARCHAR(32) NOT NULL,
version ` + integer + ` NOT NULL,
policy_version VARCHAR(64) NOT NULL,
seller_id VARCHAR(64) NOT NULL,
seller_name VARCHAR(200) NOT NULL,
tax_group VARCHAR(64) NOT NULL,
profile_id VARCHAR(64) NOT NULL,
profile_cipher TEXT NOT NULL,
note_cipher TEXT NOT NULL,
invoice_type VARCHAR(64) NOT NULL,
currency VARCHAR(8) NOT NULL,
currency_exponent ` + integer + ` NOT NULL,
amount_minor ` + integer + ` NOT NULL,
current_delivery_version ` + integer + ` NOT NULL,
active_dispute_id ` + integer + ` NOT NULL,
hold_from_status VARCHAR(32) NOT NULL,
reason_cipher TEXT NOT NULL,
ever_delivered BOOLEAN NOT NULL,
issued_evidence BOOLEAN NOT NULL,
created_at ` + integer + ` NOT NULL,
updated_at ` + integer + ` NOT NULL,
reviewed_at ` + integer + ` NOT NULL,
delivered_at ` + integer + ` NOT NULL,
confirmed_at ` + integer + ` NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_invoice_requests_public_id ON invoice_requests (public_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_invoice_owner ON invoice_requests (tenant_id, user_id, created_at)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_invoice_status ON invoice_requests (tenant_id, status, created_at)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS invoice_request_items (
id ` + identity + `,
tenant_id ` + integer + ` NOT NULL,
user_id ` + integer + ` NOT NULL,
request_id ` + integer + ` NOT NULL,
canonical_payment_key VARCHAR(64) NOT NULL,
source_type VARCHAR(32) NOT NULL,
source_id ` + integer + ` NOT NULL,
order_id VARCHAR(128) NOT NULL,
order_no VARCHAR(255) NOT NULL,
amount_minor ` + integer + ` NOT NULL,
currency VARCHAR(8) NOT NULL,
paid_at ` + integer + ` NOT NULL,
payment_label VARCHAR(64) NOT NULL,
tax_group VARCHAR(64) NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_invoice_request_items_request_id ON invoice_request_items (request_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_invoice_request_items_canonical_payment_key ON invoice_request_items (canonical_payment_key)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_invoice_request_items_order_no ON invoice_request_items (order_no)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS invoice_order_claims (
id ` + identity + `,
tenant_id ` + integer + ` NOT NULL,
canonical_payment_key VARCHAR(64) NOT NULL,
request_id ` + integer + ` NOT NULL,
created_at ` + integer + ` NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_invoice_claim ON invoice_order_claims (tenant_id, canonical_payment_key)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_invoice_order_claims_request_id ON invoice_order_claims (request_id)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS invoice_attachments (
id ` + identity + `,
public_id VARCHAR(64) NOT NULL,
tenant_id ` + integer + ` NOT NULL,
request_id ` + integer + ` NOT NULL,
uploader_id ` + integer + ` NOT NULL,
purpose VARCHAR(32) NOT NULL,
object_key VARCHAR(255) NOT NULL,
file_name VARCHAR(255) NOT NULL,
mime_type VARCHAR(64) NOT NULL,
size ` + integer + ` NOT NULL,
sha256 VARCHAR(64) NOT NULL,
status VARCHAR(32) NOT NULL,
upload_key VARCHAR(64) NOT NULL,
bound_at ` + integer + ` NOT NULL,
expires_at ` + integer + ` NOT NULL,
created_at ` + integer + ` NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_invoice_attachments_public_id ON invoice_attachments (public_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_invoice_upload ON invoice_attachments (tenant_id, request_id, uploader_id, purpose, upload_key)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_invoice_attachments_expires_at ON invoice_attachments (expires_at)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS invoice_deliveries (
id ` + identity + `,
tenant_id ` + integer + ` NOT NULL,
request_id ` + integer + ` NOT NULL,
delivery_version ` + integer + ` NOT NULL,
attachment_id ` + integer + ` NOT NULL,
invoice_no VARCHAR(128) NOT NULL,
issued_on VARCHAR(10) NOT NULL,
amount_minor ` + integer + ` NOT NULL,
currency VARCHAR(8) NOT NULL,
tax_minor ` + integer + ` NOT NULL,
actor_id ` + integer + ` NOT NULL,
created_at ` + integer + ` NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_invoice_delivery ON invoice_deliveries (request_id, delivery_version)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS invoice_disputes (
id ` + identity + `,
public_id VARCHAR(64) NOT NULL,
tenant_id ` + integer + ` NOT NULL,
request_id ` + integer + ` NOT NULL,
delivery_version ` + integer + ` NOT NULL,
round ` + integer + ` NOT NULL,
category VARCHAR(32) NOT NULL,
description_cipher TEXT NOT NULL,
evidence_attachment_id ` + integer + ` NOT NULL,
status VARCHAR(32) NOT NULL,
reply_cipher TEXT NOT NULL,
actor_id ` + integer + ` NOT NULL,
created_at ` + integer + ` NOT NULL,
updated_at ` + integer + ` NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_invoice_disputes_public_id ON invoice_disputes (public_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_invoice_disputes_request_id ON invoice_disputes (request_id)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS invoice_events (
id ` + identity + `,
public_id VARCHAR(64) NOT NULL,
tenant_id ` + integer + ` NOT NULL,
request_id ` + integer + ` NOT NULL,
user_id ` + integer + ` NOT NULL,
actor_id ` + integer + ` NOT NULL,
actor_admin BOOLEAN NOT NULL,
kind VARCHAR(32) NOT NULL,
status VARCHAR(32) NOT NULL,
delivery_version ` + integer + ` NOT NULL,
description_cipher TEXT NOT NULL,
attachment_id ` + integer + ` NOT NULL,
created_at ` + integer + ` NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_invoice_events_public_id ON invoice_events (public_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_invoice_event ON invoice_events (tenant_id, request_id, created_at)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_invoice_events_user_id ON invoice_events (user_id)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS invoice_event_reads (
id ` + identity + `,
tenant_id ` + integer + ` NOT NULL,
event_id ` + integer + ` NOT NULL,
user_id ` + integer + ` NOT NULL,
read_at ` + integer + ` NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_invoice_event_read ON invoice_event_reads (tenant_id, event_id, user_id)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS invoice_operations (
id ` + identity + `,
tenant_id ` + integer + ` NOT NULL,
actor_id ` + integer + ` NOT NULL,
operation VARCHAR(128) NOT NULL,
operation_key VARCHAR(64) NOT NULL,
digest VARCHAR(64) NOT NULL,
result_id VARCHAR(64) NOT NULL,
result_code VARCHAR(64) NOT NULL,
created_at ` + integer + ` NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_invoice_operation ON invoice_operations (tenant_id, actor_id, operation, operation_key)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS invoice_profiles (
id ` + identity + `,
public_id VARCHAR(64) NOT NULL,
tenant_id ` + integer + ` NOT NULL,
user_id ` + integer + ` NOT NULL,
data_cipher TEXT NOT NULL,
version ` + integer + ` NOT NULL,
deleted_at ` + integer + ` NOT NULL,
created_at ` + integer + ` NOT NULL,
updated_at ` + integer + ` NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_invoice_profiles_public_id ON invoice_profiles (public_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_invoice_profile_owner ON invoice_profiles (tenant_id, user_id)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS invoice_payment_receipts (
id ` + identity + `,
tenant_id ` + integer + ` NOT NULL,
user_id ` + integer + ` NOT NULL,
top_up_id ` + integer + ` NOT NULL,
provider VARCHAR(64) NOT NULL,
receipt_key VARCHAR(64) NOT NULL,
amount_minor ` + integer + ` NOT NULL,
currency VARCHAR(8) NOT NULL,
status VARCHAR(32) NOT NULL,
exception_receipt_key VARCHAR(64) NOT NULL,
paid_at ` + integer + ` NOT NULL,
created_at ` + integer + ` NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_invoice_payment_receipts_top_up_id ON invoice_payment_receipts (top_up_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_invoice_payment_receipts_receipt_key ON invoice_payment_receipts (receipt_key)`},
	}
}

type invoicerequestV17 struct {
	ID                     int64  `gorm:"primaryKey"`
	PublicID               string `gorm:"type:varchar(64);not null;uniqueIndex"`
	TenantID               int64  `gorm:"not null;index:idx_invoice_owner,priority:1;index:idx_invoice_status,priority:1"`
	UserID                 int    `gorm:"not null;index:idx_invoice_owner,priority:2"`
	Status                 string `gorm:"type:varchar(32);not null;index:idx_invoice_status,priority:2"`
	Version                int64  `gorm:"not null"`
	PolicyVersion          string `gorm:"type:varchar(64);not null"`
	SellerID               string `gorm:"type:varchar(64);not null"`
	SellerName             string `gorm:"type:varchar(200);not null"`
	TaxGroup               string `gorm:"type:varchar(64);not null"`
	ProfileID              string `gorm:"type:varchar(64);not null"`
	ProfileCipher          string `gorm:"type:text;not null"`
	NoteCipher             string `gorm:"type:text;not null"`
	InvoiceType            string `gorm:"type:varchar(64);not null"`
	Currency               string `gorm:"type:varchar(8);not null"`
	CurrencyExponent       int    `gorm:"not null"`
	AmountMinor            int64  `gorm:"not null"`
	CurrentDeliveryVersion int64  `gorm:"not null"`
	ActiveDisputeID        int64  `gorm:"not null"`
	HoldFromStatus         string `gorm:"type:varchar(32);not null"`
	ReasonCipher           string `gorm:"type:text;not null"`
	EverDelivered          bool   `gorm:"type:boolean;not null"`
	IssuedEvidence         bool   `gorm:"type:boolean;not null"`
	CreatedAt              int64  `gorm:"not null;autoCreateTime:false;index:idx_invoice_owner,priority:3;index:idx_invoice_status,priority:3"`
	UpdatedAt              int64  `gorm:"not null;autoUpdateTime:false"`
	ReviewedAt             int64  `gorm:"not null"`
	DeliveredAt            int64  `gorm:"not null"`
	ConfirmedAt            int64  `gorm:"not null"`
}

func (invoicerequestV17) TableName() string { return "invoice_requests" }

type invoicerequestitemV17 struct {
	ID                  int64  `gorm:"primaryKey"`
	TenantID            int64  `gorm:"not null"`
	UserID              int    `gorm:"not null"`
	RequestID           int64  `gorm:"not null;index"`
	CanonicalPaymentKey string `gorm:"type:varchar(64);not null;index"`
	SourceType          string `gorm:"type:varchar(32);not null"`
	SourceID            int64  `gorm:"not null"`
	OrderID             string `gorm:"type:varchar(128);not null"`
	OrderNo             string `gorm:"type:varchar(255);not null;index"`
	AmountMinor         int64  `gorm:"not null"`
	Currency            string `gorm:"type:varchar(8);not null"`
	PaidAt              int64  `gorm:"not null"`
	PaymentLabel        string `gorm:"type:varchar(64);not null"`
	TaxGroup            string `gorm:"type:varchar(64);not null"`
}

func (invoicerequestitemV17) TableName() string { return "invoice_request_items" }

type invoiceorderclaimV17 struct {
	ID                  int64  `gorm:"primaryKey"`
	TenantID            int64  `gorm:"not null;uniqueIndex:idx_invoice_claim,priority:1"`
	CanonicalPaymentKey string `gorm:"type:varchar(64);not null;uniqueIndex:idx_invoice_claim,priority:2"`
	RequestID           int64  `gorm:"not null;index"`
	CreatedAt           int64  `gorm:"not null;autoCreateTime:false"`
}

func (invoiceorderclaimV17) TableName() string { return "invoice_order_claims" }

type invoiceattachmentV17 struct {
	ID         int64  `gorm:"primaryKey"`
	PublicID   string `gorm:"type:varchar(64);not null;uniqueIndex"`
	TenantID   int64  `gorm:"not null;uniqueIndex:idx_invoice_upload,priority:1"`
	RequestID  int64  `gorm:"not null;uniqueIndex:idx_invoice_upload,priority:2"`
	UploaderID int    `gorm:"not null;uniqueIndex:idx_invoice_upload,priority:3"`
	Purpose    string `gorm:"type:varchar(32);not null;uniqueIndex:idx_invoice_upload,priority:4"`
	ObjectKey  string `gorm:"type:varchar(255);not null"`
	FileName   string `gorm:"type:varchar(255);not null"`
	MIMEType   string `gorm:"type:varchar(64);not null"`
	Size       int64  `gorm:"not null"`
	SHA256     string `gorm:"type:varchar(64);not null"`
	Status     string `gorm:"type:varchar(32);not null"`
	UploadKey  string `gorm:"type:varchar(64);not null;uniqueIndex:idx_invoice_upload,priority:5"`
	BoundAt    int64  `gorm:"not null"`
	ExpiresAt  int64  `gorm:"not null;index"`
	CreatedAt  int64  `gorm:"not null;autoCreateTime:false"`
}

func (invoiceattachmentV17) TableName() string { return "invoice_attachments" }

type invoicedeliveryV17 struct {
	ID              int64  `gorm:"primaryKey"`
	TenantID        int64  `gorm:"not null"`
	RequestID       int64  `gorm:"not null;uniqueIndex:idx_invoice_delivery,priority:1"`
	DeliveryVersion int64  `gorm:"not null;uniqueIndex:idx_invoice_delivery,priority:2"`
	AttachmentID    int64  `gorm:"not null"`
	InvoiceNo       string `gorm:"type:varchar(128);not null"`
	IssuedOn        string `gorm:"type:varchar(10);not null"`
	AmountMinor     int64  `gorm:"not null"`
	Currency        string `gorm:"type:varchar(8);not null"`
	TaxMinor        int64  `gorm:"not null"`
	ActorID         int    `gorm:"not null"`
	CreatedAt       int64  `gorm:"not null;autoCreateTime:false"`
}

func (invoicedeliveryV17) TableName() string { return "invoice_deliveries" }

type invoicedisputeV17 struct {
	ID                   int64  `gorm:"primaryKey"`
	PublicID             string `gorm:"type:varchar(64);not null;uniqueIndex"`
	TenantID             int64  `gorm:"not null"`
	RequestID            int64  `gorm:"not null;index"`
	DeliveryVersion      int64  `gorm:"not null"`
	Round                int64  `gorm:"not null"`
	Category             string `gorm:"type:varchar(32);not null"`
	DescriptionCipher    string `gorm:"type:text;not null"`
	EvidenceAttachmentID int64  `gorm:"not null"`
	Status               string `gorm:"type:varchar(32);not null"`
	ReplyCipher          string `gorm:"type:text;not null"`
	ActorID              int    `gorm:"not null"`
	CreatedAt            int64  `gorm:"not null;autoCreateTime:false"`
	UpdatedAt            int64  `gorm:"not null;autoUpdateTime:false"`
}

func (invoicedisputeV17) TableName() string { return "invoice_disputes" }

type invoiceeventV17 struct {
	ID                int64  `gorm:"primaryKey"`
	PublicID          string `gorm:"type:varchar(64);not null;uniqueIndex"`
	TenantID          int64  `gorm:"not null;index:idx_invoice_event,priority:1"`
	RequestID         int64  `gorm:"not null;index:idx_invoice_event,priority:2"`
	UserID            int    `gorm:"not null;index"`
	ActorID           int    `gorm:"not null"`
	ActorAdmin        bool   `gorm:"type:boolean;not null"`
	Kind              string `gorm:"type:varchar(32);not null"`
	Status            string `gorm:"type:varchar(32);not null"`
	DeliveryVersion   int64  `gorm:"not null"`
	DescriptionCipher string `gorm:"type:text;not null"`
	AttachmentID      int64  `gorm:"not null"`
	CreatedAt         int64  `gorm:"not null;autoCreateTime:false;index:idx_invoice_event,priority:3"`
}

func (invoiceeventV17) TableName() string { return "invoice_events" }

type invoiceeventreadV17 struct {
	ID       int64 `gorm:"primaryKey"`
	TenantID int64 `gorm:"not null;uniqueIndex:idx_invoice_event_read,priority:1"`
	EventID  int64 `gorm:"not null;uniqueIndex:idx_invoice_event_read,priority:2"`
	UserID   int   `gorm:"not null;uniqueIndex:idx_invoice_event_read,priority:3"`
	ReadAt   int64 `gorm:"not null"`
}

func (invoiceeventreadV17) TableName() string { return "invoice_event_reads" }

type invoiceoperationV17 struct {
	ID           int64  `gorm:"primaryKey"`
	TenantID     int64  `gorm:"not null;uniqueIndex:idx_invoice_operation,priority:1"`
	ActorID      int    `gorm:"not null;uniqueIndex:idx_invoice_operation,priority:2"`
	Operation    string `gorm:"type:varchar(128);not null;uniqueIndex:idx_invoice_operation,priority:3"`
	OperationKey string `gorm:"type:varchar(64);not null;uniqueIndex:idx_invoice_operation,priority:4"`
	Digest       string `gorm:"type:varchar(64);not null"`
	ResultID     string `gorm:"type:varchar(64);not null"`
	ResultCode   string `gorm:"type:varchar(64);not null"`
	CreatedAt    int64  `gorm:"not null;autoCreateTime:false"`
}

func (invoiceoperationV17) TableName() string { return "invoice_operations" }

type invoiceprofileV17 struct {
	ID         int64  `gorm:"primaryKey"`
	PublicID   string `gorm:"type:varchar(64);not null;uniqueIndex"`
	TenantID   int64  `gorm:"not null;index:idx_invoice_profile_owner,priority:1"`
	UserID     int    `gorm:"not null;index:idx_invoice_profile_owner,priority:2"`
	DataCipher string `gorm:"type:text;not null"`
	Version    int64  `gorm:"not null"`
	DeletedAt  int64  `gorm:"not null"`
	CreatedAt  int64  `gorm:"not null;autoCreateTime:false"`
	UpdatedAt  int64  `gorm:"not null;autoUpdateTime:false"`
}

func (invoiceprofileV17) TableName() string { return "invoice_profiles" }

type invoicepaymentreceiptV17 struct {
	ID                  int64  `gorm:"primaryKey"`
	TenantID            int64  `gorm:"not null"`
	UserID              int    `gorm:"not null"`
	TopUpID             int64  `gorm:"not null;uniqueIndex"`
	Provider            string `gorm:"type:varchar(64);not null"`
	ReceiptKey          string `gorm:"type:varchar(64);not null;uniqueIndex"`
	AmountMinor         int64  `gorm:"not null"`
	Currency            string `gorm:"type:varchar(8);not null"`
	Status              string `gorm:"type:varchar(32);not null"`
	ExceptionReceiptKey string `gorm:"type:varchar(64);not null"`
	PaidAt              int64  `gorm:"not null"`
	CreatedAt           int64  `gorm:"not null;autoCreateTime:false"`
}

func (invoicepaymentreceiptV17) TableName() string { return "invoice_payment_receipts" }

type invoiceIndexRequirement struct {
	Table   string
	Name    string
	Columns []string
	Unique  bool
}

var invoiceIndexes = []invoiceIndexRequirement{
	{Table: "invoice_requests", Name: "idx_invoice_requests_public_id", Columns: []string{"public_id"}, Unique: true},
	{Table: "invoice_requests", Name: "idx_invoice_owner", Columns: []string{"tenant_id", "user_id", "created_at"}, Unique: false},
	{Table: "invoice_requests", Name: "idx_invoice_status", Columns: []string{"tenant_id", "status", "created_at"}, Unique: false},
	{Table: "invoice_request_items", Name: "idx_invoice_request_items_request_id", Columns: []string{"request_id"}, Unique: false},
	{Table: "invoice_request_items", Name: "idx_invoice_request_items_canonical_payment_key", Columns: []string{"canonical_payment_key"}, Unique: false},
	{Table: "invoice_request_items", Name: "idx_invoice_request_items_order_no", Columns: []string{"order_no"}, Unique: false},
	{Table: "invoice_order_claims", Name: "idx_invoice_claim", Columns: []string{"tenant_id", "canonical_payment_key"}, Unique: true},
	{Table: "invoice_order_claims", Name: "idx_invoice_order_claims_request_id", Columns: []string{"request_id"}, Unique: false},
	{Table: "invoice_attachments", Name: "idx_invoice_attachments_public_id", Columns: []string{"public_id"}, Unique: true},
	{Table: "invoice_attachments", Name: "idx_invoice_upload", Columns: []string{"tenant_id", "request_id", "uploader_id", "purpose", "upload_key"}, Unique: true},
	{Table: "invoice_attachments", Name: "idx_invoice_attachments_expires_at", Columns: []string{"expires_at"}, Unique: false},
	{Table: "invoice_deliveries", Name: "idx_invoice_delivery", Columns: []string{"request_id", "delivery_version"}, Unique: true},
	{Table: "invoice_disputes", Name: "idx_invoice_disputes_public_id", Columns: []string{"public_id"}, Unique: true},
	{Table: "invoice_disputes", Name: "idx_invoice_disputes_request_id", Columns: []string{"request_id"}, Unique: false},
	{Table: "invoice_events", Name: "idx_invoice_events_public_id", Columns: []string{"public_id"}, Unique: true},
	{Table: "invoice_events", Name: "idx_invoice_event", Columns: []string{"tenant_id", "request_id", "created_at"}, Unique: false},
	{Table: "invoice_events", Name: "idx_invoice_events_user_id", Columns: []string{"user_id"}, Unique: false},
	{Table: "invoice_event_reads", Name: "idx_invoice_event_read", Columns: []string{"tenant_id", "event_id", "user_id"}, Unique: true},
	{Table: "invoice_operations", Name: "idx_invoice_operation", Columns: []string{"tenant_id", "actor_id", "operation", "operation_key"}, Unique: true},
	{Table: "invoice_profiles", Name: "idx_invoice_profiles_public_id", Columns: []string{"public_id"}, Unique: true},
	{Table: "invoice_profiles", Name: "idx_invoice_profile_owner", Columns: []string{"tenant_id", "user_id"}, Unique: false},
	{Table: "invoice_payment_receipts", Name: "idx_invoice_payment_receipts_top_up_id", Columns: []string{"top_up_id"}, Unique: true},
	{Table: "invoice_payment_receipts", Name: "idx_invoice_payment_receipts_receipt_key", Columns: []string{"receipt_key"}, Unique: true},
}

func validateInvoiceSchema(db *gorm.DB, complete bool) error {
	for _, step := range []tenantOnlineStep{
		{table: "invoice_requests", model: &invoicerequestV17{}},
		{table: "invoice_request_items", model: &invoicerequestitemV17{}},
		{table: "invoice_order_claims", model: &invoiceorderclaimV17{}},
		{table: "invoice_attachments", model: &invoiceattachmentV17{}},
		{table: "invoice_deliveries", model: &invoicedeliveryV17{}},
		{table: "invoice_disputes", model: &invoicedisputeV17{}},
		{table: "invoice_events", model: &invoiceeventV17{}},
		{table: "invoice_event_reads", model: &invoiceeventreadV17{}},
		{table: "invoice_operations", model: &invoiceoperationV17{}},
		{table: "invoice_profiles", model: &invoiceprofileV17{}},
		{table: "invoice_payment_receipts", model: &invoicepaymentreceiptV17{}},
	} {
		if !complete && !db.Migrator().HasTable(step.table) {
			continue
		}
		if err := step.validate(db); err != nil {
			return err
		}
	}
	for _, index := range invoiceIndexes {
		if !complete && !db.Migrator().HasIndex(index.Table, index.Name) {
			continue
		}
		if err := validateTenantOnlineIndex(db, index.Table, index.Name, index.Columns, index.Unique); err != nil {
			return err
		}
	}
	return nil
}

func InvoicePlanChecksum() string { return migrationChecksum(migrationSet()[InvoiceSchemaVersion-1]) }

type InvoiceExpandPrecheck struct {
	Schema          string `json:"schema"`
	CurrentVersion  int64  `json:"current_version"`
	TargetVersion   int64  `json:"target_version"`
	Safe            bool   `json:"safe"`
	InvoicesPresent bool   `json:"invoices_present"`
}

func PrecheckInvoiceExpand(ctx context.Context, db *gorm.DB) (*InvoiceExpandPrecheck, error) {
	if db == nil {
		return nil, ErrSchemaNotReady
	}
	state, err := loadValidatedState(ctx, db, InvoiceSchemaVersion)
	if err != nil {
		return nil, err
	}
	if state.currentVersion != RequestTicketSchemaVersion && state.currentVersion != InvoiceSchemaVersion {
		return nil, fmt.Errorf("%w: invoice expansion requires canonical schema 16 or 17", ErrSchemaNotReady)
	}
	if err := validateInvoiceSchema(db.WithContext(ctx), false); err != nil {
		return nil, err
	}
	present := false
	for _, index := range invoiceIndexes {
		present = present || db.Migrator().HasTable(index.Table)
	}
	return &InvoiceExpandPrecheck{Schema: "kkai_invoice_expand_v1", CurrentVersion: state.currentVersion, TargetVersion: InvoiceSchemaVersion, Safe: true, InvoicesPresent: present}, nil
}

// CheckInvoiceSchema is read-only: manually created tables without a canonical
// receipt never enable financial endpoints.
func CheckInvoiceSchema(ctx context.Context, db *gorm.DB) error {
	return checkThroughVersion(ctx, db, InvoiceSchemaVersion, InvoiceSchemaVersion, InvoiceSchemaVersion)
}

// ApplyInvoiceExpand is an explicit isolated rehearsal/controller entry point.
// It is never called by application startup. Production mutation requires a
// separately reviewed controller proof for the exact plan and all runtimes.
func ApplyInvoiceExpand(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	if !options.DryRun && !options.Offline && options.AuthorizeMutation == nil {
		return nil, fmt.Errorf("%w: invoice expansion needs explicit controller authorization or isolated offline mode", ErrUnsafeMigration)
	}
	result := &Result{}
	err := withTenantOnlineLock(ctx, db, options.Offline, func(locked *gorm.DB) error {
		precheck, err := PrecheckInvoiceExpand(ctx, locked)
		if err != nil {
			return err
		}
		item := migrationSet()[InvoiceSchemaVersion-1]
		entry := planItems([]migration{item})[0]
		if precheck.CurrentVersion == InvoiceSchemaVersion {
			return locked.Where("version = ?", InvoiceSchemaVersion).Find(&result.Applied).Error
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
			if err := validateInvoiceSchema(tx, true); err != nil {
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
		return locked.Where("version = ?", InvoiceSchemaVersion).Find(&result.Applied).Error
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
