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

func prepareResellerApplicationExpandFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	prepareTenantMigrationFixture(t, db)
	options := Options{Offline: db.Dialector.Name() == DialectMySQL}
	_, err := ApplyTenantOnlineExpansion(context.Background(), db, options)
	require.NoError(t, err)
	_, err = FinalizeTenantOnlineSchema(context.Background(), db, options)
	require.NoError(t, err)
}

func TestResellerApplicationExpandPreservesDataAndIsIdempotent(t *testing.T) {
	t.Run(DialectSQLite, func(t *testing.T) { assertResellerApplicationExpand(t, newMigrationTestDB(t)) })
	t.Run(DialectPostgres, func(t *testing.T) {
		assertResellerApplicationExpand(t, newImageAsyncDatabaseFixture(t, DialectPostgres))
	})
	t.Run(DialectMySQL, func(t *testing.T) {
		assertResellerApplicationExpand(t, newImageAsyncDatabaseFixture(t, DialectMySQL))
	})
}

func assertResellerApplicationExpand(t *testing.T, db *gorm.DB) {
	t.Helper()
	prepareResellerApplicationExpandFixture(t, db)
	ctx := context.Background()
	options := Options{Offline: db.Dialector.Name() == DialectMySQL}
	application := tenantV12ResellerApplication{ApplicantUserID: 101, BusinessName: "Existing", Status: "pending"}
	require.NoError(t, db.Create(&application).Error)
	var before []AppliedMigration
	require.NoError(t, db.Order("version").Find(&before).Error)
	precheck, err := PrecheckResellerApplicationExpand(ctx, db)
	require.NoError(t, err)
	assert.EqualValues(t, 14, precheck.CurrentVersion)
	assert.False(t, precheck.ColumnPresent)
	dryOptions := options
	dryOptions.DryRun = true
	dry, err := ApplyResellerApplicationExpand(ctx, db, dryOptions)
	require.NoError(t, err)
	require.Len(t, dry.Pending, 1)
	assert.False(t, db.Migrator().HasColumn("reseller_applications", "desired_domain_prefix"))
	_, err = ApplyResellerApplicationExpand(ctx, db, options)
	require.NoError(t, err)
	require.NoError(t, CheckResellerApplicationSchema(ctx, db))
	require.NoError(t, CheckTenantOnlineSchema(ctx, db))
	if RuntimeMaxVersion >= 15 {
		require.NoError(t, CheckRequired(ctx, db))
	}
	var old tenantV12ResellerApplication
	require.NoError(t, db.First(&old, application.ID).Error)
	assert.Equal(t, application.BusinessName, old.BusinessName)
	var prefix *string
	require.NoError(t, db.Table("reseller_applications").Where("id = ?", application.ID).Select("desired_domain_prefix").Scan(&prefix).Error)
	assert.Nil(t, prefix)
	precheck, err = PrecheckResellerApplicationExpand(ctx, db)
	require.NoError(t, err)
	assert.EqualValues(t, 15, precheck.CurrentVersion)
	assert.True(t, precheck.ColumnPresent)
	_, err = ApplyResellerApplicationExpand(ctx, db, options)
	require.NoError(t, err)
	var after []AppliedMigration
	require.NoError(t, db.Order("version").Find(&after).Error)
	require.Len(t, after, len(before)+1)
	assert.Equal(t, before, after[:len(before)])
	assert.EqualValues(t, 15, after[len(before)].Version)
}

func TestResellerApplicationExpandPostgresReceiptFailureRollsBackDDL(t *testing.T) {
	db := newImageAsyncDatabaseFixture(t, DialectPostgres)
	prepareResellerApplicationExpandFixture(t, db)
	interrupted := errors.New("isolated v15 receipt failure")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:interrupt-prefix-receipt", func(tx *gorm.DB) {
		if tx.Statement.Table == "kkai_schema_migrations" {
			tx.AddError(interrupted)
		}
	}))
	_, err := ApplyResellerApplicationExpand(context.Background(), db, Options{})
	assert.ErrorIs(t, err, interrupted)
	assert.False(t, db.Migrator().HasColumn("reseller_applications", "desired_domain_prefix"))
	require.NoError(t, checkThroughVersion(context.Background(), db, 14, 14, 14))
	require.NoError(t, db.Callback().Create().Remove("test:interrupt-prefix-receipt"))
	_, err = ApplyResellerApplicationExpand(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, CheckResellerApplicationSchema(context.Background(), db))
}

func TestResellerApplicationExpandRequiresCanonicalSourceAndRollsBackDeniedMutation(t *testing.T) {
	db := newMigrationTestDB(t)
	prepareTenantMigrationFixture(t, db)
	_, err := ApplyResellerApplicationExpand(context.Background(), db, Options{})
	assert.ErrorIs(t, err, ErrSchemaNotReady)
	_, err = ApplyTenantOnlineExpansion(context.Background(), db, Options{})
	require.NoError(t, err)
	_, err = FinalizeTenantOnlineSchema(context.Background(), db, Options{})
	require.NoError(t, err)
	denied := errors.New("runtime compatibility proof denied")
	_, err = ApplyResellerApplicationExpand(context.Background(), db, Options{AuthorizeMutation: func(context.Context, *gorm.DB) error { return denied }})
	assert.ErrorIs(t, err, denied)
	assert.False(t, db.Migrator().HasColumn("reseller_applications", "desired_domain_prefix"))
	require.NoError(t, CheckTenantOnlineSchema(context.Background(), db))
}

func TestResellerApplicationExpandPostgresNowaitPreservesSource(t *testing.T) {
	db := newImageAsyncDatabaseFixture(t, DialectPostgres)
	prepareResellerApplicationExpandFixture(t, db)
	blocker := db.Begin()
	require.NoError(t, blocker.Error)
	t.Cleanup(func() { _ = blocker.Rollback().Error })
	require.NoError(t, blocker.Exec("LOCK TABLE reseller_applications IN ACCESS SHARE MODE").Error)
	_, err := ApplyResellerApplicationExpand(context.Background(), db, Options{})
	var locked *pgconn.PgError
	require.ErrorAs(t, err, &locked)
	assert.Equal(t, "55P03", locked.Code)
	assert.False(t, db.Migrator().HasColumn("reseller_applications", "desired_domain_prefix"))
	require.NoError(t, checkThroughVersion(context.Background(), db, 14, 14, 14))
	require.NoError(t, blocker.Rollback().Error)
	_, err = ApplyResellerApplicationExpand(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, CheckResellerApplicationSchema(context.Background(), db))
}
