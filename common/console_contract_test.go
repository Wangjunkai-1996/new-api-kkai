package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsoleContract(t *testing.T) {
	contract := ConsoleContract()
	require.Equal(t, 1, contract.FormatVersion)
	require.Equal(t, []int{2}, contract.APIContracts)
	assert.Contains(t, contract.Capabilities, "token_group_inline")
	assert.Contains(t, contract.Capabilities, "video_sample_categories")
	assert.Contains(t, contract.Capabilities, "membership_ratio_quotes")
	assert.Contains(t, contract.Capabilities, "reseller_sites")
	assert.Contains(t, contract.Capabilities, "reseller_group_pricing")
	// Callers cannot mutate subsequent status/CLI declarations.
	contract.APIContracts[0] = 99
	assert.Equal(t, []int{2}, ConsoleContract().APIContracts)
}
