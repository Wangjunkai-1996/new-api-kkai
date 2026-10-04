package kkaimigrate

import (
	"context"
	"github.com/QuantumNous/new-api/model"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRC41ArchitectureMigrationIsAdditiveAcrossDialects(t *testing.T) {
	item := rc41ArchitectureMigration()
	require.Equal(t, int64(9), item.Version)
	require.NoError(t, validateMigrationCatalog(migrationSet()))

	for _, dialect := range requiredMigrationDialects {
		t.Run(dialect, func(t *testing.T) {
			statements := item.Statements[dialect]
			require.NotEmpty(t, statements)
			allSQL := make([]string, 0, len(statements))
			for _, statement := range statements {
				require.NotContains(t, strings.ToUpper(statement.SQL), "DROP ")
				require.NotContains(t, strings.ToUpper(statement.SQL), "TRUNCATE ")
				allSQL = append(allSQL, statement.SQL)
			}
			joined := strings.Join(allSQL, "\n")
			for _, table := range []string{"user_access_tokens", "task_plugins", "login_encryption_keys", "audit_logs"} {
				require.Contains(t, joined, "CREATE TABLE IF NOT EXISTS "+table)
			}
			require.Contains(t, joined, "CREATE UNIQUE INDEX idx_user_access_tokens_token_hash")
			require.Contains(t, joined, "CREATE UNIQUE INDEX uk_task_plugin_key_version")
			require.Contains(t, joined, "CREATE UNIQUE INDEX idx_login_encryption_keys_slot")
			if dialect == DialectMySQL {
				require.Contains(t, joined, "source LONGTEXT NOT NULL")
				require.Contains(t, joined, "icon LONGTEXT NOT NULL")
			} else {
				require.Contains(t, joined, "source TEXT NOT NULL")
				require.Contains(t, joined, "icon TEXT NOT NULL")
			}
		})
	}
}

func TestRC41ArchitectureSQLiteDDLExecutes(t *testing.T) {
	db := newAuthenticationSchemaTestDB(t)
	for _, statement := range rc41ArchitectureSchemaStatements[DialectSQLite] {
		require.NoError(t, executeMigrationStatement(db, DialectSQLite, statement), statement.SQL)
	}
	for _, table := range []string{"user_access_tokens", "task_plugins", "login_encryption_keys", "audit_logs"} {
		require.True(t, db.Migrator().HasTable(table), table)
	}
	for table, indexes := range map[string][]string{
		"user_access_tokens":    {"idx_user_access_tokens_user_id", "idx_user_access_tokens_token_hash", "idx_user_access_tokens_expires_at"},
		"task_plugins":          {"uk_task_plugin_key_version", "idx_task_plugins_active"},
		"login_encryption_keys": {"idx_login_encryption_keys_slot"},
	} {
		for _, index := range indexes {
			require.True(t, db.Migrator().HasIndex(table, index), index)
		}
	}
}

func TestRC41ExpandPreservesDataAndProvisionsAuthenticationOnce(t *testing.T) {
	db := newMigrationTestDB(t)
	require.NoError(t, db.Exec("INSERT INTO users (id, telegram_id) VALUES (1, '123')").Error)
	_, err := applyThroughVersion(context.Background(), db, Options{}, AuthenticationSchemaVersion, RC41ArchitectureSchemaVersion)
	require.NoError(t, err)
	_, err = ApplyRC41ArchitectureExpand(context.Background(), db, Options{DryRun: true})
	require.NoError(t, err)
	require.False(t, db.Migrator().HasTable("login_encryption_keys"))
	_, err = ApplyRC41ArchitectureExpand(context.Background(), db, Options{})
	require.NoError(t, err)
	var key model.LoginEncryptionKey
	require.NoError(t, db.First(&key).Error)
	require.NotEmpty(t, key.PrivateKeyPEM)
	var deadline model.Option
	require.NoError(t, db.Where(map[string]any{"key": "LegacyAccessTokenRetireAt"}).Take(&deadline).Error)
	require.NotEmpty(t, deadline.Value)
	require.NoError(t, db.Transaction(backfillRC41Authentication))
	var sameKey model.LoginEncryptionKey
	require.NoError(t, db.First(&sameKey).Error)
	require.Equal(t, key, sameKey)
	var sameDeadline model.Option
	require.NoError(t, db.Where(map[string]any{"key": "LegacyAccessTokenRetireAt"}).Take(&sameDeadline).Error)
	require.Equal(t, deadline, sameDeadline)
	var telegram string
	require.NoError(t, db.Table("users").Select("telegram_id").Where("id = ?", 1).Scan(&telegram).Error)
	require.Equal(t, "123", telegram)
	var count int64
	require.NoError(t, db.Table("external_identity_claims").Count(&count).Error)
	require.EqualValues(t, 1, count)
	token := model.UserAccessToken{UserId: 1, TokenHash: "hash", CreatedAt: time.Now().Unix()}
	require.NoError(t, db.Create(&token).Error)
	event := model.AuditLog{EventId: "event-1", UserId: 1, Other: model.AuditOther{LoginMethod: "password"}}
	require.NoError(t, db.Create(&event).Error)
	var restored model.AuditLog
	require.NoError(t, db.First(&restored).Error)
	require.Equal(t, "password", restored.Other.LoginMethod)
	require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)
	require.NoError(t, CheckRequired(context.Background(), db))
}
