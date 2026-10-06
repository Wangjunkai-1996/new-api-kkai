package model

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestExternalSchemaStartupNeverRepairsLegacyOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed.db")
	legacy, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, legacy.Exec("CREATE TABLE options (`key` TEXT, value TEXT)").Error)
	require.NoError(t, legacy.Exec("INSERT INTO options (`key`, value) VALUES (?, ?), (?, ?)",
		"theme.frontend", "classic", "theme.frontend", "default").Error)
	connection, err := legacy.DB()
	require.NoError(t, err)
	require.NoError(t, connection.Close())

	previousDB, previousPath, previousMode := DB, common.SQLitePath, common.SchemaManagementMode
	mainType, logType := common.MainDatabaseType(), common.LogDatabaseType()
	t.Cleanup(func() {
		DB, common.SQLitePath, common.SchemaManagementMode = previousDB, previousPath, previousMode
		common.SetDatabaseTypes(mainType, logType)
		initCol()
	})
	t.Setenv("SQL_DSN", "local")
	common.SQLitePath = path
	common.SchemaManagementMode = common.SchemaManagementExternal
	require.NoError(t, InitDB())
	connection, err = DB.DB()
	require.NoError(t, err)
	defer connection.Close()
	var rows []Option
	require.NoError(t, DB.Find(&rows).Error)
	assert.Equal(t, []Option{{Key: "theme.frontend", Value: "classic"}, {Key: "theme.frontend", Value: "default"}}, rows)
	assert.False(t, DB.Migrator().HasTable(&Channel{}))
}

func TestWalletSchemaRejectsLegacy32BitColumnsAcrossSQLDatabases(t *testing.T) {
	for _, test := range []struct {
		dialect    common.DatabaseType
		columnType string
		valid      bool
	}{
		{common.DatabaseTypeMySQL, "bigint", true},
		{common.DatabaseTypeMySQL, "int", false},
		{common.DatabaseTypePostgreSQL, "int8", true},
		{common.DatabaseTypePostgreSQL, "int4", false},
	} {
		columns := map[string]gorm.ColumnType{
			"quota":      schemaColumnType{name: "quota", databaseType: "bigint"},
			"used_quota": schemaColumnType{name: "used_quota", databaseType: test.columnType},
		}
		err := validateWalletQuotaColumnTypes(columns, test.dialect)
		if test.valid {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, ErrMainSchemaNotReady)
			assert.ErrorContains(t, err, "users.used_quota")
		}
	}
}
