package kkaimigrate

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestObserveReportsValidatedAppliedSchemaVersion(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := Apply(context.Background(), db, Options{})
	require.NoError(t, err)

	observed, err := Observe(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, MigrationTargetVersion, observed.CurrentVersion)
	require.Equal(t, SchemaCompatibility("0123456789abcdef0123456789abcdef01234567").MigrationSetDigest, observed.MigrationSetDigest)
}

func TestObserveRejectsMigrationHistoryGap(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := Apply(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, db.Where("version = ?", LedgerSchemaVersion).Delete(&AppliedMigration{}).Error)

	_, err = Observe(context.Background(), db)
	require.ErrorIs(t, err, ErrSchemaNotReady)
}

func TestSchemaContractIsCanonicalAndBoundToMigrationPlan(t *testing.T) {
	contract := SchemaCompatibility("0123456789abcdef0123456789abcdef01234567")
	require.EqualValues(t, 1, contract.Schema)
	require.Equal(t, RuntimeMinVersion, contract.RuntimeMinVersion)
	require.Equal(t, RuntimeMaxVersion, contract.RuntimeMaxVersion)
	require.Equal(t, MigrationTargetVersion, contract.MigrationTargetVersion)
	require.Equal(t, MigrationKindNone, contract.MigrationKind)
	require.Regexp(t, "^sha256:[0-9a-f]{64}$", contract.MigrationSetDigest)

	encoded, err := contract.CanonicalJSON()
	require.NoError(t, err)
	require.JSONEq(t, `{"schema":1,"source_revision":"0123456789abcdef0123456789abcdef01234567","runtime_min_version":4,"runtime_max_version":4,"migration_target_version":4,"migration_kind":"none","migration_set_digest":"`+contract.MigrationSetDigest+`"}`, string(encoded))

	var decoded SchemaContract
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Equal(t, contract, decoded)
}

func TestSchemaContractUsesRuntimeMaxMigrationPrefixAndValidatesKinds(t *testing.T) {
	digest := "sha256:" + migrationSetSHA256(contractPlanThroughVersion(RuntimeMaxVersion))
	require.Equal(t, digest, SchemaCompatibility("0123456789abcdef0123456789abcdef01234567").MigrationSetDigest)

	base := SchemaContract{
		Schema:             1,
		SourceRevision:     "0123456789abcdef0123456789abcdef01234567",
		RuntimeMinVersion:  3,
		RuntimeMaxVersion:  4,
		MigrationSetDigest: digest,
	}

	none := base
	none.MigrationTargetVersion = 3
	none.MigrationKind = MigrationKindNone
	_, err := none.CanonicalJSON()
	require.NoError(t, err)

	expand := base
	expand.MigrationTargetVersion = 4
	expand.MigrationKind = MigrationKindExpand
	_, err = expand.CanonicalJSON()
	require.NoError(t, err)

	invalidNone := none
	invalidNone.MigrationTargetVersion = 4
	_, err = invalidNone.CanonicalJSON()
	require.Error(t, err)

	invalidExpand := expand
	invalidExpand.MigrationTargetVersion = 3
	_, err = invalidExpand.CanonicalJSON()
	require.Error(t, err)
}

func TestMigrationContractChecksumBindsKindImplementationAndOperation(t *testing.T) {
	newMigration := func() migration {
		return migration{
			Version:          1,
			Name:             "one",
			Kind:             MigrationKindExpand,
			ImplementationID: "one_v1",
			ChecksumVersion:  migrationChecksumSchemaCurrent,
			Statements:       completeDialectStatements("CREATE TABLE one (id BIGINT)"),
		}
	}
	baseline := newMigration()
	baselineChecksum := migrationContractChecksum(baseline)

	kindChanged := newMigration()
	kindChanged.Kind = MigrationKindContract
	require.NotEqual(t, baselineChecksum, migrationContractChecksum(kindChanged))

	implementationChanged := newMigration()
	implementationChanged.ImplementationID = "one_v2"
	require.NotEqual(t, baselineChecksum, migrationContractChecksum(implementationChanged))

	operationChanged := newMigration()
	operationChanged.Statements[DialectPostgres][0].Operation = migrationOperationAddNullableColumn
	require.NotEqual(t, baselineChecksum, migrationContractChecksum(operationChanged))
}

