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

	"github.com/stretchr/testify/require"
)

func TestOpenDatabaseSupportsExplicitSQLiteDSN(t *testing.T) {
	dsn := fmt.Sprintf("file:kkai-cli-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := openDatabase(dsn)
	require.NoError(t, err)
	result, err := kkaimigrate.Apply(context.Background(), db, kkaimigrate.Options{})
	require.NoError(t, err)
	require.Empty(t, result.Pending)
	require.NoError(t, kkaimigrate.CheckRequired(context.Background(), db))
}

func TestApplyMigrationTargetRunsV4ThenV5(t *testing.T) {
	dsn := fmt.Sprintf("file:kkai-cli-target-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := openDatabase(dsn)
	require.NoError(t, err)
	_, err = kkaimigrate.Apply(context.Background(), db, kkaimigrate.Options{})
	require.NoError(t, err)

	result, err := applyMigrationTarget(context.Background(), db, 4, kkaimigrate.Options{})
	require.NoError(t, err)
	require.Len(t, result.Applied, 4)
	result, err = applyMigrationTarget(context.Background(), db, 5, kkaimigrate.Options{})
	require.NoError(t, err)
	require.Len(t, result.Applied, 5)
	require.NoError(t, kkaimigrate.Check(context.Background(), db, 5))
}

func TestApplyMigrationTargetRejectsUnknownVersion(t *testing.T) {
	_, err := applyMigrationTarget(context.Background(), nil, 6, kkaimigrate.Options{})
	require.ErrorContains(t, err, "expected 4 or 5")
}

func TestDescribeContractJSONUsesExactRuntimeFields(t *testing.T) {
	output, err := describeContractJSON("postgres")
	require.NoError(t, err)
	require.JSONEq(t, `{"compatible_prefixes":{"3":"sha256:984a638f2e2e2d370f4f2304f5acee209ebc47cd0d8af59c0f2eb116fe72634e","4":"sha256:4d1959b6eb1204aaa6a2481f6a423d395f5517f7f0a5adda88ec0547be1c751c","5":"sha256:c15230067aa89899923d1ad81f9e31f0c8e56a5113869535723bb4eea5e2d3ff"},"migration_kind":"none","migration_set_digest":"sha256:984a638f2e2e2d370f4f2304f5acee209ebc47cd0d8af59c0f2eb116fe72634e","migration_target_version":3,"runtime_max_version":5,"runtime_min_version":3,"schema_management":"runtime"}`, output)
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
	_, err = kkaimigrate.Apply(context.Background(), db, kkaimigrate.Options{})
	require.NoError(t, err)

	_, err = observeCurrentSchema(context.Background(), db)
	require.ErrorIs(t, err, model.ErrMainSchemaNotReady)
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
