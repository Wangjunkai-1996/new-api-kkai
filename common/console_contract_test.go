package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsoleContract(t *testing.T) {
	contract := ConsoleContract()
	require.Equal(t, 1, contract.FormatVersion)
	// Contract 3 requires the video-works projection; contract 2 stays supported
	// while the previous frontend serves during the backend rollout.
	require.Equal(t, []int{2, 3}, contract.APIContracts)
	assert.Contains(t, contract.Capabilities, "token_group_inline")
	assert.Contains(t, contract.Capabilities, "video_sample_categories")
	assert.Contains(t, contract.Capabilities, "membership_ratio_quotes")
	assert.Contains(t, contract.Capabilities, "reseller_sites")
	assert.Contains(t, contract.Capabilities, "reseller_group_pricing")
	assert.Contains(t, contract.Capabilities, "image_output_edit_source")
	// Callers cannot mutate subsequent status/CLI declarations.
	contract.APIContracts[0] = 99
	assert.Equal(t, []int{2, 3}, ConsoleContract().APIContracts)
}
