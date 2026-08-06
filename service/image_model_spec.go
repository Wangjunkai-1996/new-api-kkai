package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/image_pricing_setting"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"
)

const (
	ImageControlSelect  = "select"
	ImageControlInteger = "integer"
	ImageControlBoolean = "boolean"
)

var (
	ErrInvalidImageModelSpec  = errors.New("invalid image model specification")
	ErrInvalidImageParameters = errors.New("invalid image parameters")
	imageParameterKeyPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	imageRequestFieldRules    = map[string]imageRequestFieldRule{
		"n":                  {control: ImageControlInteger, minimum: 1, maximum: dto.MaxImageN},
		"size":               {control: ImageControlSelect},
		"quality":            {control: ImageControlSelect},
		"style":              {control: ImageControlSelect},
		"background":         {control: ImageControlSelect},
		"output_format":      {control: ImageControlSelect},
		"output_compression": {control: ImageControlInteger, minimum: 0, maximum: 100},
		"moderation":         {control: ImageControlSelect},
		"watermark":          {control: ImageControlBoolean},
	}
)

type ImageModelSpec struct {
	Version    int                  `json:"version"`
	Parameters []ImageParameterSpec `json:"parameters"`
}

type ImageParameterSpec struct {
	Key        string                 `json:"key"`
	Label      string                 `json:"label"`
	Control    string                 `json:"control"`
	RequestKey string                 `json:"request_key"`
	Required   bool                   `json:"required,omitempty"`
	Options    []ImageParameterOption `json:"options,omitempty"`
	Min        *int                   `json:"min,omitempty"`
	Max        *int                   `json:"max,omitempty"`
}

type ImageParameterOption struct {
	Label string `json:"label"`
	Value any    `json:"value"`
}

type imageRequestFieldRule struct {
	control string
	minimum int
	maximum int
}

func ValidateImageModelSpec(spec ImageModelSpec, defaults map[string]any) error {
	if spec.Version <= 0 {
		return fmt.Errorf("%w: version must be positive", ErrInvalidImageModelSpec)
	}
	if len(spec.Parameters) > len(imageRequestFieldRules) {
		return fmt.Errorf("%w: too many parameters", ErrInvalidImageModelSpec)
	}
	parameters := make(map[string]ImageParameterSpec, len(spec.Parameters))
	requestKeys := make(map[string]string, len(spec.Parameters))
	for _, parameter := range spec.Parameters {
		if err := validateImageParameterSpec(parameter); err != nil {
			return err
		}
		if _, duplicate := parameters[parameter.Key]; duplicate {
			return fmt.Errorf("%w: duplicate parameter %q", ErrInvalidImageModelSpec, parameter.Key)
		}
		if existing, duplicate := requestKeys[parameter.RequestKey]; duplicate {
			return fmt.Errorf(
				"%w: parameters %q and %q map to the same request field",
				ErrInvalidImageModelSpec, existing, parameter.Key,
			)
		}
		parameters[parameter.Key] = parameter
		requestKeys[parameter.RequestKey] = parameter.Key
	}
	if defaults == nil {
		defaults = map[string]any{}
	}
	for key := range defaults {
		if _, exists := parameters[key]; !exists {
			return fmt.Errorf("%w: default for unknown parameter %q", ErrInvalidImageModelSpec, key)
		}
	}
	if _, err := ValidateImageParameters(spec, defaults, true); err != nil {
		return fmt.Errorf("%w: defaults: %v", ErrInvalidImageModelSpec, err)
	}
	return nil
}

func validateImageModelPricingCoverage(modelName string, spec ImageModelSpec) error {
	for _, parameter := range spec.Parameters {
		if parameter.RequestKey != "size" {
			continue
		}
		for _, option := range parameter.Options {
			size, ok := option.Value.(string)
			if !ok {
				return fmt.Errorf("%w: size option must be a string", ErrInvalidImageModelSpec)
			}
			_, configured, err := image_pricing_setting.Resolve(modelName, size)
			if !configured {
				continue
			}
			if err != nil {
				return fmt.Errorf("%w: size option %q is not covered by image pricing", ErrInvalidImageModelSpec, size)
			}
		}
	}
	return nil
}

func validateImageParameterSpec(parameter ImageParameterSpec) error {
	if !imageParameterKeyPattern.MatchString(parameter.Key) || strings.TrimSpace(parameter.Label) == "" || len(parameter.Label) > 128 {
		return fmt.Errorf("%w: invalid parameter %q", ErrInvalidImageModelSpec, parameter.Key)
	}
	if !imageParameterKeyPattern.MatchString(parameter.RequestKey) {
		return fmt.Errorf("%w: invalid request_key %q", ErrInvalidImageModelSpec, parameter.RequestKey)
	}
	rule, allowed := imageRequestFieldRules[parameter.RequestKey]
	if !allowed || parameter.Control != rule.control {
		return fmt.Errorf("%w: unsupported request field or control for %q", ErrInvalidImageModelSpec, parameter.Key)
	}
	switch parameter.Control {
	case ImageControlSelect:
		if len(parameter.Options) == 0 || len(parameter.Options) > 32 || parameter.Min != nil || parameter.Max != nil {
			return fmt.Errorf("%w: select parameter %q needs 1-32 options", ErrInvalidImageModelSpec, parameter.Key)
		}
		seen := make(map[string]struct{}, len(parameter.Options))
		for _, option := range parameter.Options {
			value, ok := option.Value.(string)
			if strings.TrimSpace(option.Label) == "" || len(option.Label) > 128 ||
				!ok || strings.TrimSpace(value) == "" || len(value) > 128 {
				return fmt.Errorf("%w: parameter %q has an invalid option", ErrInvalidImageModelSpec, parameter.Key)
			}
			if _, duplicate := seen[value]; duplicate {
				return fmt.Errorf("%w: parameter %q has duplicate options", ErrInvalidImageModelSpec, parameter.Key)
			}
			seen[value] = struct{}{}
		}
	case ImageControlInteger:
		if len(parameter.Options) != 0 || parameter.Min == nil || parameter.Max == nil ||
			*parameter.Min < rule.minimum || *parameter.Max > rule.maximum || *parameter.Min > *parameter.Max {
			return fmt.Errorf("%w: parameter %q has invalid integer bounds", ErrInvalidImageModelSpec, parameter.Key)
		}
	case ImageControlBoolean:
		if len(parameter.Options) != 0 || parameter.Min != nil || parameter.Max != nil {
			return fmt.Errorf("%w: boolean parameter %q cannot define options or bounds", ErrInvalidImageModelSpec, parameter.Key)
		}
	}
	return nil
}

