//go:build !kkai_bridge

package kkaimigrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContractForDialectUsesImageAsyncRuntime(t *testing.T) {
	for _, dialect := range []string{DialectPostgres, DialectSQLite, DialectMySQL} {
		t.Run(dialect, func(t *testing.T) {
			contract, err := ContractForDialect(dialect)
			require.NoError(t, err)
			assert.EqualValues(t, 10, contract.RuntimeMinVersion)
			assert.EqualValues(t, 10, contract.RuntimeMaxVersion)
			assert.EqualValues(t, 10, contract.MigrationTargetVersion)
			assert.Equal(t, MigrationKindNone, contract.MigrationKind)
			assert.Equal(t, map[string]string{"10": contract.MigrationSetDigest}, contract.CompatiblePrefixes)
			assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, contract.MigrationSetDigest)
		})
	}
}

func TestFeatureRuntimeRequiresImageAsyncSchema(t *testing.T) {
	db := newMigrationTestDB(t)
	ctx := context.Background()
	_, err := applyThroughVersion(ctx, db, Options{}, RC41ArchitectureSchemaVersion, ImageAsyncSchemaVersion)
	require.NoError(t, err)
	assert.ErrorIs(t, CheckRequired(ctx, db), ErrSchemaNotReady)
	assert.False(t, db.Migrator().HasTable("kkai_image_executions"))

	_, err = ApplyImageAsyncExpand(ctx, db, Options{})
	require.NoError(t, err)
	assert.NoError(t, CheckRequired(ctx, db))
}
