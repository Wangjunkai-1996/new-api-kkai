package openai

import (
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/tidwall/gjson"
)

const (
	openAIImageJSONDiagnosticMaxNames     = 12
	openAIImageJSONDiagnosticMaxLengths   = 8
	openAIImageJSONDiagnosticMaxNameBytes = 64
	openAIImageJSONDiagnosticMaxDataItems = 16
)

type openAIImageJSONBridgeState struct {
	completedImages [][]byte
	responseMeta    []byte
	usageRaw        []byte
	createdAt       int64
	clientGone      bool
	clientGoneErr   error

	eventCount           int
	completedEventCount  int
	dataItemCount        int
	eventTypes           map[string]int
	fieldNames           map[string]int
	b64JSONLengths       []int
	urlLengths           []int
	droppedEventTypes    int
	droppedFieldNames    int
	droppedB64JSONLength int
	droppedURLLength     int
	droppedDataItems     int
}

func (state *openAIImageJSONBridgeState) consume(info *relaycommon.RelayInfo, data []byte) *types.NewAPIError {
	info.ReceivedResponseCount++
	state.eventCount++
	if !gjson.ValidBytes(data) {
		err := fmt.Errorf("invalid JSON in upstream image stream")
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
		return newOpenAIImageJSONBridgeError(err, types.ErrorCodeBadResponseBody)
	}
	state.recordDiagnostics(data)
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
	state.completedEventCount++
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

func (state *openAIImageJSONBridgeState) recordDiagnostics(data []byte) {
	eventTypeValue := gjson.GetBytes(data, "type")
	eventType := "<missing>"
	if eventTypeValue.Exists() {
		if eventTypeValue.Type == gjson.String {
			eventType = sanitizeOpenAIImageJSONDiagnosticName(eventTypeValue.String())
		} else {
			eventType = "<invalid>"
		}
	}
	addOpenAIImageJSONDiagnosticCount(&state.eventTypes, eventType, &state.droppedEventTypes)

	payload := gjson.ParseBytes(data)
	if payload.IsObject() {
		payload.ForEach(func(key, _ gjson.Result) bool {
			addOpenAIImageJSONDiagnosticCount(&state.fieldNames, sanitizeOpenAIImageJSONDiagnosticName(key.String()), &state.droppedFieldNames)
			return true
		})
	}
	state.recordImageStringLengths(payload)

	dataValue := gjson.GetBytes(data, "data")
	if dataValue.IsArray() {
		items := dataValue.Array()
		state.dataItemCount += len(items)
		if len(items) > openAIImageJSONDiagnosticMaxDataItems {
			state.droppedDataItems += len(items) - openAIImageJSONDiagnosticMaxDataItems
		}
		for index, item := range items {
			if index >= openAIImageJSONDiagnosticMaxDataItems {
				break
			}
			state.recordDiagnosticObject(item)
		}
	} else if dataValue.IsObject() {
		state.dataItemCount++
		state.recordDiagnosticObject(dataValue)
	}
}

func (state *openAIImageJSONBridgeState) recordDiagnosticObject(value gjson.Result) {
	value.ForEach(func(key, _ gjson.Result) bool {
		addOpenAIImageJSONDiagnosticCount(&state.fieldNames, sanitizeOpenAIImageJSONDiagnosticName(key.String()), &state.droppedFieldNames)
		return true
	})
	state.recordImageStringLengths(value)
}

func (state *openAIImageJSONBridgeState) recordImageStringLengths(value gjson.Result) {
	if b64JSON := value.Get("b64_json"); b64JSON.Type == gjson.String {
		addOpenAIImageJSONDiagnosticLength(&state.b64JSONLengths, len(b64JSON.String()), &state.droppedB64JSONLength)
	}
	if imageURL := value.Get("url"); imageURL.Type == gjson.String {
		addOpenAIImageJSONDiagnosticLength(&state.urlLengths, len(imageURL.String()), &state.droppedURLLength)
	}
}

func (state *openAIImageJSONBridgeState) diagnosticSummary(reason relaycommon.StreamEndReason) string {
	return fmt.Sprintf(
		"reason=%s client_gone=%t events=%d completed_events=%d completed_images=%d data_items=%d event_types=%s fields=%s b64_json_lengths=%v url_lengths=%v dropped_event_types=%d dropped_fields=%d dropped_b64_json_lengths=%d dropped_url_lengths=%d dropped_data_items=%d",
		reason,
		state.clientGone,
		state.eventCount,
		state.completedEventCount,
		len(state.completedImages),
		state.dataItemCount,
		formatOpenAIImageJSONDiagnosticCounts(state.eventTypes),
		formatOpenAIImageJSONDiagnosticCounts(state.fieldNames),
		state.b64JSONLengths,
		state.urlLengths,
		state.droppedEventTypes,
		state.droppedFieldNames,
		state.droppedB64JSONLength,
		state.droppedURLLength,
		state.droppedDataItems,
	)
}

func addOpenAIImageJSONDiagnosticCount(counts *map[string]int, name string, dropped *int) {
	if *counts == nil {
		*counts = make(map[string]int)
	}
	if _, ok := (*counts)[name]; ok {
		(*counts)[name]++
		return
	}
	if len(*counts) >= openAIImageJSONDiagnosticMaxNames {
		*dropped++
		return
	}
	(*counts)[name] = 1
}

func addOpenAIImageJSONDiagnosticLength(lengths *[]int, length int, dropped *int) {
	if len(*lengths) >= openAIImageJSONDiagnosticMaxLengths {
		*dropped++
		return
	}
	*lengths = append(*lengths, length)
}

func sanitizeOpenAIImageJSONDiagnosticName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > openAIImageJSONDiagnosticMaxNameBytes {
		return "<invalid>"
	}
	for index := 0; index < len(name); index++ {
		char := name[index]
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '.' || char == '_' || char == '-' {
			continue
		}
		return "<invalid>"
	}
	return name
}

func formatOpenAIImageJSONDiagnosticCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "[]"
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]string, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, fmt.Sprintf("%s:%d", key, counts[key]))
	}
	return "[" + strings.Join(entries, ",") + "]"
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
