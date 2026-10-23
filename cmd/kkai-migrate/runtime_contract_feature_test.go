//go:build !kkai_bridge

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
	assert.EqualValues(t, 14, result.RuntimeMinVersion)
	assert.EqualValues(t, 17, result.RuntimeMaxVersion)
	assert.EqualValues(t, 14, result.MigrationTargetVersion)
	assert.Equal(t, result.MigrationSetDigest, result.CompatiblePrefixes["14"])
	assert.Contains(t, result.CompatiblePrefixes, "15")
	assert.Contains(t, result.CompatiblePrefixes, "16")
	assert.Contains(t, result.CompatiblePrefixes, "17")
	assert.Len(t, result.CompatiblePrefixes, 4)
	assert.Equal(t, common.SchemaManagementMode, result.SchemaManagement)
}
