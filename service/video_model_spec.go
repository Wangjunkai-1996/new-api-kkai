package service

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

const (
	VideoModeTextToVideo       = "text_to_video"
	VideoModeImageToVideo      = "image_to_video"
	VideoModeReferenceToVideo  = "reference_to_video"
	VideoModeFirstFrameToVideo = "first_frame_to_video"
	VideoModeFirstLastFrame    = "first_last_frame"

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
		"group": {}, "metadata": {}, "mode": {}, "model": {}, "prompt": {}, "reference_assets": {}, "reference_images": {}, "reference_videos": {}, "reference_audios": {},
	}
)

type VideoModelSpec struct {
	Version         int                       `json:"version"`
	Modes           []string                  `json:"modes"`
	Parameters      []VideoParameterSpec      `json:"parameters"`
	ReferenceInputs []VideoReferenceInputSpec `json:"reference_inputs,omitempty"`
	ReferencePolicy *VideoReferencePolicy     `json:"reference_policy,omitempty"`
}

// VideoReferencePolicy describes the media contract for the unified Studio
// reference picker. A nil policy keeps the profile text-only; a non-nil policy
// must declare a finite total count before the profile can be enabled.
type VideoReferencePolicy struct {
	MaxTotalCount              *int                       `json:"max_total_count"`
	RequiresVisual             bool                       `json:"requires_visual"`
	RequiresVideo              bool                       `json:"requires_video"`
	AudioRequiresGenerateAudio bool                       `json:"audio_requires_generate_audio"`
	Image                      *VideoReferenceMediaPolicy `json:"image,omitempty"`
	Video                      *VideoReferenceMediaPolicy `json:"video,omitempty"`
	Audio                      *VideoReferenceMediaPolicy `json:"audio,omitempty"`
}

