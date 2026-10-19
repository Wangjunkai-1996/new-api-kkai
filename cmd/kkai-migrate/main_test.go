package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestOpenDatabaseSupportsExplicitSQLiteDSN(t *testing.T) {
	dsn := fmt.Sprintf("file:kkai-cli-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := openDatabase(dsn)
	require.NoError(t, err)
	prepareLegacyAuthenticationTables(t, db)
	result, err := applyHistoricalFixture(context.Background(), db)
	require.NoError(t, err)
	require.Empty(t, result.Pending)
	require.NoError(t, kkaimigrate.CheckMembershipSchema(context.Background(), db))
}

func TestApplyMigrationTargetRejectsUnknownVersion(t *testing.T) {
	_, err := applyMigrationTarget(context.Background(), nil, 14, kkaimigrate.Options{})
	require.ErrorContains(t, err, "expected 3, 4, 5, 6, 7, 8, 9, 10, 11, or 13")
}

func TestDescribeContractJSONUsesImmutableExternalSchemaManagement(t *testing.T) {
	previous := common.SchemaManagementMode
	common.SchemaManagementMode = common.SchemaManagementExternal
	t.Cleanup(func() { common.SchemaManagementMode = previous })

	output, err := describeContractJSON("postgres")
	require.NoError(t, err)
	require.Contains(t, output, `"schema_management":"external"`)
}

func TestObserveCurrentSchemaRejectsMissingApplicationPrerequisite(t *testing.T) {
	dsn := fmt.Sprintf("file:kkai-observe-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := openDatabase(dsn)
	require.NoError(t, err)
	prepareLegacyAuthenticationTables(t, db)
	_, err = applyHistoricalFixture(context.Background(), db)
	require.NoError(t, err)

	_, err = observeCurrentSchema(context.Background(), db)
	require.NoError(t, err)
	require.ErrorIs(t, model.ValidateMainSchemaPrerequisites(db), model.ErrMainSchemaNotReady)
}

func TestObserveHistoricalSchemaDoesNotRequireRC41Models(t *testing.T) {
	for _, version := range []int64{7, 8} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			db, err := openDatabase("file:" + t.TempDir() + "/historical.db")
			require.NoError(t, err)
			prepareLegacyAuthenticationTables(t, db)
			_, err = applyHistoricalFixture(context.Background(), db)
			require.NoError(t, err)
			// Construct the historical test database with its original ledger
			// prefix and without the tables and columns introduced later.
			for _, table := range []string{"audit_logs", "user_access_tokens", "task_plugins", "login_encryption_keys"} {
				require.NoError(t, db.Migrator().DropTable(table))
			}
			require.NoError(t, db.Exec("ALTER TABLE users DROP COLUMN access_token_created_at").Error)
			require.NoError(t, db.Exec("ALTER TABLE passkey_credentials DROP COLUMN rp_id").Error)
			require.NoError(t, db.Exec("ALTER TABLE midjourneys DROP COLUMN token_id").Error)
			require.NoError(t, db.Exec("ALTER TABLE midjourneys DROP COLUMN billing_channel_id").Error)
			if version == 7 {
				for _, table := range []string{"user_sessions", "auth_flows", "external_identity_claims"} {
					require.NoError(t, db.Migrator().DropTable(table))
				}
				require.NoError(t, db.Exec("ALTER TABLE users DROP COLUMN auth_version").Error)
				require.NoError(t, db.Exec("ALTER TABLE tokens DROP COLUMN auto_groups").Error)
			}
			require.NoError(t, db.Where("version > ?", version).Delete(&kkaimigrate.AppliedMigration{}).Error)
			require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)

			observation, err := observeCurrentSchema(context.Background(), db)
			require.NoError(t, err)
			assert.Equal(t, version, observation.CurrentVersion)
			assert.ErrorIs(t, kkaimigrate.CheckRequired(context.Background(), db), kkaimigrate.ErrSchemaNotReady)
		})
	}
}

func prepareLegacyAuthenticationTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Exec(`CREATE TABLE users (
id INTEGER PRIMARY KEY,
telegram_id TEXT
)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE tokens (
id INTEGER PRIMARY KEY
)`).Error)
	require.NoError(t, db.Exec("CREATE TABLE passkey_credentials (id INTEGER PRIMARY KEY)").Error)
	require.NoError(t, db.Exec("CREATE TABLE options (key VARCHAR(255) PRIMARY KEY, value TEXT)").Error)
	require.NoError(t, db.Exec("CREATE TABLE midjourneys (id INTEGER PRIMARY KEY)").Error)
}

