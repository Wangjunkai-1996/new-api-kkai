package kkaimigrate

var imageAsyncSchemaStatements = map[string][]migrationStatement{
	DialectSQLite:   imageAsyncStatements("INTEGER", "TEXT", ""),
	DialectMySQL:    imageAsyncStatements("INT", "LONGTEXT", " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"),
	DialectPostgres: imageAsyncStatements("INTEGER", "TEXT", ""),
}

func imageAsyncStatements(integerType, textType, suffix string) []migrationStatement {
	return []migrationStatement{
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS kkai_image_reference_uploads (
asset_id BIGINT PRIMARY KEY,
owner_user_id ` + integerType + ` NOT NULL,
upload_key VARCHAR(128) NOT NULL,
upload_id VARCHAR(512) NOT NULL,
upload_part_size BIGINT NOT NULL,
upload_expires_at BIGINT NOT NULL,
upload_manifest ` + textType + ` NOT NULL,
cleanup_at BIGINT NOT NULL,
created_at BIGINT NOT NULL,
updated_at BIGINT NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX ux_image_reference_upload_key ON kkai_image_reference_uploads (owner_user_id, upload_key)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_image_reference_upload_cleanup ON kkai_image_reference_uploads (cleanup_at, asset_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_image_reference_upload_expiry ON kkai_image_reference_uploads (upload_expires_at, asset_id)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS kkai_image_executions (
generation_id BIGINT PRIMARY KEY,
state VARCHAR(24) NOT NULL,
fence VARCHAR(128) NOT NULL,
event_id BIGINT NOT NULL,
payload ` + textType + ` NOT NULL,
dispatch_channel_id ` + integerType + ` NOT NULL DEFAULT 0,
dispatch_started_at BIGINT NOT NULL DEFAULT 0,
upstream_request_id VARCHAR(191) NOT NULL DEFAULT '',
queue_deadline_at BIGINT NOT NULL,
execution_deadline_at BIGINT NOT NULL DEFAULT 0,
recovery_deadline_at BIGINT NOT NULL DEFAULT 0,
created_at BIGINT NOT NULL,
updated_at BIGINT NOT NULL
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_image_execution_state ON kkai_image_executions (state, updated_at, generation_id)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS kkai_image_generation_references (
generation_id BIGINT NOT NULL,
position ` + integerType + ` NOT NULL,
asset_id BIGINT NOT NULL,
created_at BIGINT NOT NULL,
PRIMARY KEY (generation_id, position)
)` + suffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_image_generation_reference_asset ON kkai_image_generation_references (asset_id, generation_id)`},
	}
}
