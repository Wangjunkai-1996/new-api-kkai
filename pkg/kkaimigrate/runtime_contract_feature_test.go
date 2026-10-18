//go:build !kkai_bridge

package kkaimigrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContractForDialectUsesMembershipRuntime(t *testing.T) {
	for _, dialect := range []string{DialectPostgres, DialectSQLite, DialectMySQL} {
		t.Run(dialect, func(t *testing.T) {
			contract, err := ContractForDialect(dialect)
			require.NoError(t, err)
			assert.EqualValues(t, 11, contract.RuntimeMinVersion)
			assert.EqualValues(t, 11, contract.RuntimeMaxVersion)
			assert.EqualValues(t, 11, contract.MigrationTargetVersion)
			assert.Equal(t, MigrationKindNone, contract.MigrationKind)
			assert.Equal(t, map[string]string{"11": contract.MigrationSetDigest}, contract.CompatiblePrefixes)
			assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, contract.MigrationSetDigest)
		})
	}
}

func TestFeatureRuntimeRequiresMembershipSchema(t *testing.T) {
	db := newMigrationTestDB(t)
	ctx := context.Background()
	_, err := applyThroughVersion(ctx, db, Options{}, ImageAsyncSchemaVersion, MembershipSchemaVersion)
	require.NoError(t, err)
	assert.ErrorIs(t, CheckRequired(ctx, db), ErrSchemaNotReady)
	assert.False(t, db.Migrator().HasTable("kkai_membership_accounts"))

	_, err = ApplyMembershipExpand(ctx, db, Options{})
	require.NoError(t, err)
	require.NoError(t, CheckRequired(ctx, db))

	require.NoError(t, db.Migrator().DropTable("kkai_membership_accounts"))
	assert.ErrorIs(t, CheckRequired(ctx, db), ErrSchemaNotReady)
}
