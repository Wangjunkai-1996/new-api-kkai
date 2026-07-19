package kkaimigrate

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const migrationCatalogBaselineVersion int64 = RiskSchemaVersion

const (
	migrationChecksumSchemaLegacy  = 1
	migrationChecksumSchemaCurrent = 2

	migrationOperationCreateTable       = "create_table"
	migrationOperationCreateIndex       = "create_index_on_new_table"
	migrationOperationAddNullableColumn = "add_nullable_column"
	migrationOperationContract          = "contract"
)

var requiredMigrationDialects = [...]string{
	DialectSQLite,
	DialectMySQL,
	DialectPostgres,
}

func validateMigrationCatalog(migrations []migration) error {
	if len(migrations) == 0 {
		return unsafeMigrationCatalog("catalog is empty")
	}

	names := make(map[string]struct{}, len(migrations))
	implementationIDs := make(map[string]struct{}, len(migrations))
	for index, item := range migrations {
		expectedVersion := migrationCatalogBaselineVersion + int64(index)
		if item.Version != expectedVersion {
			return unsafeMigrationCatalog(
				"migration at position %d has version %d, expected %d",
				index, item.Version, expectedVersion,
			)
		}
		if item.Name == "" || item.Name != strings.TrimSpace(item.Name) {
			return unsafeMigrationCatalog("migration %d has no canonical name", item.Version)
		}
		if _, duplicate := names[item.Name]; duplicate {
			return unsafeMigrationCatalog("migration name %q is duplicated", item.Name)
		}
		names[item.Name] = struct{}{}
		if item.ImplementationID == "" || item.ImplementationID != strings.TrimSpace(item.ImplementationID) {
			return unsafeMigrationCatalog("migration %d has no canonical implementation ID", item.Version)
		}
		if _, duplicate := implementationIDs[item.ImplementationID]; duplicate {
			return unsafeMigrationCatalog("migration implementation ID %q is duplicated", item.ImplementationID)
		}
		implementationIDs[item.ImplementationID] = struct{}{}
		switch item.ChecksumVersion {
		case migrationChecksumSchemaLegacy:
			if item.Version > OutboxEventKeySchemaVersion {
				return unsafeMigrationCatalog("migration %d cannot use the legacy checksum schema", item.Version)
			}
		case migrationChecksumSchemaCurrent:
		default:
			return unsafeMigrationCatalog("migration %d has invalid checksum schema", item.Version)
		}
		if (item.ImportLegacy == nil) != (item.LegacyImportID == "") ||
			(item.ImportLegacy == nil) != (item.LegacyImportSpec == "") {
			return unsafeMigrationCatalog("migration %d has incomplete legacy import identity", item.Version)
		}
		if item.LegacyImportID != strings.TrimSpace(item.LegacyImportID) {
			return unsafeMigrationCatalog("migration %d has no canonical legacy import ID", item.Version)
		}
		if item.ChecksumVersion == migrationChecksumSchemaCurrent &&
			(len(item.Indexes) != 0 || item.ImportLegacy != nil) {
			return unsafeMigrationCatalog(
				"migration %d uses an out-of-band index or legacy callback", item.Version,
			)
		}

		switch item.Kind {
		case MigrationKindExpand, MigrationKindContract:
		default:
			return unsafeMigrationCatalog("migration %d has no valid kind", item.Version)
		}
		if err := validateMigrationDialectScope(item); err != nil {
			return err
		}
		if err := validateMigrationDialects(item); err != nil {
			return err
		}
	}
	return nil
}

func validateMigrationDialectScope(item migration) error {
	applyDialects := make(map[string]struct{}, len(item.ApplyDialects))
	for _, dialect := range item.ApplyDialects {
		if !isRequiredMigrationDialect(dialect) {
			return unsafeMigrationCatalog("migration %d has unsupported apply dialect %q", item.Version, dialect)
		}
		if _, duplicate := applyDialects[dialect]; duplicate {
			return unsafeMigrationCatalog("migration %d repeats apply dialect %q", item.Version, dialect)
		}
		applyDialects[dialect] = struct{}{}
	}
	legacyDialects := make(map[string]struct{}, len(item.LegacyDialects))
	for _, dialect := range item.LegacyDialects {
		if !isRequiredMigrationDialect(dialect) {
			return unsafeMigrationCatalog("migration %d has unsupported legacy dialect %q", item.Version, dialect)
		}
		if _, duplicate := legacyDialects[dialect]; duplicate {
			return unsafeMigrationCatalog("migration %d repeats legacy dialect %q", item.Version, dialect)
		}
		if len(item.ApplyDialects) == 0 {
			return unsafeMigrationCatalog("migration %d cannot add legacy dialects when it applies to every dialect", item.Version)
		}
		if _, applies := applyDialects[dialect]; applies {
			return unsafeMigrationCatalog("migration %d dialect %q cannot be both applied and legacy-only", item.Version, dialect)
		}
		legacyDialects[dialect] = struct{}{}
	}
	return nil
}

