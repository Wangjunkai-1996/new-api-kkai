//go:build kkai_bridge

package kkaimigrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContractForDialectUsesBridgeRuntime(t *testing.T) {
	for _, dialect := range []string{DialectPostgres, DialectSQLite, DialectMySQL} {
		t.Run(dialect, func(t *testing.T) {
			contract, err := ContractForDialect(dialect)
			require.NoError(t, err)
			assert.EqualValues(t, 10, contract.RuntimeMinVersion)
			assert.EqualValues(t, 11, contract.RuntimeMaxVersion)
			assert.EqualValues(t, 10, contract.MigrationTargetVersion)
			assert.Equal(t, MigrationKindNone, contract.MigrationKind)
			require.Len(t, contract.CompatiblePrefixes, 2)
			assert.Equal(t, contract.MigrationSetDigest, contract.CompatiblePrefixes["10"])
			assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, contract.CompatiblePrefixes["11"])
			assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, contract.MigrationSetDigest)
		})
	}
}

func TestBridgeRuntimeAcceptsV10AndValidatesV11WithoutImplicitExpand(t *testing.T) {
	db := newMigrationTestDB(t)
	ctx := context.Background()
	_, err := Apply(ctx, db, Options{})
	require.NoError(t, err)
	require.NoError(t, CheckRequired(ctx, db))
	assert.False(t, db.Migrator().HasTable("kkai_membership_accounts"))
	before, err := Observe(ctx, db)
	require.NoError(t, err)
	assert.EqualValues(t, 10, before.CurrentVersion)

	_, err = ApplyMembershipExpand(ctx, db, Options{})
	require.NoError(t, err)
	require.NoError(t, CheckRequired(ctx, db))
	after, err := Observe(ctx, db)
	require.NoError(t, err)
	assert.EqualValues(t, 11, after.CurrentVersion)

	require.NoError(t, db.Migrator().DropTable("kkai_membership_accounts"))
	assert.ErrorIs(t, CheckRequired(ctx, db), ErrSchemaNotReady)
}
