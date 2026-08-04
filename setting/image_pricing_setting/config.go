package image_pricing_setting

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
)

const (
	OptionKey     = "ImagePricingPolicy"
	HashOptionKey = "ImagePricingPolicyHash"
)

type state struct {
	policy *imagepricing.Policy
	hash   string
	raw    string
}

type Match struct {
	Resolution    imagepricing.Resolution
	PolicyVersion string
	PolicyHash    string
}

var current atomic.Pointer[state]

func init() {
	if err := UpdateByJSONString(DefaultJSON()); err != nil {
		panic(err)
	}
}

func DefaultConfig() imagepricing.Config {
	return imagepricing.Config{
		Version: "2026-08-04.v1",
		Enabled: false,
		Models: map[string]imagepricing.ModelConfig{
			"gpt-image-2": {
				DefaultSize: "1024x1024",
				Tiers: map[string]imagepricing.TierConfig{
					"1k": {UnitPrice: 0.67, Sizes: []string{"1024x1024"}},
					"2k": {UnitPrice: 1, Sizes: []string{"1536x1024", "1024x1536", "2048x2048", "2048x1152", "1152x2048"}},
					"4k": {UnitPrice: 1.34, Sizes: []string{"3840x2160", "2160x3840"}},
				},
			},
		},
	}
}

func DefaultJSON() string {
	encoded, err := common.Marshal(DefaultConfig())
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func ValidateJSON(raw string) error {
	_, err := compile(raw)
	return err
}

func UpdateByJSONString(raw string) error {
	next, err := compile(raw)
	if err != nil {
		return err
	}
	current.Store(next)
	return nil
}

func Resolve(model, size string) (Match, bool, error) {
	loaded := current.Load()
	if loaded == nil || loaded.policy == nil {
		return Match{}, false, nil
	}
	resolution, configured, err := loaded.policy.Resolve(model, size)
	if err != nil || !configured {
		return Match{}, configured, err
	}
	return Match{
		Resolution: resolution, PolicyVersion: loaded.policy.Version(), PolicyHash: loaded.hash,
	}, true, nil
}

func PolicyHash() string {
	loaded := current.Load()
	if loaded == nil {
		return ""
	}
	return loaded.hash
}

func JSON() string {
	loaded := current.Load()
	if loaded == nil {
		return DefaultJSON()
	}
	return loaded.raw
}

func compile(raw string) (*state, error) {
	var config imagepricing.Config
	if err := common.UnmarshalJsonStr(raw, &config); err != nil {
		return nil, fmt.Errorf("decode image pricing policy: %w", err)
	}
	policy, err := imagepricing.Compile(config)
	if err != nil {
		return nil, err
	}
	canonical, err := common.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encode image pricing policy: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return &state{policy: policy, hash: hex.EncodeToString(digest[:]), raw: string(canonical)}, nil
}
