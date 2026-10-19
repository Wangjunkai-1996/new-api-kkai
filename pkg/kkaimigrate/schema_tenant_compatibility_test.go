package kkaimigrate

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func assertTenantCompatibilityMigrationContract(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	prepareTenantMigrationFixture(t, db)
	if db.Dialector.Name() != DialectSQLite {
		if db.Dialector.Name() == DialectPostgres {
			require.NoError(t, db.Migrator().DropConstraint("users", "uni_users_username"))
		} else {
			require.NoError(t, db.Migrator().DropIndex("users", "uni_users_username"))
		}
		// Production PostgreSQL uses this database-generated constraint name.
		require.NoError(t, db.Exec("ALTER TABLE users ADD CONSTRAINT users_username_key UNIQUE(username)").Error)
	}
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX legacy_username_unique_index ON users(username)").Error)
	require.NoError(t, db.Exec("CREATE INDEX idx_users_username ON users(username)").Error)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX preserved_user_identity ON users(id,username)").Error)
	for _, column := range []struct{ name, sqlType string }{{"password", "TEXT"}, {"quota", "BIGINT"}, {"deleted_at", "TIMESTAMP"}} {
		if !db.Migrator().HasColumn("users", column.name) {
			require.NoError(t, db.Exec("ALTER TABLE users ADD COLUMN "+column.name+" "+column.sqlType).Error)
		}
	}
	require.NoError(t, db.Exec("UPDATE users SET password='preserved-hash',quota=12345 WHERE id=101").Error)
	require.NoError(t, db.Exec("INSERT INTO users(id,username,email,password,quota,deleted_at) VALUES(102,'legacy','legacy@example.com','deleted-hash',6789,'2025-01-01 00:00:00')").Error)
	var baseline []AppliedMigration
	require.NoError(t, db.Order("version").Find(&baseline).Error)

	report, err := PrecheckTenantCompatibilitySchema(ctx, db)
	require.NoError(t, err)
	assert.True(t, report.Safe)
	assert.EqualValues(t, 2, report.UserCount, "soft-deleted identities must be inspected")
	assert.EqualValues(t, 1, report.LegacyNormalizedUsernameConflictCount)
	assert.EqualValues(t, 1, report.LegacyNormalizedEmailConflictCount)
	assert.Zero(t, report.DuplicateUsernameCount)
	assert.Zero(t, report.DuplicateEmailCount)
	dry, err := ApplyTenantCompatibilitySchema(ctx, db, Options{DryRun: true})
	require.NoError(t, err)
	require.Len(t, dry.Pending, 1)
	assert.EqualValues(t, 13, dry.Pending[0].Version)
	assert.False(t, db.Migrator().HasColumn("users", "tenant_id"))

	_, err = ApplyTenantCompatibilitySchema(ctx, db, Options{})
	require.NoError(t, err)
	require.NoError(t, CheckTenantCompatibilitySchema(ctx, db))
	require.NoError(t, CheckRequired(ctx, db))
	assert.False(t, db.Migrator().HasConstraint("users", "users_username_key"))
	assert.False(t, db.Migrator().HasIndex("users", "legacy_username_unique_index"))
	assert.True(t, db.Migrator().HasIndex("users", "idx_users_username"))
	assert.True(t, db.Migrator().HasIndex("users", "preserved_user_identity"))
	var users []tenantV13User
	require.NoError(t, db.Order("id").Find(&users).Error)
	require.Len(t, users, 2)
	assert.Equal(t, " Legacy ", users[0].Username)
	assert.Equal(t, " LEGACY@example.com ", users[0].Email)
	assert.Equal(t, "legacy", users[1].Username)
	assert.Equal(t, "legacy@example.com", users[1].Email)
	for _, user := range users {
		assert.Zero(t, user.TenantID)
		assert.Nil(t, user.NormalizedUsername)
		assert.Nil(t, user.NormalizedEmail)
	}
	for id, expected := range map[int64]struct {
		password string
		quota    int64
		deleted  bool
	}{101: {"preserved-hash", 12345, false}, 102: {"deleted-hash", 6789, true}} {
		var account struct {
			Password string
			Quota    int64
			Deleted  bool
		}
		require.NoError(t, db.Table("users").Select("password", "quota", "deleted_at IS NOT NULL AS deleted").Where("id=?", id).Scan(&account).Error)
		assert.Equal(t, expected.password, account.Password)
		assert.Equal(t, expected.quota, account.Quota)
		assert.Equal(t, expected.deleted, account.Deleted)
	}
	var history []AppliedMigration
	require.NoError(t, db.Where("version<=11").Order("version").Find(&history).Error)
	assert.Equal(t, baseline, history)
	var versions []int64
	require.NoError(t, db.Model(&AppliedMigration{}).Order("version").Pluck("version", &versions).Error)
	assert.Equal(t, []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 13}, versions)
	assert.ErrorIs(t, CheckTenantSchema(ctx, db), ErrFutureMigration)
	_, err = ApplyTenantCompatibilitySchema(ctx, db, Options{})
	require.NoError(t, err)
	_, err = Apply(ctx, db, Options{})
	require.NoError(t, err)
	assert.Error(t, db.Create(&tenantV13User{ID: 103, Username: "legacy"}).Error, "platform raw usernames retain database uniqueness")
	username, email := "legacy", "legacy@example.com"
	for _, id := range []int64{201, 202} {
		require.NoError(t, db.Create(&tenantV13User{ID: id, TenantID: id, Username: "legacy", Email: email, NormalizedUsername: &username, NormalizedEmail: &email}).Error)
	}
	assert.Error(t, db.Create(&tenantV13User{ID: 203, TenantID: 201, Username: " Legacy ", NormalizedUsername: &username}).Error)
	otherUsername := "other"
	assert.Error(t, db.Create(&tenantV13User{ID: 204, TenantID: 201, Username: "other", NormalizedUsername: &otherUsername, NormalizedEmail: &email}).Error)
	require.NoError(t, CheckRequired(ctx, db))
}

