package model

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type schemaColumnType struct {
	name           string
	databaseType   string
	precision      int64
	scale          int64
	hasDecimalSize bool
}

func (column schemaColumnType) Name() string                { return column.name }
func (column schemaColumnType) DatabaseTypeName() string    { return column.databaseType }
func (column schemaColumnType) ColumnType() (string, bool)  { return column.databaseType, true }
func (column schemaColumnType) PrimaryKey() (bool, bool)    { return false, true }
func (column schemaColumnType) AutoIncrement() (bool, bool) { return false, true }
func (column schemaColumnType) Length() (int64, bool)       { return 0, false }
func (column schemaColumnType) DecimalSize() (int64, int64, bool) {
	return column.precision, column.scale, column.hasDecimalSize
}
func (column schemaColumnType) Nullable() (bool, bool)       { return false, true }
func (column schemaColumnType) Unique() (bool, bool)         { return false, true }
func (column schemaColumnType) ScanType() reflect.Type       { return reflect.TypeOf("") }
func (column schemaColumnType) Comment() (string, bool)      { return "", false }
func (column schemaColumnType) DefaultValue() (string, bool) { return "", false }

func newSchemaPrerequisiteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:model-schema-prerequisites-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(mainDatabaseAutoMigrateModels()...))
	originalDB := DB
	originalDatabaseType := common.MainDatabaseType()
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	initCol()
	require.NoError(t, ensureSubscriptionPlanTableSQLite())
	t.Cleanup(func() {
		DB = originalDB
		common.SetMainDatabaseType(originalDatabaseType)
		initCol()
	})
	return db
}

func TestValidateMainSchemaPrerequisitesIsReadOnly(t *testing.T) {
	db := newSchemaPrerequisiteTestDB(t)
	require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)
	require.NoError(t, ValidateMainSchemaPrerequisites(db))
}

func TestValidateMainSchemaPrerequisitesRejectsMissingTable(t *testing.T) {
	db := newSchemaPrerequisiteTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&Channel{}))

	err := ValidateMainSchemaPrerequisites(db)
	require.ErrorIs(t, err, ErrMainSchemaNotReady)
	require.ErrorContains(t, err, "channels")
}

func TestValidateMainSchemaPrerequisitesRejectsMissingColumn(t *testing.T) {
	db := newSchemaPrerequisiteTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&Channel{}))
	require.NoError(t, db.Exec("CREATE TABLE channels (id INTEGER PRIMARY KEY)").Error)

	err := ValidateMainSchemaPrerequisites(db)
	require.ErrorIs(t, err, ErrMainSchemaNotReady)
	require.ErrorContains(t, err, "channels.")
}

func TestValidatePostgresApplicationColumnTypesRejectsLegacyTypes(t *testing.T) {
	tests := []struct {
		name    string
		table   string
		columns map[string]gorm.ColumnType
	}{
		{
			name:  "token model limits varchar",
			table: "tokens",
			columns: map[string]gorm.ColumnType{
				"model_limits": schemaColumnType{name: "model_limits", databaseType: "varchar"},
			},
		},
		{
			name:  "subscription price double precision",
			table: "subscription_plans",
			columns: map[string]gorm.ColumnType{
				"price_amount": schemaColumnType{name: "price_amount", databaseType: "float8"},
			},
		},
		{
			name:  "subscription price wrong precision",
			table: "subscription_plans",
			columns: map[string]gorm.ColumnType{
				"price_amount": schemaColumnType{
					name: "price_amount", databaseType: "numeric", precision: 12, scale: 6, hasDecimalSize: true,
				},
			},
		},
		{
			name:  "subscription price wrong scale",
			table: "subscription_plans",
			columns: map[string]gorm.ColumnType{
				"price_amount": schemaColumnType{
					name: "price_amount", databaseType: "numeric", precision: 10, scale: 4, hasDecimalSize: true,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validatePostgresApplicationColumnTypes(test.table, test.columns)
			require.ErrorIs(t, err, ErrMainSchemaNotReady)
		})
	}
}

func TestValidatePostgresApplicationColumnTypesAcceptsCanonicalTypes(t *testing.T) {
	require.NoError(t, validatePostgresApplicationColumnTypes("tokens", map[string]gorm.ColumnType{
		"model_limits": schemaColumnType{name: "model_limits", databaseType: "text"},
	}))
	require.NoError(t, validatePostgresApplicationColumnTypes("subscription_plans", map[string]gorm.ColumnType{
		"price_amount": schemaColumnType{
			name: "price_amount", databaseType: "numeric", precision: 10, scale: 6, hasDecimalSize: true,
		},
	}))
}
