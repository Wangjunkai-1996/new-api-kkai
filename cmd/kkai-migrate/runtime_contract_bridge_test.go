//go:build kkai_bridge

package main

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDescribeContractJSONRequiresTenantSchema(t *testing.T) {
	output, err := describeContractJSON("postgres")
	require.NoError(t, err)
	var result struct {
		kkaimigrate.SchemaContract
		SchemaManagement string `json:"schema_management"`
	}
	require.NoError(t, common.UnmarshalJsonStr(output, &result))
	assert.EqualValues(t, 21, result.RuntimeMinVersion)
	assert.EqualValues(t, 21, result.RuntimeMaxVersion)
	assert.EqualValues(t, 21, result.MigrationTargetVersion)
	assert.Equal(t, result.MigrationSetDigest, result.CompatiblePrefixes["21"])
	assert.Len(t, result.CompatiblePrefixes, 1)
	assert.Equal(t, map[string]string{kkaimigrate.RC49OnlineExpansion: kkaimigrate.RC49OnlineExpansionDigest()}, result.RequiredExpansions)
	assert.Equal(t, common.SchemaManagementMode, result.SchemaManagement)
}
