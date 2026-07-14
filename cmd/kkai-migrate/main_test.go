package main

import (
	"context"
	"fmt"
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
