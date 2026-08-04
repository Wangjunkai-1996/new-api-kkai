package kkaimigrate

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAuthenticationV8DefinesOnlyAdditiveSchemaForEveryDialect(t *testing.T) {
	expectedIndexes := []string{
		"CREATE INDEX idx_user_sessions_user_status_expiry ON user_sessions (user_id, status, expires_at)",
		"CREATE INDEX idx_user_sessions_user_created ON user_sessions (user_id, created_at)",
		"CREATE INDEX idx_user_sessions_status_revoked ON user_sessions (status, revoked_at)",
		"CREATE INDEX idx_user_sessions_expires_at ON user_sessions (expires_at)",
		"CREATE UNIQUE INDEX idx_auth_flows_token_hash ON auth_flows (token_hash)",
		"CREATE INDEX idx_auth_flow_purpose_expiry ON auth_flows (purpose, expires_at)",
		"CREATE INDEX idx_auth_flows_user_id ON auth_flows (user_id)",
		"CREATE INDEX idx_auth_flows_session_id ON auth_flows (session_id)",
		"CREATE INDEX idx_auth_flows_consumed_at ON auth_flows (consumed_at)",
		"CREATE UNIQUE INDEX idx_external_identity_subject ON external_identity_claims (provider, subject)",
		"CREATE UNIQUE INDEX idx_external_identity_user ON external_identity_claims (provider, user_id)",
		"CREATE INDEX idx_external_identity_claims_user_id ON external_identity_claims (user_id)",
	}
	dialects := []struct {
		name        string
		idType      string
		integerType string
		timeType    string
		tableSuffix string
	}{
		{name: DialectSQLite, idType: "INTEGER PRIMARY KEY AUTOINCREMENT", integerType: "INTEGER", timeType: "DATETIME"},
		{name: DialectMySQL, idType: "BIGINT AUTO_INCREMENT PRIMARY KEY", integerType: "INT", timeType: "DATETIME(3)", tableSuffix: "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"},
		{name: DialectPostgres, idType: "BIGSERIAL PRIMARY KEY", integerType: "INTEGER", timeType: "TIMESTAMPTZ"},
	}
	for _, dialect := range dialects {
		t.Run(dialect.name, func(t *testing.T) {
			statements := authenticationSchemaStatements[dialect.name]
			expectedStatementCount := 17
			autoGroupsIndex := 1
			if dialect.name != DialectSQLite {
				expectedStatementCount++
				autoGroupsIndex++
			}
			require.Len(t, statements, expectedStatementCount)
			expectedAuthVersion := migrationStatement{
				Operation: migrationOperationAddNullableColumn,
				SQL:       "ALTER TABLE users ADD COLUMN auth_version BIGINT",
			}
			if dialect.name == DialectSQLite {
				expectedAuthVersion = migrationStatement{
					Operation: migrationOperationAddColumnDefault,
					SQL:       "ALTER TABLE users ADD COLUMN auth_version BIGINT DEFAULT 1",
				}
			}
			require.Equal(t, expectedAuthVersion, statements[0])
			if dialect.name != DialectSQLite {
				require.Equal(t, migrationStatement{
					Operation: migrationOperationSetColumnDefault,
					SQL:       "ALTER TABLE users ALTER COLUMN auth_version SET DEFAULT 1",
				}, statements[1])
			}
			require.Equal(t, migrationStatement{
				Operation: migrationOperationAddNullableColumn,
				SQL:       "ALTER TABLE tokens ADD COLUMN auto_groups TEXT",
			}, statements[autoGroupsIndex])
			for _, statement := range statements[:autoGroupsIndex+1] {
				upper := strings.ToUpper(statement.SQL)
				require.NotContains(t, upper, "NOT NULL")
			}
			if dialect.name == DialectSQLite {
				require.Contains(t, statements[0].SQL, "DEFAULT 1")
			} else {
				require.NotContains(t, statements[0].SQL, "DEFAULT")
			}
			require.NotContains(t, statements[autoGroupsIndex].SQL, "DEFAULT")

			allSQL := make([]string, 0, len(statements))
			for _, statement := range statements {
				upper := strings.ToUpper(statement.SQL)
				require.NotContains(t, upper, "DROP ")
				require.NotContains(t, upper, "TRUNCATE ")
				allSQL = append(allSQL, statement.SQL)
			}
			joined := strings.Join(allSQL, "\n")
			if dialect.name != DialectSQLite {
				require.NotContains(t, joined, "ADD COLUMN auth_version BIGINT DEFAULT")
			}
			for _, table := range []string{"user_sessions", "auth_flows", "external_identity_claims"} {
				require.Contains(t, joined, "CREATE TABLE IF NOT EXISTS "+table)
			}
			require.Contains(t, joined, "id "+dialect.idType)
			require.Contains(t, joined, "user_id "+dialect.integerType)
			require.Contains(t, joined, "created_at "+dialect.timeType+" NOT NULL")
			for _, index := range expectedIndexes {
				require.Contains(t, joined, index)
			}
			if dialect.tableSuffix != "" {
				require.Equal(t, 3, strings.Count(joined, dialect.tableSuffix))
			}
		})
	}
	require.NoError(t, validateMigrationCatalog(migrationSet()))
}

