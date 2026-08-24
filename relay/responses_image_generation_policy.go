package relay

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
)

var errResponsesImageGenerationGroupForbidden = errors.New(
	"Responses image_generation is not available in this group; use an image-group key with the Images API or Image Studio",
)

func responsesImageGenerationAllowedGroup(group string) bool {
	return group == service.ImageGenerationTokenGroup || group == service.ImageStudioTokenGroup
}

func responsesImageGenerationPolicyApplies(info *relaycommon.RelayInfo) bool {
	return info != nil && info.GetFinalRequestRelayFormat() == types.RelayFormatOpenAIResponses
}

// enforceResponsesImageGenerationGroupPolicy removes the Responses image tool
// outside the two image groups. Explicit attempts to force that tool are denied
// so the request cannot silently change meaning or reach an upstream provider.
func enforceResponsesImageGenerationGroupPolicy(jsonData []byte, group string) ([]byte, bool, error) {
	if responsesImageGenerationAllowedGroup(group) {
		return jsonData, false, nil
	}

	var request map[string]json.RawMessage
	if err := common.Unmarshal(jsonData, &request); err != nil {
		return nil, false, fmt.Errorf("invalid Responses request body: %w", err)
	}
	if request == nil {
		return nil, false, errors.New("invalid Responses request body: JSON object is required")
	}

	rawTools, hasTools, err := normalizeResponsesPolicyField(request, "tools")
	if err != nil {
		return nil, false, err
	}
	rawToolChoice, hasToolChoice, err := normalizeResponsesPolicyField(request, "tool_choice")
	if err != nil {
		return nil, false, err
	}

	filteredTools, removedImageTool, remainingToolCount, err := filterResponsesImageGenerationTools(rawTools)
	if err != nil {
		return nil, false, err
	}

	filteredChoice, removeChoice, choiceChanged, err := sanitizeResponsesImageGenerationToolChoice(
		rawToolChoice, removedImageTool, remainingToolCount,
	)
	if err != nil {
		return nil, false, err
	}

	// Re-encode every policy-sensitive field outside image groups. Besides
	// applying the allowlist, this collapses duplicate JSON keys using the same
	// last-value semantics used by the gateway, so an upstream parser cannot
	// reinterpret an unchecked earlier value.
	changed := hasTools || hasToolChoice || removedImageTool || choiceChanged
	if !changed {
		return jsonData, false, nil
	}

	if hasTools {
		if remainingToolCount == 0 {
			delete(request, "tools")
		} else {
			request["tools"] = filteredTools
		}
	}
	if choiceChanged {
		if removeChoice {
			delete(request, "tool_choice")
		} else {
			request["tool_choice"] = filteredChoice
		}
	}

	result, err := common.Marshal(request)
	if err != nil {
		return nil, false, fmt.Errorf("encode Responses request after image_generation policy: %w", err)
	}
	return result, true, nil
}

func normalizeResponsesPolicyField(object map[string]json.RawMessage, canonicalName string) (json.RawMessage, bool, error) {
	var matchedName string
	var value json.RawMessage
	for name, candidate := range object {
		if !strings.EqualFold(name, canonicalName) {
			continue
		}
		if matchedName != "" {
			return nil, false, fmt.Errorf("invalid Responses request: ambiguous %s field", canonicalName)
		}
		matchedName = name
		value = candidate
	}
	if matchedName == "" {
		return nil, false, nil
	}
	if matchedName != canonicalName {
		delete(object, matchedName)
		object[canonicalName] = value
	}
	return value, true, nil
}

func filterResponsesImageGenerationTools(raw json.RawMessage) (json.RawMessage, bool, int, error) {
	if len(raw) == 0 || common.GetJsonType(raw) == "null" {
		return raw, false, 0, nil
	}
	if common.GetJsonType(raw) != "array" {
		return nil, false, 0, errors.New("invalid Responses tools: JSON array is required")
	}

	var tools []json.RawMessage
	if err := common.Unmarshal(raw, &tools); err != nil {
		return nil, false, 0, fmt.Errorf("invalid Responses tools: %w", err)
	}

	filtered := make([]json.RawMessage, 0, len(tools))
	removed := false
	for index, tool := range tools {
		if common.GetJsonType(tool) != "object" {
			return nil, false, 0, fmt.Errorf("invalid Responses tool at index %d: JSON object is required", index)
		}
		var toolObject map[string]json.RawMessage
		if err := common.Unmarshal(tool, &toolObject); err != nil {
			return nil, false, 0, fmt.Errorf("invalid Responses tool at index %d: %w", index, err)
		}
		typeValue, hasType, err := normalizeResponsesPolicyField(toolObject, "type")
		if err != nil {
			return nil, false, 0, fmt.Errorf("invalid Responses tool at index %d: %w", index, err)
		}
		if !hasType || common.GetJsonType(typeValue) != "string" {
			return nil, false, 0, fmt.Errorf("invalid Responses tool at index %d: type must be a string", index)
		}
		var toolType string
		if err := common.Unmarshal(typeValue, &toolType); err != nil {
			return nil, false, 0, fmt.Errorf("invalid Responses tool type at index %d: %w", index, err)
		}
		if toolType == dto.BuildInToolImageGeneration {
			removed = true
			continue
		}
		normalizedTool, err := common.Marshal(toolObject)
		if err != nil {
			return nil, false, 0, fmt.Errorf("encode Responses tool at index %d: %w", index, err)
		}
		filtered = append(filtered, normalizedTool)
	}

	encoded, err := common.Marshal(filtered)
	if err != nil {
		return nil, false, 0, fmt.Errorf("encode filtered Responses tools: %w", err)
	}
	return encoded, removed, len(filtered), nil
}