func TestTenantCompatibilityMigrationPreservesLegacyAccounts(t *testing.T) {
	assertTenantCompatibilityMigrationContract(t, newMigrationTestDB(t))
}

func TestTenantCompatibilityMigrationRealDatabases(t *testing.T) {
	for _, dialect := range []string{DialectPostgres, DialectMySQL} {
		t.Run(dialect, func(t *testing.T) {
			assertTenantCompatibilityMigrationContract(t, newImageAsyncDatabaseFixture(t, dialect))
		})
	}
}

func TestTenantCompatibilityPrecheckIsReadOnlyAndRedacted(t *testing.T) {
	db := newMigrationTestDB(t)
	prepareTenantMigrationFixture(t, db)
	require.NoError(t, db.Exec("INSERT INTO users(id,username,email) VALUES(102,'legacy','legacy@example.com')").Error)
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	report, err := PrecheckTenantCompatibilitySchema(context.Background(), db)
	require.NoError(t, err)
	encoded, err := common.Marshal(report)
	require.NoError(t, err)
	assert.JSONEq(t, `{"schema":"kkai_tenant_compatibility_precheck_v1","target_version":13,"user_count":2,"invalid_identity_count":0,"duplicate_username_count":0,"duplicate_email_count":0,"legacy_normalized_username_conflict_count":1,"legacy_normalized_email_conflict_count":1,"safe":true}`, string(encoded))
	assert.NotContains(t, string(encoded), "legacy@example.com")
	assert.False(t, db.Migrator().HasColumn("users", "normalized_username"))
	assert.False(t, db.Migrator().HasTable("tenants"))
}

func TestTenantCompatibilityPrecheckBlocksInvalidOrRawDuplicateIdentities(t *testing.T) {
	for _, username := range []string{"", " Legacy "} {
		t.Run(username, func(t *testing.T) {
			db := newMigrationTestDB(t)
			prepareTenantMigrationFixture(t, db)
			require.NoError(t, db.Migrator().DropIndex("users", "uni_users_username"))
			require.NoError(t, db.Exec("INSERT INTO users(id,username,email) VALUES(102,?,'')", username).Error)
			report, err := PrecheckTenantCompatibilitySchema(context.Background(), db)
			require.NoError(t, err)
			assert.False(t, report.Safe)
			assert.EqualValues(t, 1, report.InvalidIdentityCount+report.DuplicateUsernameCount)
			_, err = ApplyTenantCompatibilitySchema(context.Background(), db, Options{})
			assert.ErrorIs(t, err, ErrSchemaNotReady)
			assert.False(t, db.Migrator().HasColumn("users", "tenant_id"))
		})
	}
}

