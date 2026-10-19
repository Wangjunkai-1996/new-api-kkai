package kkaimigrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

// v12 replaces global username uniqueness with tenant-scoped identities. It is
// a contract migration: old binaries must be drained before the explicit target.
func ApplyTenantSchema(ctx context.Context, db *gorm.DB, options Options) (*Result, error) {
	if db == nil {
		return nil, ErrSchemaNotReady
	}
	if err := checkThroughVersion(ctx, db, MembershipSchemaVersion, MembershipSchemaVersion, TenantSchemaVersion); err != nil {
		return nil, fmt.Errorf("KKAI maintenance target 12 requires validated schema 11: %w", err)
	}
	if err := precheckTenantSchema(db.WithContext(ctx)); err != nil {
		return nil, err
	}
	return applyThroughVersion(ctx, db, options, TenantSchemaVersion, TenantSchemaVersion)
}

func CheckTenantSchema(ctx context.Context, db *gorm.DB) error {
	return checkThroughVersion(ctx, db, TenantSchemaVersion, TenantSchemaVersion, TenantSchemaVersion)
}

type tenantV12User struct {
	ID                 int64   `gorm:"primaryKey"`
	TenantID           int64   `gorm:"type:bigint;not null;default:0;index:idx_users_tenant_username,priority:1;index:idx_users_tenant_email,priority:1;uniqueIndex:idx_users_tenant_normalized_username,priority:1;uniqueIndex:idx_users_tenant_normalized_email,priority:1"`
	Username           string  `gorm:"index:idx_users_tenant_username,priority:2"`
	Email              string  `gorm:"index:idx_users_tenant_email,priority:2"`
	NormalizedUsername *string `gorm:"type:varchar(20);uniqueIndex:idx_users_tenant_normalized_username,priority:2"`
	NormalizedEmail    *string `gorm:"type:varchar(50);uniqueIndex:idx_users_tenant_normalized_email,priority:2"`
}

func (tenantV12User) TableName() string { return "users" }

type tenantV12TopUp struct {
	PaidMinor int64  `gorm:"type:bigint;not null;default:0"`
	Currency  string `gorm:"type:varchar(8);not null;default:''"`
}

func (tenantV12TopUp) TableName() string { return "top_ups" }

var tenantV12SnapshotTables = []string{"users", "tokens", "top_ups", "user_sessions", "auth_flows", "external_identity_claims", "passkey_credentials", "user_oauth_bindings", "logs", "quota_data", "tasks"}

type tenantV12UserSnapshot struct {
	TenantID int64 `gorm:"type:bigint;not null;default:0;index"`
}

func (tenantV12UserSnapshot) TableName() string { return "users" }

type tenantV12TokenSnapshot struct {
	TenantID int64 `gorm:"type:bigint;not null;default:0;index"`
}

func (tenantV12TokenSnapshot) TableName() string { return "tokens" }

type tenantV12TopUpSnapshot struct {
	TenantID int64 `gorm:"type:bigint;not null;default:0;index"`
}

func (tenantV12TopUpSnapshot) TableName() string { return "top_ups" }

type tenantV12SessionSnapshot struct {
	TenantID int64 `gorm:"type:bigint;not null;default:0;index"`
}

func (tenantV12SessionSnapshot) TableName() string { return "user_sessions" }

type tenantV12FlowSnapshot struct {
	TenantID int64 `gorm:"type:bigint;not null;default:0;index"`
}

func (tenantV12FlowSnapshot) TableName() string { return "auth_flows" }

type tenantV12IdentitySnapshot struct {
	TenantID int64 `gorm:"type:bigint;not null;default:0;index"`
}

func (tenantV12IdentitySnapshot) TableName() string { return "external_identity_claims" }

type tenantV12PasskeySnapshot struct {
	TenantID int64 `gorm:"type:bigint;not null;default:0;index"`
}

func (tenantV12PasskeySnapshot) TableName() string { return "passkey_credentials" }

type tenantV12OAuthSnapshot struct {
	TenantID int64 `gorm:"type:bigint;not null;default:0;index"`
}

func (tenantV12OAuthSnapshot) TableName() string { return "user_oauth_bindings" }

