package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"gorm.io/gorm/schema"
)

const (
	upstreamTypeArray    = "array"
	upstreamTypeBinary   = "binary"
	upstreamTypeBoolean  = "boolean"
	upstreamTypeInteger  = "integer"
	upstreamTypeJSON     = "json"
	upstreamTypeNumeric  = "numeric"
	upstreamTypeString   = "string"
	upstreamTypeTemporal = "temporal"
	upstreamTypeUUID     = "uuid"

	upstreamDialectSQLite   = "sqlite"
	upstreamDialectMySQL    = "mysql"
	upstreamDialectPostgres = "postgres"

	upstreamDefaultNone       = "none"
	upstreamDefaultLiteral    = "literal"
	upstreamDefaultExpression = "expression"
)

type upstreamColumnTypeShape struct {
	Family    string
	Variant   string
	Length    int64
	Precision int64
	Scale     int64
	Unsigned  bool
}

type upstreamDefaultSchema struct {
	Kind      string `json:"kind"`
	ValueType string `json:"value_type,omitempty"`
	Value     string `json:"value,omitempty"`
}

func expectedDefaultSchema(field *schema.Field) (upstreamDefaultSchema, error) {
	if field.AutoIncrement || !field.HasDefaultValue || strings.EqualFold(field.DefaultValue, "null") {
		return upstreamDefaultSchema{Kind: upstreamDefaultNone}, nil
	}
	if field.DefaultValueInterface != nil {
		switch value := field.DefaultValueInterface.(type) {
		case bool:
			return upstreamDefaultSchema{Kind: upstreamDefaultLiteral, ValueType: "boolean", Value: strconv.FormatBool(value)}, nil
		case int64:
			return upstreamDefaultSchema{Kind: upstreamDefaultLiteral, ValueType: "number", Value: strconv.FormatInt(value, 10)}, nil
		case uint64:
			return upstreamDefaultSchema{Kind: upstreamDefaultLiteral, ValueType: "number", Value: strconv.FormatUint(value, 10)}, nil
		case float64:
			return upstreamDefaultSchema{Kind: upstreamDefaultLiteral, ValueType: "number", Value: strconv.FormatFloat(value, 'g', -1, 64)}, nil
		case string:
			return upstreamDefaultSchema{Kind: upstreamDefaultLiteral, ValueType: "string", Value: value}, nil
		default:
			return upstreamDefaultSchema{}, fmt.Errorf("unsupported default value type %T", value)
		}
	}
	if _, explicit := field.TagSettings["DEFAULT"]; explicit && field.DataType == schema.String {
		return upstreamDefaultSchema{Kind: upstreamDefaultLiteral, ValueType: "string", Value: field.DefaultValue}, nil
	}
	if strings.TrimSpace(field.DefaultValue) == "" {
		return upstreamDefaultSchema{Kind: upstreamDefaultNone}, nil
	}
	expression, err := normalizedDefaultExpression(field.DefaultValue)
	if err != nil {
		return upstreamDefaultSchema{}, err
	}
	return upstreamDefaultSchema{Kind: upstreamDefaultExpression, ValueType: "expression", Value: expression}, nil
}

func applyUpstreamFieldSemanticOverride(
	dialect, table, column string,
	typeShape *upstreamColumnTypeShape,
	defaultSchema *upstreamDefaultSchema,
) {
	if dialect != upstreamDialectSQLite || table != "subscription_plans" {
		return
	}
	switch column {
	case "allow_balance_pay", "allow_wallet_overflow":
		*defaultSchema = upstreamDefaultSchema{Kind: upstreamDefaultLiteral, ValueType: "boolean", Value: "true"}
	case "created_at", "updated_at":
		*typeShape = upstreamColumnTypeShape{Family: upstreamTypeInteger, Variant: "integer_64"}
	case "price_amount":
		*defaultSchema = upstreamDefaultSchema{Kind: upstreamDefaultNone}
	}
}

