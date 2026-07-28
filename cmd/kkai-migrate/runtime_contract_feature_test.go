//go:build !kkai_bridge

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDescribeContractJSONUsesFeatureRuntime(t *testing.T) {
	output, err := describeContractJSON("postgres")
	require.NoError(t, err)
	require.JSONEq(t, `{"compatible_prefixes":{"5":"sha256:c15230067aa89899923d1ad81f9e31f0c8e56a5113869535723bb4eea5e2d3ff"},"migration_kind":"none","migration_set_digest":"sha256:c15230067aa89899923d1ad81f9e31f0c8e56a5113869535723bb4eea5e2d3ff","migration_target_version":5,"runtime_max_version":5,"runtime_min_version":5,"schema_management":"runtime"}`, output)
}
