package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/kkaischemacli"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const observerTestSourceRevision = "0123456789abcdef0123456789abcdef01234567"

type sqliteSchemaObject struct {
	Type string
	Name string
	SQL  string
}

func TestObserverCurrentAndUpstreamChecksNeverChangeSchema(t *testing.T) {
	dsn, db := newObserverTestDatabase(t, "ready")
	require.NoError(t, model.BootstrapEmptyUpstreamSchema(context.Background(), db))
	_, err := kkaimigrate.Apply(context.Background(), db, kkaimigrate.Options{})
	require.NoError(t, err)
	before := sqliteSchemaSnapshot(t, db)

	previousProductionRuntime := common.ProductionImageRuntime
	common.ProductionImageRuntime = "true"
	t.Cleanup(func() { common.ProductionImageRuntime = previousProductionRuntime })

	var currentOutput bytes.Buffer
	require.NoError(t, run([]string{
		"--current", "--json", "--dsn", dsn,
	}, strings.NewReader(""), &currentOutput))
	var current kkaimigrate.SchemaObservation
	require.NoError(t, common.Unmarshal(bytes.TrimSpace(currentOutput.Bytes()), &current))
	require.Equal(t, kkaimigrate.MigrationTargetVersion, current.CurrentVersion)
	require.Equal(t, before, sqliteSchemaSnapshot(t, db))

	var upstreamOutput bytes.Buffer
	require.NoError(t, run([]string{
		"--check-upstream-baseline",
		"--json",
		"--source-revision", observerTestSourceRevision,
		"--dsn", dsn,
	}, strings.NewReader(""), &upstreamOutput))
	var adoption kkaimigrate.UpstreamSchemaAdoption
	require.NoError(t, common.Unmarshal(bytes.TrimSpace(upstreamOutput.Bytes()), &adoption))
	require.True(t, adoption.Ready)
	require.Equal(t, before, sqliteSchemaSnapshot(t, db))
}

func TestObserverLeavesEmptyDatabaseUntouched(t *testing.T) {
	dsn, db := newObserverTestDatabase(t, "empty")
	before := sqliteSchemaSnapshot(t, db)

	err := run([]string{"--current", "--json", "--dsn", dsn}, strings.NewReader(""), io.Discard)
	require.ErrorIs(t, err, kkaimigrate.ErrSchemaNotReady)
	require.Equal(t, before, sqliteSchemaSnapshot(t, db))

	var output bytes.Buffer
	err = run([]string{
		"--check-upstream-baseline",
		"--json",
		"--source-revision", observerTestSourceRevision,
		"--dsn", dsn,
	}, strings.NewReader(""), &output)
	require.ErrorContains(t, err, "upstream schema baseline is incomplete")
	var adoption kkaimigrate.UpstreamSchemaAdoption
	require.NoError(t, common.Unmarshal(bytes.TrimSpace(output.Bytes()), &adoption))
	require.False(t, adoption.Ready)
	require.Equal(t, before, sqliteSchemaSnapshot(t, db))
}

func TestObserverRejectsEveryMigrationCapability(t *testing.T) {
	for _, forbidden := range []string{
		"--apply",
		"--bootstrap-empty-upstream-baseline",
		"--check",
		"--describe",
		"--describe-upstream-schema",
		"--dry-run",
		"--min-version=4",
	} {
		t.Run(forbidden, func(t *testing.T) {
			err := run([]string{forbidden}, strings.NewReader(""), io.Discard)
			require.Error(t, err)
			require.Contains(t, err.Error(), "flag provided but not defined")
		})
	}
}

func TestObserverRequiresOneJSONObservationMode(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "mode.db")
	for _, test := range []struct {
		name      string
		arguments []string
		message   string
	}{
		{name: "none", arguments: []string{"--json", "--dsn", dsn}, message: "exactly one"},
		{name: "both", arguments: []string{"--current", "--check-upstream-baseline", "--json", "--dsn", dsn}, message: "exactly one"},
		{name: "json required", arguments: []string{"--current", "--dsn", dsn}, message: "requires --json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := run(test.arguments, strings.NewReader(""), io.Discard)
			require.ErrorContains(t, err, test.message)
		})
	}
}

func TestObserverHelpListsOnlyReadOnlyModes(t *testing.T) {
	var output bytes.Buffer
	err := run([]string{"--help"}, strings.NewReader(""), &output)
	require.True(t, errors.Is(err, flag.ErrHelp))
	require.Contains(t, output.String(), "-current")
	require.Contains(t, output.String(), "-check-upstream-baseline")
	for _, forbidden := range []string{"-apply", "-bootstrap-empty", "-dry-run", "-min-version"} {
		require.NotContains(t, output.String(), forbidden)
	}
}

func newObserverTestDatabase(t *testing.T, name string) (string, *gorm.DB) {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), fmt.Sprintf("observer-%s.db", name))
	db, err := kkaischemacli.OpenDatabase(dsn)
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return dsn, db
}

func sqliteSchemaSnapshot(t *testing.T, db *gorm.DB) []sqliteSchemaObject {
	t.Helper()
	var objects []sqliteSchemaObject
	require.NoError(t, db.Raw(`
SELECT type, name, COALESCE(sql, '') AS sql
FROM sqlite_master
WHERE name NOT LIKE 'sqlite_%'
ORDER BY type, name
`).Scan(&objects).Error)
	return objects
}