func TestMigrationSetSHA256ChangesWithMigrationIdentity(t *testing.T) {
	checksum := migrationSetSHA256([]AppliedMigration{{Version: 1, Name: "one", Checksum: "a"}})
	require.Equal(t, "f24c20c9507a00bb45e1ba42baaead692a0b3d7dc8977cb21ac7c6b4903a76d9", checksum)
	require.NotEqual(t, checksum, migrationSetSHA256([]AppliedMigration{{Version: 1, Name: "two", Checksum: "a"}}))
	require.NotEqual(t, checksum, migrationSetSHA256([]AppliedMigration{{Version: 1, Name: "one", Checksum: "b"}}))
}

func TestMigrationCatalogRejectsUnclassifiedAndDestructiveExpandDDL(t *testing.T) {
	testMigration := func(name, kind, statement string) migration {
		return migration{
			Version:          1,
			Name:             name,
			Kind:             kind,
			ImplementationID: name + "_v1",
			ChecksumVersion:  migrationChecksumSchemaCurrent,
			Statements:       completeDialectStatements(statement),
		}
	}
	tests := []migration{
		testMigration("unclassified", "", "CREATE TABLE safe_table (id BIGINT)"),
		testMigration("drop_column", MigrationKindExpand, "ALTER TABLE users DROP COLUMN legacy"),
		testMigration("truncate", MigrationKindExpand, "TRUNCATE TABLE users"),
		testMigration("drop_table", MigrationKindExpand, "DROP TABLE users"),
		testMigration("commented_drop", MigrationKindExpand, "-- additive change\nDROP TABLE users"),
		testMigration("second_statement_drop", MigrationKindExpand, "CREATE TABLE safe_table (id BIGINT); /* cleanup */ DROP TABLE users"),
		testMigration("commented_alter_drop", MigrationKindExpand, "ALTER /* online */ TABLE users\nDROP COLUMN legacy"),
	}
	for _, item := range tests {
		t.Run(item.Name, func(t *testing.T) {
			require.ErrorIs(t, validateMigrationCatalog([]migration{item}), ErrUnsafeMigration)
		})
	}
}

func TestMigrationCatalogAllowsDestructiveKeywordsOnlyInQuotedOrCommentedText(t *testing.T) {
	migrations := []migration{{
		Version:          1,
		Name:             "quoted_keywords",
		Kind:             MigrationKindExpand,
		ImplementationID: "quoted_keywords_v1",
		ChecksumVersion:  migrationChecksumSchemaCurrent,
		Statements: completeDialectStatements(
			`CREATE TABLE quoted_keywords (message TEXT DEFAULT 'TRUNCATE TABLE users')`,
			"/* DROP TABLE users */ CREATE TABLE safe_table (id BIGINT)",
		),
	}}
	require.NoError(t, validateMigrationCatalog(migrations))
}

func TestMigrationCatalogAllowsMultipleAdditiveStatements(t *testing.T) {
	migrations := []migration{{
		Version:          1,
		Name:             "multiple_additive_statements",
		Kind:             MigrationKindExpand,
		ImplementationID: "multiple_additive_statements_v1",
		ChecksumVersion:  migrationChecksumSchemaCurrent,
		Statements: completeDialectOperations(
			migrationStatement{Operation: migrationOperationCreateTable, SQL: "CREATE TABLE safe_table (id BIGINT)"},
			migrationStatement{Operation: migrationOperationAddNullableColumn, SQL: "ALTER TABLE safe_table ADD COLUMN created_at BIGINT"},
		),
	}}
	require.NoError(t, validateMigrationCatalog(migrations))
}

