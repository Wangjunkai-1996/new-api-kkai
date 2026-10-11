package model

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pricingMetadataTestPlugin(key string, channelType int) string {
	channelTypes := ""
	if channelType > 0 {
		channelTypes = `channelTypes: [` + strconv.Itoa(channelType) + `],`
	}
	return `
export const meta = {
  apiVersion: 1,
  key: "` + key + `",
  name: "` + key + `",
  version: "1.0.0",
  author: {name: "test"},
  ` + channelTypes + `
  models: ["declared-video"],
  fetchMode: "per_task",
  usageSchema: {seconds: {type: "number", unit: "second"}}
};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {}; }
`
}

func TestResolvePricingPluginMetadataUsesEnabledNativeTaskChannel(t *testing.T) {
	registry := jsplugin.NewRegistry()
	// Use a fixture with the real Sora channel type. The alias is intentionally
	// absent from plugin metadata and has no model mapping.
	_, err := registry.Register(pricingMetadataTestPlugin("sora-test", 55), jsplugin.Options{})
	require.NoError(t, err)

	metadata, ok := resolvePricingPluginMetadata(registry.Generation(), "legacy-video", map[string][]pricingChannelRef{
		"legacy-video": {{Type: 55}},
	})
	require.True(t, ok)
	assert.Equal(t, "sora-test", metadata.Plugin.Meta.Key)
	assert.Equal(t, "second", metadata.Plugin.Meta.UsageSchema["seconds"].Unit)
	assert.Equal(t, "second", metadata.LegacyBillingUnit)
}

func TestResolvePricingPluginMetadataUsesExplicitTaskPluginBinding(t *testing.T) {
	registry := jsplugin.NewRegistry()
	_, err := registry.Register(pricingMetadataTestPlugin("bound-plugin", 0), jsplugin.Options{})
	require.NoError(t, err)

	metadata, ok := resolvePricingPluginMetadata(registry.Generation(), "legacy-video", map[string][]pricingChannelRef{
		"legacy-video": {{Type: 61, PluginKeys: []string{"bound-plugin"}}},
	})
	require.True(t, ok)
	assert.Equal(t, "bound-plugin", metadata.Plugin.Meta.Key)
}

func TestResolvePricingPluginMetadataDoesNotGuessObsoleteOrGenericModels(t *testing.T) {
	registry := jsplugin.NewRegistry()
	_, err := registry.Register(pricingMetadataTestPlugin("sora-test", 55), jsplugin.Options{})
	require.NoError(t, err)

	_, ok := resolvePricingPluginMetadata(registry.Generation(), "obsolete-video", nil)
	assert.False(t, ok)
	_, ok = resolvePricingPluginMetadata(registry.Generation(), "generic-openai", map[string][]pricingChannelRef{
		"generic-openai": {{Type: 1}},
	})
	assert.False(t, ok)
	_, ok = resolvePricingPluginMetadata(registry.Generation(), "generic-volcengine", map[string][]pricingChannelRef{
		"generic-volcengine": {{Type: 45}},
	})
	assert.False(t, ok)
}