func TestAuthenticationV8SQLiteDDLAndBackfillAreIdempotent(t *testing.T) {
	db := newAuthenticationSchemaTestDB(t)
	applyAuthenticationSQLiteDDL(t, db)
	applyAuthenticationSQLiteDDL(t, db)

	require.NoError(t, db.Exec(`INSERT INTO users (id, telegram_id, auth_version) VALUES
(1, ' telegram-one ', NULL),
(2, '', 0),
(3, '   ', 7),
(4, NULL, -2),
(5, 'telegram-five', 9)`).Error)
	require.NoError(t, db.Exec("INSERT INTO users (id, telegram_id) VALUES (?, NULL)", 6).Error)
	require.NoError(t, db.Exec(`INSERT INTO external_identity_claims
(provider, subject, user_id, created_at) VALUES (?, ?, ?, ?)`,
		authenticationIdentityProviderTelegram, "telegram-five", 5, time.Now().UTC()).Error)

	for range 2 {
		require.NoError(t, db.Transaction(backfillAuthenticationSchema))
	}

	type userVersion struct {
		ID          int64 `gorm:"column:id"`
		AuthVersion int64 `gorm:"column:auth_version"`
	}
	var versions []userVersion
	require.NoError(t, db.Table("users").Select("id", "auth_version").Order("id").Find(&versions).Error)
	require.Equal(t, []userVersion{
		{ID: 1, AuthVersion: 1},
		{ID: 2, AuthVersion: 1},
		{ID: 3, AuthVersion: 7},
		{ID: 4, AuthVersion: 1},
		{ID: 5, AuthVersion: 9},
		{ID: 6, AuthVersion: 1},
	}, versions)

	var claims []authenticationIdentityClaim
	require.NoError(t, db.Table("external_identity_claims").Order("user_id").Find(&claims).Error)
	require.Len(t, claims, 2)
	require.Equal(t, int64(1), claims[0].UserID)
	require.Equal(t, "telegram-one", claims[0].Subject)
	require.Equal(t, int64(5), claims[1].UserID)
	require.Equal(t, "telegram-five", claims[1].Subject)

	for _, table := range []string{"user_sessions", "auth_flows", "external_identity_claims"} {
		require.True(t, db.Migrator().HasTable(table), table)
	}
	for table, indexes := range map[string][]string{
		"user_sessions": {
			"idx_user_sessions_user_status_expiry",
			"idx_user_sessions_user_created",
			"idx_user_sessions_status_revoked",
			"idx_user_sessions_expires_at",
		},
		"auth_flows": {
			"idx_auth_flows_token_hash",
			"idx_auth_flow_purpose_expiry",
			"idx_auth_flows_user_id",
			"idx_auth_flows_session_id",
			"idx_auth_flows_consumed_at",
		},
		"external_identity_claims": {
			"idx_external_identity_subject",
			"idx_external_identity_user",
			"idx_external_identity_claims_user_id",
		},
	} {
		for _, index := range indexes {
			require.True(t, db.Migrator().HasIndex(table, index), index)
		}
	}
}

