package model

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type fixtureColumnType struct {
	name          string
	databaseType  string
	columnType    string
	nullable      bool
	nullableKnown bool
	primary       bool
	primaryKnown  bool
	autoIncrement bool
	length        int64
	lengthKnown   bool
	precision     int64
	scale         int64
	decimalKnown  bool
	defaultValue  string
	defaultKnown  bool
}

func (column fixtureColumnType) Name() string             { return column.name }
func (column fixtureColumnType) DatabaseTypeName() string { return column.databaseType }
func (column fixtureColumnType) ColumnType() (string, bool) {
	return column.columnType, column.columnType != ""
}
func (column fixtureColumnType) PrimaryKey() (bool, bool)    { return column.primary, column.primaryKnown }
func (column fixtureColumnType) AutoIncrement() (bool, bool) { return column.autoIncrement, true }
func (column fixtureColumnType) Length() (int64, bool)       { return column.length, column.lengthKnown }
func (column fixtureColumnType) DecimalSize() (int64, int64, bool) {
	return column.precision, column.scale, column.decimalKnown
}
func (column fixtureColumnType) Nullable() (bool, bool)  { return column.nullable, column.nullableKnown }
func (column fixtureColumnType) Unique() (bool, bool)    { return false, true }
func (column fixtureColumnType) ScanType() reflect.Type  { return reflect.TypeOf("") }
func (column fixtureColumnType) Comment() (string, bool) { return "", false }
func (column fixtureColumnType) DefaultValue() (string, bool) {
	return column.defaultValue, column.defaultKnown
}

type fixtureIndex struct {
	name         string
	columns      []string
	primary      bool
	primaryKnown bool
	unique       bool
	uniqueKnown  bool
}

func (index fixtureIndex) Table() string            { return "fixture" }
func (index fixtureIndex) Name() string             { return index.name }
func (index fixtureIndex) Columns() []string        { return index.columns }
func (index fixtureIndex) PrimaryKey() (bool, bool) { return index.primary, index.primaryKnown }
func (index fixtureIndex) Unique() (bool, bool)     { return index.unique, index.uniqueKnown }
func (index fixtureIndex) Option() string           { return "" }

func TestObservedColumnsUsesStructuredMetadataForMySQLAndPostgres(t *testing.T) {
	for _, dialect := range []string{upstreamDialectMySQL, upstreamDialectPostgres} {
		t.Run(dialect, func(t *testing.T) {
			columns, err := observedColumns(nil, dialect, "fixture", []gorm.ColumnType{
				fixtureColumnType{
					name: "id", databaseType: "bigint", columnType: "bigint",
					nullable: false, nullableKnown: true, primary: true, primaryKnown: true,
				},
			})
			require.NoError(t, err)
			require.Equal(t, observedUpstreamColumn{
				TypeShape:       upstreamColumnTypeShape{Family: upstreamTypeInteger, Variant: "integer_64"},
				TypeName:        "bigint",
				Nullable:        false,
				NullableKnown:   true,
				PrimaryKey:      true,
				PrimaryKeyKnown: true,
			}, columns["id"])
		})
	}
}

func TestObservedColumnsFailsClosedOnUnknownMetadata(t *testing.T) {
	columns, err := observedColumns(nil, upstreamDialectPostgres, "fixture", []gorm.ColumnType{
		fixtureColumnType{
			name: "value", databaseType: "custom_domain", columnType: "custom_domain",
			nullableKnown: true, primaryKnown: true,
		},
	})
	require.NoError(t, err)
	require.Empty(t, columns["value"].TypeShape.Family)
	require.Equal(t, "custom_domain", columns["value"].TypeName)

	columns, err = observedColumns(nil, upstreamDialectMySQL, "fixture", []gorm.ColumnType{
		fixtureColumnType{
			name: "value", databaseType: "varchar", columnType: "varchar(255)",
			nullableKnown: false, primaryKnown: true,
		},
	})
	require.NoError(t, err)
	require.False(t, columns["value"].NullableKnown)
}

func TestUniqueIndexSignaturesAcrossMySQLAndPostgres(t *testing.T) {
	indexes := []gorm.Index{
		fixtureIndex{name: "primary", columns: []string{"id"}, primary: true, primaryKnown: true, unique: true, uniqueKnown: true},
		fixtureIndex{name: "ux_tenant_key", columns: []string{"tenant_id", "key"}, primaryKnown: true, unique: true, uniqueKnown: true},
		fixtureIndex{name: "idx_status", columns: []string{"status"}, primaryKnown: true, uniqueKnown: true},
	}

	mysql, err := uniqueIndexSignatures(upstreamDialectMySQL, indexes, nil)
	require.NoError(t, err)
	require.Equal(t, map[string]struct{}{
		"columns=key,tenant_id;predicate=none": {},
	}, mysql)

	predicate, err := normalizedIndexPredicate("deleted_at IS NULL")
	require.NoError(t, err)
	postgres, err := uniqueIndexSignatures(upstreamDialectPostgres, indexes, map[string]string{
		"primary":       "",
		"ux_tenant_key": predicate,
		"idx_status":    "",
	})
	require.NoError(t, err)
	require.Equal(t, map[string]struct{}{
		uniqueIndexSignature([]string{"key", "tenant_id"}, predicate): {},
	}, postgres)
}