func observedDefaultSchema(raw string, exists bool, expected upstreamDefaultSchema, autoIncrement bool) (upstreamDefaultSchema, error) {
	if autoIncrement || !exists {
		return upstreamDefaultSchema{Kind: upstreamDefaultNone}, nil
	}
	raw = stripPostgresDefaultCast(strings.TrimSpace(raw))
	for hasBalancedOuterParentheses(raw) {
		raw = strings.TrimSpace(raw[1 : len(raw)-1])
	}
	if raw == "" || strings.EqualFold(raw, "null") {
		return upstreamDefaultSchema{Kind: upstreamDefaultNone}, nil
	}
	if expected.Kind == upstreamDefaultLiteral {
		value, err := normalizedDefaultLiteral(raw, expected.ValueType)
		if err != nil {
			return upstreamDefaultSchema{}, err
		}
		return upstreamDefaultSchema{Kind: upstreamDefaultLiteral, ValueType: expected.ValueType, Value: value}, nil
	}
	expression, err := normalizedDefaultExpression(raw)
	if err == nil {
		return upstreamDefaultSchema{Kind: upstreamDefaultExpression, ValueType: "expression", Value: expression}, nil
	}
	return upstreamDefaultSchema{Kind: upstreamDefaultLiteral, ValueType: "opaque", Value: digestText(raw)}, nil
}

func normalizedDefaultLiteral(raw, valueType string) (string, error) {
	raw = unquoteSQLLiteral(strings.TrimSpace(raw))
	switch valueType {
	case "boolean":
		switch strings.ToLower(raw) {
		case "1", "t", "true":
			return "true", nil
		case "0", "f", "false":
			return "false", nil
		default:
			return "", fmt.Errorf("default is not a canonical boolean")
		}
	case "number":
		parsed := new(big.Rat)
		if _, ok := parsed.SetString(raw); !ok {
			return "", fmt.Errorf("default is not a canonical number")
		}
		return parsed.RatString(), nil
	case "string":
		return raw, nil
	default:
		return "", fmt.Errorf("unsupported default value type %q", valueType)
	}
}

func normalizedDefaultExpression(value string) (string, error) {
	normalized := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
	switch normalized {
	case "current_timestamp", "current_timestamp()", "now()":
		return "current_timestamp", nil
	case "current_date", "current_date()":
		return "current_date", nil
	case "current_time", "current_time()":
		return "current_time", nil
	default:
		return "", fmt.Errorf("default expression cannot be normalized")
	}
}

func stripPostgresDefaultCast(value string) string {
	quoted := false
	for index := 0; index+1 < len(value); index++ {
		if value[index] == '\'' {
			if quoted && index+1 < len(value) && value[index+1] == '\'' {
				index++
				continue
			}
			quoted = !quoted
			continue
		}
		if !quoted && value[index:index+2] == "::" {
			return strings.TrimSpace(value[:index])
		}
	}
	return value
}

func hasBalancedOuterParentheses(value string) bool {
	if len(value) < 2 || value[0] != '(' || value[len(value)-1] != ')' {
		return false
	}
	depth := 0
	quoted := false
	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '\'':
			quoted = !quoted
		case '(':
			if !quoted {
				depth++
			}
		case ')':
			if !quoted {
				depth--
				if depth == 0 && index != len(value)-1 {
					return false
				}
			}
		}
	}
	return depth == 0 && !quoted
}

func unquoteSQLLiteral(value string) string {
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	}
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		return value[1 : len(value)-1]
	}
	if len(value) >= 2 && value[0] == '`' && value[len(value)-1] == '`' {
		return value[1 : len(value)-1]
	}
	return value
}

func defaultSchemaLabel(value upstreamDefaultSchema) string {
	if value.Kind == upstreamDefaultNone {
		return upstreamDefaultNone
	}
	displayValue := value.Value
	if value.ValueType == "string" || value.ValueType == "opaque" {
		displayValue = digestText(value.Value)
	}
	return fmt.Sprintf("%s:%s:%s", value.Kind, value.ValueType, displayValue)
}

