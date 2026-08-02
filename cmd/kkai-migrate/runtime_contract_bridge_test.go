//go:build kkai_bridge

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDescribeContractJSONUsesBridgeRuntime(t *testing.T) {
	output, err := describeContractJSON("postgres")
	require.NoError(t, err)
	require.JSONEq(t, `{"compatible_prefixes":{"3":"sha256:984a638f2e2e2d370f4f2304f5acee209ebc47cd0d8af59c0f2eb116fe72634e","4":"sha256:4d1959b6eb1204aaa6a2481f6a423d395f5517f7f0a5adda88ec0547be1c751c","5":"sha256:c15230067aa89899923d1ad81f9e31f0c8e56a5113869535723bb4eea5e2d3ff","6":"sha256:226cdc461de7e456de91fd2d6b23a046a2c62c19cf5a8b30fd8559cb753b27ed","7":"sha256:d0779962929f5f47a608fa83adec278bcadf4458507d6dc36bb4503878cde15e"},"migration_kind":"none","migration_set_digest":"sha256:984a638f2e2e2d370f4f2304f5acee209ebc47cd0d8af59c0f2eb116fe72634e","migration_target_version":3,"runtime_max_version":7,"runtime_min_version":3,"schema_management":"runtime"}`, output)
}
