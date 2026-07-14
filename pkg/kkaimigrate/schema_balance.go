package kkaimigrate

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