func TestMigrationCatalogAllowsExplicitIndexOnlyOnTableCreatedBySameExpand(t *testing.T) {
	statements := completeDialectOperations(
		migrationStatement{Operation: migrationOperationCreateTable, SQL: "CREATE TABLE safe_table (id BIGINT)"},
		migrationStatement{Operation: migrationOperationCreateIndex, SQL: "CREATE INDEX idx_safe_table_id ON safe_table (id)"},
	)
	migrations := []migration{{
		Version: 1, Name: "table_with_index", Kind: MigrationKindExpand,
		ImplementationID: "table_with_index_v1", ChecksumVersion: migrationChecksumSchemaCurrent,
		Statements: statements,
	}}
	require.NoError(t, validateMigrationCatalog(migrations))

	migrations[0].Statements = completeDialectOperations(
		migrationStatement{Operation: migrationOperationCreateTable, SQL: "CREATE TABLE safe_table (id BIGINT)"},
		migrationStatement{Operation: migrationOperationCreateIndex, SQL: "CREATE INDEX idx_users_id ON users (id)"},
	)
	require.ErrorIs(t, validateMigrationCatalog(migrations), ErrUnsafeMigration)

	migrations[0].Statements = completeDialectOperations(
		migrationStatement{Operation: migrationOperationCreateIndex, SQL: "CREATE INDEX idx_safe_table_id ON safe_table (id)"},
		migrationStatement{Operation: migrationOperationCreateTable, SQL: "CREATE TABLE safe_table (id BIGINT)"},
	)
	require.ErrorIs(t, validateMigrationCatalog(migrations), ErrUnsafeMigration)
}

func TestMigrationCatalogRejectsQuotedIdentifierNewTableIndexBypass(t *testing.T) {
	migrations := []migration{{
		Version: 1, Name: "quoted_identifier_bypass", Kind: MigrationKindExpand,
		ImplementationID: "quoted_identifier_bypass_v1", ChecksumVersion: migrationChecksumSchemaCurrent,
		Statements: completeDialectOperations(
			migrationStatement{Operation: migrationOperationCreateTable, SQL: `CREATE TABLE "new_table" (users BIGINT)`},
			migrationStatement{Operation: migrationOperationCreateIndex, SQL: "CREATE INDEX idx_users_id ON users (id)"},
		),
	}}
	require.ErrorIs(t, validateMigrationCatalog(migrations), ErrUnsafeMigration)
}

func TestMigrationCatalogRejectsOutOfBandOperationsInCurrentChecksumSchema(t *testing.T) {
	base := migration{
		Version: 1, Name: "safe_table", Kind: MigrationKindExpand,
		ImplementationID: "safe_table_v1", ChecksumVersion: migrationChecksumSchemaCurrent,
		Statements: completeDialectStatements("CREATE TABLE safe_table (id BIGINT)"),
	}
	withIndex := base
	withIndex.Indexes = []indexSpec{{Name: "idx_safe_table_id", Table: "safe_table", Columns: []string{"id"}}}
	require.ErrorIs(t, validateMigrationCatalog([]migration{withIndex}), ErrUnsafeMigration)

	withCallback := base
	withCallback.LegacyImportID = "unclassified_callback_v1"
	withCallback.LegacyImportSpec = "unclassified callback"
	withCallback.ImportLegacy = func(*gorm.DB) error { return nil }
	require.ErrorIs(t, validateMigrationCatalog([]migration{withCallback}), ErrUnsafeMigration)
}

