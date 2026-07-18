package model

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

type UpstreamSchemaDifference struct {
	Kind     string `json:"kind"`
	Table    string `json:"table"`
	Column   string `json:"column,omitempty"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

type UpstreamSchemaObservation struct {
	Schema            int                        `json:"schema"`
	Dialect           string                     `json:"dialect"`
	ModelSchemaDigest string                     `json:"model_schema_digest"`
	Ready             bool                       `json:"ready"`
	MissingTables     []string                   `json:"missing_tables"`
	MissingColumns    []string                   `json:"missing_columns"`
	Differences       []UpstreamSchemaDifference `json:"differences"`
}

func ObserveUpstreamSchema(ctx context.Context, db *gorm.DB) (UpstreamSchemaObservation, error) {
	if db == nil {
		return UpstreamSchemaObservation{}, fmt.Errorf("upstream schema observation requires a database")
	}
	dialect := db.Dialector.Name()
	if dialect != upstreamDialectSQLite && dialect != upstreamDialectMySQL && dialect != upstreamDialectPostgres {
		return UpstreamSchemaObservation{}, fmt.Errorf("unsupported upstream schema observation dialect %q", dialect)
	}
	definition, err := canonicalUpstreamSchemaDefinition()
	if err != nil {
		return UpstreamSchemaObservation{}, err
	}
	definition, err = upstreamSchemaDefinitionForDialect(definition, dialect)
	if err != nil {
		return UpstreamSchemaObservation{}, err
	}
	digest, err := UpstreamSchemaDigest()
	if err != nil {
		return UpstreamSchemaObservation{}, err
	}
	observation := UpstreamSchemaObservation{
		Schema:            1,
		Dialect:           dialect,
		ModelSchemaDigest: digest,
		MissingTables:     []string{},
		MissingColumns:    []string{},
		Differences:       []UpstreamSchemaDifference{},
	}
	migrator := db.WithContext(ctx).Migrator()
	for _, expectedModel := range definition.Models {
		if !migrator.HasTable(expectedModel.Table) {
			observation.MissingTables = append(observation.MissingTables, expectedModel.Table)
			continue
		}
		columnTypes, err := migrator.ColumnTypes(expectedModel.Table)
		if err != nil {
			observation.Differences = append(observation.Differences, UpstreamSchemaDifference{
				Kind: "column_metadata", Table: expectedModel.Table,
				Expected: "available", Actual: "unavailable",
			})
			continue
		}
		actualColumns, err := observedColumns(db.WithContext(ctx), dialect, expectedModel.Table, columnTypes)
		if err != nil {
			observation.Differences = append(observation.Differences, UpstreamSchemaDifference{
				Kind: "column_metadata", Table: expectedModel.Table,
				Expected: "canonical", Actual: "unavailable",
			})
			continue
		}
		expectedColumns := make(map[string]struct{}, len(expectedModel.Fields))
		for _, expectedField := range expectedModel.Fields {
			expectedColumns[expectedField.Column] = struct{}{}
			actualField, exists := actualColumns[expectedField.Column]
			if !exists {
				observation.MissingColumns = append(observation.MissingColumns, expectedModel.Table+"."+expectedField.Column)
				continue
			}
			compareUpstreamField(&observation, expectedModel.Table, expectedField, actualField)
		}
		for column := range actualColumns {
			if _, exists := expectedColumns[column]; exists {
				continue
			}
			observation.Differences = append(observation.Differences, UpstreamSchemaDifference{
				Kind: "column_unexpected", Table: expectedModel.Table, Column: column,
				Expected: "absent", Actual: "present",
			})
		}
		actualUnique, err := observedUniqueIndexes(db.WithContext(ctx), dialect, expectedModel.Table)
		if err != nil {
			observation.Differences = append(observation.Differences, UpstreamSchemaDifference{
				Kind: "unique_metadata", Table: expectedModel.Table,
				Expected: "available", Actual: "unavailable",
			})
			continue
		}
		compareUniqueIndexes(&observation, expectedModel, actualUnique)
	}
	sort.Strings(observation.MissingTables)
	sort.Strings(observation.MissingColumns)
	sort.Slice(observation.Differences, func(left, right int) bool {
		leftKey := observation.Differences[left].Table + "\x00" + observation.Differences[left].Column + "\x00" + observation.Differences[left].Kind
		rightKey := observation.Differences[right].Table + "\x00" + observation.Differences[right].Column + "\x00" + observation.Differences[right].Kind
		return leftKey < rightKey
	})
	observation.Ready = len(observation.MissingTables) == 0 && len(observation.MissingColumns) == 0 && len(observation.Differences) == 0
	return observation, nil
}

type observedUpstreamColumn struct {
	TypeShape       upstreamColumnTypeShape
	TypeName        string
	Nullable        bool
	NullableKnown   bool
	PrimaryKey      bool
	PrimaryKeyKnown bool
	AutoIncrement   bool
	DefaultRaw      string
	DefaultExists   bool
}

func observedColumns(db *gorm.DB, dialect, table string, columnTypes []gorm.ColumnType) (map[string]observedUpstreamColumn, error) {
	if dialect == upstreamDialectSQLite {
		return observedSQLiteColumns(db, table)
	}
	columns := make(map[string]observedUpstreamColumn, len(columnTypes))
	for _, columnType := range columnTypes {
		name := columnType.Name()
		if name == "" {
			return nil, fmt.Errorf("column catalog contains an unnamed column")
		}
		if _, duplicate := columns[name]; duplicate {
			return nil, fmt.Errorf("column catalog contains duplicate column %q", name)
		}
		fullType, _ := columnType.ColumnType()
		databaseType := columnType.DatabaseTypeName()
		typeShape, typeErr := normalizedColumnTypeShape(dialect, databaseType, fullType)
		if typeErr == nil {
			typeShape = enrichObservedTypeShape(typeShape, columnType)
		}
		nullable, nullableKnown := columnType.Nullable()
		primaryKey, primaryKeyKnown := columnType.PrimaryKey()
		autoIncrement, _ := columnType.AutoIncrement()
		defaultValue, defaultExists := columnType.DefaultValue()
		columns[name] = observedUpstreamColumn{
			TypeShape:       typeShape,
			TypeName:        sanitizedTypeName(databaseType),
			Nullable:        nullable,
			NullableKnown:   nullableKnown,
			PrimaryKey:      primaryKey,
			PrimaryKeyKnown: primaryKeyKnown,
			AutoIncrement:   autoIncrement,
			DefaultRaw:      defaultValue,
			DefaultExists:   defaultExists,
		}
	}
	return columns, nil
}

func enrichObservedTypeShape(shape upstreamColumnTypeShape, columnType gorm.ColumnType) upstreamColumnTypeShape {
	if typeShapeUsesLength(shape) && shape.Length == 0 {
		if length, known := columnType.Length(); known && length > 0 {
			shape.Length = length
		}
	}
	if typeShapeUsesPrecision(shape) && shape.Precision == 0 {
		if precision, scale, known := columnType.DecimalSize(); known && precision > 0 {
			shape.Precision = precision
			shape.Scale = scale
		}
	}
	return shape
}

func observedSQLiteColumns(db *gorm.DB, table string) (map[string]observedUpstreamColumn, error) {
	type tableColumnRow struct {
		Name       string         `gorm:"column:name"`
		Type       string         `gorm:"column:type"`
		NotNull    int            `gorm:"column:notnull"`
		DefaultRaw sql.NullString `gorm:"column:dflt_value"`
		Primary    int            `gorm:"column:pk"`
		Hidden     int            `gorm:"column:hidden"`
	}
	var rows []tableColumnRow
	if err := db.Raw(
		`SELECT name, type, "notnull", dflt_value, pk, hidden FROM pragma_table_xinfo(?)`,
		table,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	primaryColumnCount := 0
	for _, row := range rows {
		if row.Primary > 0 {
			primaryColumnCount++
		}
	}
	var createSQL sql.NullString
	if err := db.Raw(
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`,
		table,
	).Scan(&createSQL).Error; err != nil {
		return nil, err
	}
	withoutRowID := strings.Contains(strings.ToUpper(createSQL.String), "WITHOUT ROWID")

	columns := make(map[string]observedUpstreamColumn, len(rows))
	for _, row := range rows {
		if row.Name == "" || row.Type == "" {
			return nil, fmt.Errorf("SQLite column catalog contains incomplete metadata")
		}
		if _, duplicate := columns[row.Name]; duplicate {
			return nil, fmt.Errorf("SQLite column catalog contains duplicate column %q", row.Name)
		}
		typeShape, typeErr := normalizedColumnTypeShape(upstreamDialectSQLite, row.Type, row.Type)
		primary := row.Primary > 0
		autoIncrement := typeErr == nil && primaryColumnCount == 1 && primary &&
			typeShape.Family == upstreamTypeInteger && typeShape.Variant == "integer_dynamic" && !withoutRowID
		columns[row.Name] = observedUpstreamColumn{
			TypeShape:       typeShape,
			TypeName:        sanitizedTypeName(row.Type),
			Nullable:        row.NotNull == 0 && !primary,
			NullableKnown:   true,
			PrimaryKey:      primary,
			PrimaryKeyKnown: true,
			AutoIncrement:   autoIncrement,
			DefaultRaw:      row.DefaultRaw.String,
			DefaultExists:   row.DefaultRaw.Valid,
		}
	}
	return columns, nil
}

