//go:build !kkai_bridge

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDescribeContractJSONUsesFeatureRuntime(t *testing.T) {
	output, err := describeContractJSON("postgres")
	require.NoError(t, err)
	require.JSONEq(t, `{"compatible_prefixes":{"9":"sha256:4e65c4c6c49ad3ce1d87e3a144df3b0a57a3f408f3323226dd81b6b7a16972c1"},"migration_kind":"none","migration_set_digest":"sha256:4e65c4c6c49ad3ce1d87e3a144df3b0a57a3f408f3323226dd81b6b7a16972c1","migration_target_version":9,"runtime_max_version":9,"runtime_min_version":9,"schema_management":"runtime"}`, output)
}
