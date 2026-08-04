package openai

import (
	"encoding/json"
	"fmt"
	"mime/multipart"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/tidwall/gjson"
)

func writeOpenAIImageMultipartFields(writer *multipart.Writer, info *relaycommon.RelayInfo, model string, formValues map[string][]string) error {
	fields := make(map[string]any, len(formValues)+1)
	for key, values := range formValues {
		if len(values) == 1 {
			fields[key] = openAIImageMultipartParamValue(key, values[0])
			continue
		}
		fields[key] = append([]string(nil), values...)
	}
	fields["model"] = model

	jsonData, err := common.Marshal(fields)
	if err != nil {
		return fmt.Errorf("marshal image edit form fields failed: %w", err)
	}
	jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
	if err != nil {
		return fmt.Errorf("apply image edit form parameter override failed: %w", err)
	}
	if err := relaycommon.ValidateOutboundImagePricingJSON(info, jsonData); err != nil {
		return err
	}
	info.UpstreamIsStream = gjson.GetBytes(jsonData, "stream").Bool()

	var overridden map[string]json.RawMessage
	if err := common.Unmarshal(jsonData, &overridden); err != nil {
		return fmt.Errorf("decode overridden image edit form fields failed: %w", err)
	}
	keys := make([]string, 0, len(overridden))
	for key := range overridden {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		values, err := openAIImageMultipartFormValues(overridden[key])
		if err != nil {
			return fmt.Errorf("decode overridden %s form field failed: %w", key, err)
		}
		for _, value := range values {
			if err := writer.WriteField(key, value); err != nil {
				return fmt.Errorf("write overridden %s form field failed: %w", key, err)
			}
		}
	}
	return nil
}

func openAIImageMultipartParamValue(key, value string) any {
	switch key {
	case "stream", "watermark":
		if parsed, err := strconv.ParseBool(strings.TrimSpace(value)); err == nil {
			return parsed
		}
	case "n", "partial_images", "output_compression":
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			return parsed
		}
	}
	return value
}

func openAIImageMultipartFormValues(raw json.RawMessage) ([]string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var items []json.RawMessage
		if err := common.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		values := make([]string, 0, len(items))
		for _, item := range items {
			value, err := openAIImageMultipartFormValues(item)
			if err != nil {
				return nil, err
			}
			values = append(values, value...)
		}
		return values, nil
	}
	if strings.HasPrefix(trimmed, `"`) {
		var value string
		if err := common.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		return []string{value}, nil
	}
	return []string{trimmed}, nil
}
