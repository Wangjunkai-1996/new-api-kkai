package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
)

func TestImageGenerationModelFamilies(t *testing.T) {
	for _, tc := range []struct {
		model string
		image bool
	}{
		{"gpt-image-1", true},
		{"gpt-image-1-mini", true},
		{"gpt-image-1.5", true},
		{"gpt-image-2", true},
		{"gpt-image-2.5-flare", true},
		{"GPT-IMAGE-2.5-SUNBURST", true},
		{"gpt-image-2-1k", true},
		{"gpt-image-3-preview", true},
		{"dall-e-3", true},
		{"imagen-4.0-generate-001", true},
		{"flux.1-kontext-pro", true},
		{"gpt-image-2k", false},
		{"gpt-image-2.", false},
		{"gpt-image-", false},
		{"gpt-4o-image", false},
		{"chatgpt-image-2", false},
		{"gpt-4o", false},
	} {
		t.Run(tc.model, func(t *testing.T) {
			assert.Equal(t, tc.image, IsImageGenerationModel(tc.model))
			endpoints := GetEndpointTypesByChannelType(constant.ChannelTypeOpenAI, tc.model)
			if tc.image {
				assert.Contains(t, endpoints, types.EndpointTypeImageGeneration)
			} else {
				assert.NotContains(t, endpoints, types.EndpointTypeImageGeneration)
			}
		})
	}
}