func validateMigrationDialects(item migration) error {
	if len(item.Statements) != len(requiredMigrationDialects) {
		return unsafeMigrationCatalog("migration %d does not define exactly the supported dialects", item.Version)
	}
	for dialect := range item.Statements {
		if !isRequiredMigrationDialect(dialect) {
			return unsafeMigrationCatalog("migration %d defines unsupported dialect %q", item.Version, dialect)
		}
	}
	for _, dialect := range requiredMigrationDialects {
		statements, exists := item.Statements[dialect]
		if !exists {
			return unsafeMigrationCatalog("migration %d is missing dialect %q", item.Version, dialect)
		}
		createdTables := map[string]struct{}{}
		for statementIndex, statement := range statements {
			if strings.TrimSpace(statement.SQL) == "" {
				return unsafeMigrationCatalog(
					"migration %d dialect %q statement %d is empty",
					item.Version, dialect, statementIndex,
				)
			}
			switch item.Kind {
			case MigrationKindExpand:
				if err := validateExpandSQL(dialect, statement, createdTables); err != nil {
					return unsafeMigrationCatalog(
						"migration %d dialect %q statement %d: %v",
						item.Version, dialect, statementIndex, err,
					)
				}
				if statement.Operation == migrationOperationCreateTable {
					tokens, _ := expandSQLTokens(dialect, statement.SQL)
					createdTables[createTableName(tokens)] = struct{}{}
				}
			case MigrationKindContract:
				if statement.Operation != migrationOperationContract {
					return unsafeMigrationCatalog(
						"migration %d dialect %q statement %d is not marked contract",
						item.Version, dialect, statementIndex,
					)
				}
			}
		}
	}
	return nil
}

func isRequiredMigrationDialect(dialect string) bool {
	for _, required := range requiredMigrationDialects {
		if dialect == required {
			return true
		}
	}
	return false
}

func validateExpandSQL(
	dialect string, statement migrationStatement, createdTables map[string]struct{},
) error {
	tokens, err := expandSQLTokens(dialect, statement.SQL)
	if err != nil {
		return err
	}
	switch statement.Operation {
	case migrationOperationCreateTable:
		if createTableName(tokens) == "" {
			return fmt.Errorf("create_table operation has no canonical table name")
		}
		for _, token := range tokens {
			switch token {
			case "AS", "DELETE", "FOREIGN", "INHERITS", "INSERT", "LIKE", "OF", "PARTITION", "REFERENCES", "SELECT", "UPDATE":
				return fmt.Errorf("create_table operation contains unproven token %s", token)
			}
		}
		return nil
	case migrationOperationCreateIndex:
		return validateCreateIndexOnNewTable(tokens, createdTables)
	case migrationOperationAddNullableColumn:
		return validateAddNullableColumn(tokens)
	default:
		return fmt.Errorf("expand SQL has unsupported operation %q", statement.Operation)
	}
}

func expandSQLTokens(dialect, sql string) ([]string, error) {
	statements, err := lexMigrationSQL(dialect, sql)
	if err != nil {
		return nil, err
	}
	if len(statements) == 0 {
		return nil, fmt.Errorf("SQL contains no executable tokens")
	}
	if len(statements) != 1 {
		return nil, fmt.Errorf("each catalog operation must contain exactly one SQL statement")
	}
	tokens := statements[0]
	for _, token := range tokens {
		switch token {
		case "DROP", "TRUNCATE":
			return nil, fmt.Errorf("expand SQL contains destructive token %s", token)
		case "CALL", "DO", "EXEC", "EXECUTE", "PREPARE":
			return nil, fmt.Errorf("expand SQL contains dynamic or procedural token %s", token)
		}
	}
	return tokens, nil
}

func createTableName(tokens []string) string {
	if !hasTokenPrefix(tokens, "CREATE", "TABLE") {
		return ""
	}
	nameIndex := 2
	if hasTokenPrefix(tokens[nameIndex:], "IF", "NOT", "EXISTS") {
		nameIndex += 3
	}
	if nameIndex >= len(tokens) || !isCanonicalSQLIdentifier(tokens[nameIndex]) {
		return ""
	}
	return tokens[nameIndex]
}

