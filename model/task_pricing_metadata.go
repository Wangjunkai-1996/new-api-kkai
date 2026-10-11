package model

import (
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
)

// pricingChannelRef is the routing evidence available for one enabled model.
// Task-plugin channels carry explicit plugin bindings; native task channels
// are resolved through the plugin registry's channel-type index.
type pricingChannelRef struct {
	Type       int
	PluginKeys []string
}

// buildPricingChannelIndex attaches channel settings to the already-loaded
// enabled abilities. Keeping this index outside the resolver avoids querying
// channels once per pricing entry.
func buildPricingChannelIndex(abilities []AbilityWithChannel) map[string][]pricingChannelRef {
	index := make(map[string][]pricingChannelRef)
	if len(abilities) == 0 || DB == nil {
		return index
	}

	ids := make([]int, 0, len(abilities))
	seen := make(map[int]struct{}, len(abilities))
	for _, ability := range abilities {
		if _, ok := seen[ability.ChannelId]; ok {
			continue
		}
		seen[ability.ChannelId] = struct{}{}
		ids = append(ids, ability.ChannelId)
	}
	var channels []Channel
	if err := DB.Select("id", "type", "status", "setting").Where("id IN ?", ids).Find(&channels).Error; err != nil {
		return index
	}
	byID := make(map[int]Channel, len(channels))
	for _, channel := range channels {
		byID[channel.Id] = channel
	}
	for _, ability := range abilities {
		channel, ok := byID[ability.ChannelId]
		if !ok || channel.Status != common.ChannelStatusEnabled {
			continue
		}
		ref := pricingChannelRef{Type: channel.Type}
		if channel.Type == constant.ChannelTypeTaskPlugin || channel.Type == constant.ChannelTypeNewAPI {
			// Do not call Channel.GetSetting here: malformed settings trigger a
			// save, and this query intentionally loaded only routing fields.
			// Pricing discovery must never write a partial Channel back.
			if channel.Setting != nil && *channel.Setting != "" {
				var settings relaydto.ChannelSettings
				if err := common.Unmarshal([]byte(*channel.Setting), &settings); err == nil {
					ref.PluginKeys = settings.TaskPluginBindings()
				}
			}
		}
		if !slices.ContainsFunc(index[ability.Model], func(existing pricingChannelRef) bool {
			return existing.Type == ref.Type && slices.Equal(existing.PluginKeys, ref.PluginKeys)
		}) {
			index[ability.Model] = append(index[ability.Model], ref)
		}
	}
	return index
}

// pricingPluginMetadata identifies the plugin and declared model whose usage
// metadata describe a client-facing model. An empty result means there is no
// reliable active-channel evidence; callers should keep the normal editor.
type pricingPluginMetadata struct {
	Plugin            *jsplugin.LoadedPlugin
	UsageModel        string
	LegacyBillingUnit string
}

func isNativeTaskPricingChannel(channelType int) bool {
	switch channelType {
	case constant.ChannelTypeKling,
		constant.ChannelTypeJimeng,
		constant.ChannelTypeVidu,
		constant.ChannelTypeDoubaoVideo,
		constant.ChannelTypeSora:
		return true
	default:
		return false
	}
}

func usageHasSecondField(plugin *jsplugin.LoadedPlugin, usageModel string) bool {
	if plugin == nil {
		return false
	}
	schema, _ := plugin.Meta.UsageForModel(usageModel)
	for _, field := range []string{"seconds", "duration"} {
		definition, ok := schema[field]
		if ok && definition.Type == "number" && definition.Unit == "second" {
			return true
		}
	}
	return false
}

func legacyBillingUnitForChannels(plugin *jsplugin.LoadedPlugin, usageModel string, channels []pricingChannelRef) string {
	if !usageHasSecondField(plugin, usageModel) {
		return ""
	}
	for _, channel := range channels {
		if isNativeTaskPricingChannel(channel.Type) {
			return "second"
		}
	}
	return ""
}

// resolvePricingPluginMetadata first follows canonical declarations and
// explicit model mappings. For legacy aliases without either, it falls back
// only to an enabled channel's plugin metadata. Generic OpenAI channels are
// intentionally excluded from this fallback because they can serve unrelated
// task and chat models.
func resolvePricingPluginMetadata(generation *jsplugin.RoutingGeneration, name string, channels map[string][]pricingChannelRef) (pricingPluginMetadata, bool) {
	if generation == nil || name == "" {
		return pricingPluginMetadata{}, false
	}
	if plugin, ok := generation.GetByModel(name); ok {
		return pricingPluginMetadata{Plugin: plugin, UsageModel: name,
			LegacyBillingUnit: legacyBillingUnitForChannels(plugin, name, channels[name])}, true
	}
	if target, ok := ResolveTaskModelAlias(generation, name); ok {
		if target.Declared != "" {
			if plugin, exists := generation.Get(target.PluginKey); exists {
				return pricingPluginMetadata{Plugin: plugin, UsageModel: target.Declared,
					LegacyBillingUnit: legacyBillingUnitForChannels(plugin, target.Declared, channels[name])}, true
			}
		}
	}

	var resolved pricingPluginMetadata
	for _, channel := range channels[name] {
		plugins := make([]*jsplugin.LoadedPlugin, 0, len(channel.PluginKeys))
		if len(channel.PluginKeys) > 0 {
			for _, key := range channel.PluginKeys {
				if plugin, ok := generation.Get(key); ok {
					plugins = append(plugins, plugin)
				}
			}
		} else if isNativeTaskPricingChannel(channel.Type) {
			if plugin, ok := generation.GetByChannelType(channel.Type); ok {
				plugins = append(plugins, plugin)
			}
		}
		for _, plugin := range plugins {
			candidate := pricingPluginMetadata{Plugin: plugin, UsageModel: name}
			if isNativeTaskPricingChannel(channel.Type) {
				candidate.LegacyBillingUnit = legacyBillingUnitForChannels(plugin, name, []pricingChannelRef{channel})
			}
			if resolved.Plugin == nil {
				resolved = candidate
				continue
			}
			if resolved.Plugin.Meta.Key != plugin.Meta.Key {
				// Different active providers make the schema ambiguous. Do not
				// guess based on an alias spelling.
				return pricingPluginMetadata{}, false
			}
		}
	}
	if resolved.Plugin == nil {
		return pricingPluginMetadata{}, false
	}
	return resolved, true
}