func TestMigrationCatalogRejectsUnprovenExpandOperations(t *testing.T) {
	tests := []migrationStatement{
		{Operation: migrationOperationAddNullableColumn, SQL: "ALTER TABLE users ALTER COLUMN quota TYPE INTEGER"},
		{Operation: migrationOperationAddNullableColumn, SQL: "ALTER TABLE users RENAME COLUMN quota TO quota_v2"},
		{Operation: migrationOperationAddNullableColumn, SQL: "ALTER TABLE users ALTER COLUMN quota SET NOT NULL"},
		{Operation: migrationOperationAddNullableColumn, SQL: "ALTER TABLE users ADD COLUMN quota INTEGER NOT NULL"},
		{Operation: migrationOperationAddNullableColumn, SQL: "ALTER TABLE users ADD COLUMN quota INTEGER DEFAULT 0"},
		{Operation: migrationOperationAddNullableColumn, SQL: "ALTER TABLE users ADD COLUMN id BIGSERIAL"},
		{Operation: migrationOperationAddNullableColumn, SQL: "ALTER TABLE users ADD COLUMN quota quota_domain"},
		{Operation: migrationOperationCreateTable, SQL: "CREATE INDEX idx_users_quota ON users (quota)"},
		{Operation: migrationOperationCreateTable, SQL: "CREATE TABLE child PARTITION OF users FOR VALUES IN (1)"},
		{Operation: migrationOperationCreateTable, SQL: "CREATE TABLE child (id BIGINT) INHERITS (users)"},
		{Operation: migrationOperationCreateIndex, SQL: "ALTER INDEX idx_users_id ON users (id)"},
		{Operation: "custom_expand", SQL: "ALTER TABLE users ADD COLUMN quota INTEGER"},
	}
	for _, statement := range tests {
		t.Run(statement.Operation+"/"+statement.SQL, func(t *testing.T) {
			migrations := []migration{{
				Version: 1, Name: "unproven_expand", Kind: MigrationKindExpand,
				ImplementationID: "unproven_expand_v1", ChecksumVersion: migrationChecksumSchemaCurrent,
				Statements: completeDialectOperations(statement),
			}}
			require.ErrorIs(t, validateMigrationCatalog(migrations), ErrUnsafeMigration)
		})
	}
}

func TestMigrationCatalogRejectsSQLThatCannotBeReliablyTokenized(t *testing.T) {
	for name, statement := range map[string]string{
		"unterminated block comment": "CREATE TABLE safe_table (id BIGINT); /*",
		"unterminated quoted text":   "CREATE TABLE safe_table (value TEXT DEFAULT 'unsafe)",
		"dollar quoted body":         "CREATE FUNCTION f() RETURNS void AS $body$ BEGIN NULL; END $body$",
		"ambiguous quote escape":     `CREATE TABLE safe_table (value TEXT DEFAULT 'a\b')`,
		"nul byte":                   "CREATE TABLE safe_table (id BIGINT)\x00",
	} {
		t.Run(name, func(t *testing.T) {
			migrations := []migration{{
				Version: 1, Name: name, Kind: MigrationKindExpand,
				ImplementationID: name + "_v1", ChecksumVersion: migrationChecksumSchemaCurrent,
				Statements: completeDialectStatements(statement),
			}}
			require.ErrorIs(t, validateMigrationCatalog(migrations), ErrUnsafeMigration)
		})
	}
}

func TestMigrationCatalogRejectsExecutableCommentsAndDynamicSQL(t *testing.T) {
	tests := map[string]map[string][]migrationStatement{
		"mysql executable comment": {
			DialectSQLite:   {{Operation: migrationOperationCreateTable, SQL: "CREATE TABLE safe_table (id BIGINT)"}},
			DialectMySQL:    {{Operation: migrationOperationCreateTable, SQL: "/*! DROP TABLE users */"}},
			DialectPostgres: {{Operation: migrationOperationCreateTable, SQL: "CREATE TABLE safe_table (id BIGINT)"}},
		},
		"mysql nested comment": {
			DialectSQLite:   {{Operation: migrationOperationCreateTable, SQL: "CREATE TABLE safe_table (id BIGINT)"}},
			DialectMySQL:    {{Operation: migrationOperationCreateTable, SQL: "/* outer /* inner */ DROP TABLE users */ SELECT 1"}},
			DialectPostgres: {{Operation: migrationOperationCreateTable, SQL: "CREATE TABLE safe_table (id BIGINT)"}},
		},
		"dynamic execute": completeDialectStatements("EXECUTE 'DROP TABLE users'"),
		"prepared SQL":    completeDialectStatements("PREPARE destructive FROM 'DROP TABLE users'"),
	}
	for name, statements := range tests {
		t.Run(name, func(t *testing.T) {
			migrations := []migration{{
				Version: 1, Name: name, Kind: MigrationKindExpand, Statements: statements,
				ImplementationID: name + "_v1", ChecksumVersion: migrationChecksumSchemaCurrent,
			}}
			require.ErrorIs(t, validateMigrationCatalog(migrations), ErrUnsafeMigration)
		})
	}
}

