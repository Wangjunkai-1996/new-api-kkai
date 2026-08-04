package kkaimigrate

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const authenticationIdentityProviderTelegram = "telegram"

var errAuthenticationIdentityOwnership = errors.New("authentication identity ownership conflict")

var authenticationSchemaStatements = map[string][]migrationStatement{
	DialectSQLite: authenticationStatements(
		"INTEGER PRIMARY KEY AUTOINCREMENT", "INTEGER", "DATETIME", "",
	),
	DialectMySQL: authenticationStatements(
		"BIGINT AUTO_INCREMENT PRIMARY KEY", "INT", "DATETIME(3)", " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
	),
	DialectPostgres: authenticationStatements(
		"BIGSERIAL PRIMARY KEY", "INTEGER", "TIMESTAMPTZ", "",
	),
}

func authenticationStatements(idType string, integerType string, timeType string, tableSuffix string) []migrationStatement {
	return []migrationStatement{
		{Operation: migrationOperationAddNullableColumn, SQL: `ALTER TABLE users ADD COLUMN auth_version BIGINT`},
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
	}
}

type authenticationLegacyUser struct {
	ID         int64          `gorm:"column:id"`
	TelegramID sql.NullString `gorm:"column:telegram_id"`
}

type authenticationIdentityClaim struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Provider  string    `gorm:"column:provider"`
	Subject   string    `gorm:"column:subject"`
	UserID    int64     `gorm:"column:user_id"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func backfillAuthenticationSchema(tx *gorm.DB) error {
	if tx == nil {
		return ErrSchemaNotReady
	}
	if err := tx.Exec(`UPDATE users
SET auth_version = 1
WHERE auth_version IS NULL OR auth_version < 1`).Error; err != nil {
		return fmt.Errorf("backfill users.auth_version: %w", err)
	}

	var users []authenticationLegacyUser
	if err := tx.Table("users").
		Select("id", "telegram_id").
		Order("id ASC").
		Find(&users).Error; err != nil {
		return fmt.Errorf("load legacy Telegram identities: %w", err)
	}

	createdAt := time.Now().UTC()
	for _, user := range users {
		subject := strings.TrimSpace(user.TelegramID.String)
		if !user.TelegramID.Valid || subject == "" {
			continue
		}
		if user.ID <= 0 {
			return fmt.Errorf("backfill Telegram identity for user %d: invalid user ID", user.ID)
		}
		if err := claimAuthenticationIdentity(tx, user.ID, subject, createdAt); err != nil {
			return err
		}
	}
	return nil
}

func claimAuthenticationIdentity(tx *gorm.DB, userID int64, subject string, createdAt time.Time) error {
	claim := authenticationIdentityClaim{
		Provider:  authenticationIdentityProviderTelegram,
		Subject:   subject,
		UserID:    userID,
		CreatedAt: createdAt,
	}
	if err := tx.Table("external_identity_claims").
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&claim).Error; err != nil {
		return fmt.Errorf("backfill Telegram identity for user %d: create claim: %w", userID, err)
	}

	// A no-op insert can be either the exact mapping or a conflict on either
	// unique key, so confirm both ownership directions before accepting it.
	var subjectOwner struct {
		UserID int64 `gorm:"column:user_id"`
	}
	result := tx.Table("external_identity_claims").
		Select("user_id").
		Where("provider = ? AND subject = ?", authenticationIdentityProviderTelegram, subject).
		Limit(1).
		Scan(&subjectOwner)
	if result.Error != nil {
		return fmt.Errorf("backfill Telegram identity for user %d: verify subject owner: %w", userID, result.Error)
	}
	if result.RowsAffected != 1 || subjectOwner.UserID != userID {
		return authenticationIdentityConflict(userID)
	}

	var userClaim struct {
		Subject string `gorm:"column:subject"`
	}
	result = tx.Table("external_identity_claims").
		Select("subject").
		Where("provider = ? AND user_id = ?", authenticationIdentityProviderTelegram, userID).
		Limit(1).
		Scan(&userClaim)
	if result.Error != nil {
		return fmt.Errorf("backfill Telegram identity for user %d: verify user owner: %w", userID, result.Error)
	}
	if result.RowsAffected != 1 || userClaim.Subject != subject {
		return authenticationIdentityConflict(userID)
	}
	return nil
}

func authenticationIdentityConflict(userID int64) error {
	return fmt.Errorf("backfill Telegram identity for user %d: %w", userID, errAuthenticationIdentityOwnership)
}
