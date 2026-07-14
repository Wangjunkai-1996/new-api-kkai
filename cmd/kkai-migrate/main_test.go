package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

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
	require.NoError(t, kkaimigrate.Check(context.Background(), db, kkaimigrate.CurrentVersion))
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
