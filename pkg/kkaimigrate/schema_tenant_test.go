package kkaimigrate

import (
	"context"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func prepareTenantMigrationFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, column := range []struct{ Name, Type string }{{"username", "VARCHAR(20)"}, {"email", "VARCHAR(50)"}} {
		if !db.Migrator().HasColumn("users", column.Name) {
			require.NoError(t, db.Exec("ALTER TABLE users ADD COLUMN "+column.Name+" "+column.Type).Error)
		}
	}
	for _, table := range []string{"top_ups", "user_oauth_bindings", "logs", "quota_data", "tasks"} {
		require.NoError(t, db.Exec("CREATE TABLE "+table+" (id BIGINT PRIMARY KEY)").Error)
	}
	require.NoError(t, db.Exec("INSERT INTO users (id,username,email) VALUES (101,' Legacy ',' LEGACY@example.com ')").Error)
	if !db.Migrator().HasIndex("users", "uni_users_username") && !db.Migrator().HasConstraint("users", "uni_users_username") {
		statement := "ALTER TABLE users ADD CONSTRAINT uni_users_username UNIQUE(username)"
		if db.Dialector.Name() == DialectSQLite {
			statement = "CREATE UNIQUE INDEX uni_users_username ON users(username)"
		}
		require.NoError(t, db.Exec(statement).Error)
	}
	_, err := applyThroughVersion(context.Background(), db, Options{}, MembershipSchemaVersion, KnownCatalogVersion)
	require.NoError(t, err)
}

func assertTenantMigrationContract(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	prepareTenantMigrationFixture(t, db)
	var history []AppliedMigration
	require.NoError(t, db.Order("version").Find(&history).Error)
	_, err := Apply(ctx, db, Options{})
	require.ErrorIs(t, err, ErrUnsafeMigration)
	dry, err := ApplyTenantSchema(ctx, db, Options{DryRun: true})
	require.NoError(t, err)
	require.Len(t, dry.Pending, 1)
	assert.False(t, db.Migrator().HasTable("tenants"))
	_, err = ApplyTenantSchema(ctx, db, Options{})
	require.NoError(t, err)
	require.NoError(t, CheckTenantSchema(ctx, db))
	var user tenantV12User
	require.NoError(t, db.First(&user, 101).Error)
	assert.EqualValues(t, 0, user.TenantID)
	require.NotNil(t, user.NormalizedUsername)
	assert.Equal(t, "legacy", *user.NormalizedUsername)
	require.NotNil(t, user.NormalizedEmail)
	assert.Equal(t, "legacy@example.com", *user.NormalizedEmail)
	assert.Equal(t, " Legacy ", user.Username)
	for _, tenantID := range []int64{1, 2} {
		require.NoError(t, db.Create(&tenantV12User{ID: 101 + tenantID, TenantID: tenantID, Username: user.Username, Email: user.Email, NormalizedUsername: user.NormalizedUsername, NormalizedEmail: user.NormalizedEmail}).Error)
	}
	require.Error(t, db.Create(&tenantV12User{ID: 104, TenantID: 1, Username: "different", Email: user.Email, NormalizedUsername: user.NormalizedUsername, NormalizedEmail: user.NormalizedEmail}).Error)
	_, err = ApplyTenantSchema(ctx, db, Options{})
	require.NoError(t, err)
	var preserved []AppliedMigration
	require.NoError(t, db.Where("version <= ?", MembershipSchemaVersion).Order("version").Find(&preserved).Error)
	assert.Equal(t, history, preserved)
	require.NoError(t, db.Migrator().DropIndex(&tenantV12User{}, "idx_users_tenant_normalized_username"))
	assert.ErrorIs(t, CheckTenantSchema(ctx, db), ErrSchemaNotReady)
}

