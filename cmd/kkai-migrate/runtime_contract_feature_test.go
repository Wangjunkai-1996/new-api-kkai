//go:build !kkai_bridge

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescribeContractJSONUsesFeatureRuntime(t *testing.T) {
	output, err := describeContractJSON("postgres")
	require.NoError(t, err)
	assert.JSONEq(t, `{"compatible_prefixes":{"10":"sha256:89b77ac581db5794572c596f8e37dd341de8864a71446a1cbf6a61f73a2d3779"},"migration_kind":"none","migration_set_digest":"sha256:89b77ac581db5794572c596f8e37dd341de8864a71446a1cbf6a61f73a2d3779","migration_target_version":10,"runtime_max_version":10,"runtime_min_version":10,"schema_management":"runtime"}`, output)
}
