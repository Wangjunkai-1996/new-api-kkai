//go:build !kkai_bridge

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDescribeContractJSONUsesFeatureRuntime(t *testing.T) {
	output, err := describeContractJSON("postgres")
	require.NoError(t, err)
	require.JSONEq(t, `{"compatible_prefixes":{"8":"sha256:46d3b075a5b59bd3f220536f3a5782d8cea21ce5c2758083df837d717595f2ef"},"migration_kind":"none","migration_set_digest":"sha256:46d3b075a5b59bd3f220536f3a5782d8cea21ce5c2758083df837d717595f2ef","migration_target_version":8,"runtime_max_version":8,"runtime_min_version":8,"schema_management":"runtime"}`, output)
}
