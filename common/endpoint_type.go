package common

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// GetEndpointTypesByChannelType 获取渠道最优先端点类型（所有的渠道都支持 OpenAI 端点）
func GetEndpointTypesByChannelType(channelType int, modelName string) []types.EndpointType {
	var endpointTypes []types.EndpointType
	switch channelType {
	case constant.ChannelTypeJina:
		endpointTypes = []types.EndpointType{types.EndpointTypeJinaRerank}
	//case constant.ChannelTypeMidjourney, constant.ChannelTypeMidjourneyPlus:
	//	endpointTypes = []types.EndpointType{types.EndpointTypeMidjourney}
	//case constant.ChannelTypeSunoAPI:
	//	endpointTypes = []types.EndpointType{types.EndpointTypeSuno}
	//case constant.ChannelTypeKling:
	//	endpointTypes = []types.EndpointType{types.EndpointTypeKling}
	//case constant.ChannelTypeJimeng:
	//	endpointTypes = []types.EndpointType{types.EndpointTypeJimeng}
	case constant.ChannelTypeAws:
		fallthrough
	case constant.ChannelTypeAnthropic:
		endpointTypes = []types.EndpointType{types.EndpointTypeAnthropic, types.EndpointTypeOpenAI}
	case constant.ChannelTypeVertexAi:
		fallthrough
	case constant.ChannelTypeGemini:
		endpointTypes = []types.EndpointType{types.EndpointTypeGemini, types.EndpointTypeOpenAI}
	case constant.ChannelTypeOpenRouter: // OpenRouter 只支持 OpenAI 端点
		endpointTypes = []types.EndpointType{types.EndpointTypeOpenAI}
	case constant.ChannelTypeXai:
		endpointTypes = []types.EndpointType{types.EndpointTypeOpenAI, types.EndpointTypeOpenAIResponse}
	case constant.ChannelTypeVLLM, constant.ChannelTypeSGLang:
		endpointTypes = GetAdvancedCustomPreset(channelType).SupportedEndpointTypesForModel(modelName)
	case constant.ChannelTypeSora:
		endpointTypes = []types.EndpointType{types.EndpointTypeOpenAIVideo}
	case constant.ChannelTypeSub2API, constant.ChannelTypeNewAPI:
		endpointTypes = []types.EndpointType{
			types.EndpointTypeOpenAI,
			types.EndpointTypeOpenAIResponse,
			types.EndpointTypeOpenAIResponseCompact,
			types.EndpointTypeAnthropic,
			types.EndpointTypeGemini,
			types.EndpointTypeOpenAIAlphaSearch,
		}
	case constant.ChannelTypeCodex:
		endpointTypes = []types.EndpointType{
			types.EndpointTypeOpenAIResponse,
			types.EndpointTypeOpenAIResponseCompact,
			types.EndpointTypeOpenAIAlphaSearch,
		}
	default:
		if IsOpenAIResponseOnlyModel(modelName) {
			endpointTypes = []types.EndpointType{types.EndpointTypeOpenAIResponse}
		} else {
			endpointTypes = []types.EndpointType{types.EndpointTypeOpenAI}
		}
	}
	if IsImageGenerationModel(modelName) {
		// add to first
		endpointTypes = append([]types.EndpointType{types.EndpointTypeImageGeneration}, endpointTypes...)
	}
	return endpointTypes
}