func TestCompareUpstreamFieldReportsExactShapeDrift(t *testing.T) {
	observation := UpstreamSchemaObservation{Dialect: upstreamDialectPostgres}
	expected := upstreamFieldSchema{
		Column: "amount", TypeFamily: upstreamTypeNumeric, TypeVariant: "decimal",
		Precision: 10, Scale: 6, Nullable: false,
		Default: upstreamDefaultSchema{Kind: upstreamDefaultLiteral, ValueType: "number", Value: "0"},
	}
	actual := observedUpstreamColumn{
		TypeShape:       upstreamColumnTypeShape{Family: upstreamTypeNumeric, Variant: "decimal", Precision: 12, Scale: 4},
		NullableKnown:   true,
		PrimaryKeyKnown: true,
		DefaultRaw:      "0.0",
		DefaultExists:   true,
	}

	compareUpstreamField(&observation, "plans", expected, actual)
	require.Contains(t, observation.Differences, UpstreamSchemaDifference{
		Kind: "precision", Table: "plans", Column: "amount", Expected: "10", Actual: "12",
	})
	require.Contains(t, observation.Differences, UpstreamSchemaDifference{
		Kind: "scale", Table: "plans", Column: "amount", Expected: "6", Actual: "4",
	})
	require.NotContains(t, observation.Differences, UpstreamSchemaDifference{
		Kind: "default", Table: "plans", Column: "amount",
	})

	observation.Differences = nil
	expected = upstreamFieldSchema{
		Column: "name", TypeFamily: upstreamTypeString, TypeVariant: "variable", Length: 64,
		Nullable: true, Default: upstreamDefaultSchema{Kind: upstreamDefaultLiteral, ValueType: "string", Value: ""},
	}
	actual = observedUpstreamColumn{
		TypeShape:       upstreamColumnTypeShape{Family: upstreamTypeString, Variant: "variable", Length: 32},
		Nullable:        true,
		NullableKnown:   true,
		PrimaryKeyKnown: true,
		DefaultRaw:      "''",
		DefaultExists:   true,
	}
	compareUpstreamField(&observation, "plans", expected, actual)
	require.Contains(t, observation.Differences, UpstreamSchemaDifference{
		Kind: "length", Table: "plans", Column: "name", Expected: "64", Actual: "32",
	})
	for _, difference := range observation.Differences {
		require.NotEqual(t, "default", difference.Kind)
	}
}

func TestDefaultNormalizationIgnoresDialectRenderingOnly(t *testing.T) {
	stringDefault := upstreamDefaultSchema{Kind: upstreamDefaultLiteral, ValueType: "string", Value: "USD"}
	actual, err := observedDefaultSchema("'USD'::character varying", true, stringDefault, false)
	require.NoError(t, err)
	require.Equal(t, stringDefault, actual)

	emptyDefault := upstreamDefaultSchema{Kind: upstreamDefaultLiteral, ValueType: "string", Value: ""}
	actual, err = observedDefaultSchema(`""`, true, emptyDefault, false)
	require.NoError(t, err)
	require.Equal(t, emptyDefault, actual)

	none := upstreamDefaultSchema{Kind: upstreamDefaultNone}
	actual, err = observedDefaultSchema("nextval('users_id_seq'::regclass)", true, none, true)
	require.NoError(t, err)
	require.Equal(t, none, actual)
}

func TestPostgresPartialPredicateIsComparedExactly(t *testing.T) {
	expectedPredicate, err := normalizedIndexPredicate("deleted_at IS NULL")
	require.NoError(t, err)
	actualPredicate, err := normalizedIndexPredicate("(deleted_at IS NOT NULL)")
	require.NoError(t, err)
	require.NotEqual(t, expectedPredicate, actualPredicate)

	observation := UpstreamSchemaObservation{}
	compareUniqueIndexes(&observation, upstreamModelSchema{
		Table: "prefill_groups",
		UniqueIndexes: []upstreamUniqueIndexSchema{{
			Columns: []string{"name"}, Predicate: expectedPredicate,
		}},
	}, map[string]struct{}{
		uniqueIndexSignature([]string{"name"}, actualPredicate): {},
	})
	require.Len(t, observation.Differences, 2)
	require.Equal(t, "unique_missing", observation.Differences[0].Kind)
	require.Equal(t, "unique_unexpected", observation.Differences[1].Kind)
}