type tenantV12LogSnapshot struct {
	TenantID int64 `gorm:"type:bigint;not null;default:0;index"`
}

func (tenantV12LogSnapshot) TableName() string { return "logs" }

type tenantV12QuotaSnapshot struct {
	TenantID int64 `gorm:"type:bigint;not null;default:0;index"`
}

func (tenantV12QuotaSnapshot) TableName() string { return "quota_data" }

type tenantV12TaskSnapshot struct {
	TenantID int64 `gorm:"type:bigint;not null;default:0;index"`
}

func (tenantV12TaskSnapshot) TableName() string { return "tasks" }

func tenantV12SnapshotModels() []any {
	return []any{&tenantV12UserSnapshot{}, &tenantV12TokenSnapshot{}, &tenantV12TopUpSnapshot{}, &tenantV12SessionSnapshot{}, &tenantV12FlowSnapshot{}, &tenantV12IdentitySnapshot{}, &tenantV12PasskeySnapshot{}, &tenantV12OAuthSnapshot{}, &tenantV12LogSnapshot{}, &tenantV12QuotaSnapshot{}, &tenantV12TaskSnapshot{}}
}

// The checksum includes the frozen field names, types and GORM tags as well as
// the algorithm ID, so schema edits cannot silently keep a stored v12 checksum.
func tenantSchemaSpec() string {
	var b strings.Builder
	b.WriteString("v12: add snapshots default0; canonicalize lower(trim) identities in batches; reject duplicates and invalid lengths; create scoped unique keys before dropping uni_users_username; preserve original values and all balances; preserve SQLite indexes and triggers when replacing legacy constraints; no cash backfill\n")
	for _, table := range tenantV12SnapshotTables {
		fmt.Fprintf(&b, "snapshot:%s\n", table)
	}
	models := append(tenantV12NewModels(), tenantV12SnapshotModels()...)
	models = append(models, &tenantV12User{}, &tenantV12TopUp{})
	for _, value := range models {
		typ := reflect.TypeOf(value).Elem()
		fmt.Fprintf(&b, "model:%s\n", typ.Name())
		if named, ok := value.(interface{ TableName() string }); ok {
			fmt.Fprintf(&b, "table:%s\n", named.TableName())
		}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			fmt.Fprintf(&b, "%s:%s:%s\n", f.Name, f.Type, f.Tag.Get("gorm"))
		}
	}
	return b.String()
}

func precheckTenantSchema(db *gorm.DB) error {
	for _, table := range tenantV12SnapshotTables {
		if !db.Migrator().HasTable(table) {
			return fmt.Errorf("%w: tenant migration needs existing table %s", ErrSchemaNotReady, table)
		}
	}
	for _, column := range []string{"id", "username", "email"} {
		if !db.Migrator().HasColumn("users", column) {
			return fmt.Errorf("%w: tenant migration needs users.%s", ErrSchemaNotReady, column)
		}
	}
	return nil
}

type TenantSchemaPrecheck struct {
	Schema                 string `json:"schema"`
	TargetVersion          int64  `json:"target_version"`
	UserCount              int64  `json:"user_count"`
	InvalidIdentityCount   int64  `json:"invalid_identity_count"`
	DuplicateUsernameCount int64  `json:"duplicate_username_count"`
	DuplicateEmailCount    int64  `json:"duplicate_email_count"`
	Safe                   bool   `json:"safe"`
}

