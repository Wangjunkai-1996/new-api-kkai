//go:build !kkai_bridge

package kkaimigrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContractForDialectRequiresTenantSchema(t *testing.T) {
	for _, dialect := range []string{DialectPostgres, DialectSQLite, DialectMySQL} {
		t.Run(dialect, func(t *testing.T) {
			contract, err := ContractForDialect(dialect)
			require.NoError(t, err)
			assert.EqualValues(t, 14, contract.RuntimeMinVersion)
			assert.EqualValues(t, 15, contract.RuntimeMaxVersion)
			assert.EqualValues(t, 14, contract.MigrationTargetVersion)
			assert.Equal(t, MigrationKindNone, contract.MigrationKind)
			assert.Equal(t, contract.MigrationSetDigest, contract.CompatiblePrefixes["14"])
			assert.Contains(t, contract.CompatiblePrefixes, "15")
			assert.Len(t, contract.CompatiblePrefixes, 2)
		})
	}
}
func TestRuntimeRejectsV11UntilExplicitTenantMigration(t *testing.T) {
	db := newMigrationTestDB(t)
	prepareTenantMigrationFixture(t, db)
	assert.ErrorIs(t, CheckRequired(context.Background(), db), ErrSchemaNotReady)
	_, err := ApplyTenantOnlineExpansion(context.Background(), db, Options{})
	require.NoError(t, err)
	assert.ErrorIs(t, CheckRequired(context.Background(), db), ErrSchemaNotReady)
	_, err = FinalizeTenantOnlineSchema(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, CheckRequired(context.Background(), db))
	_, err = ApplyResellerApplicationExpand(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, CheckRequired(context.Background(), db), "the compatibility runtime must still start after the additive v15 receipt")
}