func TestMigrationCatalogRequiresCanonicalIdentityAndCompleteDialects(t *testing.T) {
	valid := func(version int64, name string) migration {
		return migration{
			Version:          version,
			Name:             name,
			Kind:             MigrationKindExpand,
			ImplementationID: name + "_v1",
			ChecksumVersion:  migrationChecksumSchemaCurrent,
			Statements: completeDialectStatements(
				"CREATE TABLE safe_table (id BIGINT)",
			),
		}
	}

	tests := map[string][]migration{
		"empty catalog":       {},
		"wrong baseline":      {valid(2, "two")},
		"version gap":         {valid(1, "one"), valid(3, "three")},
		"duplicate version":   {valid(1, "one"), valid(1, "two")},
		"empty name":          {valid(1, " ")},
		"duplicate name":      {valid(1, "same"), valid(2, "same")},
		"missing dialect":     {valid(1, "one")},
		"unsupported dialect": {valid(1, "one")},
		"blank statement":     {valid(1, "one")},
	}
	delete(tests["missing dialect"][0].Statements, DialectSQLite)
	tests["unsupported dialect"][0].Statements["oracle"] = []migrationStatement{{Operation: migrationOperationCreateTable, SQL: "CREATE TABLE safe_table (id BIGINT)"}}
	tests["blank statement"][0].Statements[DialectPostgres] = []migrationStatement{{Operation: migrationOperationCreateTable, SQL: "  "}}

	for name, migrations := range tests {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, validateMigrationCatalog(migrations), ErrUnsafeMigration)
		})
	}

	require.NoError(t, validateMigrationCatalog([]migration{valid(1, "one"), valid(2, "two")}))
}

func TestMigrationCatalogClassifiesExplicitContract(t *testing.T) {
	kind, err := migrationKindForRange(1, 2, []migration{
		{
			Version: 1, Name: "baseline", Kind: MigrationKindExpand,
			ImplementationID: "baseline_v1", ChecksumVersion: migrationChecksumSchemaCurrent,
			Statements: completeDialectStatements("CREATE TABLE baseline (id BIGINT)"),
		},
		{
			Version: 2, Name: "contract", Kind: MigrationKindContract,
			ImplementationID: "contract_v1", ChecksumVersion: migrationChecksumSchemaCurrent,
			Statements: completeDialectOperations(
				migrationStatement{Operation: migrationOperationContract, SQL: "ALTER TABLE baseline ALTER COLUMN id TYPE INTEGER"},
			),
		},
	})
	require.NoError(t, err)
	require.Equal(t, MigrationKindContract, kind)
}

func TestExistingNarrowingMigrationIsClassifiedAsContract(t *testing.T) {
	kind, err := migrationKindForRange(JobLeaseSchemaVersion, OutboxEventKeySchemaVersion, migrationSet())
	require.NoError(t, err)
	require.Equal(t, MigrationKindContract, kind)
}

func completeDialectStatements(statements ...string) map[string][]migrationStatement {
	operations := make([]migrationStatement, 0, len(statements))
	for _, statement := range statements {
		operations = append(operations, migrationStatement{Operation: migrationOperationCreateTable, SQL: statement})
	}
	return completeDialectOperations(operations...)
}

func completeDialectOperations(statements ...migrationStatement) map[string][]migrationStatement {
	return map[string][]migrationStatement{
		DialectSQLite:   append([]migrationStatement(nil), statements...),
		DialectMySQL:    append([]migrationStatement(nil), statements...),
		DialectPostgres: append([]migrationStatement(nil), statements...),
	}
}
