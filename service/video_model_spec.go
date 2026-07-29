package service

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

const (
	VideoModeTextToVideo    = "text_to_video"
	VideoModeImageToVideo   = "image_to_video"
	VideoModeFirstLastFrame = "first_last_frame"

	VideoControlSegmented = "segmented"
	VideoControlSelect    = "select"
	VideoControlSlider    = "slider"
	VideoControlSwitch    = "switch"
	VideoControlNumber    = "number"
)

var (
	ErrInvalidVideoModelSpec  = errors.New("invalid video model specification")
	ErrInvalidVideoParameters = errors.New("invalid video parameters")
	videoParameterKeyPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	videoTaskEnvelopeKeys     = map[string]struct{}{
		"group": {}, "metadata": {}, "mode": {}, "model": {}, "prompt": {},
	}
)

type VideoModelSpec struct {
	Version         int                       `json:"version"`
	Modes           []string                  `json:"modes"`
	Parameters      []VideoParameterSpec      `json:"parameters"`
	ReferenceInputs []VideoReferenceInputSpec `json:"reference_inputs,omitempty"`
}

type VideoParameterSpec struct {
	Key        string                 `json:"key"`
	Label      string                 `json:"label"`
	Control    string                 `json:"control"`
	RequestKey string                 `json:"request_key,omitempty"`
	Modes      []string               `json:"modes,omitempty"`
	Required   bool                   `json:"required,omitempty"`
	Default    any                    `json:"default,omitempty"`
	Options    []VideoParameterOption `json:"options,omitempty"`
	Min        *float64               `json:"min,omitempty"`
	Max        *float64               `json:"max,omitempty"`
	Step       *float64               `json:"step,omitempty"`
}

type VideoParameterOption struct {
	Label string `json:"label"`
	Value any    `json:"value"`
}

type VideoReferenceInputSpec struct {
	Role       string `json:"role"`
	RequestKey string `json:"request_key"`
	Required   bool   `json:"required"`
}

func ValidateVideoModelSpec(spec VideoModelSpec, defaults map[string]any) error {
	if spec.Version <= 0 {
		return fmt.Errorf("%w: version must be positive", ErrInvalidVideoModelSpec)
	}
	if len(spec.Modes) == 0 || len(spec.Modes) > 3 {
		return fmt.Errorf("%w: at least one supported mode is required", ErrInvalidVideoModelSpec)
	}
	modes := make(map[string]struct{}, len(spec.Modes))
	for _, mode := range spec.Modes {
		if !isVideoMode(mode) {
			return fmt.Errorf("%w: unsupported mode %q", ErrInvalidVideoModelSpec, mode)
		}
		if _, duplicate := modes[mode]; duplicate {
			return fmt.Errorf("%w: duplicate mode %q", ErrInvalidVideoModelSpec, mode)
		}
		modes[mode] = struct{}{}
	}

	if len(spec.Parameters) > 32 {
		return fmt.Errorf("%w: at most 32 parameters are allowed", ErrInvalidVideoModelSpec)
	}
	parameters := make(map[string]VideoParameterSpec, len(spec.Parameters))
	requestKeys := make(map[string]string, len(spec.Parameters))
	for _, parameter := range spec.Parameters {
		if err := validateVideoParameterSpec(parameter, modes); err != nil {
			return err
		}
		if _, duplicate := parameters[parameter.Key]; duplicate {
			return fmt.Errorf("%w: duplicate parameter %q", ErrInvalidVideoModelSpec, parameter.Key)
		}
		requestKey := parameter.RequestKey
		if requestKey == "" {
			requestKey = parameter.Key
		}
		if _, reserved := videoTaskEnvelopeKeys[requestKey]; reserved || requestKey == "image" || requestKey == "images" {
			return fmt.Errorf("%w: parameter %q uses reserved request_key %q", ErrInvalidVideoModelSpec, parameter.Key, requestKey)
		}
		if existing, duplicate := requestKeys[requestKey]; duplicate {
			return fmt.Errorf("%w: parameters %q and %q use the same request_key", ErrInvalidVideoModelSpec, existing, parameter.Key)
		}
		requestKeys[requestKey] = parameter.Key
		parameters[parameter.Key] = parameter
	}
	if err := validateVideoReferenceInputs(spec.ReferenceInputs, modes, requestKeys); err != nil {
		return err
	}
	for key := range defaults {
		if _, ok := parameters[key]; !ok {
			return fmt.Errorf("%w: default for unknown parameter %q", ErrInvalidVideoModelSpec, key)
		}
	}
	for _, mode := range spec.Modes {
		if _, err := ValidateVideoParameters(spec, mode, defaults, true); err != nil {
			return fmt.Errorf("%w: defaults for mode %s: %v", ErrInvalidVideoModelSpec, mode, err)
		}
	}
	return nil
}

