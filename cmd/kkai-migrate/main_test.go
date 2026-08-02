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

func TestApplyMigrationTargetRunsThroughV7(t *testing.T) {
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
	result, err = applyMigrationTarget(context.Background(), db, 6, kkaimigrate.Options{})
	require.NoError(t, err)
	require.Len(t, result.Applied, 6)
	result, err = applyMigrationTarget(context.Background(), db, 7, kkaimigrate.Options{})
	require.NoError(t, err)
	require.Len(t, result.Applied, 7)
	require.NoError(t, kkaimigrate.Check(context.Background(), db, 7))
}

func TestApplyMigrationTargetRejectsUnknownVersion(t *testing.T) {
	_, err := applyMigrationTarget(context.Background(), nil, 8, kkaimigrate.Options{})
	require.ErrorContains(t, err, "expected 4, 5, 6, or 7")
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
