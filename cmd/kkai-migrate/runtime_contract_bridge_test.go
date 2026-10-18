//go:build kkai_bridge

package main

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDescribeContractJSONUsesBridgeRuntime(t *testing.T) {
	output, err := describeContractJSON("postgres")
	require.NoError(t, err)
	var result struct {
		kkaimigrate.SchemaContract
		SchemaManagement string `json:"schema_management"`
	}
	require.NoError(t, common.UnmarshalJsonStr(output, &result))
	require.EqualValues(t, 9, result.RuntimeMinVersion)
	require.EqualValues(t, 11, result.RuntimeMaxVersion)
	require.EqualValues(t, 9, result.MigrationTargetVersion)
	require.Equal(t, "none", result.MigrationKind)
	require.Equal(t, "runtime", result.SchemaManagement)
	require.Equal(t, "sha256:4e65c4c6c49ad3ce1d87e3a144df3b0a57a3f408f3323226dd81b6b7a16972c1", result.MigrationSetDigest)
	require.Equal(t, result.MigrationSetDigest, result.CompatiblePrefixes["9"])
	require.Equal(t, "sha256:89b77ac581db5794572c596f8e37dd341de8864a71446a1cbf6a61f73a2d3779", result.CompatiblePrefixes["10"])
	require.Regexp(t, `^sha256:[0-9a-f]{64}$`, result.CompatiblePrefixes["11"])
	require.NotEqual(t, result.CompatiblePrefixes["10"], result.CompatiblePrefixes["11"])
	require.Len(t, result.CompatiblePrefixes, 3)
}
