package model

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestObserveUpstreamSchemaIsReadOnlyOnExistingDatabase(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:upstream-observe-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TABLE existing_production_state (id INTEGER PRIMARY KEY)").Error)

	observation, err := ObserveUpstreamSchema(context.Background(), db)
	require.NoError(t, err)
	require.False(t, observation.Ready)
	require.NotEmpty(t, observation.MissingTables)
	require.True(t, db.Migrator().HasTable("existing_production_state"))
	require.False(t, db.Migrator().HasTable(&User{}))
	require.False(t, db.Migrator().HasTable(&upstreamSchemaBaseline{}))
}

func TestObserveUpstreamSchemaAcceptsBootstrappedBaseline(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:upstream-ready-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, BootstrapEmptyUpstreamSchema(context.Background(), db))

	observation, err := ObserveUpstreamSchema(context.Background(), db)
	require.NoError(t, err)
	require.True(t, observation.Ready, "%+v", observation)
	require.Empty(t, observation.MissingTables)
	require.Empty(t, observation.MissingColumns)
	require.Empty(t, observation.Differences)
	require.Regexp(t, `^sha256:[0-9a-f]{64}$`, observation.ModelSchemaDigest)
}

func TestObserveUpstreamSchemaReportsSanitizedShapeDrift(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:upstream-drift-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, BootstrapEmptyUpstreamSchema(context.Background(), db))
	require.NoError(t, db.Exec("ALTER TABLE options RENAME TO options_original").Error)
	require.NoError(t, db.Exec("CREATE TABLE options (key INTEGER PRIMARY KEY, value TEXT NOT NULL)").Error)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX ux_options_value_unexpected ON options (value)").Error)

	observation, err := ObserveUpstreamSchema(context.Background(), db)
	require.NoError(t, err)
	require.False(t, observation.Ready)
	require.Empty(t, observation.MissingTables)
	require.Empty(t, observation.MissingColumns)
	require.Contains(t, observation.Differences, UpstreamSchemaDifference{
		Kind: "type_family", Table: "options", Column: "key", Expected: upstreamTypeString, Actual: upstreamTypeInteger,
	})
	require.Contains(t, observation.Differences, UpstreamSchemaDifference{
		Kind: "nullable", Table: "options", Column: "value", Expected: "true", Actual: "false",
	})
	require.Contains(t, observation.Differences, UpstreamSchemaDifference{
		Kind: "unique_unexpected", Table: "options", Expected: "", Actual: "columns=value;predicate=none",
	})
}

func TestObserveUpstreamSchemaRejectsUnexpectedColumn(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:upstream-extra-column-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, BootstrapEmptyUpstreamSchema(context.Background(), db))
	require.NoError(t, db.Exec("ALTER TABLE options ADD COLUMN unexpected_state TEXT").Error)

	observation, err := ObserveUpstreamSchema(context.Background(), db)
	require.NoError(t, err)
	require.False(t, observation.Ready)
	require.Contains(t, observation.Differences, UpstreamSchemaDifference{
		Kind: "column_unexpected", Table: "options", Column: "unexpected_state", Expected: "absent", Actual: "present",
	})
}

type semanticContractAlias int64

type semanticContractFixtureA struct {
	Identifier semanticContractAlias `gorm:"column:id;primaryKey;autoIncrement:false"`
	Name       string                `gorm:"column:name;size:64;not null"`
}

func (semanticContractFixtureA) TableName() string { return "semantic_contract_fixture" }

type semanticContractFixtureB struct {
	RenamedID int64  `gorm:"autoIncrement:false;primaryKey;column:id"`
	Label     string `gorm:"not null;size:64;column:name"`
}

func (semanticContractFixtureB) TableName() string { return "semantic_contract_fixture" }

type semanticContractFixtureDrift struct {
	Identifier int64  `gorm:"column:id;primaryKey;autoIncrement:false"`
	Name       string `gorm:"column:name;size:65;not null"`
}

