//go:build kkai_bridge

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
			assert.EqualValues(t, 13, contract.RuntimeMinVersion)
			assert.EqualValues(t, 13, contract.RuntimeMaxVersion)
			assert.EqualValues(t, 13, contract.MigrationTargetVersion)
			assert.Equal(t, MigrationKindNone, contract.MigrationKind)
			assert.Equal(t, map[string]string{"13": contract.MigrationSetDigest}, contract.CompatiblePrefixes)
		})
	}
}
func TestRuntimeRejectsV11UntilExplicitTenantMigration(t *testing.T) {
	db := newMigrationTestDB(t)
	prepareTenantMigrationFixture(t, db)
	assert.ErrorIs(t, CheckRequired(context.Background(), db), ErrSchemaNotReady)
	_, err := ApplyTenantCompatibilitySchema(context.Background(), db, Options{})
	require.NoError(t, err)
	require.NoError(t, CheckRequired(context.Background(), db))
}