func TestAuthenticationBackfillRejectsBothOwnershipDirectionsWithoutSubjectLeak(t *testing.T) {
	const legacySubject = "sensitive-legacy-subject"
	const existingSubject = "sensitive-existing-subject"
	tests := []struct {
		name          string
		legacyUserID  int64
		existingUser  int64
		existingValue string
	}{
		{
			name:          "subject already belongs to another user",
			legacyUserID:  11,
			existingUser:  22,
			existingValue: legacySubject,
		},
		{
			name:          "user already owns another subject",
			legacyUserID:  33,
			existingUser:  33,
			existingValue: existingSubject,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newAuthenticationSchemaTestDB(t)
			applyAuthenticationSQLiteDDL(t, db)
			require.NoError(t, db.Exec(
				"INSERT INTO users (id, telegram_id, auth_version) VALUES (?, ?, NULL)",
				test.legacyUserID, " "+legacySubject+" ",
			).Error)
			require.NoError(t, db.Exec(`INSERT INTO external_identity_claims
(provider, subject, user_id, created_at) VALUES (?, ?, ?, ?)`,
				authenticationIdentityProviderTelegram, test.existingValue, test.existingUser, time.Now().UTC(),
			).Error)

			err := db.Transaction(backfillAuthenticationSchema)
			require.ErrorIs(t, err, errAuthenticationIdentityOwnership)
			require.ErrorContains(t, err, "unmapped_legacy_telegram_identity_count=1")
			require.NotContains(t, err.Error(), legacySubject)
			require.NotContains(t, err.Error(), existingSubject)

			var authVersion *int64
			require.NoError(t, db.Table("users").Select("auth_version").Where("id = ?", test.legacyUserID).Scan(&authVersion).Error)
			require.Nil(t, authVersion, "the auth-version update must roll back with the identity conflict")
			var count int64
			require.NoError(t, db.Table("external_identity_claims").Count(&count).Error)
			require.EqualValues(t, 1, count, "the pre-existing claim must remain the only claim")
		})
	}
}

func TestAuthenticationBackfillRejectsAmbiguousLegacySubjectsAtomically(t *testing.T) {
	const subject = "sensitive-duplicate-subject"
	db := newAuthenticationSchemaTestDB(t)
	applyAuthenticationSQLiteDDL(t, db)
	require.NoError(t, db.Exec(`INSERT INTO users (id, telegram_id, auth_version) VALUES
(41, ?, NULL),
(42, ?, NULL)`, subject, " "+subject+" ").Error)

	err := db.Transaction(backfillAuthenticationSchema)
	require.ErrorIs(t, err, errAuthenticationIdentityOwnership)
	require.ErrorContains(t, err, "ambiguous_telegram_subject_count=1")
	require.NotContains(t, err.Error(), subject)

	var count int64
	require.NoError(t, db.Table("external_identity_claims").Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Table("users").Where("auth_version IS NOT NULL").Count(&count).Error)
	require.Zero(t, count)
}

func TestAuthenticationPrecheckReturnsOnlySanitizedAggregates(t *testing.T) {
	db := newAuthenticationSchemaTestDB(t)
	const sensitiveSubject = "sensitive-duplicate-subject"
	require.NoError(t, db.Exec(`INSERT INTO users (id, telegram_id) VALUES
(51, ?),
(52, ?),
(53, NULL)`, sensitiveSubject, " "+sensitiveSubject+" ").Error)

	result, err := PrecheckAuthenticationExpand(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, "kkai_authentication_expand_precheck_v1", result.Schema)
	require.EqualValues(t, AuthenticationSchemaVersion, result.TargetVersion)
	require.EqualValues(t, 3, result.UserCount)
	require.EqualValues(t, 2, result.LegacyTelegramIdentityCount)
	require.EqualValues(t, 1, result.AmbiguousTelegramSubjectCount)
	require.False(t, result.Safe)
}

func TestAuthenticationV8AppliesAfterV7OnSQLite(t *testing.T) {
	db := newAuthenticationSchemaTestDB(t)
	require.NoError(t, db.Exec(
		"INSERT INTO users (id, telegram_id) VALUES (?, ?)", 71, "telegram-seventy-one",
	).Error)

	_, err := applyThroughVersion(
		context.Background(), db, Options{}, ImageStudioSchemaVersion, AuthenticationSchemaVersion,
	)
	require.NoError(t, err)
	result, err := applyThroughVersion(
		context.Background(), db, Options{}, AuthenticationSchemaVersion, AuthenticationSchemaVersion,
	)
	require.NoError(t, err)
	require.Len(t, result.Applied, int(AuthenticationSchemaVersion))
	require.Empty(t, result.Pending)

	var version int64
	require.NoError(t, db.Table("users").Select("auth_version").Where("id = ?", 71).Scan(&version).Error)
	require.EqualValues(t, 1, version)
	var claim authenticationIdentityClaim
	require.NoError(t, db.Table("external_identity_claims").Where("user_id = ?", 71).Take(&claim).Error)
	require.Equal(t, "telegram-seventy-one", claim.Subject)

	second, err := applyThroughVersion(
		context.Background(), db, Options{}, AuthenticationSchemaVersion, AuthenticationSchemaVersion,
	)
	require.NoError(t, err)
	require.Len(t, second.Applied, int(AuthenticationSchemaVersion))
}

func newAuthenticationSchemaTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newMigrationTestDB(t)
}

func applyAuthenticationSQLiteDDL(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, statement := range authenticationSchemaStatements[DialectSQLite] {
		require.NoError(t, executeMigrationStatement(db, DialectSQLite, statement), statement.SQL)
	}
}