func compareUpstreamField(observation *UpstreamSchemaObservation, table string, expected upstreamFieldSchema, actual observedUpstreamColumn) {
	if actual.TypeShape.Family == "" {
		appendFieldDifference(observation, "type_unmapped", table, expected.Column, expected.TypeFamily, actual.TypeName)
	} else {
		appendShapeDifference(observation, table, expected.Column, "type_family", expected.TypeFamily, actual.TypeShape.Family)
		appendShapeDifference(observation, table, expected.Column, "type_variant", expected.TypeVariant, actual.TypeShape.Variant)
		appendShapeDifference(observation, table, expected.Column, "length", expected.Length, actual.TypeShape.Length)
		expectedPrecision := expected.Precision
		actualPrecision := actual.TypeShape.Precision
		if observation.Dialect == upstreamDialectPostgres && expected.TypeFamily == upstreamTypeTemporal && expectedPrecision == 0 && actualPrecision == 6 {
			actualPrecision = 0
		}
		appendShapeDifference(observation, table, expected.Column, "precision", expectedPrecision, actualPrecision)
		appendShapeDifference(observation, table, expected.Column, "scale", expected.Scale, actual.TypeShape.Scale)
		appendShapeDifference(observation, table, expected.Column, "unsigned", expected.Unsigned, actual.TypeShape.Unsigned)
	}
	if !actual.NullableKnown {
		appendFieldDifference(observation, "nullable_metadata", table, expected.Column, fmt.Sprintf("%t", expected.Nullable), "unavailable")
	} else {
		appendShapeDifference(observation, table, expected.Column, "nullable", expected.Nullable, actual.Nullable)
	}
	if !actual.PrimaryKeyKnown {
		appendFieldDifference(observation, "primary_key_metadata", table, expected.Column, fmt.Sprintf("%t", expected.PrimaryKey), "unavailable")
	} else {
		appendShapeDifference(observation, table, expected.Column, "primary_key", expected.PrimaryKey, actual.PrimaryKey)
	}
	appendShapeDifference(observation, table, expected.Column, "auto_increment", expected.AutoIncrement, actual.AutoIncrement)
	actualDefault, err := observedDefaultSchema(actual.DefaultRaw, actual.DefaultExists, expected.Default, actual.AutoIncrement)
	if err != nil {
		appendFieldDifference(observation, "default_metadata", table, expected.Column, defaultSchemaLabel(expected.Default), "unavailable")
	} else if expected.Default != actualDefault {
		appendFieldDifference(observation, "default", table, expected.Column, defaultSchemaLabel(expected.Default), defaultSchemaLabel(actualDefault))
	}
}

