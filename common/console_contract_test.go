package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsoleContract(t *testing.T) {
	contract := ConsoleContract()
	require.Equal(t, 1, contract.FormatVersion)
	require.Contains(t, contract.APIContracts, 1)
	assert.Equal(t, consoleContractProfile == "feature", len(contract.Capabilities) > 0)
	if consoleContractProfile == "feature" {
		assert.Contains(t, contract.Capabilities, "video_sample_categories")
	}
	// Callers cannot mutate subsequent status/CLI declarations.
	contract.APIContracts[0] = 99
	assert.Equal(t, []int{1}, ConsoleContract().APIContracts)
}