// PrecheckTenantSchema reads original identities without creating the v12
// columns or changing accounts. The report contains counts, never identity data.
func PrecheckTenantSchema(ctx context.Context, db *gorm.DB) (*TenantSchemaPrecheck, error) {
	if db == nil {
		return nil, ErrSchemaNotReady
	}
	if err := checkThroughVersion(ctx, db, MembershipSchemaVersion, MembershipSchemaVersion, TenantSchemaVersion); err != nil {
		return nil, fmt.Errorf("tenant precheck requires validated schema 11: %w", err)
	}
	query := db.WithContext(ctx)
	if err := precheckTenantSchema(query); err != nil {
		return nil, err
	}
	result := &TenantSchemaPrecheck{Schema: "kkai_tenant_schema_precheck_v1", TargetVersion: TenantSchemaVersion}
	tenantColumn := "0 AS tenant_id"
	if query.Migrator().HasColumn("users", "tenant_id") {
		tenantColumn = "tenant_id"
	}
	type identityKey struct {
		tenantID int64
		value    string
	}
	usernames, emails := map[identityKey]int{}, map[identityKey]int{}
	var rows []tenantV12User
	err := query.Select("id", tenantColumn, "username", "email").FindInBatches(&rows, 1000, func(_ *gorm.DB, _ int) error {
		for _, row := range rows {
			result.UserCount++
			if err := normalizeTenantMigrationIdentity(&row); err != nil {
				result.InvalidIdentityCount++
				continue
			}
			key := identityKey{row.TenantID, *row.NormalizedUsername}
			usernames[key]++
			if usernames[key] == 2 {
				result.DuplicateUsernameCount++
			}
			if row.NormalizedEmail != nil {
				key.value = *row.NormalizedEmail
				emails[key]++
				if emails[key] == 2 {
					result.DuplicateEmailCount++
				}
			}
		}
		return nil
	}).Error
	if err != nil {
		return nil, fmt.Errorf("inspect legacy tenant identities: %w", err)
	}
	result.Safe = result.InvalidIdentityCount == 0 && result.DuplicateUsernameCount == 0 && result.DuplicateEmailCount == 0
	return result, nil
}

func normalizeTenantMigrationIdentity(row *tenantV12User) error {
	username := strings.ToLower(strings.TrimSpace(row.Username))
	email := strings.ToLower(strings.TrimSpace(row.Email))
	if username == "" || utf8.RuneCountInString(username) > 20 || utf8.RuneCountInString(email) > 50 || row.TenantID < 0 {
		return fmt.Errorf("%w: invalid legacy identity at user %d; operator repair required", ErrSchemaNotReady, row.ID)
	}
	row.NormalizedUsername = &username
	row.NormalizedEmail = nil
	if email != "" {
		row.NormalizedEmail = &email
	}
	return nil
}

func applyTenantSchema(db *gorm.DB) error {
	if err := precheckTenantSchema(db); err != nil {
		return err
	}
	// Preserve retryability on MySQL, where DDL commits independently. Existing
	// tables/columns are inspected and skipped; a conflicting schema fails closed.
	for _, value := range tenantV12SnapshotModels() {
		if !db.Migrator().HasColumn(value, "tenant_id") {
			if err := db.Migrator().AddColumn(value, "TenantID"); err != nil {
				return err
			}
		}
	}
	for _, entry := range []struct {
		value  any
		fields []string
	}{
		{&tenantV12User{}, []string{"NormalizedUsername", "NormalizedEmail"}},
		{&tenantV12TopUp{}, []string{"PaidMinor", "Currency"}},
	} {
		for _, field := range entry.fields {
			if !db.Migrator().HasColumn(entry.value, field) {
				if err := db.Migrator().AddColumn(entry.value, field); err != nil {
					return err
				}
			}
		}
	}
	if err := backfillTenantIdentities(db); err != nil {
		return err
	}
	for _, name := range []string{"idx_users_tenant_normalized_username", "idx_users_tenant_normalized_email", "idx_users_tenant_username", "idx_users_tenant_email"} {
		if !db.Migrator().HasIndex(&tenantV12User{}, name) {
			if err := db.Migrator().CreateIndex(&tenantV12User{}, name); err != nil {
				return err
			}
		}
	}
	// The legacy GORM `unique` constraint is the only constraint this migration
	// is permitted to remove. Composite identity keys already protect all rows.
	if db.Migrator().HasConstraint("users", "uni_users_username") {
		// SQLite emulates DROP CONSTRAINT by rebuilding the table. GORM does
		// not restore separate indexes or triggers, so preserve exactly the existing DDL.
		var schemaDDL []string
		if db.Dialector.Name() == DialectSQLite {
			if err := db.Raw("SELECT sql FROM sqlite_master WHERE type IN ('index', 'trigger') AND tbl_name = 'users' AND sql IS NOT NULL").Scan(&schemaDDL).Error; err != nil {
				return err
			}
		}
		if err := db.Migrator().DropConstraint("users", "uni_users_username"); err != nil {
			return err
		}
		for _, ddl := range schemaDDL {
			if err := db.Exec(ddl).Error; err != nil {
				return err
			}
		}
	}
	if db.Migrator().HasIndex("users", "uni_users_username") {
		if err := db.Migrator().DropIndex("users", "uni_users_username"); err != nil {
			return err
		}
	}
	for _, value := range tenantV12NewModels() {
		if !db.Migrator().HasTable(value) {
			if err := db.Migrator().CreateTable(value); err != nil {
				return err
			}
		}
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(value); err != nil {
			return err
		}
		for _, index := range statement.Schema.ParseIndexes() {
			if !db.Migrator().HasIndex(value, index.Name) {
				if err := db.Migrator().CreateIndex(value, index.Name); err != nil {
					return err
				}
			}
		}
	}
	for _, value := range tenantV12SnapshotModels() {
		if !db.Migrator().HasIndex(value, "TenantID") {
			if err := db.Migrator().CreateIndex(value, "TenantID"); err != nil {
				return err
			}
		}
	}
	return validateTenantRuntimeSchema(db)
}