func ValidateImageParameters(spec ImageModelSpec, values map[string]any, requireRequired bool) (map[string]any, error) {
	if values == nil {
		values = map[string]any{}
	}
	parameters := make(map[string]ImageParameterSpec, len(spec.Parameters))
	for _, parameter := range spec.Parameters {
		parameters[parameter.Key] = parameter
	}
	for key := range values {
		if _, exists := parameters[key]; !exists {
			return nil, fmt.Errorf("%w: unknown parameter %q", ErrInvalidImageParameters, key)
		}
	}
	result := make(map[string]any, len(values))
	for _, parameter := range spec.Parameters {
		value, exists := values[parameter.Key]
		if !exists {
			if requireRequired && parameter.Required {
				return nil, fmt.Errorf("%w: parameter %q is required", ErrInvalidImageParameters, parameter.Key)
			}
			continue
		}
		normalized, err := normalizeImageParameterValue(parameter, value)
		if err != nil {
			return nil, err
		}
		result[parameter.Key] = normalized
	}
	return result, nil
}

func normalizeImageParameterValue(parameter ImageParameterSpec, value any) (any, error) {
	switch parameter.Control {
	case ImageControlSelect:
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%w: parameter %q must be a string", ErrInvalidImageParameters, parameter.Key)
		}
		for _, option := range parameter.Options {
			if option.Value == text {
				return text, nil
			}
		}
	case ImageControlInteger:
		number, ok := imageInteger(value)
		if ok && parameter.Min != nil && parameter.Max != nil && number >= *parameter.Min && number <= *parameter.Max {
			if parameter.RequestKey == "n" && number > image_studio_setting.Get().MaxImagesPerGeneration {
				break
			}
			return number, nil
		}
	case ImageControlBoolean:
		flag, ok := value.(bool)
		if ok {
			return flag, nil
		}
	}
	return nil, fmt.Errorf("%w: invalid value for parameter %q", ErrInvalidImageParameters, parameter.Key)
}

func imageInteger(value any) (int, bool) {
	maxInt := int64(^uint(0) >> 1)
	minInt := -maxInt - 1
	switch number := value.(type) {
	case int:
		return number, true
	case int32:
		return int(number), true
	case int64:
		if number < minInt || number > maxInt {
			return 0, false
		}
		return int(number), true
	case float64:
		if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number ||
			number < float64(minInt) || number > float64(maxInt) {
			return 0, false
		}
		return int(number), true
	case json.Number:
		parsed, err := number.Int64()
		if err != nil || parsed < minInt || parsed > maxInt {
			return 0, false
		}
		return int(parsed), true
	default:
		return 0, false
	}
}

func BuildImageRelayRequest(modelName string, prompt string, spec ImageModelSpec, parameters map[string]any) (*dto.ImageRequest, error) {
	request := &dto.ImageRequest{Model: strings.TrimSpace(modelName), Prompt: strings.TrimSpace(prompt)}
	stream := false
	request.Stream = &stream
	for _, parameter := range spec.Parameters {
		value, exists := parameters[parameter.Key]
		if !exists {
			continue
		}
		switch parameter.RequestKey {
		case "n":
			number, _ := imageInteger(value)
			count := uint(number)
			request.N = &count
		case "size":
			request.Size, _ = value.(string)
		case "quality":
			request.Quality, _ = value.(string)
		case "watermark":
			flag, _ := value.(bool)
			request.Watermark = &flag
		default:
			encoded, err := common.Marshal(value)
			if err != nil {
				return nil, fmt.Errorf("encode image parameter %q: %w", parameter.Key, err)
			}
			raw := json.RawMessage(encoded)
			switch parameter.RequestKey {
			case "style":
				request.Style = raw
			case "background":
				request.Background = raw
			case "output_format":
				request.OutputFormat = raw
			case "output_compression":
				request.OutputCompression = raw
			case "moderation":
				request.Moderation = raw
			default:
				return nil, fmt.Errorf("%w: unsupported request field %q", ErrInvalidImageModelSpec, parameter.RequestKey)
			}
		}
	}
	if request.N == nil {
		count := uint(1)
		request.N = &count
	}
	if request.Model == "" || request.Prompt == "" || *request.N == 0 || *request.N > dto.MaxImageN {
		return nil, ErrInvalidImageParameters
	}
	return request, nil
}