func TestFirstNonEmptyIgnoresWhitespace(t *testing.T) {
	require.Equal(t, "postgres://example", firstNonEmpty("", "  ", "postgres://example", "ignored"))
	require.Empty(t, firstNonEmpty("", "  "))
}

func TestResolveMigrationDSNReadsSingleValueFromStdin(t *testing.T) {
	for name, input := range map[string]string{
		"without terminator": "postgres://example/db",
		"with LF":            "postgres://example/db\n",
		"with CRLF":          "postgres://example/db\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			dsn, err := resolveMigrationDSN("", true, strings.NewReader(input))
			require.NoError(t, err)
			require.Equal(t, "postgres://example/db", dsn)
		})
	}
}

func TestResolveMigrationDSNRejectsAmbiguousOrUnsafeStdin(t *testing.T) {
	_, err := resolveMigrationDSN("postgres://environment/db", true, strings.NewReader("postgres://stdin/db\n"))
	require.ErrorContains(t, err, "cannot be combined")

	_, err = resolveMigrationDSN("", true, strings.NewReader("postgres://first/db\npostgres://second/db\n"))
	require.ErrorContains(t, err, "exactly one line")

	_, err = resolveMigrationDSN("", true, strings.NewReader("\npostgres://example/db\n"))
	require.ErrorContains(t, err, "exactly one line")

	_, err = resolveMigrationDSN("", true, strings.NewReader("postgres://example/db\n\n"))
	require.ErrorContains(t, err, "exactly one line")

	_, err = resolveMigrationDSN("", true, strings.NewReader(strings.Repeat("x", 8193)))
	require.ErrorContains(t, err, "exceeds 8192 bytes")
}

