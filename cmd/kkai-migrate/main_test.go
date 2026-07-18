package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"

	"github.com/stretchr/testify/require"
)

func TestDescribeJSONReportsCanonicalSchemaCompatibility(t *testing.T) {
	encoded, err := describeJSON("0123456789abcdef0123456789abcdef01234567")
	require.NoError(t, err)

	var contract kkaimigrate.SchemaContract
	require.NoError(t, common.Unmarshal(encoded, &contract))
	require.Equal(t, kkaimigrate.SchemaCompatibility("0123456789abcdef0123456789abcdef01234567"), contract)
}

func TestDescribeUpstreamJSONReportsVersionedModelSchema(t *testing.T) {
	encoded, err := describeUpstreamJSON("0123456789abcdef0123456789abcdef01234567")
	require.NoError(t, err)

	var contract kkaimigrate.UpstreamSchemaContract
	require.NoError(t, common.Unmarshal(encoded, &contract))
	require.Equal(t, 1, contract.Schema)
	require.Equal(t, 1, contract.CatalogVersion)
	require.Regexp(t, `^sha256:[0-9a-f]{64}$`, contract.ModelSchemaDigest)
	require.Equal(t, contract.ModelSchemaDigest, contract.BeforeModelSchemaDigest)
	require.Equal(t, contract.ModelSchemaDigest, contract.AfterModelSchemaDigest)
	require.Zero(t, contract.MigrationVersion)
	require.Equal(t, "baseline", contract.MigrationKind)
	require.NotEmpty(t, contract.OwnershipImplementationID)
}

func TestCheckJSONIsMachineReadable(t *testing.T) {
	contract := kkaimigrate.SchemaCompatibility("0123456789abcdef0123456789abcdef01234567")
	encoded, err := checkJSON(kkaimigrate.SchemaObservation{
		Schema:             1,
		CurrentVersion:     4,
		MigrationSetDigest: contract.MigrationSetDigest,
	}, contract)
	require.NoError(t, err)
	require.JSONEq(t, `{"schema":1,"ready":true,"current_version":4,"migration_set_digest":"`+contract.MigrationSetDigest+`","runtime_min_version":4,"runtime_max_version":4,"migration_target_version":4}`, string(encoded))
}

