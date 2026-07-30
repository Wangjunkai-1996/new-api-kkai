//go:build !kkai_bridge

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDescribeContractJSONUsesFeatureRuntime(t *testing.T) {
	output, err := describeContractJSON("postgres")
	require.NoError(t, err)
	require.JSONEq(t, `{"compatible_prefixes":{"6":"sha256:226cdc461de7e456de91fd2d6b23a046a2c62c19cf5a8b30fd8559cb753b27ed"},"migration_kind":"none","migration_set_digest":"sha256:226cdc461de7e456de91fd2d6b23a046a2c62c19cf5a8b30fd8559cb753b27ed","migration_target_version":6,"runtime_max_version":6,"runtime_min_version":6,"schema_management":"runtime"}`, output)
}
