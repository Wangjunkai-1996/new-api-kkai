package imagepricing

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
)

var (
	ErrInvalidPolicy    = errors.New("invalid image pricing policy")
	ErrUnsupportedSize  = errors.New("image size is not priced")
	ErrOutboundMismatch = errors.New("outbound image request does not match billing snapshot")
)

// SizeConstraints describes the dimensions accepted by a provider. Values are
// intentionally explicit in the model profile so the UI and request path use
// the same contract. A nil profile capability means custom dimensions are not
// accepted.
type SizeConstraints struct {
	MaxEdge        int     `json:"max_edge"`
	Step           int     `json:"step"`
	MinPixels      int     `json:"min_pixels"`
	MaxPixels      int     `json:"max_pixels"`
	MaxAspectRatio float64 `json:"max_aspect_ratio"`
}

// DefaultSizeConstraints are the GPT Image 2 custom-dimension limits.
var DefaultSizeConstraints = SizeConstraints{
	MaxEdge:        3840,
	Step:           16,
	MinPixels:      655360,
	MaxPixels:      8294400,
	MaxAspectRatio: 3,
}

var customSizePattern = regexp.MustCompile(`^(\d+)x(\d+)$`)

// ParseSize returns positive width and height from a canonical WIDTHxHEIGHT
// value. It rejects whitespace and alternate separators so request hashes and
// billing snapshots remain stable.
func ParseSize(size string) (int, int, bool) {
	matches := customSizePattern.FindStringSubmatch(size)
	if len(matches) != 3 {
		return 0, 0, false
	}
	width, errWidth := strconv.Atoi(matches[1])
	height, errHeight := strconv.Atoi(matches[2])
	if errWidth != nil || errHeight != nil || width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

// ValidateSize checks a custom dimension against provider constraints and
// returns a canonical lower-case value on success.
func ValidateSize(size string, constraints SizeConstraints) (string, error) {
	width, height, ok := ParseSize(size)
	if !ok {
		return "", fmt.Errorf("%w: invalid size %q", ErrUnsupportedSize, size)
	}
	if err := ValidateSizeConstraints(constraints); err != nil {
		return "", err
	}
	if width > constraints.MaxEdge || height > constraints.MaxEdge || width%constraints.Step != 0 || height%constraints.Step != 0 {
		return "", fmt.Errorf("%w: size %dx%d exceeds edge or step constraints", ErrUnsupportedSize, width, height)
	}
	pixels := int64(width) * int64(height)
	if pixels < int64(constraints.MinPixels) || pixels > int64(constraints.MaxPixels) {
		return "", fmt.Errorf("%w: size %dx%d exceeds pixel constraints", ErrUnsupportedSize, width, height)
	}
	longEdge, shortEdge := width, height
	if shortEdge > longEdge {
		longEdge, shortEdge = shortEdge, longEdge
	}
	if float64(longEdge)/float64(shortEdge) > constraints.MaxAspectRatio {
		return "", fmt.Errorf("%w: size %dx%d exceeds aspect ratio constraints", ErrUnsupportedSize, width, height)
	}
	return fmt.Sprintf("%dx%d", width, height), nil
}

func ValidateSizeConstraints(constraints SizeConstraints) error {
	if constraints.MaxEdge <= 0 || constraints.MaxEdge > 32768 || constraints.Step <= 0 ||
		constraints.Step > constraints.MaxEdge || constraints.MinPixels <= 0 ||
		constraints.MaxPixels < constraints.MinPixels || constraints.MaxPixels > math.MaxInt32 ||
		constraints.MaxAspectRatio < 1 || math.IsNaN(constraints.MaxAspectRatio) ||
		math.IsInf(constraints.MaxAspectRatio, 0) {
		return fmt.Errorf("%w: invalid size constraints", ErrInvalidPolicy)
	}
	return nil
}

type Config struct {
	Version string                 `json:"version"`
	Enabled bool                   `json:"enabled"`
	Models  map[string]ModelConfig `json:"models"`
}

type ModelConfig struct {
	DefaultSize     string                `json:"default_size"`
	AllowCustomSize bool                  `json:"allow_custom_size,omitempty"`
	Tiers           map[string]TierConfig `json:"tiers"`
}

type TierConfig struct {
	UnitPrice float64  `json:"unit_price"`
	Sizes     []string `json:"sizes"`
}

type Resolution struct {
	Model     string  `json:"model"`
	Size      string  `json:"size"`
	Tier      string  `json:"tier"`
	UnitPrice float64 `json:"unit_price"`
}

// Snapshot is the complete per-request billing contract. Values are copied
// into the request before pre-consume and never read back from live settings.
type Snapshot struct {
	PolicyVersion     string  `json:"policy_version"`
	PolicyHash        string  `json:"policy_hash"`
	Model             string  `json:"model"`
	Size              string  `json:"size"`
	Tier              string  `json:"tier"`
	UnitPrice         float64 `json:"unit_price"`
	QuotaPerUnit      float64 `json:"quota_per_unit"`
	GroupRatio        float64 `json:"group_ratio"`
	GroupSpecialRatio float64 `json:"group_special_ratio"`
	HasSpecialRatio   bool    `json:"has_special_ratio"`
	RequestedCount    int     `json:"requested_count"`
}

type pricedSize struct {
	tier      string
	unitPrice float64
}

type compiledModel struct {
	defaultSize     string
	allowCustomSize bool
	sizes           map[string]pricedSize
	tiers           map[string]pricedSize
}

type Policy struct {
	version string
	enabled bool
	models  map[string]compiledModel
}

func Compile(config Config) (*Policy, error) {
	config.Version = strings.TrimSpace(config.Version)
	if config.Version == "" || len(config.Models) == 0 {
		return nil, fmt.Errorf("%w: version and models are required", ErrInvalidPolicy)
	}

	policy := &Policy{
		version: config.Version,
		enabled: config.Enabled,
		models:  make(map[string]compiledModel, len(config.Models)),
	}
	for rawModel, modelConfig := range config.Models {
		model := strings.TrimSpace(rawModel)
		modelConfig.DefaultSize = normalizeSize(modelConfig.DefaultSize)
		if model == "" || modelConfig.DefaultSize == "" || len(modelConfig.Tiers) == 0 {
			return nil, fmt.Errorf("%w: model, default_size, and tiers are required", ErrInvalidPolicy)
		}
		if _, duplicate := policy.models[model]; duplicate {
			return nil, fmt.Errorf("%w: duplicate model %q", ErrInvalidPolicy, model)
		}

		compiled := compiledModel{
			defaultSize:     modelConfig.DefaultSize,
			allowCustomSize: modelConfig.AllowCustomSize,
			sizes:           make(map[string]pricedSize),
			tiers:           make(map[string]pricedSize),
		}
		for rawTier, tierConfig := range modelConfig.Tiers {
			tier := strings.TrimSpace(rawTier)
			if tier == "" || tierConfig.UnitPrice <= 0 || math.IsNaN(tierConfig.UnitPrice) || math.IsInf(tierConfig.UnitPrice, 0) || len(tierConfig.Sizes) == 0 {
				return nil, fmt.Errorf("%w: tier %q has an invalid price or no sizes", ErrInvalidPolicy, rawTier)
			}
			compiled.tiers[tier] = pricedSize{tier: tier, unitPrice: tierConfig.UnitPrice}
			for _, rawSize := range tierConfig.Sizes {
				size := normalizeSize(rawSize)
				if size == "" || size == "auto" {
					return nil, fmt.Errorf("%w: tier %q has invalid size %q", ErrInvalidPolicy, tier, rawSize)
				}
				if existing, duplicate := compiled.sizes[size]; duplicate {
					return nil, fmt.Errorf("%w: size %q appears in tiers %q and %q", ErrInvalidPolicy, size, existing.tier, tier)
				}
				compiled.sizes[size] = pricedSize{tier: tier, unitPrice: tierConfig.UnitPrice}
			}
		}
		if modelConfig.AllowCustomSize {
			for _, tier := range []string{"1k", "2k", "4k"} {
				if _, ok := compiled.tiers[tier]; !ok {
					return nil, fmt.Errorf("%w: custom-size model %q requires %q, %q, and %q tiers", ErrInvalidPolicy, model, "1k", "2k", "4k")
				}
			}
		}
		if _, ok := compiled.sizes[compiled.defaultSize]; !ok {
			return nil, fmt.Errorf("%w: default size %q is not priced for model %q", ErrInvalidPolicy, compiled.defaultSize, model)
		}
		policy.models[model] = compiled
	}
	return policy, nil
}

func (p *Policy) Version() string {
	if p == nil {
		return ""
	}
	return p.version
}

func (p *Policy) AllowsCustomSize(model string) (bool, bool) {
	if p == nil || !p.enabled {
		return false, false
	}
	compiled, configured := p.models[strings.TrimSpace(model)]
	if !configured {
		return false, false
	}
	return compiled.allowCustomSize, true
}

func (p *Policy) Resolve(model, size string) (Resolution, bool, error) {
	if p == nil || !p.enabled {
		return Resolution{}, false, nil
	}
	model = strings.TrimSpace(model)
	compiled, configured := p.models[model]
	if !configured {
		return Resolution{}, false, nil
	}
	size = normalizeSize(size)
	if size == "" {
		size = compiled.defaultSize
	}
	priced, ok := compiled.sizes[size]
	if !ok && compiled.allowCustomSize {
		canonical, err := ValidateSize(size, DefaultSizeConstraints)
		if err != nil {
			return Resolution{}, true, err
		}
		size = canonical
		priced, ok = compiled.sizes[size]
		if !ok {
			priced, ok = compiled.customSizePrice(size)
		}
	}
	if !ok {
		return Resolution{}, true, fmt.Errorf("%w: model %q size %q", ErrUnsupportedSize, model, size)
	}
	return Resolution{Model: model, Size: size, Tier: priced.tier, UnitPrice: priced.unitPrice}, true, nil
}

func (compiled compiledModel) customSizePrice(size string) (pricedSize, bool) {
	width, height, ok := ParseSize(size)
	if !ok {
		return pricedSize{}, false
	}
	pixels := int64(width) * int64(height)
	tier := ""
	switch {
	case pixels <= 1048576:
		tier = "1k"
	case pixels <= 4194304:
		tier = "2k"
	case pixels <= 8294400:
		tier = "4k"
	default:
		return pricedSize{}, false
	}
	priced, ok := compiled.tiers[tier]
	return priced, ok
}

func ValidateSnapshot(snapshot *Snapshot) error {
	if snapshot == nil || strings.TrimSpace(snapshot.PolicyVersion) == "" || strings.TrimSpace(snapshot.PolicyHash) == "" ||
		strings.TrimSpace(snapshot.Model) == "" || normalizeSize(snapshot.Size) == "" || strings.TrimSpace(snapshot.Tier) == "" ||
		snapshot.UnitPrice <= 0 || math.IsNaN(snapshot.UnitPrice) || math.IsInf(snapshot.UnitPrice, 0) ||
		snapshot.QuotaPerUnit <= 0 || math.IsNaN(snapshot.QuotaPerUnit) || math.IsInf(snapshot.QuotaPerUnit, 0) ||
		snapshot.GroupRatio < 0 || math.IsNaN(snapshot.GroupRatio) || math.IsInf(snapshot.GroupRatio, 0) ||
		snapshot.RequestedCount <= 0 {
		return ErrInvalidPolicy
	}
	return nil
}

func ValidateOutbound(snapshot *Snapshot, size string, count int) error {
	if err := ValidateSnapshot(snapshot); err != nil {
		return err
	}
	if normalizeSize(size) != snapshot.Size || count != snapshot.RequestedCount {
		return fmt.Errorf(
			"%w: expected size=%s n=%d, got size=%s n=%d",
			ErrOutboundMismatch, snapshot.Size, snapshot.RequestedCount, normalizeSize(size), count,
		)
	}
	return nil
}

// CalculateQuota applies the one rounding rule used by image pricing from
// quote through settlement. The count is bounded at request and response
// boundaries; streaming providers may legitimately report more completed
// images than the requested count, which must increase the final charge.
func CalculateQuota(snapshot *Snapshot, count int) (int, *common.QuotaClamp, error) {
	if err := ValidateSnapshot(snapshot); err != nil {
		return 0, nil, err
	}
	if count <= 0 {
		return 0, nil, fmt.Errorf("%w: invalid billed image count %d", ErrOutboundMismatch, count)
	}
	quota := decimal.NewFromFloat(snapshot.UnitPrice).
		Mul(decimal.NewFromFloat(snapshot.QuotaPerUnit)).
		Mul(decimal.NewFromFloat(snapshot.GroupRatio)).
		Mul(decimal.NewFromInt(int64(count)))
	value, clamp := common.QuotaFromDecimalChecked(quota)
	return value, clamp, nil
}

func CalculateQuotaStrict(snapshot *Snapshot, count int) (int, error) {
	quota, clamp, err := CalculateQuota(snapshot, count)
	if err != nil {
		return 0, err
	}
	if clamp != nil {
		return 0, clamp
	}
	return quota, nil
}

func normalizeSize(size string) string {
	return strings.ToLower(strings.TrimSpace(size))
}
