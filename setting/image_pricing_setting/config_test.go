package image_pricing_setting

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateByJSONStringPublishesCompletePolicyAtomically(t *testing.T) {
	original := JSON()
	t.Cleanup(func() { require.NoError(t, UpdateByJSONString(original)) })

	config := DefaultConfig()
	config.Enabled = true
	raw, err := common.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, UpdateByJSONString(string(raw)))
	wantHash := PolicyHash()

	var wait sync.WaitGroup
	for range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			match, configured, resolveErr := Resolve("gpt-image-2", "3840x2160")
			require.NoError(t, resolveErr)
			require.True(t, configured)
			assert.Equal(t, "4k", match.Resolution.Tier)
			assert.Equal(t, wantHash, PolicyHash())
			assert.Equal(t, wantHash, match.PolicyHash)
		}()
	}
	wait.Wait()
}

func TestInvalidUpdateKeepsLastGoodPolicy(t *testing.T) {
	original := JSON()
	t.Cleanup(func() { require.NoError(t, UpdateByJSONString(original)) })

	config := DefaultConfig()
	config.Enabled = true
	raw, err := common.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, UpdateByJSONString(string(raw)))
	wantHash := PolicyHash()

	require.Error(t, UpdateByJSONString(`{"version":"broken","enabled":true,"models":{}}`))
	assert.Equal(t, wantHash, PolicyHash())
	_, configured, resolveErr := Resolve("gpt-image-2", "1024x1024")
	require.NoError(t, resolveErr)
	assert.True(t, configured)
}
