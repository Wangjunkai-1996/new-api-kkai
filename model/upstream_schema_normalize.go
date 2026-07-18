package model

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

func normalizedTypeCategory(dialect, databaseType, columnType string) (string, error) {
	shape, err := normalizedColumnTypeShape(dialect, databaseType, columnType)
	return shape.Family, err
}

func normalizedColumnTypeShape(dialect, databaseType, columnType string) (upstreamColumnTypeShape, error) {
	rawType := strings.TrimSpace(columnType)
	if rawType == "" {
		rawType = strings.TrimSpace(databaseType)
	}
	if rawType == "" {
		return upstreamColumnTypeShape{}, fmt.Errorf("unsupported %s empty schema type", dialect)
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(rawType), " "))
	databaseType = strings.ToLower(strings.TrimSpace(databaseType))
	if strings.HasPrefix(databaseType, "_") || strings.HasSuffix(normalized, "[]") {
		element := strings.TrimPrefix(databaseType, "_")
		if element == "" {
			element = strings.TrimSuffix(normalized, "[]")
		}
		return upstreamColumnTypeShape{Family: upstreamTypeArray, Variant: "array_" + normalizedTypeBase(element)}, nil
	}

	base := normalizedTypeBase(normalized)
	parameters, err := normalizedTypeParameters(normalized)
	if err != nil {
		return upstreamColumnTypeShape{}, err
	}
	shape := upstreamColumnTypeShape{Unsigned: containsSQLWord(normalized, "unsigned")}
	switch base {
	case "bool", "boolean":
		shape.Family, shape.Variant = upstreamTypeBoolean, "boolean"
	case "tinyint":
		if dialect == upstreamDialectMySQL && len(parameters) == 1 && parameters[0] == 1 {
			shape.Family, shape.Variant = upstreamTypeBoolean, "boolean"
		} else {
			shape.Family, shape.Variant = upstreamTypeInteger, "integer_8"
		}
	case "smallint", "int2", "smallserial":
		shape.Family, shape.Variant = upstreamTypeInteger, "integer_16"
	case "mediumint":
		shape.Family, shape.Variant = upstreamTypeInteger, "integer_24"
	case "int", "int4", "serial":
		shape.Family, shape.Variant = upstreamTypeInteger, "integer_32"
	case "integer":
		shape.Family = upstreamTypeInteger
		if dialect == upstreamDialectSQLite {
			shape.Variant = "integer_dynamic"
		} else {
			shape.Variant = "integer_32"
		}
	case "bigint", "int8", "bigserial", "uint":
		shape.Family, shape.Variant = upstreamTypeInteger, "integer_64"
	case "decimal", "numeric":
		shape.Family, shape.Variant = upstreamTypeNumeric, "decimal"
		if len(parameters) > 0 {
			shape.Precision = parameters[0]
		}
		if len(parameters) > 1 {
			shape.Scale = parameters[1]
		}
	case "float", "float4":
		shape.Family, shape.Variant = upstreamTypeNumeric, "float_32"
	case "double", "double precision", "float8":
		shape.Family, shape.Variant = upstreamTypeNumeric, "float_64"
	case "real":
		shape.Family = upstreamTypeNumeric
		if dialect == upstreamDialectPostgres {
			shape.Variant = "float_32"
		} else {
			shape.Variant = "float_64"
		}
	case "char", "character":
		shape.Family, shape.Variant = upstreamTypeString, "fixed"
		shape.Length = firstTypeParameter(parameters)
	case "varchar", "character varying", "nvarchar", "citext":
		shape.Family, shape.Variant = upstreamTypeString, "variable"
		shape.Length = firstTypeParameter(parameters)
	case "clob", "longtext", "mediumtext", "name", "text", "tinytext":
		shape.Family, shape.Variant = upstreamTypeString, "text"
	case "enum", "set":
		shape.Family, shape.Variant = upstreamTypeString, base
	case "binary":
		shape.Family, shape.Variant = upstreamTypeBinary, "fixed"
		shape.Length = firstTypeParameter(parameters)
	case "varbinary":
		shape.Family, shape.Variant = upstreamTypeBinary, "variable"
		shape.Length = firstTypeParameter(parameters)
	case "blob", "bytea", "bytes", "longblob", "mediumblob", "tinyblob":
		shape.Family, shape.Variant = upstreamTypeBinary, "blob"
	case "date":
		shape.Family, shape.Variant = upstreamTypeTemporal, "date"
	case "time", "timetz":
		shape.Family, shape.Variant = upstreamTypeTemporal, "time"
		shape.Precision = firstTypeParameter(parameters)
	case "datetime", "timestamp", "timestamp with time zone", "timestamp without time zone", "timestamptz":
		shape.Family, shape.Variant = upstreamTypeTemporal, "timestamp"
		shape.Precision = firstTypeParameter(parameters)
	case "interval":
		shape.Family, shape.Variant = upstreamTypeTemporal, "interval"
	case "json", "jsonb":
		shape.Family, shape.Variant = upstreamTypeJSON, base
	case "uuid":
		shape.Family, shape.Variant = upstreamTypeUUID, "uuid"
	default:
		return upstreamColumnTypeShape{}, fmt.Errorf("unsupported %s schema type %q", dialect, databaseType)
	}
	return shape, nil
}

func normalizedTypeBase(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, prefix := range []string{
		"timestamp without time zone", "timestamp with time zone", "character varying", "double precision",
	} {
		if value == prefix || strings.HasPrefix(value, prefix+"(") || strings.HasPrefix(value, prefix+" ") {
			return prefix
		}
	}
	if index := strings.IndexAny(value, "( "); index >= 0 {
		return value[:index]
	}
	return value
}

func normalizedTypeParameters(value string) ([]int64, error) {
	open := strings.IndexByte(value, '(')
	if open < 0 {
		return nil, nil
	}
	close := strings.IndexByte(value[open+1:], ')')
	if close < 0 {
		return nil, fmt.Errorf("schema type has an unterminated parameter list")
	}
	parameterText := value[open+1 : open+1+close]
	if strings.ContainsAny(parameterText, "'\"") {
		return nil, nil
	}
	parts := strings.Split(parameterText, ",")
	parameters := make([]int64, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("schema type contains an empty parameter")
		}
		parsed, err := strconv.ParseInt(part, 10, 64)
		if err != nil || parsed < 0 {
			return nil, fmt.Errorf("schema type contains a non-numeric parameter")
		}
		parameters = append(parameters, parsed)
	}
	return parameters, nil
}

func firstTypeParameter(parameters []int64) int64 {
	if len(parameters) == 0 {
		return 0
	}
	return parameters[0]
}

func containsSQLWord(value, word string) bool {
	for _, candidate := range strings.FieldsFunc(value, func(current rune) bool {
		return !unicode.IsLetter(current) && !unicode.IsDigit(current) && current != '_'
	}) {
		if candidate == word {
			return true
		}
	}
	return false
}
