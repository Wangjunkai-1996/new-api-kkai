//go:build !kkai_bridge

package kkaimigrate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContractForDialectUsesImageAsyncPreparationRuntime(t *testing.T) {
	for _, dialect := range []string{DialectPostgres, DialectSQLite, DialectMySQL} {
		t.Run(dialect, func(t *testing.T) {
			contract, err := ContractForDialect(dialect)
			require.NoError(t, err)
			require.EqualValues(t, 9, contract.RuntimeMinVersion)
			require.EqualValues(t, 10, contract.RuntimeMaxVersion)
			require.EqualValues(t, 9, contract.MigrationTargetVersion)
			require.Equal(t, MigrationKindNone, contract.MigrationKind)
			require.Len(t, contract.CompatiblePrefixes, 2)
			require.Equal(t, contract.MigrationSetDigest, contract.CompatiblePrefixes["9"])
			require.Regexp(t, `^sha256:[0-9a-f]{64}$`, contract.CompatiblePrefixes["10"])
			require.Regexp(t, `^sha256:[0-9a-f]{64}$`, contract.MigrationSetDigest)
		})
	}
}