func TestTenantSchemaMigrationContract(t *testing.T) {
	assertTenantMigrationContract(t, newMigrationTestDB(t))
}
func TestTenantSchemaMigrationRealDatabases(t *testing.T) {
	for _, dialect := range []string{DialectPostgres, DialectMySQL} {
		t.Run(dialect, func(t *testing.T) { assertTenantMigrationContract(t, newImageAsyncDatabaseFixture(t, dialect)) })
	}
}
func TestTenantSchemaRejectsAmbiguousLegacyIdentities(t *testing.T) {
	db := newMigrationTestDB(t)
	prepareTenantMigrationFixture(t, db)
	require.NoError(t, db.Exec("INSERT INTO users(id,username,email) VALUES (102,'legacy','another@example.com')").Error)
	_, err := ApplyTenantSchema(context.Background(), db, Options{})
	require.ErrorContains(t, err, "duplicate tenant normalized_username")
	var count int64
	require.NoError(t, db.Model(&AppliedMigration{}).Where("version = 12").Count(&count).Error)
	assert.Zero(t, count)
	assert.True(t, db.Migrator().HasIndex("users", "uni_users_username"))
}
func assertTenantLogSchemaSeparateDatabase(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, db.Exec("CREATE TABLE logs(id BIGINT PRIMARY KEY)").Error)
	require.NoError(t, db.Exec("INSERT INTO logs(id) VALUES(1)").Error)
	assert.ErrorIs(t, CheckTenantLogSchema(ctx, db), ErrSchemaNotReady)
	require.NoError(t, ApplyTenantLogSchema(ctx, db, Options{DryRun: true}))
	assert.False(t, db.Migrator().HasColumn("logs", "tenant_id"))
	require.NoError(t, ApplyTenantLogSchema(ctx, db, Options{}))
	require.NoError(t, ApplyTenantLogSchema(ctx, db, Options{}))
	require.NoError(t, CheckTenantLogSchema(ctx, db))
	assert.False(t, db.Migrator().HasTable(&AppliedMigration{}))
	var tenantID int64
	require.NoError(t, db.Table("logs").Where("id = 1").Pluck("tenant_id", &tenantID).Error)
	assert.Zero(t, tenantID)
	require.NoError(t, db.Model(&tenantLogSchemaRecord{}).Where("id=12").Update("checksum", "invalid").Error)
	assert.ErrorIs(t, CheckTenantLogSchema(ctx, db), ErrChecksumMismatch)
}
func TestTenantSchemaRequiresValidatedV11(t *testing.T) {
	db := newMigrationTestDB(t)
	_, err := ApplyTenantSchema(context.Background(), db, Options{})
	assert.ErrorIs(t, err, ErrSchemaNotReady)
	assert.False(t, db.Migrator().HasTable("tenants"))
}

func TestTenantSchemaPreservesLegacyNamedConstraintData(t *testing.T) {
	db := newMigrationTestDB(t)
	require.NoError(t, db.Migrator().DropTable("users"))
	require.NoError(t, db.Exec(`CREATE TABLE users(id BIGINT PRIMARY KEY, telegram_id TEXT, username VARCHAR(20), email VARCHAR(50), password TEXT NOT NULL DEFAULT 'preserved-hash', CONSTRAINT uni_users_username UNIQUE(username))`).Error)
	prepareTenantMigrationFixture(t, db)
	require.NoError(t, db.Exec("CREATE INDEX idx_preserved_users_email ON users(email)").Error)
	require.NoError(t, db.Exec("CREATE TABLE user_audit(user_id BIGINT)").Error)
	require.NoError(t, db.Exec("CREATE TRIGGER users_audit AFTER INSERT ON users BEGIN INSERT INTO user_audit(user_id) VALUES(NEW.id); END").Error)
	_, err := ApplyTenantSchema(context.Background(), db, Options{})
	require.NoError(t, err)
	assert.True(t, db.Migrator().HasIndex("users", "idx_preserved_users_email"))
	var password string
	require.NoError(t, db.Table("users").Where("id=101").Pluck("password", &password).Error)
	assert.Equal(t, "preserved-hash", password)
	normalized := "legacy"
	require.NoError(t, db.Create(&tenantV12User{ID: 102, TenantID: 2, Username: " Legacy ", NormalizedUsername: &normalized}).Error)
	var auditUser int64
	require.NoError(t, db.Table("user_audit").Pluck("user_id", &auditUser).Error)
	assert.EqualValues(t, 102, auditUser)
}

func TestTenantExplicitMigrationMatchesApplicationSchema(t *testing.T) {
	db := newMigrationTestDB(t)
	prepareTenantMigrationFixture(t, db)
	_, err := ApplyTenantSchema(context.Background(), db, Options{})
	require.NoError(t, err)
	for _, value := range []any{&model.Tenant{}, &model.TenantDomain{}, &model.TenantMembership{}, &model.ResellerApplication{}, &model.TenantProfile{}, &model.TenantCatalogEntry{}, &model.TenantPriceVersion{}, &model.PricingQuote{}, &model.TenantConsumption{}, &model.CustomerPaymentLot{}, &model.ResellerWallet{}, &model.ResellerLedgerEntry{}, &model.ResellerWithdrawal{}, &model.CustomerPaymentReversal{}} {
		statement := &gorm.Statement{DB: db}
		require.NoError(t, statement.Parse(value))
		columns, err := db.Migrator().ColumnTypes(value)
		require.NoError(t, err)
		actual := map[string]bool{}
		for _, column := range columns {
			actual[column.Name()] = true
		}
		for _, field := range statement.Schema.Fields {
			if !field.IgnoreMigration && field.DBName != "" {
				assert.True(t, actual[field.DBName], "missing %s.%s", statement.Schema.Table, field.DBName)
			}
		}
	}
}