func validateCreateIndexOnNewTable(tokens []string, createdTables map[string]struct{}) error {
	if len(tokens) < 2 || tokens[0] != "CREATE" {
		return fmt.Errorf("create_index_on_new_table operation does not start with CREATE")
	}
	position := 1
	if len(tokens) > position && tokens[position] == "UNIQUE" {
		position++
	}
	if len(tokens) <= position || tokens[position] != "INDEX" {
		return fmt.Errorf("create_index_on_new_table operation does not contain CREATE INDEX SQL")
	}
	position++
	if hasTokenPrefix(tokens[position:], "IF", "NOT", "EXISTS") {
		position += 3
	}
	if len(tokens) <= position || !isCanonicalSQLIdentifier(tokens[position]) {
		return fmt.Errorf("create_index_on_new_table operation has no canonical index name")
	}
	position++
	if len(tokens) <= position+2 || tokens[position] != "ON" ||
		!isCanonicalSQLIdentifier(tokens[position+1]) {
		return fmt.Errorf("create_index_on_new_table operation has no canonical target table")
	}
	table := tokens[position+1]
	if _, exists := createdTables[table]; !exists {
		return fmt.Errorf("create_index_on_new_table targets table %s not created by this migration", table)
	}
	for _, column := range tokens[position+2:] {
		if !isCanonicalSQLIdentifier(column) || column == "INCLUDE" || column == "USING" || column == "WHERE" {
			return fmt.Errorf("create_index_on_new_table has an unproven column or clause")
		}
	}
	return nil
}

func validateAddNullableColumn(tokens []string) error {
	if !hasTokenPrefix(tokens, "ALTER", "TABLE") {
		return fmt.Errorf("add_nullable_column operation does not contain ALTER TABLE SQL")
	}
	if len(tokens) < 6 || !isCanonicalSQLIdentifier(tokens[2]) || tokens[3] != "ADD" {
		return fmt.Errorf("add_nullable_column operation must use ALTER TABLE <table> ADD")
	}
	columnIndex := 4
	if tokens[columnIndex] == "COLUMN" {
		columnIndex++
	}
	if columnIndex+1 >= len(tokens) || !isCanonicalSQLIdentifier(tokens[columnIndex]) {
		return fmt.Errorf("add_nullable_column operation has no canonical column name")
	}
	typeIndex := columnIndex + 1
	if !isAllowedNullableColumnType(tokens[typeIndex]) {
		return fmt.Errorf("add_nullable_column operation uses unproven type %s", tokens[typeIndex])
	}
	for _, token := range tokens[typeIndex+1:] {
		switch token {
		case "AUTO_INCREMENT", "AUTOINCREMENT", "CHECK", "CONSTRAINT", "DEFAULT", "FOREIGN", "GENERATED", "IDENTITY", "NOT", "PRIMARY", "REFERENCES", "UNIQUE":
			return fmt.Errorf("add_nullable_column operation contains non-additive constraint token %s", token)
		case "ALTER", "RENAME", "SET", "TYPE":
			return fmt.Errorf("add_nullable_column operation contains incompatible token %s", token)
		}
	}
	return nil
}

func isCanonicalSQLIdentifier(token string) bool {
	if token == "" || !isASCIIIdentifierStart(token[0]) {
		return false
	}
	for index := 1; index < len(token); index++ {
		if !isASCIIIdentifierPart(token[index]) {
			return false
		}
	}
	return true
}

func isASCIIIdentifierStart(value byte) bool {
	return value == '_' || value >= 'A' && value <= 'Z'
}

func isASCIIIdentifierPart(value byte) bool {
	return isASCIIIdentifierStart(value) || value >= '0' && value <= '9' || value == '$'
}

func isAllowedNullableColumnType(token string) bool {
	switch token {
	case "BIGINT", "BINARY", "BLOB", "BOOL", "BOOLEAN", "BYTEA", "CHAR", "CHARACTER",
		"DATE", "DATETIME", "DECIMAL", "DOUBLE", "INT", "INTEGER", "JSON", "JSONB",
		"LONGTEXT", "MEDIUMINT", "NUMERIC", "REAL", "SMALLINT", "TEXT", "TIME",
		"TIMESTAMP", "TIMESTAMPTZ", "TINYINT", "UUID", "VARBINARY", "VARCHAR":
		return true
	default:
		return false
	}
}

func hasTokenPrefix(tokens []string, prefix ...string) bool {
	if len(tokens) < len(prefix) {
		return false
	}
	for index, expected := range prefix {
		if tokens[index] != expected {
			return false
		}
	}
	return true
}