func TestTenantCompatibilityPreservesSQLiteConstraintIndexesAndTriggers(t *testing.T) {
	db := newMigrationTestDB(t)
	require.NoError(t, db.Migrator().DropTable("users"))
	require.NoError(t, db.Exec("CREATE TABLE users(id BIGINT PRIMARY KEY,telegram_id TEXT,username VARCHAR(20),email VARCHAR(50),CONSTRAINT users_username_key UNIQUE(username))").Error)
	prepareTenantMigrationFixture(t, db)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX another_legacy_username_key ON users(username)").Error)
	require.NoError(t, db.Exec("CREATE INDEX idx_preserved_users_email ON users(email)").Error)
	require.NoError(t, db.Exec("CREATE TABLE user_audit(user_id BIGINT)").Error)
	require.NoError(t, db.Exec("CREATE TRIGGER users_audit AFTER INSERT ON users BEGIN INSERT INTO user_audit(user_id) VALUES(NEW.id); END").Error)
	_, err := ApplyTenantCompatibilitySchema(context.Background(), db, Options{})
	require.NoError(t, err)
	assert.False(t, db.Migrator().HasConstraint("users", "users_username_key"))
	assert.False(t, db.Migrator().HasIndex("users", "another_legacy_username_key"))
	assert.True(t, db.Migrator().HasIndex("users", "idx_preserved_users_email"))
	username := "legacy"
	require.NoError(t, db.Create(&tenantV13User{ID: 201, TenantID: 1, Username: " Legacy ", NormalizedUsername: &username}).Error)
	var auditID int64
	require.NoError(t, db.Table("user_audit").Pluck("user_id", &auditID).Error)
	assert.EqualValues(t, 201, auditID)
	require.NoError(t, CheckRequired(context.Background(), db))
}

func TestTenantCompatibilityRejectsUnnamedSQLiteUniquenessBeforeDDL(t *testing.T) {
	for _, identityDDL := range []string{"username VARCHAR(20) UNIQUE", "username VARCHAR(20), UNIQUE(username)"} {
		t.Run(identityDDL, func(t *testing.T) {
			db := newMigrationTestDB(t)
			require.NoError(t, db.Migrator().DropTable("users"))
			require.NoError(t, db.Exec("CREATE TABLE users(id BIGINT PRIMARY KEY,telegram_id TEXT,email VARCHAR(50),"+identityDDL+")").Error)
			prepareTenantMigrationFixture(t, db)
			var before []AppliedMigration
			require.NoError(t, db.Order("version").Find(&before).Error)
			_, err := ApplyTenantCompatibilitySchema(context.Background(), db, Options{})
			assert.ErrorContains(t, err, "unnamed SQLite username uniqueness")
			assert.False(t, db.Migrator().HasColumn("users", "tenant_id"))
			assert.False(t, db.Migrator().HasTable("tenants"))
			var after []AppliedMigration
			require.NoError(t, db.Order("version").Find(&after).Error)
			assert.Equal(t, before, after)
		})
	}
}

func TestTenantCompatibilityFailureDoesNotRecordAppliedVersion(t *testing.T) {
	db := newMigrationTestDB(t)
	prepareTenantMigrationFixture(t, db)
	// An unexpected existing index aborts the DDL transaction after columns were added.
	require.NoError(t, db.Exec("CREATE INDEX idx_users_tenant_normalized_email ON users(email)").Error)
	_, err := ApplyTenantCompatibilitySchema(context.Background(), db, Options{})
	require.Error(t, err)
	assert.False(t, db.Migrator().HasColumn("users", "tenant_id"))
	var count int64
	require.NoError(t, db.Model(&AppliedMigration{}).Where("version=13").Count(&count).Error)
	assert.Zero(t, count)
}

func TestTenantCompatibilityRequiresPristineV11(t *testing.T) {
	for name, partial := range map[string]string{
		"snapshot":   "ALTER TABLE tokens ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 0",
		"canonical":  "ALTER TABLE users ADD COLUMN normalized_username VARCHAR(20)",
		"cash":       "ALTER TABLE top_ups ADD COLUMN paid_minor BIGINT NOT NULL DEFAULT 0",
		"table":      "CREATE TABLE tenants(id BIGINT PRIMARY KEY)",
		"log marker": "CREATE TABLE kkai_tenant_log_schema(id BIGINT PRIMARY KEY)",
	} {
		t.Run(name, func(t *testing.T) {
			db := newMigrationTestDB(t)
			prepareTenantMigrationFixture(t, db)
			require.NoError(t, db.Exec(partial).Error)
			_, err := ApplyTenantCompatibilitySchema(context.Background(), db, Options{})
			assert.ErrorIs(t, err, ErrSchemaNotReady)
			var count int64
			require.NoError(t, db.Model(&AppliedMigration{}).Where("version>11").Count(&count).Error)
			assert.Zero(t, count)
			assert.False(t, db.Migrator().HasColumn("users", "tenant_id"))
		})
	}
	t.Run("historical 12 remains observable but cannot migrate to 13", func(t *testing.T) {
		db := newMigrationTestDB(t)
		prepareTenantMigrationFixture(t, db)
		_, err := ApplyTenantSchema(context.Background(), db, Options{})
		require.NoError(t, err)
		require.NoError(t, CheckTenantSchema(context.Background(), db))
		observation, err := Observe(context.Background(), db)
		require.NoError(t, err)
		assert.EqualValues(t, 12, observation.CurrentVersion)
		_, err = ApplyTenantCompatibilitySchema(context.Background(), db, Options{})
		assert.ErrorIs(t, err, ErrSchemaNotReady)
		assert.ErrorIs(t, CheckRequired(context.Background(), db), ErrSchemaNotReady)
	})
}

