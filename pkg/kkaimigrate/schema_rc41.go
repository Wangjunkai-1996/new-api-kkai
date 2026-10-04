package kkaimigrate

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// rc41ArchitectureMigration adds the official rc.41 token, task-plugin, and
// persisted login-encryption-key stores, account metadata, and Midjourney billing
// attribution while preserving existing records.
func rc41ArchitectureMigration() migration {
	return migration{
		Version:          9,
		Name:             "rc41_scoped_tokens_and_task_plugins",
		Kind:             MigrationKindExpand,
		ImplementationID: "rc41_scoped_tokens_and_task_plugins_v2",
		ChecksumVersion:  migrationChecksumSchemaBackfill,
		BackfillSpec:     "provision one persisted RSA login key and the official 30-day legacy token retirement deadline with identity-only conflict updates; preserve existing values and omit key material from SQL logs",
		BackfillID:       "provision_rc41_authentication_v2",
		Backfill:         backfillRC41Authentication,
		Statements:       rc41ArchitectureSchemaStatements,
	}
}

var rc41ArchitectureSchemaStatements = map[string][]migrationStatement{
	DialectSQLite: {
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS audit_logs (
id INTEGER PRIMARY KEY AUTOINCREMENT,
event_id VARCHAR(64), user_id INTEGER, username VARCHAR(64), actor_role INTEGER,
created_at BIGINT, category VARCHAR(24), action VARCHAR(128), token_ref VARCHAR(64),
auth_method VARCHAR(24), ip VARCHAR(64), user_agent VARCHAR(512), method VARCHAR(16),
route VARCHAR(255), status INTEGER, success BOOLEAN, request_id VARCHAR(64), content TEXT, other TEXT
)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_audit_logs_event_id ON audit_logs (event_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_audit_user_time ON audit_logs (user_id, created_at)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_audit_token_time ON audit_logs (token_ref, created_at)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_audit_logs_request_id ON audit_logs (request_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_audit_logs_created_at ON audit_logs (created_at)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_audit_logs_username ON audit_logs (username)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_audit_logs_category ON audit_logs (category)`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE users ADD COLUMN access_token_created_at BIGINT`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE passkey_credentials ADD COLUMN rp_id VARCHAR(253)`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE midjourneys ADD COLUMN token_id BIGINT`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE midjourneys ADD COLUMN billing_channel_id BIGINT`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS user_access_tokens (
id INTEGER PRIMARY KEY AUTOINCREMENT,
user_id INTEGER,
name VARCHAR(64),
token_hash VARCHAR(64),
token_hint VARCHAR(8),
scopes VARCHAR(2048),
expires_at BIGINT,
last_used_at BIGINT,
last_used_ip VARCHAR(64),
created_at BIGINT
)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_user_access_tokens_user_id ON user_access_tokens (user_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_user_access_tokens_token_hash ON user_access_tokens (token_hash)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_user_access_tokens_expires_at ON user_access_tokens (expires_at)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS task_plugins (
id INTEGER PRIMARY KEY AUTOINCREMENT,
"key" VARCHAR(128) NOT NULL,
api_version INTEGER NOT NULL,
version VARCHAR(64) NOT NULL,
source TEXT NOT NULL,
source_hash VARCHAR(64) NOT NULL,
icon TEXT NOT NULL,
enabled BOOLEAN NOT NULL,
active BOOLEAN NOT NULL,
created_at BIGINT NOT NULL,
remark TEXT
)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX uk_task_plugin_key_version ON task_plugins (key, version)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_task_plugins_active ON task_plugins (active)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS login_encryption_keys (
id INTEGER PRIMARY KEY AUTOINCREMENT,
slot VARCHAR(32) NOT NULL,
private_key_pem TEXT NOT NULL
)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_login_encryption_keys_slot ON login_encryption_keys (slot)`},
	},
	DialectMySQL: {
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS audit_logs (
id BIGINT AUTO_INCREMENT PRIMARY KEY,
event_id VARCHAR(64), user_id INT, username VARCHAR(64), actor_role INT,
created_at BIGINT, category VARCHAR(24), action VARCHAR(128), token_ref VARCHAR(64),
auth_method VARCHAR(24), ip VARCHAR(64), user_agent VARCHAR(512), method VARCHAR(16),
route VARCHAR(255), status INT, success BOOLEAN, request_id VARCHAR(64), content TEXT, other TEXT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_audit_logs_event_id ON audit_logs (event_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_audit_user_time ON audit_logs (user_id, created_at)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_audit_token_time ON audit_logs (token_ref, created_at)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_audit_logs_request_id ON audit_logs (request_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_audit_logs_created_at ON audit_logs (created_at)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_audit_logs_username ON audit_logs (username)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_audit_logs_category ON audit_logs (category)`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE users ADD COLUMN access_token_created_at BIGINT`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE passkey_credentials ADD COLUMN rp_id VARCHAR(253)`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE midjourneys ADD COLUMN token_id BIGINT`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE midjourneys ADD COLUMN billing_channel_id BIGINT`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS user_access_tokens (
id BIGINT AUTO_INCREMENT PRIMARY KEY,
user_id INT,
name VARCHAR(64),
token_hash VARCHAR(64),
token_hint VARCHAR(8),
scopes VARCHAR(2048),
expires_at BIGINT,
last_used_at BIGINT,
last_used_ip VARCHAR(64),
created_at BIGINT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_user_access_tokens_user_id ON user_access_tokens (user_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_user_access_tokens_token_hash ON user_access_tokens (token_hash)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_user_access_tokens_expires_at ON user_access_tokens (expires_at)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS task_plugins (
id BIGINT AUTO_INCREMENT PRIMARY KEY,
` + "`key`" + ` VARCHAR(128) NOT NULL,
api_version INT NOT NULL,
version VARCHAR(64) NOT NULL,
source LONGTEXT NOT NULL,
source_hash VARCHAR(64) NOT NULL,
icon LONGTEXT NOT NULL,
enabled BOOLEAN NOT NULL,
active BOOLEAN NOT NULL,
created_at BIGINT NOT NULL,
remark TEXT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
		{Operation: migrationOperationCreateIndex, SQL: "CREATE UNIQUE INDEX uk_task_plugin_key_version ON task_plugins (`key`, version)"},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_task_plugins_active ON task_plugins (active)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS login_encryption_keys (
id BIGINT AUTO_INCREMENT PRIMARY KEY,
slot VARCHAR(32) NOT NULL,
private_key_pem TEXT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_login_encryption_keys_slot ON login_encryption_keys (slot)`},
	},
	DialectPostgres: {
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS audit_logs (
id BIGSERIAL PRIMARY KEY,
event_id VARCHAR(64), user_id INTEGER, username VARCHAR(64), actor_role INTEGER,
created_at BIGINT, category VARCHAR(24), action VARCHAR(128), token_ref VARCHAR(64),
auth_method VARCHAR(24), ip VARCHAR(64), user_agent VARCHAR(512), method VARCHAR(16),
route VARCHAR(255), status INTEGER, success BOOLEAN, request_id VARCHAR(64), content TEXT, other TEXT
)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_audit_logs_event_id ON audit_logs (event_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_audit_user_time ON audit_logs (user_id, created_at)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_audit_token_time ON audit_logs (token_ref, created_at)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_audit_logs_request_id ON audit_logs (request_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_audit_logs_created_at ON audit_logs (created_at)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_audit_logs_username ON audit_logs (username)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_audit_logs_category ON audit_logs (category)`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE users ADD COLUMN access_token_created_at BIGINT`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE passkey_credentials ADD COLUMN rp_id VARCHAR(253)`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE midjourneys ADD COLUMN token_id BIGINT`},
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE midjourneys ADD COLUMN billing_channel_id BIGINT`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS user_access_tokens (
id BIGSERIAL PRIMARY KEY,
user_id INTEGER,
name VARCHAR(64),
token_hash VARCHAR(64),
token_hint VARCHAR(8),
scopes VARCHAR(2048),
expires_at BIGINT,
last_used_at BIGINT,
last_used_ip VARCHAR(64),
created_at BIGINT
)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_user_access_tokens_user_id ON user_access_tokens (user_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_user_access_tokens_token_hash ON user_access_tokens (token_hash)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_user_access_tokens_expires_at ON user_access_tokens (expires_at)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS task_plugins (
id BIGSERIAL PRIMARY KEY,
"key" VARCHAR(128) NOT NULL,
api_version INTEGER NOT NULL,
version VARCHAR(64) NOT NULL,
source TEXT NOT NULL,
source_hash VARCHAR(64) NOT NULL,
icon TEXT NOT NULL,
enabled BOOLEAN NOT NULL,
active BOOLEAN NOT NULL,
created_at BIGINT NOT NULL,
remark TEXT
)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX uk_task_plugin_key_version ON task_plugins ("key", version)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_task_plugins_active ON task_plugins (active)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS login_encryption_keys (
id BIGSERIAL PRIMARY KEY,
slot VARCHAR(32) NOT NULL,
private_key_pem TEXT NOT NULL
)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_login_encryption_keys_slot ON login_encryption_keys (slot)`},
	},
}

// Authentication material is provisioned by the explicit migrator, never by a
// managed serving/candidate/readonly process. The unique slot makes retries safe.
func backfillRC41Authentication(tx *gorm.DB) error {
	var stored struct{ PrivateKeyPEM string }
	err := tx.Table("login_encryption_keys").Where("slot = ?", "active").Take(&stored).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		key, err := common.GeneratePasswordEncryptionPrivateKey()
		if err != nil {
			return err
		}
		// A map has no primary-key metadata for MySQL's DoNothing emulation.
		// Updating only the conflicting identity preserves the existing key.
		// Suppress SQL logging because even an error must not print the PEM.
		if err := tx.Session(&gorm.Session{Logger: tx.Logger.LogMode(logger.Silent)}).
			Table("login_encryption_keys").Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "slot"}},
			DoUpdates: clause.AssignmentColumns([]string{"slot"}),
		}).Create(map[string]any{"slot": "active", "private_key_pem": key}).Error; err != nil {
			return err
		}
		if err := tx.Table("login_encryption_keys").Where("slot = ?", "active").Take(&stored).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := common.LoadPasswordEncryptionPrivateKey(stored.PrivateKeyPEM); err != nil {
		return fmt.Errorf("invalid persisted login encryption key: %w", err)
	}
	deadline := strconv.FormatInt(time.Now().Unix()+30*24*60*60, 10)
	if err := tx.Table("options").Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"key"}),
	}).Create(map[string]any{"key": "LegacyAccessTokenRetireAt", "value": deadline}).Error; err != nil {
		return err
	}
	var option struct{ Value string }
	if err := tx.Table("options").Where(map[string]any{"key": "LegacyAccessTokenRetireAt"}).Take(&option).Error; err != nil {
		return err
	}
	retireAt, err := strconv.ParseInt(option.Value, 10, 64)
	if err != nil || retireAt <= 0 {
		return fmt.Errorf("invalid LegacyAccessTokenRetireAt option")
	}
	return nil
}
