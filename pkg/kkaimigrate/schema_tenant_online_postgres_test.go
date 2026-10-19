package kkaimigrate

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTenantOnlinePostgresExpansionAndContract(t *testing.T) {
	assertTenantOnlineMigration(t, newImageAsyncDatabaseFixture(t, DialectPostgres))
}

func TestTenantOnlinePostgresNowaitStopsAndResumes(t *testing.T) {
	db := newImageAsyncDatabaseFixture(t, DialectPostgres)
	prepareTenantMigrationFixture(t, db)
	blocker := db.Begin()
	require.NoError(t, blocker.Error)
	t.Cleanup(func() { _ = blocker.Rollback().Error })
	require.NoError(t, blocker.Exec("LOCK TABLE users IN ACCESS SHARE MODE").Error)
	_, err := ApplyTenantOnlineExpansion(context.Background(), db, Options{})
	var lockError *pgconn.PgError
	require.ErrorAs(t, err, &lockError)
	assert.Equal(t, "55P03", lockError.Code)
	assert.Contains(t, lockError.Message, "could not obtain lock", "NOWAIT must not enqueue an exclusive lock behind online traffic")
	assert.False(t, db.Migrator().HasColumn("users", "tenant_id"))
	assert.False(t, db.Migrator().HasColumn("tokens", "tenant_id"))
	require.NoError(t, blocker.Rollback().Error)
	_, err = ApplyTenantOnlineExpansion(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, CheckTenantOnlineExpansion(context.Background(), db))
	keys, err := tenantGlobalUsernameUniqueKeys(db)
	require.NoError(t, err)
	assert.NotEmpty(t, keys)
}

func TestTenantOnlinePostgresContractNowaitPreservesLegacyKeysAndLedger(t *testing.T) {
	db := newImageAsyncDatabaseFixture(t, DialectPostgres)
	prepareTenantMigrationFixture(t, db)
	ctx := context.Background()
	_, err := ApplyTenantOnlineExpansion(ctx, db, Options{})
	require.NoError(t, err)
	blocker := db.Begin()
	require.NoError(t, blocker.Error)
	t.Cleanup(func() { _ = blocker.Rollback().Error })
	require.NoError(t, blocker.Exec("LOCK TABLE users IN ACCESS SHARE MODE").Error)
	_, err = FinalizeTenantOnlineSchema(ctx, db, Options{})
	var lockError *pgconn.PgError
	require.ErrorAs(t, err, &lockError)
	assert.Equal(t, "55P03", lockError.Code)
	assert.Contains(t, lockError.Message, "could not obtain lock")
	keys, err := tenantGlobalUsernameUniqueKeys(db)
	require.NoError(t, err)
	assert.NotEmpty(t, keys)
	require.NoError(t, checkThroughVersion(ctx, db, 11, 11, 11))
	require.NoError(t, blocker.Rollback().Error)
	_, err = FinalizeTenantOnlineSchema(ctx, db, Options{})
	require.NoError(t, err)
	require.NoError(t, CheckTenantOnlineSchema(ctx, db))
}

func TestTenantOnlinePostgresInvalidIndexIsNeverAdoptedOrDropped(t *testing.T) {
	db := newImageAsyncDatabaseFixture(t, DialectPostgres)
	prepareTenantMigrationFixture(t, db)
	_, err := ApplyTenantOnlineExpansion(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("INSERT INTO tokens(id) VALUES(1),(2)").Error)
	require.NoError(t, db.Exec("DROP INDEX idx_tokens_tenant_id").Error)
	err = db.Exec("CREATE UNIQUE INDEX CONCURRENTLY idx_tokens_tenant_id ON tokens(tenant_id)").Error
	var indexError *pgconn.PgError
	require.ErrorAs(t, err, &indexError)
	assert.Equal(t, "23505", indexError.Code)
	assert.ErrorIs(t, CheckTenantOnlineExpansion(context.Background(), db), ErrSchemaNotReady)
	_, err = ApplyTenantOnlineExpansion(context.Background(), db, Options{})
	assert.ErrorIs(t, err, ErrSchemaNotReady)
	var valid bool
	require.NoError(t, db.Raw("SELECT indisvalid FROM pg_index WHERE indexrelid=to_regclass('idx_tokens_tenant_id')").Scan(&valid).Error)
	assert.False(t, valid)
	assert.True(t, db.Migrator().HasIndex("tokens", "idx_tokens_tenant_id"), "a failed step must never silently delete an invalid artifact")
}

