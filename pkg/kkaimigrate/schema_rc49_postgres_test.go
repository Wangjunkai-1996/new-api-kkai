package kkaimigrate

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRC49RuntimeContractRequiresExpandedPostgres(t *testing.T) {
	contract, err := ContractForDialect(DialectPostgres)
	require.NoError(t, err)
	assert.Equal(t, MembershipBillingRecoverySchemaVersion, contract.RuntimeMinVersion)
	assert.Equal(t, MembershipBillingRecoverySchemaVersion, contract.RuntimeMaxVersion)
	assert.Len(t, contract.CompatiblePrefixes, 1)
	assert.Contains(t, contract.CompatiblePrefixes, "21")
	assert.Equal(t, map[string]string{RC49OnlineExpansion: RC49OnlineExpansionDigest()}, contract.RequiredExpansions)
	for _, dialect := range []string{DialectSQLite, DialectMySQL} {
		_, err := ContractForDialect(dialect)
		require.Error(t, err, "these builds do not provide rc49 support for other dialects")
	}
}

func TestRC49PostgresColumnsPreserveLegacyPermissionAndCounters(t *testing.T) {
	db := newShopSchemaDatabaseFixture(t, DialectPostgres)
	require.NoError(t, db.Exec("CREATE TABLE quota_data (id BIGINT PRIMARY KEY)").Error)
	require.NoError(t, db.Exec("INSERT INTO tokens (id) VALUES (1)").Error)
	require.NoError(t, db.Exec("INSERT INTO quota_data (id) VALUES (1)").Error)
	require.NoError(t, validateRC49Schema(db, false))
	require.ErrorIs(t, validateRC49Schema(db, true), ErrSchemaNotReady)
	item := migrationSet()[RC49SchemaVersion-1]
	require.NoError(t, executeMigrationSchema(db, DialectPostgres, item))
	require.NoError(t, validateRC49Schema(db, true))
	var allowed bool
	require.NoError(t, db.Table("tokens").Select("allow_user_quota_query").Where("id = 1").Scan(&allowed).Error)
	assert.False(t, allowed)
	const cacheTokens int64 = 1 << 40
	require.NoError(t, db.Exec("UPDATE quota_data SET cache_read_tokens=?, cache_creation_tokens=? WHERE id=1", cacheTokens, cacheTokens+1).Error)
	require.NoError(t, executeMigrationSchema(db, DialectPostgres, item))
	var counters struct {
		CacheReadTokens     int64
		CacheCreationTokens int64
	}
	require.NoError(t, db.Table("quota_data").Where("id = 1").Take(&counters).Error)
	assert.Equal(t, cacheTokens, counters.CacheReadTokens)
	assert.Equal(t, cacheTokens+1, counters.CacheCreationTokens)
	require.NoError(t, db.Exec("ALTER TABLE tokens ALTER COLUMN allow_user_quota_query SET DEFAULT TRUE").Error)
	require.ErrorIs(t, validateRC49Schema(db, true), ErrSchemaNotReady)
}

func TestRC49AuditExpansionChecksActualLogDatabase(t *testing.T) {
	mainDB := newShopSchemaDatabaseFixture(t, DialectPostgres)
	logDB := newShopSchemaDatabaseFixture(t, DialectPostgres)
	ctx := context.Background()
	require.NoError(t, logDB.Exec("CREATE TABLE audit_logs (created_at BIGINT NOT NULL, event_id VARCHAR(64) NOT NULL)").Error)
	require.ErrorIs(t, ApplyRC49LogExpand(ctx, logDB, Options{}), ErrUnsafeMigration)
	require.NoError(t, ApplyRC49LogExpand(ctx, logDB, Options{DryRun: true}))
	require.ErrorIs(t, CheckRC49LogSchema(ctx, logDB), ErrSchemaNotReady)
	require.NoError(t, ApplyRC49LogExpand(ctx, logDB, Options{Offline: true}))
	require.NoError(t, CheckRC49LogSchema(ctx, logDB))
	require.ErrorIs(t, CheckRC49LogSchema(ctx, mainDB), ErrSchemaNotReady)
	require.NoError(t, ApplyRC49LogExpand(ctx, logDB, Options{Offline: true}))
	require.NoError(t, logDB.Exec("DROP INDEX idx_audit_time_event").Error)
	require.NoError(t, logDB.Exec("CREATE INDEX idx_audit_time_event ON audit_logs (event_id, created_at)").Error)
	require.ErrorIs(t, CheckRC49LogSchema(ctx, logDB), ErrSchemaNotReady)
}

func TestRC49ExpansionRequiresAuthorizationAndPreservesCanonicalHistory(t *testing.T) {
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
	var before []AppliedMigration
	require.NoError(t, db.Order("version").Find(&before).Error)
	_, err = ApplyRC49Expand(ctx, db, Options{})
	require.ErrorIs(t, err, ErrUnsafeMigration)
	denied := errors.New("controller authorization expired")
	calls := 0
	_, err = ApplyRC49Expand(ctx, db, Options{AuthorizeMutation: func(context.Context, *gorm.DB) error {
		calls++
		if calls == 2 {
			return denied
		}
		return nil
	}})
	require.ErrorIs(t, err, denied)
	assert.Equal(t, 2, calls)
	require.False(t, db.Migrator().HasColumn("tokens", "allow_user_quota_query"))
	plan, err := ApplyRC49Expand(ctx, db, Options{DryRun: true})
	require.NoError(t, err)
	require.Len(t, plan.Pending, 1)
	assert.Equal(t, RC49SchemaVersion, plan.Pending[0].Version)
	_, err = ApplyRC49Expand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	require.ErrorIs(t, CheckRequired(ctx, db), ErrSchemaNotReady, "canonical22 finalization is not part of the online rc49 runtime contract")
	assert.ErrorIs(t, checkThroughVersion(ctx, db, 14, 14, 21), ErrFutureMigration, "previous runtime cannot roll back this ledger")
	var after []AppliedMigration
	require.NoError(t, db.Order("version").Find(&after).Error)
	require.Len(t, after, len(before)+1)
	assert.Equal(t, before, after[:len(before)])
	_, err = ApplyRC49Expand(ctx, db, Options{Offline: true})
	require.NoError(t, err)
	_, err = ApplyRC49Expand(ctx, db, Options{AuthorizeMutation: func(context.Context, *gorm.DB) error { return denied }})
	require.ErrorIs(t, err, denied, "completed expansion still requires current authorization")
}