func appendShapeDifference[T comparable](observation *UpstreamSchemaObservation, table, column, kind string, expected, actual T) {
	if expected == actual {
		return
	}
	appendFieldDifference(observation, kind, table, column, fmt.Sprint(expected), fmt.Sprint(actual))
}

func appendFieldDifference(observation *UpstreamSchemaObservation, kind, table, column, expected, actual string) {
	observation.Differences = append(observation.Differences, UpstreamSchemaDifference{
		Kind: kind, Table: table, Column: column, Expected: expected, Actual: actual,
	})
}

func typeShapeUsesLength(shape upstreamColumnTypeShape) bool {
	return (shape.Family == upstreamTypeString || shape.Family == upstreamTypeBinary) &&
		(shape.Variant == "fixed" || shape.Variant == "variable")
}

func typeShapeUsesPrecision(shape upstreamColumnTypeShape) bool {
	return shape.Family == upstreamTypeNumeric && shape.Variant == "decimal" ||
		shape.Family == upstreamTypeTemporal && (shape.Variant == "time" || shape.Variant == "timestamp")
}

func compareUniqueIndexes(observation *UpstreamSchemaObservation, expected upstreamModelSchema, actual map[string]struct{}) {
	expectedSet := make(map[string]struct{}, len(expected.UniqueIndexes))
	for _, index := range expected.UniqueIndexes {
		signature := uniqueIndexSignature(index.Columns, index.Predicate)
		expectedSet[signature] = struct{}{}
		if _, exists := actual[signature]; !exists {
			observation.Differences = append(observation.Differences, UpstreamSchemaDifference{
				Kind: "unique_missing", Table: expected.Table, Expected: signature, Actual: "",
			})
		}
	}
	for signature := range actual {
		if _, exists := expectedSet[signature]; !exists {
			observation.Differences = append(observation.Differences, UpstreamSchemaDifference{
				Kind: "unique_unexpected", Table: expected.Table, Expected: "", Actual: signature,
			})
		}
	}
}

