package constant

// TenantTextChannelTypes lists adaptors using the shared HTTP dispatch and
// durable tenant billing path. SDK and dedicated media/task transports need
// their own tenant funding integration.
func TenantTextChannelTypes() []int {
	return []int{
		ChannelTypeOpenAI, ChannelTypeAzure, ChannelTypeOpenRouter, ChannelTypeXinference,
		ChannelTypeAnthropic, ChannelTypeSiliconFlow, ChannelTypeDeepSeek, ChannelTypeMistral,
		ChannelTypeNewAPI, ChannelTypeSub2API, ChannelTypeAdvancedCustom, ChannelTypeVLLM, ChannelTypeSGLang,
		ChannelTypeGemini, ChannelTypeCodex,
	}
}
