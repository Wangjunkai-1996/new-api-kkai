package openai

import (
	"fmt"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/tidwall/gjson"
)

type openAIImageJSONBridgeState struct {
	completedImages [][]byte
	responseMeta    []byte
	usageRaw        []byte
	createdAt       int64
}

func (state *openAIImageJSONBridgeState) consume(info *relaycommon.RelayInfo, data []byte) *types.NewAPIError {
	info.ReceivedResponseCount++
	if !gjson.ValidBytes(data) {
		err := fmt.Errorf("invalid JSON in upstream image stream")
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
		return newOpenAIImageJSONBridgeError(err, types.ErrorCodeBadResponseBody)
	}
	if isOpenAIImageStreamErrorEvent(data) {
		bridgeErr := openAIImageJSONBridgeUpstreamError(data)
		info.StreamStatus.RecordError(bridgeErr.Error())
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, bridgeErr)
		return bridgeErr
	}
	if usage := gjson.GetBytes(data, "usage"); usage.Exists() && usage.IsObject() {
		state.usageRaw = append(state.usageRaw[:0], usage.Raw...)
	}
	if eventCreated := gjson.GetBytes(data, "created_at").Int(); eventCreated > 0 {
		state.createdAt = eventCreated
	}
	eventType := gjson.GetBytes(data, "type").String()
	if eventType != "image_generation.completed" && eventType != "image_edit.completed" {
		return nil
	}
	imageCountBefore := len(state.completedImages)
	if imageData := gjson.GetBytes(data, "data"); imageData.IsArray() {
		for _, item := range imageData.Array() {
			itemData := []byte(item.Raw)
			if openAIImageJSONDataExists(itemData) {
				if bridgeErr := state.addCompletedImage(info, itemData); bridgeErr != nil {
					return bridgeErr
				}
			}
		}
	} else if openAIImageJSONDataExists(data) {
		if bridgeErr := state.addCompletedImage(info, data); bridgeErr != nil {
			return bridgeErr
		}
	}
	if len(state.responseMeta) == 0 && len(state.completedImages) > imageCountBefore {
		state.responseMeta = data
	}
	return nil
}

func (state *openAIImageJSONBridgeState) addCompletedImage(info *relaycommon.RelayInfo, data []byte) *types.NewAPIError {
	if len(state.completedImages) >= dto.MaxImageN {
		err := fmt.Errorf("upstream image stream exceeded the maximum image count")
		info.StreamStatus.RecordError(err.Error())
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, err)
		return newOpenAIImageJSONBridgeError(err, types.ErrorCodeBadResponseBody)
	}
	state.completedImages = append(state.completedImages, data)
	return nil
}
