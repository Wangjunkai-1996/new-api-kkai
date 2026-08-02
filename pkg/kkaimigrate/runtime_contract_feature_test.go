//go:build !kkai_bridge

package kkaimigrate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContractForDialectUsesVideoStudioRuntime(t *testing.T) {
	for _, dialect := range []string{DialectPostgres, DialectSQLite, DialectMySQL} {
		t.Run(dialect, func(t *testing.T) {
			contract, err := ContractForDialect(dialect)
			require.NoError(t, err)
			require.EqualValues(t, 7, contract.RuntimeMinVersion)
			require.EqualValues(t, 7, contract.RuntimeMaxVersion)
			require.EqualValues(t, 7, contract.MigrationTargetVersion)
			require.Equal(t, MigrationKindNone, contract.MigrationKind)
			require.Equal(t, map[string]string{
				"7": contract.MigrationSetDigest,
			}, contract.CompatiblePrefixes)
			require.Regexp(t, `^sha256:[0-9a-f]{64}$`, contract.MigrationSetDigest)
		})
	}
}
