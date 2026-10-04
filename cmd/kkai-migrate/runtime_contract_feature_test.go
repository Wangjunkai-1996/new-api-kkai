//go:build !kkai_bridge

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDescribeContractJSONUsesFeatureRuntime(t *testing.T) {
	output, err := describeContractJSON("postgres")
	require.NoError(t, err)
	require.JSONEq(t, `{"compatible_prefixes":{"9":"sha256:bbe53e4809b059aceb0e7fe0c5ab83f86143878f28471fedacb842d39b33fa03"},"migration_kind":"none","migration_set_digest":"sha256:bbe53e4809b059aceb0e7fe0c5ab83f86143878f28471fedacb842d39b33fa03","migration_target_version":9,"runtime_max_version":9,"runtime_min_version":9,"schema_management":"runtime"}`, output)
}
