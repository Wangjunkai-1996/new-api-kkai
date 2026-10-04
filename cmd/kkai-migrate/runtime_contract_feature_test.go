//go:build !kkai_bridge

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDescribeContractJSONUsesFeatureRuntime(t *testing.T) {
	output, err := describeContractJSON("postgres")
	require.NoError(t, err)
	require.JSONEq(t, `{"compatible_prefixes":{"9":"sha256:cc1fd8e06a943a01531257e66929183734bdb1377f2584f0f57ab66f7c991034"},"migration_kind":"none","migration_set_digest":"sha256:cc1fd8e06a943a01531257e66929183734bdb1377f2584f0f57ab66f7c991034","migration_target_version":9,"runtime_max_version":9,"runtime_min_version":9,"schema_management":"runtime"}`, output)
}
