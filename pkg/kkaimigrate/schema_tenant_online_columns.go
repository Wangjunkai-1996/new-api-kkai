package kkaimigrate

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

var tenantOnlineSQLType = regexp.MustCompile(`^([a-z][a-z0-9 ]*)(?:\(([0-9]+)\))?$`)

func validateTenantOnlineColumn(db *gorm.DB, table string, field *schema.Field, column gorm.ColumnType) error {
	expected := strings.ToLower(db.Dialector.DataTypeOf(field))
	if dataType, ok := reflect.New(field.IndirectFieldType).Interface().(interface {
		GormDBDataType(*gorm.DB, *schema.Field) string
	}); ok {
		expected = strings.ToLower(dataType.GormDBDataType(db, field))
	}
	expected = strings.TrimSuffix(expected, " primary key autoincrement")
	expected = strings.TrimSuffix(expected, " auto_increment")
	expected = strings.TrimSuffix(expected, " null")
	match := tenantOnlineSQLType.FindStringSubmatch(expected)
	if match == nil {
		return fmt.Errorf("%w: unsupported tenant column contract %s.%s", ErrSchemaNotReady, table, field.DBName)
	}
	typeName := tenantOnlineTypeName(column.DatabaseTypeName())
	if db.Dialector.Name() == DialectMySQL && match[1] == "boolean" && typeName == "tinyint" {
		if definition, known := column.ColumnType(); known && strings.EqualFold(definition, "tinyint(1)") {
			typeName = "boolean"
		}
	}
	if typeName != tenantOnlineTypeName(match[1]) {
		return fmt.Errorf("%w: %s.%s has type %s, expected %s", ErrSchemaNotReady, table, field.DBName, typeName, expected)
	}
	if match[2] != "" {
		if typeName == "varchar" || typeName == "char" {
			length, known := column.Length()
			wanted, _ := strconv.ParseInt(match[2], 10, 64)
			if !known || length != wanted {
				return fmt.Errorf("%w: %s.%s has an invalid length", ErrSchemaNotReady, table, field.DBName)
			}
		} else if definition, known := column.ColumnType(); !known || strings.ToLower(definition) != expected {
			return fmt.Errorf("%w: %s.%s has invalid type precision", ErrSchemaNotReady, table, field.DBName)
		}
	}
	if field.PrimaryKey {
		// PostgreSQL's GORM constraint lookup hides primary keys from SELECT-only
		// observers; the complete ordered key is checked through pg_catalog.
		if db.Dialector.Name() != DialectPostgres {
			primary, known := column.PrimaryKey()
			if !known || !primary {
				return fmt.Errorf("%w: %s.%s must be a primary key", ErrSchemaNotReady, table, field.DBName)
			}
		}
	} else {
		nullable, known := column.Nullable()
		if !known || nullable == field.NotNull {
			return fmt.Errorf("%w: %s.%s has invalid nullability", ErrSchemaNotReady, table, field.DBName)
		}
	}
	if field.PrimaryKey && field.AutoIncrement {
		return nil // Dialect-native generated identity defaults differ by database.
	}
	actualDefault, hasDefault := column.DefaultValue()
	actualDefault = tenantOnlineDefault(actualDefault)
	if !field.HasDefaultValue {
		if hasDefault && actualDefault != "NULL" {
			return fmt.Errorf("%w: %s.%s must not acquire a non-NULL default", ErrSchemaNotReady, table, field.DBName)
		}
		return nil
	}
	if !hasDefault || actualDefault != tenantOnlineDefault(field.DefaultValue) {
		return fmt.Errorf("%w: %s.%s has an invalid default", ErrSchemaNotReady, table, field.DBName)
	}
	return nil
}

func validateTenantOnlinePostgresPrimaryKey(db *gorm.DB, table string, expected []string) error {
	var actual []string
	err := db.Raw(`SELECT a.attname
FROM pg_catalog.pg_index i
JOIN LATERAL unnest(i.indkey) WITH ORDINALITY k(attnum,position) ON true
JOIN pg_catalog.pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=k.attnum
WHERE i.indrelid=pg_catalog.to_regclass(?) AND i.indisprimary AND i.indisunique
 AND i.indisvalid AND i.indisready AND i.indimmediate AND i.indnatts=?
 AND i.indpred IS NULL AND i.indexprs IS NULL
ORDER BY k.position`, table, len(expected)).Scan(&actual).Error
	if err != nil {
		return err
	}
	if !slices.Equal(actual, expected) {
		return fmt.Errorf("%w: %s has an invalid primary key", ErrSchemaNotReady, table)
	}
	return nil
}

func tenantOnlineTypeName(value string) string {
	switch value = strings.ToLower(strings.TrimSpace(value)); value {
	case "int8", "bigserial":
		return "bigint"
	case "int4", "serial", "int":
		return "integer"
	case "bool":
		return "boolean"
	case "character varying":
		return "varchar"
	case "bpchar", "character":
		return "char"
	case "timestamp with time zone":
		return "timestamptz"
	case "timestamp without time zone":
		return "timestamp"
	default:
		return value
	}
}

func tenantOnlineDefault(value string) string {
	value = strings.TrimSpace(strings.SplitN(value, "::", 2)[0])
	return strings.Trim(value, "()'\"")
}