func sanitizeResponsesImageGenerationToolChoice(
	raw json.RawMessage,
	removedImageTool bool,
	remainingToolCount int,
) (json.RawMessage, bool, bool, error) {
	if len(raw) == 0 || common.GetJsonType(raw) == "null" {
		return raw, false, false, nil
	}

	switch common.GetJsonType(raw) {
	case "string":
		var choice string
		if err := common.Unmarshal(raw, &choice); err != nil {
			return nil, false, false, fmt.Errorf("invalid Responses tool_choice: %w", err)
		}
		if choice == dto.BuildInToolImageGeneration {
			return nil, false, false, errResponsesImageGenerationGroupForbidden
		}
		if !removedImageTool || remainingToolCount > 0 {
			return raw, false, false, nil
		}
		switch choice {
		case "required":
			return nil, false, false, errResponsesImageGenerationGroupForbidden
		case "auto":
			return nil, true, true, nil
		default:
			return raw, false, false, nil
		}

	case "object":
		var choice map[string]json.RawMessage
		if err := common.Unmarshal(raw, &choice); err != nil {
			return nil, false, false, fmt.Errorf("invalid Responses tool_choice: %w", err)
		}
		choiceTypeValue, hasChoiceType, err := normalizeResponsesPolicyField(choice, "type")
		if err != nil {
			return nil, false, false, fmt.Errorf("invalid Responses tool_choice: %w", err)
		}
		var choiceType string
		if !hasChoiceType || common.GetJsonType(choiceTypeValue) != "string" {
			return nil, false, false, errors.New("invalid Responses tool_choice: type must be a string")
		}
		if err := common.Unmarshal(choiceTypeValue, &choiceType); err != nil {
			return nil, false, false, fmt.Errorf("invalid Responses tool_choice type: %w", err)
		}
		if choiceType == dto.BuildInToolImageGeneration {
			return nil, false, false, errResponsesImageGenerationGroupForbidden
		}
		if choiceType != "allowed_tools" {
			normalizedChoice, err := common.Marshal(choice)
			if err != nil {
				return nil, false, false, fmt.Errorf("encode Responses tool_choice: %w", err)
			}
			return normalizedChoice, false, true, nil
		}

		allowedToolsValue, hasAllowedTools, err := normalizeResponsesPolicyField(choice, "tools")
		if err != nil {
			return nil, false, false, fmt.Errorf("invalid Responses allowed_tools tool_choice: %w", err)
		}
		if !hasAllowedTools {
			return nil, false, false, errors.New("invalid Responses allowed_tools tool_choice: tools is required")
		}
		filteredAllowedTools, removedAllowedImageTool, remainingAllowedToolCount, err := filterResponsesImageGenerationTools(allowedToolsValue)
		if err != nil {
			return nil, false, false, fmt.Errorf("invalid Responses allowed_tools tool_choice: %w", err)
		}
		modeValue, hasMode, err := normalizeResponsesPolicyField(choice, "mode")
		if err != nil {
			return nil, false, false, fmt.Errorf("invalid Responses allowed_tools tool_choice: %w", err)
		}
		var mode string
		if !hasMode || common.GetJsonType(modeValue) != "string" {
			return nil, false, false, errors.New("invalid Responses allowed_tools tool_choice: mode must be a string")
		}
		if err := common.Unmarshal(modeValue, &mode); err != nil {
			return nil, false, false, fmt.Errorf("invalid Responses allowed_tools mode: %w", err)
		}
		if mode != "auto" && mode != "required" {
			return nil, false, false, errors.New("invalid Responses allowed_tools mode: auto or required is required")
		}
		if !removedAllowedImageTool {
			choice["tools"] = filteredAllowedTools
			encoded, err := common.Marshal(choice)
			if err != nil {
				return nil, false, false, fmt.Errorf("encode filtered Responses tool_choice: %w", err)
			}
			return encoded, false, true, nil
		}
		if remainingAllowedToolCount == 0 {
			if mode == "required" {
				return nil, false, false, errResponsesImageGenerationGroupForbidden
			}
			return nil, true, true, nil
		}

		choice["tools"] = filteredAllowedTools
		encoded, err := common.Marshal(choice)
		if err != nil {
			return nil, false, false, fmt.Errorf("encode filtered Responses tool_choice: %w", err)
		}
		return encoded, false, true, nil

	default:
		return nil, false, false, errors.New("invalid Responses tool_choice: string or JSON object is required")
	}
}

func newResponsesImageGenerationPolicyError(err error) *types.NewAPIError {
	if errors.Is(err, errResponsesImageGenerationGroupForbidden) {
		return types.NewErrorWithStatusCode(
			err,
			types.ErrorCodeAccessDenied,
			http.StatusForbidden,
			types.ErrOptionWithSkipRetry(),
		)
	}
	return types.NewErrorWithStatusCode(
		err,
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)
}
