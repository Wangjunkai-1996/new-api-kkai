package kkaimigrate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

const authenticationIdentityProviderTelegram = "telegram"

var errAuthenticationIdentityOwnership = errors.New("authentication identity ownership conflict")

var authenticationSchemaStatements = map[string][]migrationStatement{
	DialectSQLite: authenticationStatements(
		"INTEGER PRIMARY KEY AUTOINCREMENT", "INTEGER", "DATETIME", "",
		migrationStatement{Operation: migrationOperationAddColumnDefault, SQL: `ALTER TABLE users ADD COLUMN auth_version BIGINT DEFAULT 1`},
	),
	DialectMySQL: authenticationStatements(
		"BIGINT AUTO_INCREMENT PRIMARY KEY", "INT", "DATETIME(3)", " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		migrationStatement{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE users ADD COLUMN auth_version BIGINT`},
		migrationStatement{Operation: migrationOperationSetColumnDefault, SQL: `ALTER TABLE users ALTER COLUMN auth_version SET DEFAULT 1`},
	),
	DialectPostgres: authenticationStatements(
		"BIGSERIAL PRIMARY KEY", "INTEGER", "TIMESTAMPTZ", "",
		migrationStatement{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE users ADD COLUMN auth_version BIGINT`},
		migrationStatement{Operation: migrationOperationSetColumnDefault, SQL: `ALTER TABLE users ALTER COLUMN auth_version SET DEFAULT 1`},
	),
}

func authenticationStatements(
	idType string,
	integerType string,
	timeType string,
	tableSuffix string,
	authVersionStatements ...migrationStatement,
) []migrationStatement {
	statements := append([]migrationStatement(nil), authVersionStatements...)
	return append(statements, []migrationStatement{
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE tokens ADD COLUMN auto_groups TEXT`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS user_sessions (
sid VARCHAR(64) NOT NULL PRIMARY KEY,
user_id ` + integerType + ` NOT NULL,
version BIGINT NOT NULL DEFAULT 1,
user_auth_version BIGINT NOT NULL,
status VARCHAR(16) NOT NULL,
refresh_hash CHAR(64) NOT NULL,
previous_refresh_hash VARCHAR(64),
previous_valid_until BIGINT NOT NULL DEFAULT 0,
login_method VARCHAR(32) NOT NULL,
ip VARCHAR(64),
user_agent TEXT,
created_at BIGINT NOT NULL,
last_active_at BIGINT NOT NULL,
expires_at BIGINT NOT NULL,
revoked_at BIGINT NOT NULL DEFAULT 0,
revoked_reason VARCHAR(64)
)` + tableSuffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_user_sessions_user_status_expiry ON user_sessions (user_id, status, expires_at)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_user_sessions_user_created ON user_sessions (user_id, created_at)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_user_sessions_status_revoked ON user_sessions (status, revoked_at)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_user_sessions_expires_at ON user_sessions (expires_at)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS auth_flows (
id ` + idType + `,
token_hash CHAR(64) NOT NULL,
purpose VARCHAR(32) NOT NULL,
provider VARCHAR(64),
intent VARCHAR(16),
user_id ` + integerType + `,
session_id VARCHAR(64),
payload TEXT,
created_at ` + timeType + ` NOT NULL,
expires_at ` + timeType + ` NOT NULL,
consumed_at ` + timeType + `
)` + tableSuffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_auth_flows_token_hash ON auth_flows (token_hash)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_auth_flow_purpose_expiry ON auth_flows (purpose, expires_at)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_auth_flows_user_id ON auth_flows (user_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_auth_flows_session_id ON auth_flows (session_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_auth_flows_consumed_at ON auth_flows (consumed_at)`},
		{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE IF NOT EXISTS external_identity_claims (
id ` + idType + `,
provider VARCHAR(32) NOT NULL,
subject VARCHAR(128) NOT NULL,
user_id ` + integerType + ` NOT NULL,
created_at ` + timeType + ` NOT NULL
)` + tableSuffix},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_external_identity_subject ON external_identity_claims (provider, subject)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE UNIQUE INDEX idx_external_identity_user ON external_identity_claims (provider, user_id)`},
		{Operation: migrationOperationCreateIndex, SQL: `CREATE INDEX idx_external_identity_claims_user_id ON external_identity_claims (user_id)`},
	}...)
}

type AuthenticationExpandPrecheck struct {
	Schema                        string `json:"schema"`
	TargetVersion                 int64  `json:"target_version"`
	UserCount                     int64  `json:"user_count"`
	LegacyTelegramIdentityCount   int64  `json:"legacy_telegram_identity_count"`
	AmbiguousTelegramSubjectCount int64  `json:"ambiguous_telegram_subject_count"`
	Safe                          bool   `json:"safe"`
}

type authenticationIdentityClaim struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Provider  string    `gorm:"column:provider"`
	Subject   string    `gorm:"column:subject"`
	UserID    int64     `gorm:"column:user_id"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func PrecheckAuthenticationExpand(ctx context.Context, db *gorm.DB) (*AuthenticationExpandPrecheck, error) {
	if db == nil {
		return nil, ErrSchemaNotReady
	}
	query := db.WithContext(ctx)
	result := &AuthenticationExpandPrecheck{
		Schema:        "kkai_authentication_expand_precheck_v1",
		TargetVersion: AuthenticationSchemaVersion,
	}
	if err := query.Table("users").Count(&result.UserCount).Error; err != nil {
		return nil, fmt.Errorf("count users for authentication expand: %w", err)
	}
	if err := query.Table("users").
		Where("telegram_id IS NOT NULL AND TRIM(telegram_id) <> ''").
		Count(&result.LegacyTelegramIdentityCount).Error; err != nil {
		return nil, fmt.Errorf("count legacy Telegram identities: %w", err)
	}
	if err := query.Raw(`SELECT COUNT(*) FROM (
SELECT TRIM(telegram_id) AS subject
FROM users
WHERE telegram_id IS NOT NULL AND TRIM(telegram_id) <> ''
GROUP BY TRIM(telegram_id)
HAVING COUNT(*) > 1
) AS ambiguous_telegram_subjects`).Scan(&result.AmbiguousTelegramSubjectCount).Error; err != nil {
		return nil, fmt.Errorf("count ambiguous legacy Telegram subjects: %w", err)
	}
	result.Safe = result.AmbiguousTelegramSubjectCount == 0
	return result, nil
}

func backfillAuthenticationSchema(tx *gorm.DB) error {
	if tx == nil {
		return ErrSchemaNotReady
	}
	precheck, err := PrecheckAuthenticationExpand(tx.Statement.Context, tx)
	if err != nil {
		return err
	}
	if precheck.AmbiguousTelegramSubjectCount != 0 {
		return fmt.Errorf(
			"%w: ambiguous_telegram_subject_count=%d",
			errAuthenticationIdentityOwnership,
			precheck.AmbiguousTelegramSubjectCount,
		)
	}

	dialect, err := dialectName(tx)
	if err != nil {
		return err
	}
	if err := tx.Exec(`UPDATE users
SET auth_version = 1
WHERE auth_version IS NULL OR auth_version < 1`).Error; err != nil {
		return fmt.Errorf("initialize users.auth_version: %w", err)
	}

	insertStatements := map[string]string{
		DialectSQLite: `INSERT OR IGNORE INTO external_identity_claims
(provider, subject, user_id, created_at)
SELECT ?, TRIM(telegram_id), id, ? FROM users
WHERE telegram_id IS NOT NULL AND TRIM(telegram_id) <> ''`,
		DialectMySQL: `INSERT IGNORE INTO external_identity_claims
(provider, subject, user_id, created_at)
SELECT ?, TRIM(telegram_id), id, ? FROM users
WHERE telegram_id IS NOT NULL AND TRIM(telegram_id) <> ''`,
		DialectPostgres: `INSERT INTO external_identity_claims
(provider, subject, user_id, created_at)
SELECT ?, TRIM(telegram_id), id, ? FROM users
WHERE telegram_id IS NOT NULL AND TRIM(telegram_id) <> ''
ON CONFLICT DO NOTHING`,
	}
	if err := tx.Exec(
		insertStatements[dialect],
		authenticationIdentityProviderTelegram,
		time.Now().UTC(),
	).Error; err != nil {
		return fmt.Errorf("backfill legacy Telegram identities: %w", err)
	}

	unmapped, err := countUnmappedLegacyTelegramIdentities(tx)
	if err != nil {
		return err
	}
	if unmapped != 0 {
		return fmt.Errorf("%w: unmapped_legacy_telegram_identity_count=%d", errAuthenticationIdentityOwnership, unmapped)
	}
	return nil
}

func countUnmappedLegacyTelegramIdentities(db *gorm.DB) (int64, error) {
	var count int64
	err := db.Raw(`SELECT COUNT(*) FROM users AS u
LEFT JOIN external_identity_claims AS c
  ON c.provider = ?
 AND c.subject = TRIM(u.telegram_id)
 AND c.user_id = u.id
WHERE u.telegram_id IS NOT NULL
  AND TRIM(u.telegram_id) <> ''
  AND c.id IS NULL`, authenticationIdentityProviderTelegram).Scan(&count).Error
	if err != nil {
		return 0, fmt.Errorf("verify legacy Telegram identity backfill: %w", err)
	}
	return count, nil
}
