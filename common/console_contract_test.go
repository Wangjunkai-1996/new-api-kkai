package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsoleContract(t *testing.T) {
	contract := ConsoleContract()
	require.Equal(t, 1, contract.FormatVersion)
	// Contract 3 retains the frozen Studio facade for existing tabs during
	// rollout and rollback; contract 4 adds the multimodal Studio protocol.
	require.Equal(t, []int{3, 4}, contract.APIContracts)
	assert.Contains(t, contract.Capabilities, "token_group_inline")
	assert.Contains(t, contract.Capabilities, "video_sample_categories")
	assert.Contains(t, contract.Capabilities, "video_studio_multimodal")
	assert.Contains(t, contract.Capabilities, "membership_ratio_quotes")
	assert.Contains(t, contract.Capabilities, "reseller_sites")
	assert.Contains(t, contract.Capabilities, "reseller_group_pricing")
	assert.Contains(t, contract.Capabilities, "image_output_edit_source")
	assert.Contains(t, contract.Capabilities, "token_create_result_id")
	// Callers cannot mutate subsequent status/CLI declarations.
	contract.APIContracts[0] = 99
	assert.Equal(t, []int{3, 4}, ConsoleContract().APIContracts)
}
