package kkaimigrate

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
)

// v13 replaces the never-applied v12 on pristine v11 databases. The v12
// definition remains frozen so its historical checksum and observation survive.
type tenantV13User struct {
	ID                 int64   `gorm:"primaryKey"`
	TenantID           int64   `gorm:"type:bigint;not null;default:0;uniqueIndex:idx_users_tenant_username,priority:1;index:idx_users_tenant_email,priority:1;uniqueIndex:idx_users_tenant_normalized_username,priority:1;uniqueIndex:idx_users_tenant_normalized_email,priority:1"`
	Username           string  `gorm:"uniqueIndex:idx_users_tenant_username,priority:2"`
	Email              string  `gorm:"index:idx_users_tenant_email,priority:2"`
	NormalizedUsername *string `gorm:"type:varchar(20);uniqueIndex:idx_users_tenant_normalized_username,priority:2"`
	NormalizedEmail    *string `gorm:"type:varchar(50);uniqueIndex:idx_users_tenant_normalized_email,priority:2"`
}

func (tenantV13User) TableName() string { return "users" }

func tenantCompatibilitySchemaSpec() string {
	var b strings.Builder
	b.WriteString("v13: replaces never-applied v12 only from pristine v11; ledger path 1..11,13; preserve all original usernames, emails, account IDs and balances including deleted users; platform canonical keys NULL and scoped raw username unique under existing database collation; tenant>0 canonical lower(trim) unique; reject applied or partial v12; no cash backfill\n")
	b.WriteString("global username uniqueness: inspect database metadata by single raw username column, never by fixed name; install scoped unique first, then remove all matching PostgreSQL unique constraints/standalone indexes and MySQL unique indexes; preserve nonunique and composite indexes; SQLite supports standalone indexes and named table constraints through GORM rebuild preserving unrelated indexes/triggers; reject unnamed/unsupported SQLite uniqueness before DDL; runtime rejects remaining global username unique keys\n")
	// Every unchanged table/snapshot shape stays bound to its frozen v12 spec.
	b.WriteString(tenantSchemaSpec())
	typ := reflect.TypeOf(tenantV13User{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fmt.Fprintf(&b, "%s:%s:%s\n", field.Name, field.Type, field.Tag.Get("gorm"))
	}
	return b.String()
}

func CheckTenantCompatibilitySchema(ctx context.Context, db *gorm.DB) error {
	return checkThroughVersion(ctx, db, TenantCompatibilitySchemaVersion, TenantCompatibilitySchemaVersion, TenantCompatibilitySchemaVersion)
}

func ApplyTenantCompatibilitySchema(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	observation, err := Observe(ctx, db)
	if err != nil {
		return nil, err
	}
	if observation.CurrentVersion == TenantCompatibilitySchemaVersion {
		if err := CheckTenantCompatibilitySchema(ctx, db); err != nil {
			return nil, err
		}
	} else {
		report, err := PrecheckTenantCompatibilitySchema(ctx, db)
		if err != nil {
			return nil, err
		}
		if !report.Safe {
			return nil, fmt.Errorf("%w: incompatible legacy identities; operator repair required", ErrSchemaNotReady)
		}
	}
	return applyThroughVersion(ctx, db, options, TenantCompatibilitySchemaVersion, TenantCompatibilitySchemaVersion)
}

type TenantCompatibilityPrecheck struct {
	Schema                                string `json:"schema"`
	TargetVersion                         int64  `json:"target_version"`
	UserCount                             int64  `json:"user_count"`
	InvalidIdentityCount                  int64  `json:"invalid_identity_count"`
	DuplicateUsernameCount                int64  `json:"duplicate_username_count"`
	DuplicateEmailCount                   int64  `json:"duplicate_email_count"`
	LegacyNormalizedUsernameConflictCount int64  `json:"legacy_normalized_username_conflict_count"`
	LegacyNormalizedEmailConflictCount    int64  `json:"legacy_normalized_email_conflict_count"`
	Safe                                  bool   `json:"safe"`
}

func requirePristineTenantCompatibilitySource(ctx context.Context, db *gorm.DB) error {
	observation, err := Observe(ctx, db)
	if err != nil {
		return err
	}
	if observation.CurrentVersion != MembershipSchemaVersion {
		return fmt.Errorf("%w: v13 requires pristine schema 11, not applied schema %d", ErrSchemaNotReady, observation.CurrentVersion)
	}
	if err := precheckTenantSchema(db); err != nil {
		return err
	}
	for _, value := range tenantV12SnapshotModels() {
		if db.Migrator().HasColumn(value, "tenant_id") {
			return fmt.Errorf("%w: partial tenant schema is not a pristine v11 source", ErrSchemaNotReady)
		}
	}
	for _, column := range []struct{ table, name string }{
		{"users", "normalized_username"}, {"users", "normalized_email"},
		{"top_ups", "paid_minor"}, {"top_ups", "currency"},
	} {
		if db.Migrator().HasColumn(column.table, column.name) {
			return fmt.Errorf("%w: partial tenant schema is not a pristine v11 source", ErrSchemaNotReady)
		}
	}
	for _, value := range append(tenantV12NewModels(), &tenantLogSchemaRecord{}) {
		if db.Migrator().HasTable(value) {
			return fmt.Errorf("%w: partial tenant tables are not a pristine v11 source", ErrSchemaNotReady)
		}
	}
	return nil
}

// PrecheckTenantCompatibilitySchema reports legacy canonical collisions without
// changing or rejecting those identities. Raw username uniqueness is checked
// by the database so its original collation remains authoritative.
func PrecheckTenantCompatibilitySchema(ctx context.Context, db *gorm.DB) (*TenantCompatibilityPrecheck, error) {
	if db == nil {
		return nil, ErrSchemaNotReady
	}
	query := db.WithContext(ctx)
	if err := requirePristineTenantCompatibilitySource(ctx, query); err != nil {
		return nil, err
	}
	if _, err := tenantGlobalUsernameUniqueKeys(query); err != nil {
		return nil, err
	}
	report := &TenantCompatibilityPrecheck{Schema: "kkai_tenant_compatibility_precheck_v1", TargetVersion: TenantCompatibilitySchemaVersion}
	usernames, emails := map[string]int{}, map[string]int{}
	var rows []tenantV12User
	if err := query.Select("id", "username", "email").FindInBatches(&rows, 1000, func(_ *gorm.DB, _ int) error {
		for _, row := range rows {
			report.UserCount++
			if strings.TrimSpace(row.Username) == "" || utf8.RuneCountInString(row.Username) > 20 || utf8.RuneCountInString(row.Email) > 50 {
				report.InvalidIdentityCount++
			}
			username, email := strings.ToLower(strings.TrimSpace(row.Username)), strings.ToLower(strings.TrimSpace(row.Email))
			usernames[username]++
			if usernames[username] == 2 {
				report.LegacyNormalizedUsernameConflictCount++
			}
			if email != "" {
				emails[email]++
				if emails[email] == 2 {
					report.LegacyNormalizedEmailConflictCount++
				}
			}
		}
		return nil
	}).Error; err != nil {
		return nil, err
	}
	var duplicates []struct{ Count int64 }
	if err := query.Model(&tenantV12User{}).Select("COUNT(*) AS count").Group("username").Having("COUNT(*) > 1").Scan(&duplicates).Error; err != nil {
		return nil, err
	}
	report.DuplicateUsernameCount = int64(len(duplicates))
	report.Safe = report.InvalidIdentityCount == 0 && report.DuplicateUsernameCount == 0
	return report, nil
}

func applyTenantCompatibilitySchema(db *gorm.DB) error {
	// Recheck under the migration lock, before the first DDL operation.
	report, err := PrecheckTenantCompatibilitySchema(db.Statement.Context, db)
	if err != nil {
		return err
	}
	if !report.Safe {
		return fmt.Errorf("%w: incompatible legacy identities; operator repair required", ErrSchemaNotReady)
	}
	legacyKeys, err := tenantGlobalUsernameUniqueKeys(db)
	if err != nil {
		return err
	}
	for _, value := range tenantV12SnapshotModels() {
		if err := db.Migrator().AddColumn(value, "TenantID"); err != nil {
			return err
		}
	}
	for _, entry := range []struct {
		value  any
		fields []string
	}{
		{&tenantV13User{}, []string{"NormalizedUsername", "NormalizedEmail"}},
		{&tenantV12TopUp{}, []string{"PaidMinor", "Currency"}},
	} {
		for _, field := range entry.fields {
			if err := db.Migrator().AddColumn(entry.value, field); err != nil {
				return err
			}
		}
	}
	for _, name := range []string{"idx_users_tenant_username", "idx_users_tenant_email", "idx_users_tenant_normalized_username", "idx_users_tenant_normalized_email"} {
		if err := db.Migrator().CreateIndex(&tenantV13User{}, name); err != nil {
			return err
		}
	}
	// Drop standalone global indexes before capturing the SQLite schema, so a
	// subsequent constraint rebuild cannot accidentally recreate their uniqueness.
	for _, key := range legacyKeys {
		if !key.Constraint {
			if err := db.Migrator().DropIndex("users", key.Name); err != nil {
				return err
			}
		}
	}
	var schemaDDL []string
	if db.Dialector.Name() == DialectSQLite {
		if err := db.Raw("SELECT sql FROM sqlite_master WHERE type IN ('index', 'trigger') AND tbl_name = 'users' AND sql IS NOT NULL").Scan(&schemaDDL).Error; err != nil {
			return err
		}
	}
	rebuiltSQLite := false
	for _, key := range legacyKeys {
		if key.Constraint {
			if err := db.Migrator().DropConstraint("users", key.Name); err != nil {
				return err
			}
			rebuiltSQLite = db.Dialector.Name() == DialectSQLite
		}
	}
	if rebuiltSQLite {
		for _, ddl := range schemaDDL {
			if err := db.Exec(ddl).Error; err != nil {
				return err
			}
		}
	}
	for _, value := range tenantV12NewModels() {
		if err := db.Migrator().CreateTable(value); err != nil {
			return err
		}
	}
	for _, value := range tenantV12SnapshotModels() {
		if !db.Migrator().HasIndex(value, "TenantID") {
			if err := db.Migrator().CreateIndex(value, "TenantID"); err != nil {
				return err
			}
		}
	}
	return validateTenantCompatibilityRuntimeSchema(db)
}

func validateTenantCompatibilityRuntimeSchema(db *gorm.DB) error {
	for _, value := range append(tenantV12NewModels(), &tenantV13User{}, &tenantV12TopUp{}) {
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(value); err != nil {
			return err
		}
		if !db.Migrator().HasTable(value) {
			return fmt.Errorf("%w: missing tenant table %s", ErrSchemaNotReady, statement.Schema.Table)
		}
		columns, err := db.Migrator().ColumnTypes(value)
		if err != nil {
			return err
		}
		actual := map[string]gorm.ColumnType{}
		for _, column := range columns {
			actual[column.Name()] = column
		}
		for _, field := range statement.Schema.Fields {
			column, ok := actual[field.DBName]
			if !ok {
				return fmt.Errorf("%w: missing tenant column %s.%s", ErrSchemaNotReady, statement.Schema.Table, field.DBName)
			}
			if field.IndirectFieldType.Kind() == reflect.Int64 && field.Name != "ID" {
				if err := validateTenantIntegerColumn(db, statement.Schema.Table, field.DBName, column, field.NotNull); err != nil {
					return err
				}
			}
		}
		for _, index := range statement.Schema.ParseIndexes() {
			if index.Class != "UNIQUE" {
				continue
			}
			requirement := runtimeIndexRequirement{Table: statement.Schema.Table, Name: index.Name}
			for _, field := range index.Fields {
				requirement.Columns = append(requirement.Columns, field.DBName)
			}
			if err := validateRuntimeUniqueIndex(db, db.Dialector.Name(), requirement); err != nil {
				return err
			}
		}
	}
	for _, table := range tenantV12SnapshotTables {
		if err := validateTenantSnapshot(db, table); err != nil {
			return err
		}
	}
	legacyKeys, err := tenantGlobalUsernameUniqueKeys(db)
	if err != nil {
		return err
	}
	if len(legacyKeys) != 0 {
		return fmt.Errorf("%w: legacy global username uniqueness is still installed", ErrSchemaNotReady)
	}
	var rows []tenantV13User
	return db.Select("id", "tenant_id", "username", "email", "normalized_username", "normalized_email").FindInBatches(&rows, 1000, func(_ *gorm.DB, _ int) error {
		for _, row := range rows {
			if row.TenantID == 0 {
				if row.NormalizedUsername != nil || row.NormalizedEmail != nil {
					return fmt.Errorf("%w: platform canonical identity must remain NULL", ErrSchemaNotReady)
				}
				continue
			}
			expected := tenantV12User{ID: row.ID, TenantID: row.TenantID, Username: row.Username, Email: row.Email}
			if err := normalizeTenantMigrationIdentity(&expected); err != nil {
				return err
			}
			if !reflect.DeepEqual(row.NormalizedUsername, expected.NormalizedUsername) || !reflect.DeepEqual(row.NormalizedEmail, expected.NormalizedEmail) {
				return fmt.Errorf("%w: tenant canonical identity is incomplete or inconsistent", ErrSchemaNotReady)
			}
		}
		return nil
	}).Error
}

type tenantUsernameUniqueKey struct {
	Name       string
	Constraint bool
}

// The legacy global key may have a PostgreSQL-generated name, a GORM name,
// or a standalone index name. Its column shape is the migration boundary.
func tenantGlobalUsernameUniqueKeys(db *gorm.DB) ([]tenantUsernameUniqueKey, error) {
	var keys []tenantUsernameUniqueKey
	switch db.Dialector.Name() {
	case DialectPostgres:
		var rows []struct {
			Name           string
			ConstraintName string
			Primary        bool
		}
		if err := db.Raw(`SELECT idx.relname AS name, COALESCE(c.conname, '') AS constraint_name, i.indisprimary AS primary
FROM pg_catalog.pg_index i
JOIN pg_catalog.pg_class idx ON idx.oid = i.indexrelid
JOIN pg_catalog.pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = i.indkey[0]
LEFT JOIN pg_catalog.pg_constraint c ON c.conindid = i.indexrelid AND c.contype IN ('u', 'p')
WHERE i.indrelid = pg_catalog.to_regclass('users') AND i.indisunique
 AND i.indnatts = 1 AND i.indexprs IS NULL AND a.attname = 'username'
ORDER BY idx.relname`).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row.Primary {
				return nil, fmt.Errorf("%w: username primary key requires operator repair", ErrSchemaNotReady)
			}
			key := tenantUsernameUniqueKey{Name: row.Name}
			if row.ConstraintName != "" {
				key.Name, key.Constraint = row.ConstraintName, true
			}
			keys = append(keys, key)
		}
	case DialectMySQL:
		indexes, err := db.Migrator().GetIndexes("users")
		if err != nil {
			return nil, err
		}
		for _, index := range indexes {
			unique, known := index.Unique()
			if !known || !unique || !equalRuntimeIndexColumns(index.Columns(), []string{"username"}) {
				continue
			}
			if primary, _ := index.PrimaryKey(); primary {
				return nil, fmt.Errorf("%w: username primary key requires operator repair", ErrSchemaNotReady)
			}
			keys = append(keys, tenantUsernameUniqueKey{Name: index.Name()})
		}
	case DialectSQLite:
		var indexes []struct{ Name, Origin string }
		if err := db.Raw(`SELECT name, origin FROM pragma_index_list('users') WHERE "unique" = 1`).Scan(&indexes).Error; err != nil {
			return nil, err
		}
		constraintFound := false
		for _, index := range indexes {
			var columns []string
			if err := db.Raw("SELECT name FROM pragma_index_info(?) ORDER BY seqno", index.Name).Scan(&columns).Error; err != nil {
				return nil, err
			}
			if !equalRuntimeIndexColumns(columns, []string{"username"}) {
				continue
			}
			switch index.Origin {
			case "c":
				keys = append(keys, tenantUsernameUniqueKey{Name: index.Name})
			case "u":
				constraintFound = true
			default:
				return nil, fmt.Errorf("%w: unsupported SQLite username unique key requires operator repair", ErrSchemaNotReady)
			}
		}
		if constraintFound {
			var ddl string
			if err := db.Raw("SELECT sql FROM sqlite_master WHERE type='table' AND name='users'").Scan(&ddl).Error; err != nil {
				return nil, err
			}
			// GORM can safely remove named table constraints. Inline or unnamed
			// UNIQUE needs explicit operator repair instead of a custom DDL rewrite.
			unnamed := regexp.MustCompile("(?i)(?:^|,)\\s*UNIQUE\\s*\\(\\s*[\"`]?username[\"`]?\\s*\\)")
			inline := regexp.MustCompile("(?i)(?:^|[,\\(])\\s*[\"`]?username[\"`]?\\s+[^,]*\\bUNIQUE\\b")
			if unnamed.MatchString(ddl) || inline.MatchString(ddl) {
				return nil, fmt.Errorf("%w: unnamed SQLite username uniqueness requires operator repair before migration", ErrSchemaNotReady)
			}
			pattern := regexp.MustCompile("(?i)CONSTRAINT\\s+[\"`]?([a-zA-Z_][a-zA-Z0-9_]*)[\"`]?\\s+UNIQUE\\s*\\(\\s*[\"`]?username[\"`]?\\s*\\)")
			matches := pattern.FindAllStringSubmatch(ddl, -1)
			if len(matches) == 0 {
				return nil, fmt.Errorf("%w: unnamed SQLite username uniqueness requires operator repair before migration", ErrSchemaNotReady)
			}
			for _, match := range matches {
				keys = append(keys, tenantUsernameUniqueKey{Name: match[1], Constraint: true})
			}
		}
	default:
		return nil, ErrUnsupportedDialect
	}
	return keys, nil
}