func observedUniqueIndexes(db *gorm.DB, dialect, table string) (map[string]struct{}, error) {
	if dialect == upstreamDialectSQLite {
		return observedSQLiteUniqueIndexes(db, table)
	}
	indexes, err := db.Migrator().GetIndexes(table)
	if err != nil {
		return nil, err
	}
	predicates := map[string]string{}
	if dialect == upstreamDialectPostgres {
		var rows []struct {
			Name      string `gorm:"column:name"`
			Predicate string `gorm:"column:predicate"`
		}
		if err := db.Raw(`
SELECT index_class.relname AS name,
       COALESCE(pg_catalog.pg_get_expr(index_data.indpred, index_data.indrelid, true), '') AS predicate
FROM pg_catalog.pg_index AS index_data
JOIN pg_catalog.pg_class AS table_class ON table_class.oid = index_data.indrelid
JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = table_class.relnamespace
JOIN pg_catalog.pg_class AS index_class ON index_class.oid = index_data.indexrelid
WHERE namespace.nspname = current_schema() AND table_class.relname = ?
`, table).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			predicate := ""
			if strings.TrimSpace(row.Predicate) != "" {
				predicate, err = normalizedIndexPredicate(row.Predicate)
				if err != nil {
					return nil, err
				}
			}
			predicates[row.Name] = predicate
		}
	}
	return uniqueIndexSignatures(dialect, indexes, predicates)
}

func uniqueIndexSignatures(dialect string, indexes []gorm.Index, predicates map[string]string) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	for _, index := range indexes {
		primary, primaryKnown := index.PrimaryKey()
		unique, uniqueKnown := index.Unique()
		if !primaryKnown || !uniqueKnown {
			return nil, fmt.Errorf("index %q has incomplete constraint metadata", index.Name())
		}
		if primary || !unique {
			continue
		}
		columns := append([]string(nil), index.Columns()...)
		if len(columns) == 0 {
			return nil, fmt.Errorf("unique index %q has no columns", index.Name())
		}
		for _, column := range columns {
			if column == "" {
				return nil, fmt.Errorf("unique index %q contains an expression", index.Name())
			}
		}
		sort.Strings(columns)
		predicate := ""
		if dialect == upstreamDialectPostgres {
			var exists bool
			predicate, exists = predicates[index.Name()]
			if !exists {
				return nil, fmt.Errorf("index %q has no PostgreSQL predicate metadata", index.Name())
			}
		}
		result[uniqueIndexSignature(columns, predicate)] = struct{}{}
	}
	return result, nil
}

func observedSQLiteUniqueIndexes(db *gorm.DB, table string) (map[string]struct{}, error) {
	type indexListRow struct {
		Name    string `gorm:"column:name"`
		Unique  int    `gorm:"column:is_unique"`
		Origin  string `gorm:"column:origin"`
		Partial int    `gorm:"column:partial"`
	}
	var indexes []indexListRow
	if err := db.Raw(
		`SELECT name, "unique" AS is_unique, origin, partial FROM pragma_index_list(?)`,
		table,
	).Scan(&indexes).Error; err != nil {
		return nil, err
	}
	result := make(map[string]struct{})
	for _, index := range indexes {
		if index.Unique != 1 || index.Origin == "pk" {
			continue
		}
		var columns []struct {
			Name string `gorm:"column:name"`
		}
		if err := db.Raw(
			`SELECT name FROM pragma_index_info(?) ORDER BY seqno`,
			index.Name,
		).Scan(&columns).Error; err != nil {
			return nil, err
		}
		columnNames := make([]string, 0, len(columns))
		for _, column := range columns {
			if column.Name == "" {
				return nil, fmt.Errorf("unique index %q contains an expression", index.Name)
			}
			columnNames = append(columnNames, column.Name)
		}
		if len(columnNames) == 0 {
			return nil, fmt.Errorf("unique index %q has no columns", index.Name)
		}
		sort.Strings(columnNames)
		predicate := ""
		if index.Partial == 1 {
			predicate = "partial"
		}
		result[uniqueIndexSignature(columnNames, predicate)] = struct{}{}
	}
	return result, nil
}

func sanitizedTypeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unavailable"
	}
	if len(value) > 64 {
		return "unmapped"
	}
	for _, current := range value {
		if current >= 'a' && current <= 'z' || current >= '0' && current <= '9' || strings.ContainsRune("_ [](),", current) {
			continue
		}
		return "unmapped"
	}
	return value
}
