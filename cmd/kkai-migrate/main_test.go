package main

import (
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
	result, err := kkaimigrate.Apply(context.Background(), db, kkaimigrate.Options{})
	require.NoError(t, err)
	require.Empty(t, result.Pending)
	require.NoError(t, kkaimigrate.CheckRequired(context.Background(), db))
}

func TestApplyMigrationTargetRejectsUnknownVersion(t *testing.T) {
	_, err := applyMigrationTarget(context.Background(), nil, 10, kkaimigrate.Options{})
	require.ErrorContains(t, err, "expected 4, 5, 6, 7, 8, or 9")
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
	_, err = kkaimigrate.Apply(context.Background(), db, kkaimigrate.Options{})
	require.NoError(t, err)

	_, err = observeCurrentSchema(context.Background(), db)
	require.ErrorIs(t, err, model.ErrMainSchemaNotReady)
}

func TestObserveHistoricalSchemaDoesNotRequireRC41Models(t *testing.T) {
	for _, version := range []int64{7, 8} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			db, err := openDatabase("file:" + t.TempDir() + "/historical.db")
			require.NoError(t, err)
			prepareLegacyAuthenticationTables(t, db)
			_, err = kkaimigrate.Apply(context.Background(), db, kkaimigrate.Options{})
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