func expectedUniqueIndexes(parsed *schema.Schema, dialect string) ([]upstreamUniqueIndexSchema, error) {
	unique := make(map[string]upstreamUniqueIndexSchema)
	for _, field := range parsed.Fields {
		if field.DBName == "" || field.IgnoreMigration || !field.Unique {
			continue
		}
		index := upstreamUniqueIndexSchema{Columns: []string{field.DBName}}
		unique[uniqueIndexKey(index.Columns, index.Predicate)] = index
	}
	for _, index := range parsed.ParseIndexes() {
		if index.Class != "UNIQUE" {
			continue
		}
		columns := make([]string, 0, len(index.Fields))
		for _, option := range index.Fields {
			if option.Expression != "" || option.Field == nil || option.DBName == "" {
				return nil, fmt.Errorf("unique index %q contains an expression or unmapped field", index.Name)
			}
			columns = append(columns, option.DBName)
		}
		if len(columns) == 0 {
			return nil, fmt.Errorf("unique index %q has no columns", index.Name)
		}
		sort.Strings(columns)
		predicate := ""
		where := strings.TrimSpace(index.Where)
		if where != "" && dialect == upstreamDialectSQLite {
			predicate = "partial"
		}
		if where != "" && dialect == upstreamDialectPostgres {
			var err error
			predicate, err = normalizedIndexPredicate(where)
			if err != nil {
				return nil, fmt.Errorf("unique index %q predicate: %w", index.Name, err)
			}
		}
		key := uniqueIndexKey(columns, predicate)
		unique[key] = upstreamUniqueIndexSchema{Columns: columns, Predicate: predicate}
	}
	result := make([]upstreamUniqueIndexSchema, 0, len(unique))
	for _, index := range unique {
		result = append(result, index)
	}
	sort.Slice(result, func(left, right int) bool {
		return uniqueIndexKey(result[left].Columns, result[left].Predicate) < uniqueIndexKey(result[right].Columns, result[right].Predicate)
	})
	return result, nil
}

func normalizedIndexPredicate(value string) (string, error) {
	tokens := make([]string, 0, 8)
	for index := 0; index < len(value); {
		current := value[index]
		switch {
		case unicode.IsSpace(rune(current)):
			index++
		case current == '\'' || current == '"' || current == '`':
			quote := current
			start := index
			index++
			for index < len(value) {
				if value[index] == quote {
					if index+1 < len(value) && value[index+1] == quote {
						index += 2
						continue
					}
					index++
					break
				}
				index++
			}
			if index > len(value) || value[index-1] != quote {
				return "", fmt.Errorf("unterminated quoted predicate token")
			}
			literal := value[start:index]
			if quote == '\'' {
				tokens = append(tokens, "literal:"+digestText(unquoteSQLLiteral(literal)))
			} else {
				tokens = append(tokens, strings.ToLower(unquoteSQLLiteral(literal)))
			}
		case isPredicateIdentifierByte(current):
			start := index
			for index < len(value) && isPredicateIdentifierByte(value[index]) {
				index++
			}
			tokens = append(tokens, strings.ToLower(value[start:index]))
		case strings.ContainsRune("(),.=<>!:+-*/", rune(current)):
			if index+1 < len(value) {
				pair := value[index : index+2]
				switch pair {
				case "<=", ">=", "<>", "!=", "::":
					tokens = append(tokens, pair)
					index += 2
					continue
				}
			}
			tokens = append(tokens, string(current))
			index++
		default:
			return "", fmt.Errorf("predicate contains an unsupported token")
		}
	}
	for hasOuterPredicateParentheses(tokens) {
		tokens = tokens[1 : len(tokens)-1]
	}
	if len(tokens) == 0 {
		return "", fmt.Errorf("predicate is empty")
	}
	return strings.Join(tokens, " "), nil
}

func isPredicateIdentifierByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '_' || value == '$'
}

func hasOuterPredicateParentheses(tokens []string) bool {
	if len(tokens) < 2 || tokens[0] != "(" || tokens[len(tokens)-1] != ")" {
		return false
	}
	depth := 0
	for index, token := range tokens {
		switch token {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 && index != len(tokens)-1 {
				return false
			}
		}
	}
	return depth == 0
}

func uniqueIndexKey(columns []string, predicate string) string {
	return strings.Join(columns, ",") + "\x00" + predicate
}

func uniqueIndexSignature(columns []string, predicate string) string {
	predicateDigest := "none"
	if predicate != "" {
		predicateDigest = digestText(predicate)
	}
	return fmt.Sprintf("columns=%s;predicate=%s", strings.Join(columns, ","), predicateDigest)
}

func digestText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}
