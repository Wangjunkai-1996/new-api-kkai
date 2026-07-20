package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/kkaischemacli"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestRunBootstrapsCanonicalSQLiteSchemaWithoutBusinessData(t *testing.T) {
	dsn := fmt.Sprintf("file:%s/schema.db", t.TempDir())
	t.Setenv("REDIS_CONN_STRING", "redis://127.0.0.1:1")
	t.Setenv("KKAI_RISK_STREAM_SECRET", "bootstrap-must-not-use-redis")

	var output bytes.Buffer
	require.NoError(t, run([]string{"--dsn-stdin"}, strings.NewReader(dsn+"\n"), &output))
	require.Contains(t, output.String(), "schema bootstrap complete")

	db, err := kkaischemacli.OpenDatabase(dsn)
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, model.ValidateMainSchemaPrerequisites(db))

	for table, destination := range map[string]any{
		"users":   &model.User{},
		"setups":  &model.Setup{},
		"options": &model.Option{},
	} {
		var count int64
		require.NoError(t, db.Model(destination).Count(&count).Error, table)
		require.Zero(t, count, table)
	}
}

func TestRunBootstrapIsIdempotent(t *testing.T) {
	dsn := fmt.Sprintf("file:%s/schema.db", t.TempDir())

	for range 2 {
		require.NoError(t, run([]string{"--dsn-stdin"}, strings.NewReader(dsn+"\n"), &bytes.Buffer{}))
	}

	db, err := kkaischemacli.OpenDatabase(dsn)
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, model.ValidateMainSchemaPrerequisites(db))
	for table, destination := range map[string]any{
		"users":   &model.User{},
		"setups":  &model.Setup{},
		"options": &model.Option{},
	} {
		var count int64
		require.NoError(t, db.Model(destination).Count(&count).Error, table)
		require.Zero(t, count, table)
	}
}

func TestRunRejectsMissingAndInvalidDSN(t *testing.T) {
	err := run([]string{"--dsn-stdin"}, strings.NewReader("\n"), &bytes.Buffer{})
	require.ErrorContains(t, err, "empty")

	command := exec.Command(os.Args[0], "-test.run=TestInvalidDatabaseDSNHelperProcess")
	command.Env = append(os.Environ(), "KKAI_SCHEMA_BOOTSTRAP_HELPER=invalid-dsn")
	err = command.Run()
	require.Error(t, err)
	var exitError *exec.ExitError
	require.ErrorAs(t, err, &exitError)
	require.NotZero(t, exitError.ExitCode())
}

func TestInvalidDatabaseDSNHelperProcess(t *testing.T) {
	if os.Getenv("KKAI_SCHEMA_BOOTSTRAP_HELPER") != "invalid-dsn" {
		return
	}
	if err := run([]string{"--dsn", "postgres://%"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestRunReportsBuildSourceRevisionWithoutDatabase(t *testing.T) {
	previousRevision := sourceRevision
	sourceRevision = "0123456789abcdef0123456789abcdef01234567"
	t.Cleanup(func() { sourceRevision = previousRevision })

	var output bytes.Buffer
	require.NoError(t, run([]string{"--source-revision"}, strings.NewReader(""), &output))
	require.Equal(t, sourceRevision+"\n", output.String())
}

func TestRunRejectsAmbiguousDSNSources(t *testing.T) {
	t.Setenv("SQL_DSN", fmt.Sprintf("file:%s/environment.db", t.TempDir()))

	err := run(
		[]string{"--dsn-stdin"},
		strings.NewReader(fmt.Sprintf("file:%s/stdin.db\n", t.TempDir())),
		&bytes.Buffer{},
	)
	require.ErrorContains(t, err, "cannot be combined")
}