func (semanticContractFixtureDrift) TableName() string { return "semantic_contract_fixture" }

func TestCanonicalSchemaIgnoresGoAndTagRefactorsButBindsDBSemantics(t *testing.T) {
	first, err := canonicalUpstreamSchemaDefinitionForModels([]any{&semanticContractFixtureA{}})
	require.NoError(t, err)
	second, err := canonicalUpstreamSchemaDefinitionForModels([]any{&semanticContractFixtureB{}})
	require.NoError(t, err)
	require.Equal(t, first, second)

	drifted, err := canonicalUpstreamSchemaDefinitionForModels([]any{&semanticContractFixtureDrift{}})
	require.NoError(t, err)
	require.NotEqual(t, first, drifted)
}

func TestCanonicalUpstreamSchemaIsDeterministic(t *testing.T) {
	first, err := CanonicalUpstreamSchema()
	require.NoError(t, err)
	second, err := CanonicalUpstreamSchema()
	require.NoError(t, err)
	require.Equal(t, first, second)
}

func TestNormalizedTypeCategoryAcrossDialects(t *testing.T) {
	tests := []struct {
		name         string
		dialect      string
		databaseType string
		columnType   string
		want         string
	}{
		{name: "sqlite boolean", dialect: upstreamDialectSQLite, databaseType: "BOOLEAN", columnType: "BOOLEAN", want: upstreamTypeBoolean},
		{name: "sqlite numeric affinity", dialect: upstreamDialectSQLite, databaseType: "NUMERIC", columnType: "NUMERIC", want: upstreamTypeNumeric},
		{name: "mysql boolean tinyint", dialect: upstreamDialectMySQL, databaseType: "tinyint", columnType: "tinyint(1)", want: upstreamTypeBoolean},
		{name: "mysql integer", dialect: upstreamDialectMySQL, databaseType: "bigint", columnType: "bigint", want: upstreamTypeInteger},
		{name: "mysql decimal", dialect: upstreamDialectMySQL, databaseType: "decimal", columnType: "decimal(20,6)", want: upstreamTypeNumeric},
		{name: "mysql json", dialect: upstreamDialectMySQL, databaseType: "json", columnType: "json", want: upstreamTypeJSON},
		{name: "postgres integer alias", dialect: upstreamDialectPostgres, databaseType: "int8", columnType: "bigint", want: upstreamTypeInteger},
		{name: "postgres timestamp", dialect: upstreamDialectPostgres, databaseType: "timestamptz", columnType: "timestamp with time zone", want: upstreamTypeTemporal},
		{name: "postgres binary", dialect: upstreamDialectPostgres, databaseType: "bytea", columnType: "bytea", want: upstreamTypeBinary},
		{name: "postgres array", dialect: upstreamDialectPostgres, databaseType: "_text", columnType: "text[]", want: upstreamTypeArray},
		{name: "postgres uuid", dialect: upstreamDialectPostgres, databaseType: "uuid", columnType: "uuid", want: upstreamTypeUUID},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizedTypeCategory(test.dialect, test.databaseType, test.columnType)
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}

	_, err := normalizedTypeCategory(upstreamDialectPostgres, "custom_domain", "custom_domain")
	require.Error(t, err)

	varchar, err := normalizedColumnTypeShape(upstreamDialectMySQL, "varchar", "varchar(64)")
	require.NoError(t, err)
	require.Equal(t, upstreamColumnTypeShape{Family: upstreamTypeString, Variant: "variable", Length: 64}, varchar)
	decimal, err := normalizedColumnTypeShape(upstreamDialectPostgres, "numeric", "numeric(10,6)")
	require.NoError(t, err)
	require.Equal(t, upstreamColumnTypeShape{Family: upstreamTypeNumeric, Variant: "decimal", Precision: 10, Scale: 6}, decimal)
}
