package kkaimigrate

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestObserveAcceptsPostgresSingleColumnUniqueAsSelectOnlyRole(t *testing.T) {
	adminDSN := os.Getenv("KKAI_POSTGRES_OBSERVER_TEST_DSN")
	if adminDSN == "" {
		t.Skip("KKAI_POSTGRES_OBSERVER_TEST_DSN is not set")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	schemaName := "kkai_observer_" + suffix
	roleName := "kkai_observer_role_" + suffix
	rolePassword := "test-only-observer-password"

	adminDB, err := gorm.Open(postgres.Open(postgresDSNForRole(t, adminDSN, "", "", schemaName)), &gorm.Config{})
	require.NoError(t, err)
	adminSQLDB, err := adminDB.DB()
	require.NoError(t, err)
	adminSQLDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, adminSQLDB.Close()) })

	require.NoError(t, adminDB.Exec(fmt.Sprintf("CREATE SCHEMA %s", schemaName)).Error)
	require.NoError(t, adminDB.Exec(fmt.Sprintf(
		"CREATE ROLE %s LOGIN PASSWORD '%s' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS",
		roleName,
		rolePassword,
	)).Error)
	t.Cleanup(func() {
		require.NoError(t, adminDB.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName)).Error)
		require.NoError(t, adminDB.Exec(fmt.Sprintf("DROP ROLE IF EXISTS %s", roleName)).Error)
	})

	_, err = Apply(context.Background(), adminDB, Options{})
	require.NoError(t, err)
	require.NoError(t, adminDB.Exec(fmt.Sprintf("ALTER ROLE %s SET default_transaction_read_only = on", roleName)).Error)
	require.NoError(t, adminDB.Exec(fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s", schemaName, roleName)).Error)
	require.NoError(t, adminDB.Exec(fmt.Sprintf("GRANT SELECT ON ALL TABLES IN SCHEMA %s TO %s", schemaName, roleName)).Error)

	observerDB, err := gorm.Open(
		postgres.Open(postgresDSNForRole(t, adminDSN, roleName, rolePassword, schemaName)),
		&gorm.Config{},
	)
	require.NoError(t, err)
	observerSQLDB, err := observerDB.DB()
	require.NoError(t, err)
	observerSQLDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, observerSQLDB.Close()) })

	columnTypes, err := observerDB.Migrator().ColumnTypes("kkai_outbox")
	require.NoError(t, err)
	eventKeyFound := false
	for _, columnType := range columnTypes {
		if columnType.Name() != "event_key" {
			continue
		}
		eventKeyFound = true
		unique, ok := columnType.Unique()
		require.True(t, ok)
		require.False(t, unique, "GORM information_schema lookup should reproduce the readonly-role false negative")
		break
	}
	require.True(t, eventKeyFound)

	observation, err := Observe(context.Background(), observerDB)
	require.NoError(t, err)
	require.Equal(t, RequiredRuntimeVersion, observation.CurrentVersion)
}

func postgresDSNForRole(t *testing.T, rawDSN string, username string, password string, schemaName string) string {
	t.Helper()

	dsn, err := url.Parse(rawDSN)
	require.NoError(t, err)
	if username != "" {
		dsn.User = url.UserPassword(username, password)
	}
	query := dsn.Query()
	query.Set("search_path", schemaName)
	dsn.RawQuery = query.Encode()
	return dsn.String()
}
