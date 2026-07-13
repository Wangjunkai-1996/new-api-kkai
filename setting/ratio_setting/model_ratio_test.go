package ratio_setting

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompletionRatioUsesExactConfiguredValueBeforeHardcodedFallback(t *testing.T) {
	original := CompletionRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateCompletionRatioByJSONString(original))
	})

	configured := map[string]float64{
		"gpt-5.4-fast":        4.5,
		"gpt-5.6-sol":         6,
		"gpt-5.6-luna":        6,
		"gpt-5.6-terra":       6,
		"openai/gpt-5.4-fast": 4.25,
	}
	data, err := json.Marshal(configured)
	require.NoError(t, err)
	require.NoError(t, UpdateCompletionRatioByJSONString(string(data)))

	for model, expected := range configured {
		t.Run(model, func(t *testing.T) {
			assert.InDelta(t, expected, GetCompletionRatio(model), 1e-9)
			info := GetCompletionRatioInfo(model)
			assert.InDelta(t, expected, info.Ratio, 1e-9)
			assert.False(t, info.Locked)
		})
	}
}

func TestCompletionRatioFallsBackWhenNoExactConfigurationExists(t *testing.T) {
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
		{
			name:   "hardcoded gpt fallback",
			model:  "gpt-5.4-fast",
			ratio:  6,
			locked: true,
		},
		{
			name:   "provider qualified name without exact configuration",
			model:  "provider/gpt-5.4-fast",
			ratio:  1,
			locked: false,
		},
		{
			name:   "unknown model default",
			model:  "unconfigured-model",
			ratio:  1,
			locked: false,
		},
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
