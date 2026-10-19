package constant

// TenantTextChannelTypes is the pilot's verified HTTP transport allowlist.
// These adaptors use DoApiRequest and the shared outbound budget validation;
// SDK, WebSocket and unverified provider transports stay unavailable.
func TenantTextChannelTypes() []int {
	return []int{
		ChannelTypeOpenAI, ChannelTypeAzure, ChannelTypeOpenRouter, ChannelTypeXinference,
		ChannelTypeAnthropic, ChannelTypeSiliconFlow, ChannelTypeDeepSeek, ChannelTypeMistral,
		ChannelTypeNewAPI, ChannelTypeSub2API, ChannelTypeAdvancedCustom, ChannelTypeVLLM, ChannelTypeSGLang,
	}
}