func TestTenantCompatibilityLedgerHasOnlyOneReviewedReplacement(t *testing.T) {
	catalog := migrationSet()
	catalog[len(catalog)-1] = migration{
		Version: 13, Name: "unreviewed-replacement", Kind: MigrationKindExpand,
		ImplementationID: "unreviewed-v13", ChecksumVersion: migrationChecksumSchemaCurrent,
		Statements: map[string][]migrationStatement{
			DialectSQLite:   {{Operation: migrationOperationCreateTable, SQL: "CREATE TABLE unreviewed(id BIGINT)"}},
			DialectPostgres: {{Operation: migrationOperationCreateTable, SQL: "CREATE TABLE unreviewed(id BIGINT)"}},
			DialectMySQL:    {{Operation: migrationOperationCreateTable, SQL: "CREATE TABLE unreviewed(id BIGINT)"}},
		},
	}
	assert.ErrorIs(t, validateMigrationCatalog(catalog), ErrUnsafeMigration)
	for _, test := range []struct {
		name      string
		remove    int64
		include12 bool
		corrupt13 bool
		expected  error
	}{
		{name: "canonical"},
		{name: "both 12 and 13", include12: true, expected: ErrMigrationHole},
		{name: "missing prior migration", remove: 11, expected: ErrMigrationHole},
		{name: "tampered 13", corrupt13: true, expected: ErrChecksumMismatch},
	} {
		t.Run(test.name, func(t *testing.T) {
			applied := map[int64]AppliedMigration{}
			for _, item := range Plan() {
				if item.Version == test.remove || item.Version == 12 && !test.include12 {
					continue
				}
				if item.Version == 13 && test.corrupt13 {
					item.Checksum = "invalid"
				}
				applied[item.Version] = item
			}
			err := validateApplied(applied, DialectSQLite, TenantCompatibilitySchemaVersion)
			if test.expected == nil {
				require.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, test.expected)
			}
		})
	}
	for version, digest := range map[int64]string{
		11: "sha256:17961fa198d4280f89459808712cfceda4c313187580032e228c8af25934b71c",
		12: "sha256:ab50914ae132abdb57d3604e7aa0936f83b96299f2b70501a8615242ddeec1dd",
	} {
		items, ok := storedPrefixItemsForDialect(DialectPostgres, version)
		require.True(t, ok)
		assert.Equal(t, digest, migrationSetDigest(DialectPostgres, items))
	}
}

func TestTenantCompatibilityRuntimeChecksCanonicalScopeAndUniqueIndex(t *testing.T) {
	db := newMigrationTestDB(t)
	prepareTenantMigrationFixture(t, db)
	_, err := ApplyTenantCompatibilitySchema(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, db.Table("users").Where("id=101").Update("normalized_username", "legacy").Error)
	assert.ErrorIs(t, CheckRequired(context.Background(), db), ErrSchemaNotReady)
	require.NoError(t, db.Table("users").Where("id=101").Update("normalized_username", nil).Error)
	wrongUsername := "wrong"
	require.NoError(t, db.Create(&tenantV13User{ID: 201, TenantID: 1, Username: "tenant", NormalizedUsername: &wrongUsername}).Error)
	assert.ErrorIs(t, CheckRequired(context.Background(), db), ErrSchemaNotReady)
	require.NoError(t, db.Table("users").Where("id=201").Update("normalized_username", "tenant").Error)
	require.NoError(t, CheckRequired(context.Background(), db))
	require.NoError(t, db.Migrator().DropIndex(&tenantV13User{}, "idx_users_tenant_username"))
	require.NoError(t, db.Exec("CREATE INDEX idx_users_tenant_username ON users(tenant_id,username)").Error)
	assert.ErrorIs(t, CheckRequired(context.Background(), db), ErrSchemaNotReady)
	require.NoError(t, db.Migrator().DropIndex(&tenantV13User{}, "idx_users_tenant_username"))
	require.NoError(t, db.Migrator().CreateIndex(&tenantV13User{}, "idx_users_tenant_username"))
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX unexpected_global_username ON users(username)").Error)
	assert.ErrorIs(t, CheckRequired(context.Background(), db), ErrSchemaNotReady)
	require.NoError(t, db.Migrator().DropIndex("users", "unexpected_global_username"))
	require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
	observation, err := Observe(context.Background(), db)
	require.NoError(t, err)
	assert.EqualValues(t, 13, observation.CurrentVersion)
	contract, err := ContractForDialect(DialectSQLite)
	require.NoError(t, err)
	assert.Equal(t, contract.MigrationSetDigest, observation.MigrationSetDigest)
	require.NoError(t, CheckRequired(context.Background(), db))
}