func isVideoMode(mode string) bool {
	switch mode {
	case VideoModeTextToVideo, VideoModeImageToVideo, VideoModeFirstLastFrame:
		return true
	default:
		return false
	}
}

func validateVideoParameterSpec(parameter VideoParameterSpec, modes map[string]struct{}) error {
	if !videoParameterKeyPattern.MatchString(parameter.Key) {
		return fmt.Errorf("%w: invalid parameter key %q", ErrInvalidVideoModelSpec, parameter.Key)
	}
	if strings.TrimSpace(parameter.Label) == "" || len(parameter.Label) > 128 {
		return fmt.Errorf("%w: parameter %q needs a label", ErrInvalidVideoModelSpec, parameter.Key)
	}
	if parameter.RequestKey != "" && !videoParameterKeyPattern.MatchString(parameter.RequestKey) {
		return fmt.Errorf("%w: invalid request_key %q", ErrInvalidVideoModelSpec, parameter.RequestKey)
	}
	for _, mode := range parameter.Modes {
		if _, ok := modes[mode]; !ok {
			return fmt.Errorf("%w: parameter %q references unsupported mode %q", ErrInvalidVideoModelSpec, parameter.Key, mode)
		}
	}
	switch parameter.Control {
	case VideoControlSegmented, VideoControlSelect:
		if len(parameter.Options) == 0 || len(parameter.Options) > 32 {
			return fmt.Errorf("%w: parameter %q needs 1-32 options", ErrInvalidVideoModelSpec, parameter.Key)
		}
		seen := make(map[string]struct{}, len(parameter.Options))
		for _, option := range parameter.Options {
			if strings.TrimSpace(option.Label) == "" {
				return fmt.Errorf("%w: parameter %q has an unlabeled option", ErrInvalidVideoModelSpec, parameter.Key)
			}
			canonical, ok := canonicalVideoScalar(option.Value)
			if !ok {
				return fmt.Errorf("%w: parameter %q option values must be scalar", ErrInvalidVideoModelSpec, parameter.Key)
			}
			if _, duplicate := seen[canonical]; duplicate {
				return fmt.Errorf("%w: parameter %q has duplicate option values", ErrInvalidVideoModelSpec, parameter.Key)
			}
			if err := validateVideoBillingMultiplierValue(parameter, option.Value); err != nil {
				return fmt.Errorf("%w: parameter %q option: %v", ErrInvalidVideoModelSpec, parameter.Key, err)
			}
			seen[canonical] = struct{}{}
		}
	case VideoControlSlider, VideoControlNumber:
		if parameter.Min == nil || parameter.Max == nil || parameter.Step == nil ||
			!isFinite(*parameter.Min) || !isFinite(*parameter.Max) || !isFinite(*parameter.Step) ||
			*parameter.Min > *parameter.Max || *parameter.Step <= 0 {
			return fmt.Errorf("%w: parameter %q has invalid numeric bounds", ErrInvalidVideoModelSpec, parameter.Key)
		}
		if err := validateVideoBillingMultiplierBounds(parameter); err != nil {
			return fmt.Errorf("%w: parameter %q: %v", ErrInvalidVideoModelSpec, parameter.Key, err)
		}
	case VideoControlSwitch:
		if len(parameter.Options) != 0 || parameter.Min != nil || parameter.Max != nil || parameter.Step != nil {
			return fmt.Errorf("%w: switch parameter %q cannot define options or numeric bounds", ErrInvalidVideoModelSpec, parameter.Key)
		}
	default:
		return fmt.Errorf("%w: parameter %q uses unsupported control %q", ErrInvalidVideoModelSpec, parameter.Key, parameter.Control)
	}
	if parameter.Default != nil {
		if err := validateVideoParameterValue(parameter, parameter.Default); err != nil {
			return fmt.Errorf("%w: default for %q: %v", ErrInvalidVideoModelSpec, parameter.Key, err)
		}
	}
	return nil
}