type VideoReferenceMediaPolicy struct {
	AllowedMIMETypes        []string `json:"allowed_mime_types,omitempty"`
	MaxCount                int      `json:"max_count"`
	MaxSizeBytes            int64    `json:"max_size_bytes"`
	MinWidth                int      `json:"min_width,omitempty"`
	MaxWidth                int      `json:"max_width,omitempty"`
	MinHeight               int      `json:"min_height,omitempty"`
	MaxHeight               int      `json:"max_height,omitempty"`
	MinAspectRatioExclusive float64  `json:"min_aspect_ratio_exclusive,omitempty"`
	MaxAspectRatioExclusive float64  `json:"max_aspect_ratio_exclusive,omitempty"`
	MinDurationSeconds      float64  `json:"min_duration_seconds,omitempty"`
	MaxDurationSeconds      float64  `json:"max_duration_seconds,omitempty"`
	MaxTotalDurationSeconds float64  `json:"max_total_duration_seconds,omitempty"`
	MinFrameRate            float64  `json:"min_frame_rate,omitempty"`
	MaxFrameRate            float64  `json:"max_frame_rate,omitempty"`
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
	if len(spec.Modes) == 0 || len(spec.Modes) > 4 {
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
	if len(spec.ReferenceInputs) != 0 {
		return fmt.Errorf("%w: reference_inputs is no longer supported", ErrInvalidVideoModelSpec)
	}
	if err := validateVideoReferencePolicy(spec.ReferencePolicy); err != nil {
		return err
	}
	if (containsVideoMode(spec.Modes, VideoModeReferenceToVideo) || containsVideoMode(spec.Modes, VideoModeFirstFrameToVideo) || containsVideoMode(spec.Modes, VideoModeFirstLastFrame)) && spec.ReferencePolicy == nil {
		return fmt.Errorf("%w: reference modes require reference_policy", ErrInvalidVideoModelSpec)
	}
	for key := range defaults {
		if _, ok := parameters[key]; !ok {
			return fmt.Errorf("%w: default for unknown parameter %q", ErrInvalidVideoModelSpec, key)
		}
	}
	for _, mode := range spec.Modes {
		modeDefaults := filterVideoParametersForMode(spec, mode, defaults)
		if _, err := ValidateVideoParameters(spec, mode, modeDefaults, true); err != nil {
			return fmt.Errorf("%w: defaults for mode %s: %v", ErrInvalidVideoModelSpec, mode, err)
		}
	}
	return nil
}

func validateVideoReferencePolicy(policy *VideoReferencePolicy) error {
	if policy == nil {
		return nil
	}
	if policy.MaxTotalCount != nil && *policy.MaxTotalCount <= 0 {
		return fmt.Errorf("%w: reference max_total_count must be positive when present", ErrInvalidVideoModelSpec)
	}
	if err := validateVideoReferenceMediaPolicy("image", policy.Image, true); err != nil {
		return err
	}
	if err := validateVideoReferenceMediaPolicy("video", policy.Video, false); err != nil {
		return err
	}
	if err := validateVideoReferenceMediaPolicy("audio", policy.Audio, false); err != nil {
		return err
	}
	if policy.RequiresVisual && policy.Image == nil && policy.Video == nil {
		return fmt.Errorf("%w: requires_visual needs image or video policy", ErrInvalidVideoModelSpec)
	}
	if policy.RequiresVideo && policy.Video == nil {
		return fmt.Errorf("%w: requires_video needs video policy", ErrInvalidVideoModelSpec)
	}
	return nil
}

func validateVideoReferenceMediaPolicy(kind string, policy *VideoReferenceMediaPolicy, image bool) error {
	if policy == nil {
		return nil
	}
	if policy.MaxCount <= 0 || policy.MaxSizeBytes <= 0 {
		return fmt.Errorf("%w: %s policy requires positive max_count and max_size_bytes", ErrInvalidVideoModelSpec, kind)
	}
	if len(policy.AllowedMIMETypes) == 0 {
		return fmt.Errorf("%w: %s policy requires allowed_mime_types", ErrInvalidVideoModelSpec, kind)
	}
	for _, mimeType := range policy.AllowedMIMETypes {
		mimeType = strings.ToLower(strings.TrimSpace(mimeType))
		if !strings.HasPrefix(mimeType, kind+"/") {
			return fmt.Errorf("%w: invalid %s mime type", ErrInvalidVideoModelSpec, kind)
		}
	}
	if image {
		if policy.MinWidth <= 0 || policy.MaxWidth < policy.MinWidth || policy.MinHeight <= 0 || policy.MaxHeight < policy.MinHeight {
			return fmt.Errorf("%w: image dimensions are invalid", ErrInvalidVideoModelSpec)
		}
		if !isFinite(policy.MinAspectRatioExclusive) || !isFinite(policy.MaxAspectRatioExclusive) ||
			policy.MinAspectRatioExclusive <= 0 || policy.MaxAspectRatioExclusive <= policy.MinAspectRatioExclusive {
			return fmt.Errorf("%w: image aspect ratio bounds are invalid", ErrInvalidVideoModelSpec)
		}
	}
	if policy.MinDurationSeconds < 0 || policy.MaxDurationSeconds < policy.MinDurationSeconds ||
		policy.MaxTotalDurationSeconds < 0 || !isFinite(policy.MinDurationSeconds) ||
		!isFinite(policy.MaxDurationSeconds) || !isFinite(policy.MaxTotalDurationSeconds) {
		return fmt.Errorf("%w: %s duration bounds are invalid", ErrInvalidVideoModelSpec, kind)
	}
	if !image && (policy.MinDurationSeconds <= 0 || policy.MaxDurationSeconds <= 0 || policy.MaxTotalDurationSeconds < policy.MaxDurationSeconds) {
		return fmt.Errorf("%w: %s duration limits are required", ErrInvalidVideoModelSpec, kind)
	}
	if kind == "video" && (policy.MinFrameRate <= 0 || policy.MaxFrameRate < policy.MinFrameRate ||
		!isFinite(policy.MinFrameRate) || !isFinite(policy.MaxFrameRate)) {
		return fmt.Errorf("%w: video frame rate bounds are invalid", ErrInvalidVideoModelSpec)
	}
	return nil
}

func isVideoMode(mode string) bool {
	switch mode {
	case VideoModeTextToVideo, VideoModeReferenceToVideo, VideoModeFirstFrameToVideo, VideoModeFirstLastFrame:
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

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
