package service

import (
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// ValidateTenantTextPayload is applied both before pricing and to the effective
// upstream JSON, after channel overrides. Function tools run on the caller;
// provider-hosted tools and media require a separate supported billing path.
func ValidateTenantTextPayload(data []byte, modelName string) error {
	var payload map[string]any
	if err := common.Unmarshal(data, &payload); err != nil || payload == nil {
		return fmt.Errorf("reseller requests require a JSON object")
	}
	for _, key := range []string{"extra_body", "audio", "mcp_servers", "container", "web_search_options", "search_parameters"} {
		if value, ok := payload[key]; ok && value != nil {
			return fmt.Errorf("%s is unavailable on reseller sites", key)
		}
	}
	if modalities, ok := payload["modalities"]; ok && modalities != nil {
		values, valid := modalities.([]any)
		if !valid {
			return fmt.Errorf("only text output is supported on reseller sites")
		}
		for _, value := range values {
			if value != "text" {
				return fmt.Errorf("only text output is supported on reseller sites")
			}
		}
	}
	if rawTools := payload["tools"]; rawTools != nil {
		tools, valid := rawTools.([]any)
		if !valid {
			return fmt.Errorf("invalid reseller function tools")
		}
		for _, raw := range tools {
			tool, valid := raw.(map[string]any)
			if !valid {
				return fmt.Errorf("invalid reseller function tool")
			}
			kind, _ := tool["type"].(string)
			if kind != "" && kind != "function" && kind != "custom" {
				return fmt.Errorf("provider-hosted tools are unavailable on reseller sites")
			}
			name, _ := tool["name"].(string)
			if function, ok := tool["function"].(map[string]any); ok {
				name, _ = function["name"].(string)
			}
			price := operation_setting.GetToolPriceForModel(name, modelName)
			if math.IsNaN(price) || math.IsInf(price, 0) || price != 0 {
				return fmt.Errorf("paid tools are unavailable on reseller sites")
			}
		}
	}
	for _, key := range []string{"messages", "input", "system"} {
		if err := validateTenantTextContent(payload[key]); err != nil {
			return err
		}
	}
	return nil
}

func validateTenantTextContent(content any) error {
	switch value := content.(type) {
	case nil, string, float64:
		return nil
	case []any:
		for _, item := range value {
			if err := validateTenantTextContent(item); err != nil {
				return err
			}
		}
	case map[string]any:
		kind, _ := value["type"].(string)
		switch strings.TrimSpace(kind) {
		case "", "message", "text", "input_text", "output_text", "tool_use", "tool_result", "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output", "reasoning", "thinking", "redacted_thinking":
		default:
			return fmt.Errorf("only text and caller function content is supported on reseller sites")
		}
		if value["tools"] != nil {
			return fmt.Errorf("declare reseller function tools at the request level")
		}
		for _, key := range []string{"image_url", "input_audio", "video_url", "file", "image", "source", "inline_data", "inlineData", "file_data", "fileData"} {
			if value[key] != nil {
				return fmt.Errorf("media input is unavailable on reseller sites")
			}
		}
		return validateTenantTextContent(value["content"])
	default:
		return fmt.Errorf("invalid reseller text content")
	}
	return nil
}