func validateVideoBillingMultiplierBounds(parameter VideoParameterSpec) error {
	minimum, maximum, bounded := videoBillingMultiplierBounds(parameter)
	if !bounded {
		return nil
	}
	if *parameter.Min < minimum || *parameter.Max > maximum {
		return fmt.Errorf("billing multiplier bounds must be between %v and %v", minimum, maximum)
	}
	return nil
}

func validateVideoBillingMultiplierValue(parameter VideoParameterSpec, value any) error {
	minimum, maximum, bounded := videoBillingMultiplierBounds(parameter)
	if !bounded {
		return nil
	}
	number, ok := videoBillingMultiplierNumber(value)
	if !ok || !isFinite(number) || number < minimum || number > maximum {
		return fmt.Errorf("billing multiplier must be between %v and %v", minimum, maximum)
	}
	return nil
}

func videoBillingMultiplierBounds(parameter VideoParameterSpec) (float64, float64, bool) {
	keys := []string{parameter.Key, parameter.RequestKey}
	for _, key := range keys {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "duration", "seconds", "duration_seconds":
			return 0, relaycommon.MaxTaskDurationSeconds, true
		case "n", "count", "batch", "batch_size":
			return 1, dto.MaxImageN, true
		}
	}
	return 0, 0, false
}

func videoBillingMultiplierNumber(value any) (float64, bool) {
	if number, ok := videoNumericValue(value); ok {
		return number, true
	}
	text, ok := value.(string)
	if !ok {
		return 0, false
	}
	number, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	return number, err == nil
}

func validateVideoReferenceInputs(inputs []VideoReferenceInputSpec, modes map[string]struct{}, requestKeys map[string]string) error {
	roles := make(map[string]VideoReferenceInputSpec, len(inputs))
	referenceRequestKeys := make(map[string]string, len(inputs))
	for _, input := range inputs {
		switch input.Role {
		case model.VideoTaskAssetRoleReference, model.VideoTaskAssetRoleReferenceVideo,
			model.VideoTaskAssetRoleFirstFrame, model.VideoTaskAssetRoleLastFrame:
		default:
			return fmt.Errorf("%w: unsupported reference role %q", ErrInvalidVideoModelSpec, input.Role)
		}
		if _, duplicate := roles[input.Role]; duplicate {
			return fmt.Errorf("%w: duplicate reference role %q", ErrInvalidVideoModelSpec, input.Role)
		}
		if !videoParameterKeyPattern.MatchString(input.RequestKey) {
			return fmt.Errorf("%w: reference role %q has invalid request_key", ErrInvalidVideoModelSpec, input.Role)
		}
		if _, reserved := videoTaskEnvelopeKeys[input.RequestKey]; reserved || input.RequestKey == "images" {
			return fmt.Errorf("%w: reference role %q uses reserved request_key %q", ErrInvalidVideoModelSpec, input.Role, input.RequestKey)
		}
		if parameter, duplicate := requestKeys[input.RequestKey]; duplicate {
			return fmt.Errorf("%w: reference role %q conflicts with parameter %q", ErrInvalidVideoModelSpec, input.Role, parameter)
		}
		if role, duplicate := referenceRequestKeys[input.RequestKey]; duplicate {
			return fmt.Errorf("%w: reference roles %q and %q use the same request_key", ErrInvalidVideoModelSpec, role, input.Role)
		}
		referenceRequestKeys[input.RequestKey] = input.Role
		roles[input.Role] = input
	}
	if _, supported := modes[VideoModeImageToVideo]; supported {
		requiredReferences := 0
		for _, role := range []string{model.VideoTaskAssetRoleReference, model.VideoTaskAssetRoleReferenceVideo} {
			if input, ok := roles[role]; ok && input.Required {
				requiredReferences++
			}
		}
		if requiredReferences != 1 {
			return fmt.Errorf("%w: image_to_video requires exactly one image or video reference mapping", ErrInvalidVideoModelSpec)
		}
	}
	if _, supported := modes[VideoModeFirstLastFrame]; supported {
		firstFrame, hasFirstFrame := roles[model.VideoTaskAssetRoleFirstFrame]
		if !hasFirstFrame || !firstFrame.Required {
			return fmt.Errorf("%w: first_last_frame requires a required first_frame mapping", ErrInvalidVideoModelSpec)
		}
		lastFrame, hasLastFrame := roles[model.VideoTaskAssetRoleLastFrame]
		if !hasLastFrame || !lastFrame.Required {
			return fmt.Errorf("%w: first_last_frame requires a required last_frame mapping", ErrInvalidVideoModelSpec)
		}
	}
	return nil
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