func TestTenantOnlinePostgresSharesMigrationLockAndRestoresSession(t *testing.T) {
	db := newImageAsyncDatabaseFixture(t, DialectPostgres)
	prepareTenantMigrationFixture(t, db)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	blocker, err := sqlDB.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Close() })
	_, err = blocker.ExecContext(context.Background(), "SELECT pg_advisory_lock(hashtext('kkai_schema_migrations'))")
	require.NoError(t, err)
	_, err = ApplyTenantOnlineExpansion(context.Background(), db, Options{})
	assert.ErrorIs(t, err, ErrUnsafeMigration)
	assert.False(t, db.Migrator().HasTable(&tenantOnlineReceipt{}))
	_, err = blocker.ExecContext(context.Background(), "SELECT pg_advisory_unlock(hashtext('kkai_schema_migrations'))")
	require.NoError(t, err)
	require.NoError(t, blocker.Close())
	// A single pooled connection makes session setting restoration observable.
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.Exec("SET lock_timeout='7s'").Error)
	require.NoError(t, db.Exec("SET statement_timeout='9s'").Error)
	require.NoError(t, withTenantOnlineLock(context.Background(), db, false, func(locked *gorm.DB) error {
		var value string
		require.NoError(t, locked.Raw("SHOW lock_timeout").Scan(&value).Error)
		assert.Equal(t, "1s", value)
		return nil
	}))
	for setting, expected := range map[string]string{"lock_timeout": "7s", "statement_timeout": "9s"} {
		var value string
		require.NoError(t, db.Raw("SHOW "+setting).Scan(&value).Error)
		assert.Equal(t, expected, value)
	}
}

func TestTenantOnlinePostgresRejectsColumnContractDrift(t *testing.T) {
	for _, statement := range []string{
		"ALTER TABLE top_ups ALTER COLUMN currency DROP DEFAULT",
		"ALTER TABLE top_ups ALTER COLUMN currency TYPE VARCHAR(9)",
		"ALTER TABLE users ALTER COLUMN normalized_username SET DEFAULT 'unexpected'",
		"ALTER TABLE tenants DROP CONSTRAINT tenants_pkey",
	} {
		t.Run(statement, func(t *testing.T) {
			db := newImageAsyncDatabaseFixture(t, DialectPostgres)
			prepareTenantMigrationFixture(t, db)
			_, err := ApplyTenantOnlineExpansion(context.Background(), db, Options{})
			require.NoError(t, err)
			require.NoError(t, db.Exec(statement).Error)
			assert.ErrorIs(t, CheckTenantOnlineExpansion(context.Background(), db), ErrSchemaNotReady)
		})
	}
}

func TestTenantOnlineMySQLExplicitOfflineMigration(t *testing.T) {
	db := newImageAsyncDatabaseFixture(t, DialectMySQL)
	prepareTenantMigrationFixture(t, db)
	_, err := ApplyTenantOnlineExpansion(context.Background(), db, Options{})
	assert.ErrorIs(t, err, ErrUnsupportedDialect)
	assert.False(t, db.Migrator().HasTable(&tenantOnlineReceipt{}))
	_, err = ApplyTenantOnlineExpansion(context.Background(), db, Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, CheckTenantOnlineExpansion(context.Background(), db))
	// MySQL commits DROP INDEX independently. Simulate losing the process at
	// the next ledger write, then resume through the same public library call.
	interrupted := errors.New("isolated contract ledger write interrupted")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:interrupt-online-contract", func(tx *gorm.DB) {
		if tx.Statement.Table == "kkai_schema_migrations" {
			tx.AddError(interrupted)
		}
	}))
	_, err = FinalizeTenantOnlineSchema(context.Background(), db, Options{Offline: true})
	assert.ErrorIs(t, err, interrupted)
	require.NoError(t, db.Callback().Create().Remove("test:interrupt-online-contract"))
	keys, err := tenantGlobalUsernameUniqueKeys(db)
	require.NoError(t, err)
	assert.Empty(t, keys)
	var finalCount int64
	require.NoError(t, db.Model(&AppliedMigration{}).Where("version=14").Count(&finalCount).Error)
	assert.Zero(t, finalCount)
	_, err = FinalizeTenantOnlineSchema(context.Background(), db, Options{Offline: true})
	require.NoError(t, err)
	assertTenantOnlineAggregateStorage(t, db)
	require.NoError(t, CheckTenantOnlineSchema(context.Background(), db))
}

func TestTenantOnlineLegacyPostgresExplicitOfflineMigration(t *testing.T) {
	db := newImageAsyncDatabaseFixture(t, DialectPostgres)
	var version int
	require.NoError(t, db.Raw("SELECT current_setting('server_version_num')::int").Scan(&version).Error)
	if version >= 120000 {
		t.Skip("requires a PostgreSQL <12 fixture")
	}
	prepareTenantMigrationFixture(t, db)
	_, err := ApplyTenantOnlineExpansion(context.Background(), db, Options{})
	assert.ErrorIs(t, err, ErrUnsafeMigration)
	assert.False(t, db.Migrator().HasTable(&tenantOnlineReceipt{}))
	_, err = ApplyTenantOnlineExpansion(context.Background(), db, Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, CheckTenantOnlineExpansion(context.Background(), db))
	_, err = FinalizeTenantOnlineSchema(context.Background(), db, Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, CheckTenantOnlineSchema(context.Background(), db))
}
