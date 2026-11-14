//go:build !kkai_bridge

package kkaimigrate

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestContractForDialectRequiresRC49Schema(t *testing.T) {
	contract, err := ContractForDialect(DialectPostgres)
	require.NoError(t, err)
	assert.EqualValues(t, 21, contract.RuntimeMinVersion)
	assert.EqualValues(t, 21, contract.RuntimeMaxVersion)
	assert.EqualValues(t, 21, contract.MigrationTargetVersion)
	assert.Equal(t, MigrationKindNone, contract.MigrationKind)
	assert.Equal(t, contract.MigrationSetDigest, contract.CompatiblePrefixes["21"])
	assert.Len(t, contract.CompatiblePrefixes, 1, "canonical21 remains the only accepted ledger")
	assert.Equal(t, map[string]string{RC49OnlineExpansion: RC49OnlineExpansionDigest(), VideoMediaMetadataExpansion: VideoMediaMetadataExpansionDigest()}, contract.RequiredExpansions)
	for _, dialect := range []string{DialectSQLite, DialectMySQL} {
		_, err := ContractForDialect(dialect)
		require.Error(t, err)
	}
}
