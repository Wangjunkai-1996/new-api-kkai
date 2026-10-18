package kkaimigrate

// Membership is the v11 additive expand after the immutable v10 image async
// migration. Existing application tables and migration entries remain unchanged.
var membershipSchemaStatements = map[string][]migrationStatement{
	DialectSQLite:   membershipStatements("INTEGER PRIMARY KEY AUTOINCREMENT", "INTEGER", "TEXT", ""),
	DialectMySQL:    membershipStatements("BIGINT AUTO_INCREMENT PRIMARY KEY", "INT", "LONGTEXT", " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"),
	DialectPostgres: membershipStatements("BIGSERIAL PRIMARY KEY", "INTEGER", "TEXT", ""),
}

func membershipStatements(idType, intType, textType, suffix string) []migrationStatement {
	return []migrationStatement{
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS kkai_membership_catalog (
id ` + intType + ` PRIMARY KEY,
release_id BIGINT NOT NULL,
sales_enabled BOOLEAN NOT NULL,
topup_discounts_disabled BOOLEAN NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS kkai_membership_releases (
id ` + idType + `,
payload ` + textType + ` NOT NULL,
actor_id ` + intType + ` NOT NULL,
created_at BIGINT NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS kkai_membership_groups (
group_key VARCHAR(64) PRIMARY KEY,
enabled BOOLEAN NOT NULL,
first_release_id BIGINT NOT NULL,
created_at BIGINT NOT NULL,
updated_at BIGINT NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS kkai_membership_users (
user_id ` + intType + ` PRIMARY KEY,
current_account_id BIGINT NOT NULL,
pending_order_id BIGINT NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS kkai_membership_accounts (
id ` + idType + `,
user_id ` + intType + ` NOT NULL,
tier_id VARCHAR(64) NOT NULL,
tier_rank ` + intType + ` NOT NULL,
release_id BIGINT NOT NULL,
current_order_id BIGINT NOT NULL,
amount_total BIGINT NOT NULL,
amount_used BIGINT NOT NULL,
amount_reserved BIGINT NOT NULL,
started_at BIGINT NOT NULL,
expires_at BIGINT NOT NULL,
revision BIGINT NOT NULL,
status VARCHAR(24) NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_membership_accounts_user ON kkai_membership_accounts (user_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_membership_accounts_status ON kkai_membership_accounts (status, expires_at)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS kkai_membership_orders (
id ` + idType + `,
trade_no VARCHAR(128) NOT NULL,
user_id ` + intType + ` NOT NULL,
tier_id VARCHAR(64) NOT NULL,
release_id BIGINT NOT NULL,
amount_minor BIGINT NOT NULL,
currency VARCHAR(8) NOT NULL,
provider VARCHAR(32) NOT NULL,
payment_method VARCHAR(32) NOT NULL,
status VARCHAR(24) NOT NULL,
expires_at BIGINT NOT NULL,
terms ` + textType + ` NOT NULL,
receipt_id VARCHAR(128) NOT NULL,
receipt_key VARCHAR(191),
actual_amount_minor BIGINT NOT NULL,
actual_currency VARCHAR(8) NOT NULL,
actual_provider VARCHAR(32) NOT NULL,
actual_one_time BOOLEAN NOT NULL,
exception_reason VARCHAR(512) NOT NULL,
account_id BIGINT NOT NULL,
created_at BIGINT NOT NULL,
paid_at BIGINT NOT NULL,
resolved_at BIGINT NOT NULL,
resolution ` + textType + ` NOT NULL,
checkout_state VARCHAR(24) NOT NULL,
checkout_data ` + textType + ` NOT NULL,
checkout_started_at BIGINT NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX ux_membership_orders_trade ON kkai_membership_orders (trade_no)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX ux_membership_orders_receipt ON kkai_membership_orders (receipt_key)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_membership_orders_user ON kkai_membership_orders (user_id, id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_membership_orders_status ON kkai_membership_orders (status, expires_at)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS kkai_membership_payments (
id ` + idType + `,
receipt_key VARCHAR(191) NOT NULL,
order_id BIGINT NOT NULL,
provider VARCHAR(32) NOT NULL,
receipt_id VARCHAR(128) NOT NULL,
amount_minor BIGINT NOT NULL,
currency VARCHAR(8) NOT NULL,
one_time BOOLEAN NOT NULL,
status VARCHAR(24) NOT NULL,
reason VARCHAR(512) NOT NULL,
created_at BIGINT NOT NULL,
resolution ` + textType + ` NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX ux_membership_payments_receipt ON kkai_membership_payments (receipt_key)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_membership_payments_order ON kkai_membership_payments (order_id)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS kkai_membership_billing (
request_id VARCHAR(128) PRIMARY KEY,
billing_scope VARCHAR(16) NOT NULL,
user_id ` + intType + ` NOT NULL,
token_id ` + intType + ` NOT NULL,
token_reserved BIGINT NOT NULL,
token_quota_debited BOOLEAN NOT NULL,
token_refund_unapplied BIGINT NOT NULL,
token_used_refund_unapplied BIGINT NOT NULL,
account_id BIGINT NOT NULL,
account_revision BIGINT NOT NULL,
source VARCHAR(16) NOT NULL,
group_key VARCHAR(64) NOT NULL,
snapshot ` + textType + ` NOT NULL,
request_hash VARCHAR(64) NOT NULL,
reserved_quota BIGINT NOT NULL,
actual_quota BIGINT NOT NULL,
charged_quota BIGINT NOT NULL,
excess_quota BIGINT NOT NULL,
state VARCHAR(24) NOT NULL,
created_at BIGINT NOT NULL,
settled_at BIGINT NOT NULL,
dispatched_at BIGINT NOT NULL,
review_after BIGINT NOT NULL,
review_reason VARCHAR(512) NOT NULL,
review_started_at BIGINT NOT NULL,
review_channel_id ` + intType + ` NOT NULL,
review_resolution ` + textType + ` NOT NULL,
review_resolved_at BIGINT NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_membership_billing_user ON kkai_membership_billing (user_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_membership_billing_state ON kkai_membership_billing (state, created_at)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_membership_billing_review ON kkai_membership_billing (billing_scope, state, review_after)`},
	}
}

var membershipRuntimeSchemaRequirements = []runtimeSchemaRequirement{
	{Table: "kkai_membership_catalog", Columns: []string{"id", "release_id", "sales_enabled", "topup_discounts_disabled"}},
	{Table: "kkai_membership_releases", Columns: []string{"id", "payload", "actor_id", "created_at"}},
	{Table: "kkai_membership_groups", Columns: []string{"group_key", "enabled", "first_release_id", "created_at", "updated_at"}},
	{Table: "kkai_membership_users", Columns: []string{"user_id", "current_account_id", "pending_order_id"}},
	{Table: "kkai_membership_accounts", Columns: []string{"id", "user_id", "tier_id", "tier_rank", "release_id", "current_order_id", "amount_total", "amount_used", "amount_reserved", "started_at", "expires_at", "revision", "status"}},
	{Table: "kkai_membership_orders", Columns: []string{"id", "trade_no", "user_id", "tier_id", "release_id", "amount_minor", "currency", "provider", "payment_method", "status", "expires_at", "terms", "receipt_id", "receipt_key", "actual_amount_minor", "actual_currency", "actual_provider", "actual_one_time", "exception_reason", "account_id", "created_at", "paid_at", "resolved_at", "resolution", "checkout_state", "checkout_data", "checkout_started_at"}},
	{Table: "kkai_membership_payments", Columns: []string{"id", "receipt_key", "order_id", "provider", "receipt_id", "amount_minor", "currency", "one_time", "status", "reason", "created_at", "resolution"}},
	{Table: "kkai_membership_billing", Columns: []string{"request_id", "billing_scope", "user_id", "token_id", "token_reserved", "token_quota_debited", "token_refund_unapplied", "token_used_refund_unapplied", "account_id", "account_revision", "source", "group_key", "snapshot", "request_hash", "reserved_quota", "actual_quota", "charged_quota", "excess_quota", "state", "created_at", "settled_at", "dispatched_at", "review_after", "review_reason", "review_started_at", "review_channel_id", "review_resolution", "review_resolved_at"}},
}