func lexMigrationSQL(dialect, statement string) ([][]string, error) {
	if !utf8.ValidString(statement) {
		return nil, fmt.Errorf("SQL is not valid UTF-8")
	}

	statements := make([][]string, 0, 1)
	tokens := make([]string, 0, 16)
	for index := 0; index < len(statement); {
		current := statement[index]
		switch {
		case isSQLWhitespace(current):
			index++
		case current == 0 || current < 0x20:
			return nil, fmt.Errorf("SQL contains unsupported control byte")
		case current == '-' && hasSQLPrefix(statement, index, "--"):
			index = consumeSQLLineComment(statement, index+2)
		case current == '#' && dialect == DialectMySQL:
			index = consumeSQLLineComment(statement, index+1)
		case current == '/' && hasSQLPrefix(statement, index, "/*"):
			if dialect == DialectMySQL && (hasSQLPrefix(statement, index, "/*!") ||
				hasSQLPrefix(statement, index, "/*M!") || hasSQLPrefix(statement, index, "/*m!")) {
				return nil, fmt.Errorf("MySQL executable comments cannot be safely classified")
			}
			next, err := consumeSQLBlockComment(dialect, statement, index+2)
			if err != nil {
				return nil, err
			}
			index = next
		case current == '\'':
			next, err := consumeSQLQuotedValue(statement, index, current)
			if err != nil {
				return nil, err
			}
			index = next
		case current == '"', current == '`':
			return nil, fmt.Errorf("quoted SQL identifiers cannot be safely classified")
		case current == '$' && dialect == DialectPostgres && sqlDollarQuoteDelimiter(statement[index:]) != "":
			return nil, fmt.Errorf("PostgreSQL dollar-quoted SQL cannot be safely classified")
		case current == ';':
			if len(tokens) > 0 {
				statements = append(statements, tokens)
				tokens = make([]string, 0, 16)
			}
			index++
		case isSQLTokenByte(current):
			start := index
			for index < len(statement) && isSQLTokenByte(statement[index]) {
				index++
			}
			tokens = append(tokens, strings.ToUpper(statement[start:index]))
		default:
			index++
		}
	}
	if len(tokens) > 0 {
		statements = append(statements, tokens)
	}
	return statements, nil
}

func consumeSQLLineComment(statement string, index int) int {
	for index < len(statement) && statement[index] != '\n' && statement[index] != '\r' {
		index++
	}
	return index
}

func consumeSQLBlockComment(dialect, statement string, index int) (int, error) {
	depth := 1
	for index < len(statement) {
		switch {
		case hasSQLPrefix(statement, index, "/*"):
			if dialect != DialectPostgres {
				return 0, fmt.Errorf("nested block comments cannot be safely classified for dialect %s", dialect)
			}
			depth++
			index += 2
		case hasSQLPrefix(statement, index, "*/"):
			depth--
			index += 2
			if depth == 0 {
				return index, nil
			}
		default:
			index++
		}
	}
	return 0, fmt.Errorf("SQL contains an unterminated block comment")
}

func consumeSQLQuotedValue(statement string, index int, quote byte) (int, error) {
	for index++; index < len(statement); index++ {
		switch statement[index] {
		case '\\':
			return 0, fmt.Errorf("backslash escaping in quoted SQL cannot be safely classified")
		case quote:
			if index+1 < len(statement) && statement[index+1] == quote {
				index++
				continue
			}
			return index + 1, nil
		case 0:
			return 0, fmt.Errorf("SQL contains a NUL byte")
		}
	}
	return 0, fmt.Errorf("SQL contains an unterminated quoted value")
}

func sqlDollarQuoteDelimiter(statement string) string {
	if len(statement) < 2 || statement[0] != '$' {
		return ""
	}
	for index := 1; index < len(statement); index++ {
		switch current := statement[index]; {
		case current == '$':
			return statement[:index+1]
		case current == '_' || current >= 'a' && current <= 'z' || current >= 'A' && current <= 'Z':
		case index > 1 && current >= '0' && current <= '9':
		default:
			return ""
		}
	}
	return ""
}

func hasSQLPrefix(statement string, index int, prefix string) bool {
	return index+len(prefix) <= len(statement) && statement[index:index+len(prefix)] == prefix
}

func isSQLWhitespace(value byte) bool {
	switch value {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	default:
		return false
	}
}

func isSQLTokenByte(value byte) bool {
	return value == '_' || value == '$' || value >= '0' && value <= '9' ||
		value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= utf8.RuneSelf
}

func unsafeMigrationCatalog(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", ErrUnsafeMigration, fmt.Sprintf(format, arguments...))
}
