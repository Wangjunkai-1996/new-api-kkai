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

func TestRC49OnlineRequiresGenuineOldOrExpansionAwareRuntime(t *testing.T) {
	prefixes, err := compatiblePrefixDigests(DialectPostgres, 14, 21)
	require.NoError(t, err)
	legacy := SchemaContract{RuntimeMinVersion: 14, RuntimeMaxVersion: 21, MigrationTargetVersion: 14,
		MigrationKind: MigrationKindNone, MigrationSetDigest: prefixes["14"], CompatiblePrefixes: prefixes}
	require.NoError(t, ValidateRC49OnlineRuntimeContract(legacy))
	feature, err := ContractForDialect(DialectPostgres)
	require.NoError(t, err)
	require.NoError(t, ValidateRC49OnlineRuntimeContract(feature))
	feature.RequiredExpansions = nil
	assert.Error(t, ValidateRC49OnlineRuntimeContract(feature), "a version-only claim cannot substitute for an expansion-aware runtime")
	delete(legacy.CompatiblePrefixes, "20")
	assert.Error(t, ValidateRC49OnlineRuntimeContract(legacy), "the entire frozen old contract must match")
}

func newRC49OnlineFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db := newShopSchemaDatabaseFixture(t, DialectPostgres)
	ctx := context.Background()
	options := prepareRequestTicketExpandFixture(t, db)
	_, err := ApplyRequestTicketExpand(ctx, db, options)
	require.NoError(t, err)
	_, err = ApplyInvoiceExpand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	for _, ddl := range []string{"ALTER TABLE top_ups ADD COLUMN user_id BIGINT NULL", "ALTER TABLE top_ups ADD COLUMN create_time BIGINT NULL"} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	for _, apply := range []func(context.Context, *gorm.DB, Options) (*Result, error){ApplyWalletPaymentExpand, ApplyShopExpand, ApplyMembershipPaymentExpand, ApplyMembershipBillingRecoveryExpand} {
		_, err = apply(ctx, db, Options{Offline: true})
		require.NoError(t, err)
	}
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	return db
}

func TestRC49OnlineExpansionPreservesCanonical21AndOldWrites(t *testing.T) {
	db := newRC49OnlineFixture(t)
	ctx := context.Background()
	var before []AppliedMigration
	require.NoError(t, db.Order("version").Find(&before).Error)
	status, err := PlanRC49OnlineExpansion(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, "pristine", status.State)
	assert.Equal(t, "sha256:84687631f06a71630ad6035a28ce29c91bd24fb5cf824e72e394b7f8fc0680f0", status.SourceDigest)
	assert.Equal(t, "ff1dbaacac62dd31c931ecb62a497c0dc16be5dbba6e46538ec427739bb7db4e", status.PlanChecksum)
	assert.ErrorIs(t, CheckRequired(ctx, db), ErrSchemaNotReady)
	_, err = ApplyRC49OnlineExpansion(ctx, db, Options{})
	require.ErrorIs(t, err, ErrUnsafeMigration)
	_, err = ApplyRC49OnlineExpansion(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, CheckRequired(ctx, db))
	require.NoError(t, CheckRC49LogSchema(ctx, db))
	require.NoError(t, checkThroughVersion(ctx, db, 14, 14, 21), "the old runtime still accepts the canonical ledger and extra columns")
	var after []AppliedMigration
	require.NoError(t, db.Order("version").Find(&after).Error)
	assert.Equal(t, before, after, "online expansion must not create or rewrite any canonical receipt")
	require.NoError(t, db.Exec("INSERT INTO tokens (id) VALUES (9001)").Error)
	require.NoError(t, db.Exec("INSERT INTO quota_data (id) VALUES (9001)").Error)
	var allowed bool
	require.NoError(t, db.Table("tokens").Select("allow_user_quota_query").Where("id=9001").Scan(&allowed).Error)
	assert.False(t, allowed, "old binaries create denied tokens using the new column default")
	var counters struct{ CacheReadTokens, CacheCreationTokens int64 }
	require.NoError(t, db.Table("quota_data").Where("id=9001").Take(&counters).Error)
	assert.Zero(t, counters)
	observation, err := Observe(ctx, db)
	require.NoError(t, err)
	assert.EqualValues(t, 21, observation.CurrentVersion)
	assert.Equal(t, map[string]string{RC49OnlineExpansion: RC49OnlineExpansionDigest()}, observation.Expansions)
	_, err = RepairRC49OnlineIndex(ctx, db, Options{Offline: true})
	require.ErrorIs(t, err, ErrUnsafeMigration, "repair must not remove a valid completed index")
	_, err = ApplyRC49OnlineExpansion(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, db.Model(&rc49OnlineReceipt{}).Where("step=?", "columns").Update("plan_checksum", "wrong").Error)
	assert.ErrorIs(t, CheckRequired(ctx, db), ErrChecksumMismatch)
	require.NoError(t, db.Model(&rc49OnlineReceipt{}).Where("step=?", "columns").Update("plan_checksum", RC49OnlinePlanChecksum()).Error)
	require.NoError(t, db.Exec("ALTER TABLE kkai_rc49_online_steps DROP CONSTRAINT kkai_rc49_online_steps_pkey").Error)
	assert.ErrorIs(t, CheckRequired(ctx, db), ErrSchemaNotReady, "receipt contents alone cannot replace its identity constraint")
}

