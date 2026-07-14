package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKKAICompletionRatioPrefersExactConfiguration(t *testing.T) {
	original := CompletionRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateCompletionRatioByJSONString(original))
	})

	require.NoError(t, UpdateCompletionRatioByJSONString(`{
		"gpt-5.4-fast": 4.5,
		"gpt-5.6-sol": 7.25,
		"openai/gpt-5.4-fast": 4.25
	}`))

	tests := []struct {
		model string
		ratio float64
	}{
		{model: "gpt-5.4-fast", ratio: 4.5},
		{model: "gpt-5.6-sol", ratio: 7.25},
		{model: "openai/gpt-5.4-fast", ratio: 4.25},
	}

	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			assert.InDelta(t, test.ratio, GetCompletionRatio(test.model), 1e-9)

			info := GetCompletionRatioInfo(test.model)
			assert.InDelta(t, test.ratio, info.Ratio, 1e-9)
			assert.False(t, info.Locked)
		})
	}
}

func TestKKAICompletionRatioPreservesOfficialFallbacks(t *testing.T) {
	original := CompletionRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateCompletionRatioByJSONString(original))
	})
	require.NoError(t, UpdateCompletionRatioByJSONString(`{}`))

	tests := []struct {
		name   string
		model  string
		ratio  float64
		locked bool
	}{
		{name: "hardcoded GPT fallback", model: "gpt-5.4-fast", ratio: 6, locked: true},
		{name: "provider-qualified fallback", model: "provider/gpt-5.4-fast", ratio: 1, locked: false},
		{name: "unknown model fallback", model: "unconfigured-model", ratio: 1, locked: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.InDelta(t, test.ratio, GetCompletionRatio(test.model), 1e-9)

			info := GetCompletionRatioInfo(test.model)
			assert.InDelta(t, test.ratio, info.Ratio, 1e-9)
			assert.Equal(t, test.locked, info.Locked)
		})
	}
}
