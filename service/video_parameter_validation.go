package service

import (
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
)

func ValidateVideoParameters(spec VideoModelSpec, mode string, values map[string]any, applyDefaults bool) (map[string]any, error) {
	if !containsVideoMode(spec.Modes, mode) {
		return nil, fmt.Errorf("%w: mode %q is not supported", ErrInvalidVideoParameters, mode)
	}
	provided := values
	if provided == nil {
		provided = map[string]any{}
	}
	definitions := make(map[string]VideoParameterSpec, len(spec.Parameters))
	for _, parameter := range spec.Parameters {
		definitions[parameter.Key] = parameter
	}
	for key := range provided {
		parameter, ok := definitions[key]
		if !ok || !parameterAppliesToMode(parameter, mode) {
			return nil, fmt.Errorf("%w: unknown parameter %q for mode %q", ErrInvalidVideoParameters, key, mode)
		}
	}

	normalized := make(map[string]any, len(provided))
	for _, parameter := range spec.Parameters {
		if !parameterAppliesToMode(parameter, mode) {
			continue
		}
		value, ok := provided[parameter.Key]
		if !ok && applyDefaults && parameter.Default != nil {
			value = parameter.Default
			ok = true
		}
		if !ok {
			if parameter.Required {
				return nil, fmt.Errorf("%w: parameter %q is required", ErrInvalidVideoParameters, parameter.Key)
			}
			continue
		}
		if err := validateVideoParameterValue(parameter, value); err != nil {
			return nil, fmt.Errorf("%w: parameter %q: %v", ErrInvalidVideoParameters, parameter.Key, err)
		}
		normalized[parameter.Key] = normalizeVideoParameterValue(parameter, value)
	}
	return normalized, nil
}

func validateVideoParameterValue(parameter VideoParameterSpec, value any) error {
	switch parameter.Control {
	case VideoControlSegmented, VideoControlSelect:
		candidate, ok := canonicalVideoScalar(value)
		if !ok {
			return fmt.Errorf("value must be a string, number, or boolean")
		}
		for _, option := range parameter.Options {
			optionValue, _ := canonicalVideoScalar(option.Value)
			if candidate == optionValue {
				return validateVideoBillingMultiplierValue(parameter, value)
			}
		}
		return fmt.Errorf("value is not in the allowed options")
	case VideoControlSlider, VideoControlNumber:
		number, ok := videoNumericValue(value)
		if !ok || !isFinite(number) {
			return fmt.Errorf("value must be a finite number")
		}
		if number < *parameter.Min || number > *parameter.Max {
			return fmt.Errorf("value must be between %v and %v", *parameter.Min, *parameter.Max)
		}
		steps := (number - *parameter.Min) / *parameter.Step
		if math.Abs(steps-math.Round(steps)) > 1e-8 {
			return fmt.Errorf("value must align to step %v", *parameter.Step)
		}
		return validateVideoBillingMultiplierValue(parameter, value)
	case VideoControlSwitch:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("value must be a boolean")
		}
		return nil
	default:
		return fmt.Errorf("unsupported control %q", parameter.Control)
	}
}

func normalizeVideoParameterValue(parameter VideoParameterSpec, value any) any {
	if parameter.Control != VideoControlSlider && parameter.Control != VideoControlNumber {
		return value
	}
	number, _ := videoNumericValue(value)
	return number
}

func parameterAppliesToMode(parameter VideoParameterSpec, mode string) bool {
	return len(parameter.Modes) == 0 || containsVideoMode(parameter.Modes, mode)
}

func filterVideoParametersForMode(spec VideoModelSpec, mode string, values map[string]any) map[string]any {
	filtered := make(map[string]any, len(values))
	definitions := make(map[string]VideoParameterSpec, len(spec.Parameters))
	for _, parameter := range spec.Parameters {
		definitions[parameter.Key] = parameter
	}
	for key, value := range values {
		if parameter, ok := definitions[key]; ok && !parameterAppliesToMode(parameter, mode) {
			continue
		}
		filtered[key] = value
	}
	return filtered
}

func containsVideoMode(modes []string, mode string) bool {
	for _, candidate := range modes {
		if candidate == mode {
			return true
		}
	}
	return false
}

func canonicalVideoScalar(value any) (string, bool) {
	switch value.(type) {
	case string, bool, float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		encoded, err := common.Marshal(value)
		return string(encoded), err == nil
	default:
		return "", false
	}
}

func videoNumericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		if typed > 1<<53 {
			return 0, false
		}
		return float64(typed), true
	default:
		return 0, false
	}
}