func TestApplyTenantMigrationTargetIsExplicit(t *testing.T) {
	db, err := openDatabase("file:" + t.TempDir() + "/tenant.db")
	require.NoError(t, err)
	prepareLegacyAuthenticationTables(t, db)
	_, err = applyMigrationTarget(context.Background(), db, 0, kkaimigrate.Options{})
	require.ErrorContains(t, err, "explicit --target 13")
	_, err = applyMigrationTarget(context.Background(), db, kkaimigrate.TenantSchemaVersion, kkaimigrate.Options{})
	assert.ErrorIs(t, err, kkaimigrate.ErrUnsafeMigration)
	_, err = applyHistoricalFixture(context.Background(), db)
	require.NoError(t, err)
	for _, statement := range []string{
		"ALTER TABLE users ADD COLUMN username VARCHAR(20)",
		"ALTER TABLE users ADD COLUMN email VARCHAR(50)",
		"CREATE TABLE top_ups(id BIGINT PRIMARY KEY)",
		"CREATE TABLE user_oauth_bindings(id BIGINT PRIMARY KEY)",
		"CREATE TABLE logs(id BIGINT PRIMARY KEY)",
		"CREATE TABLE quota_data(id BIGINT PRIMARY KEY)",
		"CREATE TABLE tasks(id BIGINT PRIMARY KEY)",
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	_, err = applyMigrationTarget(context.Background(), db, 13, kkaimigrate.Options{DryRun: true})
	require.NoError(t, err)
	assert.False(t, db.Migrator().HasTable("tenants"))
	_, err = applyMigrationTarget(context.Background(), db, 13, kkaimigrate.Options{})
	require.NoError(t, err)
	require.NoError(t, kkaimigrate.CheckTenantCompatibilitySchema(context.Background(), db))
}

func applyHistoricalFixture(ctx context.Context, db *gorm.DB) (*kkaimigrate.Result, error) {
	var result *kkaimigrate.Result
	var err error
	for _, target := range []int64{3, 4, 5, 6, 7, 8, 9, 10, 11} {
		result, err = applyMigrationTarget(ctx, db, target, kkaimigrate.Options{})
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func TestTenantLogSchemaCLIOperations(t *testing.T) {
	db, err := openDatabase("file:" + t.TempDir() + "/logs.db")
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TABLE logs(id BIGINT PRIMARY KEY)").Error)
	var output bytes.Buffer
	require.NoError(t, runTenantLogSchema(context.Background(), db, "status", &output))
	assert.JSONEq(t, `{"schema":"kkai-tenant-log-v12","ready":false}`, output.String())
	require.ErrorIs(t, runTenantLogSchema(context.Background(), db, "check", &output), kkaimigrate.ErrSchemaNotReady)
	require.NoError(t, runTenantLogSchema(context.Background(), db, "apply", &output))
	require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)
	output.Reset()
	require.NoError(t, runTenantLogSchema(context.Background(), db, "status", &output))
	assert.JSONEq(t, `{"schema":"kkai-tenant-log-v12","ready":true}`, output.String())
	require.NoError(t, runTenantLogSchema(context.Background(), db, "check", &output))
}

func TestTenantPrecheckCLIBlocksUnsafeIdentities(t *testing.T) {
	db, err := openDatabase("file:" + t.TempDir() + "/tenant-precheck.db")
	require.NoError(t, err)
	prepareLegacyAuthenticationTables(t, db)
	_, err = applyHistoricalFixture(context.Background(), db)
	require.NoError(t, err)
	for _, statement := range []string{
		"ALTER TABLE users ADD COLUMN username VARCHAR(20)",
		"ALTER TABLE users ADD COLUMN email VARCHAR(50)",
		"CREATE TABLE top_ups(id BIGINT PRIMARY KEY)",
		"CREATE TABLE user_oauth_bindings(id BIGINT PRIMARY KEY)",
		"CREATE TABLE logs(id BIGINT PRIMARY KEY)",
		"CREATE TABLE quota_data(id BIGINT PRIMARY KEY)",
		"CREATE TABLE tasks(id BIGINT PRIMARY KEY)",
		"INSERT INTO users(id,username,email) VALUES(1,' Legacy ','private@example.com'),(2,'legacy','other@example.com')",
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)
	var output bytes.Buffer
	err = runMigrationPrecheck(context.Background(), db, 12, &output)
	assert.ErrorIs(t, err, kkaimigrate.ErrSchemaNotReady)
	assert.ErrorContains(t, err, "operator repair required")
	assert.JSONEq(t, `{"schema":"kkai_tenant_schema_precheck_v1","target_version":12,"user_count":2,"invalid_identity_count":0,"duplicate_username_count":1,"duplicate_email_count":0,"safe":false}`, output.String())
	assert.False(t, db.Migrator().HasColumn("users", "normalized_username"))
	assert.False(t, db.Migrator().HasTable("tenants"))
}

func TestTenantCompatibilityPrecheckCLIReportsLegacyCollisions(t *testing.T) {
	db, err := openDatabase("file:" + t.TempDir() + "/tenant-compatibility.db")
	require.NoError(t, err)
	prepareLegacyAuthenticationTables(t, db)
	_, err = applyHistoricalFixture(context.Background(), db)
	require.NoError(t, err)
	for _, statement := range []string{
		"ALTER TABLE users ADD COLUMN username VARCHAR(20)",
		"ALTER TABLE users ADD COLUMN email VARCHAR(50)",
		"CREATE TABLE top_ups(id BIGINT PRIMARY KEY)",
		"CREATE TABLE user_oauth_bindings(id BIGINT PRIMARY KEY)",
		"CREATE TABLE logs(id BIGINT PRIMARY KEY)",
		"CREATE TABLE quota_data(id BIGINT PRIMARY KEY)",
		"CREATE TABLE tasks(id BIGINT PRIMARY KEY)",
		"INSERT INTO users(id,username,email) VALUES(1,' Legacy ',' LEGACY@example.com '),(2,'legacy','legacy@example.com')",
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	var output bytes.Buffer
	require.NoError(t, runMigrationPrecheck(context.Background(), db, 13, &output))
	assert.JSONEq(t, `{"schema":"kkai_tenant_compatibility_precheck_v1","target_version":13,"user_count":2,"invalid_identity_count":0,"duplicate_username_count":0,"duplicate_email_count":0,"legacy_normalized_username_conflict_count":1,"legacy_normalized_email_conflict_count":1,"safe":true}`, output.String())
	assert.False(t, db.Migrator().HasColumn("users", "tenant_id"))
}

func TestAuthenticationPrecheckCLIPreservesTarget8Report(t *testing.T) {
	db, err := openDatabase("file:" + t.TempDir() + "/authentication-precheck.db")
	require.NoError(t, err)
	prepareLegacyAuthenticationTables(t, db)
	require.NoError(t, db.Exec("INSERT INTO users(id,telegram_id) VALUES(1,' private-subject '),(2,'private-subject')").Error)
	require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)
	var output bytes.Buffer
	require.NoError(t, runMigrationPrecheck(context.Background(), db, 8, &output))
	assert.JSONEq(t, `{"schema":"kkai_authentication_expand_precheck_v1","target_version":8,"user_count":2,"legacy_telegram_identity_count":2,"ambiguous_telegram_subject_count":1,"safe":false}`, output.String())
}