func TestTenantLogSchemaSeparateDatabase(t *testing.T) {
	assertTenantLogSchemaSeparateDatabase(t, newMigrationTestDB(t))
}
func TestTenantLogSchemaRealDatabases(t *testing.T) {
	for _, dialect := range []string{DialectPostgres, DialectMySQL} {
		t.Run(dialect, func(t *testing.T) { assertTenantLogSchemaSeparateDatabase(t, newImageAsyncDatabaseFixture(t, dialect)) })
	}
}
func TestTenantGORMContractCannotMixUnreviewedSchema(t *testing.T) {
	migrations := migrationSet()
	migrations[len(migrations)-1].SchemaSpec = "different schema"
	assert.ErrorIs(t, validateMigrationCatalog(migrations), ErrUnsafeMigration)
	migrations = migrationSet()
	migrations[len(migrations)-1].SchemaApply = func(*gorm.DB) error { return nil }
	assert.ErrorIs(t, validateMigrationCatalog(migrations), ErrUnsafeMigration)
	migrations = migrationSet()
	migrations[len(migrations)-1].Statements[DialectPostgres] = []migrationStatement{{Operation: migrationOperationContract, SQL: "DROP TABLE users"}}
	assert.ErrorIs(t, validateMigrationCatalog(migrations), ErrUnsafeMigration)
}

func TestTenantSchemaImmutableChecksum(t *testing.T) {
	require.Equal(t, "4a2edf08e0e75fa7c6827288edbd4a7bb83d0084b0b93204568043214c751519", Plan()[TenantSchemaVersion-1].Checksum)
}

func TestTenantSchemaPrecheckReadsOriginalIdentitiesWithoutWrites(t *testing.T) {
	for _, test := range []struct {
		name        string
		username    string
		email       string
		tenantID    int64
		invalid     int64
		usernameDup int64
		emailDup    int64
	}{
		{name: "Unicode whitespace and blank email", username: "\u2003Other\u2003", email: "\u2003"},
		{name: "normalized username collision", username: "\u2003legacy\u2003", email: "other@example.com", usernameDup: 1},
		{name: "normalized email collision", username: "other", email: "\u2003legacy@EXAMPLE.COM\u2003", emailDup: 1},
		{name: "blank username", username: "\u2003", invalid: 1},
		{name: "long Unicode username", username: strings.Repeat("界", 21), invalid: 1},
		{name: "long Unicode email", username: "other", email: strings.Repeat("界", 51), invalid: 1},
		{name: "same identity in another tenant", username: "legacy", email: "legacy@example.com", tenantID: 1},
		{name: "negative tenant", username: "other", tenantID: -1, invalid: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := newMigrationTestDB(t)
			prepareTenantMigrationFixture(t, db)
			require.NoError(t, db.Exec("INSERT INTO users(id,username,email) VALUES(102,?,?)", test.username, test.email).Error)
			if test.tenantID != 0 {
				require.NoError(t, db.Exec("ALTER TABLE users ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 0").Error)
				require.NoError(t, db.Table("users").Where("id=102").Update("tenant_id", test.tenantID).Error)
			}
			var before []tenantV12User
			require.NoError(t, db.Select("id", "username", "email").Order("id").Find(&before).Error)
			require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)

			report, err := PrecheckTenantSchema(context.Background(), db)

			require.NoError(t, err)
			assert.Equal(t, "kkai_tenant_schema_precheck_v1", report.Schema)
			assert.EqualValues(t, 12, report.TargetVersion)
			assert.EqualValues(t, 2, report.UserCount)
			assert.Equal(t, test.invalid, report.InvalidIdentityCount)
			assert.Equal(t, test.usernameDup, report.DuplicateUsernameCount)
			assert.Equal(t, test.emailDup, report.DuplicateEmailCount)
			assert.Equal(t, test.invalid+test.usernameDup+test.emailDup == 0, report.Safe)
			assert.False(t, db.Migrator().HasColumn("users", "normalized_username"))
			assert.False(t, db.Migrator().HasTable("tenants"))
			var after []tenantV12User
			require.NoError(t, db.Select("id", "username", "email").Order("id").Find(&after).Error)
			assert.Equal(t, before, after)
			var v12Count int64
			require.NoError(t, db.Model(&AppliedMigration{}).Where("version=12").Count(&v12Count).Error)
			assert.Zero(t, v12Count)
			encoded, err := common.Marshal(report)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "legacy@example.com")
			assert.NotContains(t, string(encoded), test.username)
		})
	}
}

func TestTenantSchemaPrecheckRequiresValidatedV11(t *testing.T) {
	db := newMigrationTestDB(t)
	require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)
	report, err := PrecheckTenantSchema(context.Background(), db)
	assert.ErrorIs(t, err, ErrSchemaNotReady)
	assert.Nil(t, report)
}