func backfillTenantIdentities(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var rows []tenantV12User
		if err := tx.Select("id", "tenant_id", "username", "email").FindInBatches(&rows, 1000, func(_ *gorm.DB, _ int) error {
			for _, row := range rows {
				if err := normalizeTenantMigrationIdentity(&row); err != nil {
					return err
				}
				if err := tx.Model(&tenantV12User{}).Where("id = ?", row.ID).Updates(map[string]any{"normalized_username": row.NormalizedUsername, "normalized_email": row.NormalizedEmail}).Error; err != nil {
					return fmt.Errorf("canonicalize tenant identity user %d: %w", row.ID, err)
				}
			}
			return nil
		}).Error; err != nil {
			return err
		}
		for _, column := range []string{"normalized_username", "normalized_email"} {
			var duplicate struct{ Count int64 }
			if err := tx.Model(&tenantV12User{}).Select("COUNT(*) AS count").Where(column + " IS NOT NULL").Group("tenant_id, " + column).Having("COUNT(*) > 1").Limit(1).Scan(&duplicate).Error; err != nil {
				return err
			}
			if duplicate.Count > 0 {
				return fmt.Errorf("%w: duplicate tenant %s; operator repair required", ErrSchemaNotReady, column)
			}
		}
		return nil
	})
}