func TestRC49OnlineColumnsFailAtomicallyWithoutQueuingTraffic(t *testing.T) {
	db := newRC49OnlineFixture(t)
	ctx := context.Background()
	holder := db.Begin()
	require.NoError(t, holder.Error)
	t.Cleanup(func() { _ = holder.Rollback().Error })
	require.NoError(t, holder.Exec("LOCK TABLE tokens IN ACCESS SHARE MODE").Error)
	_, err := ApplyRC49OnlineExpansion(ctx, db, Options{Offline: true})
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "55P03", pgErr.Code, "NOWAIT must fail instead of queuing a writer-blocking lock")
	require.NoError(t, holder.Rollback().Error)
	assert.False(t, db.Migrator().HasTable(&rc49OnlineReceipt{}))
	assert.False(t, db.Migrator().HasColumn("tokens", "allow_user_quota_query"))

	denied := errors.New("authorization expired before column commit")
	_, err = ApplyRC49OnlineExpansion(ctx, db, Options{AuthorizeMutation: func(_ context.Context, tx *gorm.DB) error {
		if tx.Migrator().HasColumn("tokens", "allow_user_quota_query") {
			return denied
		}
		return nil
	}})
	require.ErrorIs(t, err, denied)
	assert.False(t, db.Migrator().HasTable(&rc49OnlineReceipt{}))
	assert.False(t, db.Migrator().HasColumn("tokens", "allow_user_quota_query"))
	assert.False(t, db.Migrator().HasColumn("quota_data", "cache_creation_tokens"))
	require.NoError(t, CheckMembershipBillingRecoverySchema(ctx, db))
	require.NoError(t, db.Exec(rc49AuditIndexSQL).Error)
	_, err = PlanRC49OnlineExpansion(ctx, db)
	require.ErrorIs(t, err, ErrSchemaNotReady, "a preexisting index cannot be adopted without this plan's ownership receipt")
	_, err = RepairRC49OnlineIndex(ctx, db, Options{Offline: true})
	require.ErrorIs(t, err, ErrSchemaNotReady)
	require.True(t, db.Migrator().HasIndex("audit_logs", "idx_audit_time_event"), "repair cannot remove an unowned index")
}

func TestRC49ConcurrentIndexFailureRequiresOwnedRepair(t *testing.T) {
	db := newRC49OnlineFixture(t)
	ctx := context.Background()
	writer := db.Begin()
	require.NoError(t, writer.Error)
	t.Cleanup(func() { _ = writer.Rollback().Error })
	// An ordinary writer may remain active while CREATE INDEX CONCURRENTLY
	// starts, but prevents its completion. The bounded deadline leaves a real
	// PostgreSQL invalid index, exercising the actual recovery path.
	require.NoError(t, writer.Exec("LOCK TABLE audit_logs IN ROW EXCLUSIVE MODE").Error)
	_, err := ApplyRC49OnlineExpansion(ctx, db, Options{AuthorizeMutation: func(_ context.Context, locked *gorm.DB) error {
		if locked.Migrator().HasTable(&rc49OnlineReceipt{}) {
			var count int64
			if err := locked.Model(&rc49OnlineReceipt{}).Where("step=?", "audit_index").Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return locked.Exec("SET statement_timeout='100ms'").Error
			}
		}
		return nil
	}})
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "57014", pgErr.Code)
	require.NoError(t, writer.Rollback().Error)
	status, err := PlanRC49OnlineExpansion(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, "index_invalid", status.State)
	assert.False(t, status.Safe)
	require.NoError(t, checkThroughVersion(ctx, db, 14, 14, 21), "interrupted index work must preserve the old application")
	assert.ErrorIs(t, CheckRequired(ctx, db), ErrSchemaNotReady)
	_, err = ApplyRC49OnlineExpansion(ctx, db, Options{Offline: true})
	require.ErrorIs(t, err, ErrSchemaNotReady)
	_, err = RepairRC49OnlineIndex(ctx, db, Options{})
	require.ErrorIs(t, err, ErrUnsafeMigration)
	status, err = RepairRC49OnlineIndex(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	assert.Equal(t, "index_pending", status.State)
	_, err = ApplyRC49OnlineExpansion(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	require.NoError(t, CheckRequired(ctx, db))
}
