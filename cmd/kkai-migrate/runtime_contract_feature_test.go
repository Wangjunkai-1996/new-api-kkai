//go:build !kkai_bridge

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDescribeContractJSONUsesFeatureRuntime(t *testing.T) {
	output, err := describeContractJSON("postgres")
	require.NoError(t, err)
	require.JSONEq(t, `{"compatible_prefixes":{"7":"sha256:d0779962929f5f47a608fa83adec278bcadf4458507d6dc36bb4503878cde15e"},"migration_kind":"none","migration_set_digest":"sha256:d0779962929f5f47a608fa83adec278bcadf4458507d6dc36bb4503878cde15e","migration_target_version":7,"runtime_max_version":7,"runtime_min_version":7,"schema_management":"runtime"}`, output)
}