func validateTenantRuntimeSchema(db *gorm.DB) error {
	models := append(tenantV12NewModels(), &tenantV12User{}, &tenantV12TopUp{})
	for _, value := range models {
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
	if db.Migrator().HasConstraint("users", "uni_users_username") || db.Migrator().HasIndex("users", "uni_users_username") {
		return fmt.Errorf("%w: legacy global username uniqueness is still installed", ErrSchemaNotReady)
	}
	var missing int64
	if err := db.Model(&tenantV12User{}).Where("normalized_username IS NULL OR (email <> '' AND normalized_email IS NULL)").Count(&missing).Error; err != nil {
		return err
	}
	if missing > 0 {
		return fmt.Errorf("%w: tenant identity backfill is incomplete", ErrSchemaNotReady)
	}
	return nil
}

func validateTenantIntegerColumn(db *gorm.DB, table, name string, column gorm.ColumnType, notNull bool) error {
	typeName := strings.ToLower(column.DatabaseTypeName())
	validType := typeName == "bigint" || typeName == "int8"
	if db.Dialector.Name() == DialectSQLite {
		validType = validType || typeName == "integer"
	}
	if !validType {
		return fmt.Errorf("%w: %s.%s must be a 64-bit integer", ErrSchemaNotReady, table, name)
	}
	if nullable, known := column.Nullable(); notNull && (!known || nullable) {
		return fmt.Errorf("%w: %s.%s must be NOT NULL", ErrSchemaNotReady, table, name)
	}
	return nil
}

func validateTenantSnapshot(db *gorm.DB, table string) error {
	if !db.Migrator().HasTable(table) {
		return fmt.Errorf("%w: missing tenant snapshot table %s", ErrSchemaNotReady, table)
	}
	columns, err := db.Migrator().ColumnTypes(table)
	if err != nil {
		return err
	}
	var tenantColumn gorm.ColumnType
	for _, column := range columns {
		if column.Name() == "tenant_id" {
			tenantColumn = column
			break
		}
	}
	if tenantColumn == nil {
		return fmt.Errorf("%w: missing %s.tenant_id", ErrSchemaNotReady, table)
	}
	if err := validateTenantIntegerColumn(db, table, "tenant_id", tenantColumn, true); err != nil {
		return err
	}
	if value, known := tenantColumn.DefaultValue(); !known || strings.Trim(value, "()'\"") != "0" {
		return fmt.Errorf("%w: %s.tenant_id must default to 0", ErrSchemaNotReady, table)
	}
	if !db.Migrator().HasIndex(table, "idx_"+table+"_tenant_id") {
		return fmt.Errorf("%w: missing %s tenant snapshot index", ErrSchemaNotReady, table)
	}
	var missing int64
	if err := db.Table(table).Where("tenant_id IS NULL OR tenant_id < 0").Count(&missing).Error; err != nil {
		return err
	}
	if missing > 0 {
		return fmt.Errorf("%w: invalid tenant snapshot in %s", ErrSchemaNotReady, table)
	}
	return nil
}

type tenantLogSchemaRecord struct {
	ID        int    `gorm:"primaryKey;autoIncrement:false"`
	Checksum  string `gorm:"type:char(64);not null"`
	AppliedAt int64  `gorm:"not null"`
}

func (tenantLogSchemaRecord) TableName() string { return "kkai_tenant_log_schema" }
func tenantLogSchemaChecksum() string {
	sum := sha256.Sum256([]byte("logs.tenant_id:bigint:not null:default0:index:v12"))
	return hex.EncodeToString(sum[:])
}

// ApplyTenantLogSchema targets a separate LOG_DB without touching the main
// migration ledger. Run before activating a v12 binary; old log rows stay tenant 0.
func ApplyTenantLogSchema(ctx context.Context, db *gorm.DB, options Options) error {
	if db == nil {
		return ErrSchemaNotReady
	}
	dialect, err := dialectName(db)
	if err != nil {
		return err
	}
	return withMigrationLock(ctx, db, dialect, func(locked *gorm.DB) error {
		if !locked.Migrator().HasTable("logs") {
			return fmt.Errorf("%w: missing logs table", ErrSchemaNotReady)
		}
		if options.DryRun {
			return nil
		}
		if !locked.Migrator().HasColumn(&tenantV12LogSnapshot{}, "tenant_id") {
			if err := locked.Migrator().AddColumn(&tenantV12LogSnapshot{}, "TenantID"); err != nil {
				return err
			}
		}
		if !locked.Migrator().HasIndex(&tenantV12LogSnapshot{}, "TenantID") {
			if err := locked.Migrator().CreateIndex(&tenantV12LogSnapshot{}, "TenantID"); err != nil {
				return err
			}
		}
		if !locked.Migrator().HasTable(&tenantLogSchemaRecord{}) {
			if err := locked.Migrator().CreateTable(&tenantLogSchemaRecord{}); err != nil {
				return err
			}
		}
		var records []tenantLogSchemaRecord
		if err := locked.Find(&records).Error; err != nil {
			return err
		}
		if len(records) == 0 {
			if err := locked.Create(&tenantLogSchemaRecord{ID: 12, Checksum: tenantLogSchemaChecksum(), AppliedAt: time.Now().Unix()}).Error; err != nil {
				return err
			}
		}
		return CheckTenantLogSchema(ctx, locked)
	})
}
func CheckTenantLogSchema(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return ErrSchemaNotReady
	}
	db = db.WithContext(ctx)
	if !db.Migrator().HasTable(&tenantLogSchemaRecord{}) {
		return fmt.Errorf("%w: tenant log migration is not recorded", ErrSchemaNotReady)
	}
	var records []tenantLogSchemaRecord
	if err := db.Find(&records).Error; err != nil {
		return err
	}
	if len(records) != 1 || records[0].ID != 12 || records[0].Checksum != tenantLogSchemaChecksum() {
		return ErrChecksumMismatch
	}
	return validateTenantSnapshot(db, "logs")
}
