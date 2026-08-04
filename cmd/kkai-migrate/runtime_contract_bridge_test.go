//go:build kkai_bridge

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDescribeContractJSONUsesBridgeRuntime(t *testing.T) {
	output, err := describeContractJSON("postgres")
	require.NoError(t, err)
	require.JSONEq(t, `{"compatible_prefixes":{"7":"sha256:d0779962929f5f47a608fa83adec278bcadf4458507d6dc36bb4503878cde15e","8":"sha256:826caefa93619049351ef9f32cd259c9089ffb02a0869ab14cedc302738af71a"},"migration_kind":"none","migration_set_digest":"sha256:d0779962929f5f47a608fa83adec278bcadf4458507d6dc36bb4503878cde15e","migration_target_version":7,"runtime_max_version":8,"runtime_min_version":7,"schema_management":"runtime"}`, output)
}