func TestCurrentJSONReportsObservedDatabaseVersion(t *testing.T) {
	encoded, err := currentJSON(kkaimigrate.SchemaObservation{
		Schema:             1,
		CurrentVersion:     4,
		MigrationSetDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"schema":1,"current_version":4,"migration_set_digest":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`, string(encoded))
}

func TestOpenDatabaseSupportsExplicitSQLiteDSN(t *testing.T) {
	dsn := fmt.Sprintf("file:kkai-cli-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := openDatabase(dsn)
	require.NoError(t, err)
	result, err := kkaimigrate.Apply(context.Background(), db, kkaimigrate.Options{})
	require.NoError(t, err)
	require.Empty(t, result.Pending)
	require.NoError(t, kkaimigrate.Check(context.Background(), db, kkaimigrate.CurrentVersion))
}

func TestCLIExpansionPostcheckAcceptsMigrationTarget(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "kkai-cli-postcheck.db")
	db, err := openDatabase(dsn)
	require.NoError(t, err)
	_, err = kkaimigrate.Apply(context.Background(), db, kkaimigrate.Options{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	output, err := runKKAICommand(
		t,
		"--check",
		"--json",
		"--min-version", strconv.FormatInt(kkaimigrate.MigrationTargetVersion, 10),
		"--dsn", dsn,
	)
	require.NoError(t, err)

	jsonLine := strings.SplitN(string(output), "\n", 2)[0]
	var result struct {
		Ready          bool  `json:"ready"`
		CurrentVersion int64 `json:"current_version"`
	}
	require.NoError(t, common.Unmarshal([]byte(jsonLine), &result))
	require.True(t, result.Ready)
	require.Equal(t, kkaimigrate.MigrationTargetVersion, result.CurrentVersion)
}

func TestCLIEmptyUpstreamBaselineBootstrapIsSingleUse(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "kkai-cli-empty-bootstrap.db")
	output, err := runKKAICommand(t, "--bootstrap-empty-upstream-baseline", "--dsn", dsn)
	require.NoError(t, err)
	require.Contains(t, string(output), "upstream schema baseline bootstrapped")

	db, err := openDatabase(dsn)
	require.NoError(t, err)
	require.True(t, db.Migrator().HasTable("users"))
	require.True(t, db.Migrator().HasTable("kkai_upstream_schema_baselines"))

	_, err = runKKAICommand(t, "--bootstrap-empty-upstream-baseline", "--dsn", dsn)
	require.Error(t, err)
}

func TestCLIUpstreamBaselineCheckIsReadOnlyOnExistingDatabase(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "kkai-cli-upstream-existing.db")
	db, err := openDatabase(dsn)
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TABLE existing_production_state (id INTEGER PRIMARY KEY)").Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	output, err := runKKAICommand(
		t,
		"--check-upstream-baseline",
		"--json",
		"--source-revision", "0123456789abcdef0123456789abcdef01234567",
		"--dsn", dsn,
	)
	require.Error(t, err)
	jsonLine := strings.SplitN(string(output), "\n", 2)[0]
	var adoption kkaimigrate.UpstreamSchemaAdoption
	require.NoError(t, common.Unmarshal([]byte(jsonLine), &adoption))
	require.False(t, adoption.Ready)
	require.NotEmpty(t, adoption.MissingTables)

	db, err = openDatabase(dsn)
	require.NoError(t, err)
	require.True(t, db.Migrator().HasTable("existing_production_state"))
	require.False(t, db.Migrator().HasTable("users"))
	require.False(t, db.Migrator().HasTable("kkai_upstream_schema_baselines"))
}

func TestCLIUpstreamBaselineCheckReportsSanitizedShapeDifferences(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "kkai-cli-upstream-drift.db")
	db, err := openDatabase(dsn)
	require.NoError(t, err)
	require.NoError(t, model.BootstrapEmptyUpstreamSchema(context.Background(), db))
	require.NoError(t, db.Exec("ALTER TABLE options RENAME TO options_original").Error)
	require.NoError(t, db.Exec("CREATE TABLE options (key CUSTOM_DOMAIN PRIMARY KEY, value TEXT NOT NULL)").Error)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX ux_options_expression ON options (lower(value))").Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	output, err := runKKAICommand(
		t,
		"--check-upstream-baseline",
		"--json",
		"--source-revision", "0123456789abcdef0123456789abcdef01234567",
		"--dsn", dsn,
	)
	require.Error(t, err)
	jsonLine := strings.SplitN(string(output), "\n", 2)[0]
	var adoption kkaimigrate.UpstreamSchemaAdoption
	require.NoError(t, common.Unmarshal([]byte(jsonLine), &adoption))
	require.False(t, adoption.Ready)
	require.Contains(t, adoption.Differences, model.UpstreamSchemaDifference{
		Kind: "type_unmapped", Table: "options", Column: "key", Expected: "string", Actual: "custom_domain",
	})
	require.Contains(t, adoption.Differences, model.UpstreamSchemaDifference{
		Kind: "nullable", Table: "options", Column: "value", Expected: "true", Actual: "false",
	})
	require.Contains(t, adoption.Differences, model.UpstreamSchemaDifference{
		Kind: "unique_metadata", Table: "options", Expected: "available", Actual: "unavailable",
	})
	require.NotContains(t, jsonLine, "CREATE TABLE")
	require.NotContains(t, jsonLine, "lower(value)")
}

func TestEmptyUpstreamBaselineBootstrapRejectsApplicationRoles(t *testing.T) {
	require.NoError(t, validateEmptyBootstrapRuntime(""))
	for _, role := range []string{"leader", "serving", "standby-readonly"} {
		require.Error(t, validateEmptyBootstrapRuntime(role))
	}
}

func runKKAICommand(t *testing.T, arguments ...string) ([]byte, error) {
	t.Helper()
	commandArguments := []string{"-test.run=^TestKKAICommandHelperProcess$", "--"}
	commandArguments = append(commandArguments, arguments...)
	command := exec.Command(os.Args[0], commandArguments...)
	command.Env = append(os.Environ(), "KKAI_COMMAND_HELPER_PROCESS=1", "KKAI_NODE_ROLE=")
	return command.Output()
}

func TestKKAICommandHelperProcess(t *testing.T) {
	if os.Getenv("KKAI_COMMAND_HELPER_PROCESS") != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 {
		t.Fatal("missing CLI argument separator")
	}

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	os.Args = append([]string{os.Args[0]}, os.Args[separator+1:]...)
	main()
}

func TestFirstNonEmptyIgnoresWhitespace(t *testing.T) {
	require.Equal(t, "postgres://example", firstNonEmpty("", "  ", "postgres://example", "ignored"))
	require.Empty(t, firstNonEmpty("", "  "))
}

func TestEnabledModeCountRejectsAmbiguousOperations(t *testing.T) {
	require.Equal(t, 0, enabledModeCount(false, false))
	require.Equal(t, 1, enabledModeCount(false, true, false))
	require.Equal(t, 2, enabledModeCount(true, false, true))
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
