package imagepricing

import (
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testConfig() Config {
	return Config{
		Version: "2026-08-04.v1",
		Enabled: true,
		Models: map[string]ModelConfig{
			"gpt-image-2": {
				DefaultSize: "1024x1024",
				Tiers: map[string]TierConfig{
					"1k": {UnitPrice: 0.67, Sizes: []string{"1024x1024"}},
					"2k": {UnitPrice: 1, Sizes: []string{"1536x1024", "1024x1536", "2048x2048", "2048x1152", "1152x2048"}},
					"4k": {UnitPrice: 1.34, Sizes: []string{"3840x2160", "2160x3840"}},
				},
			},
		},
	}
}

func TestPolicyResolveUsesExplicitDefaultAndExactTierPrices(t *testing.T) {
	policy, err := Compile(testConfig())
	require.NoError(t, err)

	tests := []struct {
		size      string
		wantSize  string
		wantTier  string
		wantPrice float64
	}{
		{size: "", wantSize: "1024x1024", wantTier: "1k", wantPrice: 0.67},
		{size: "1536x1024", wantSize: "1536x1024", wantTier: "2k", wantPrice: 1},
		{size: "1024x1536", wantSize: "1024x1536", wantTier: "2k", wantPrice: 1},
		{size: "3840x2160", wantSize: "3840x2160", wantTier: "4k", wantPrice: 1.34},
	}
	for _, test := range tests {
		t.Run(test.wantSize, func(t *testing.T) {
			resolution, configured, resolveErr := policy.Resolve("gpt-image-2", test.size)
			require.NoError(t, resolveErr)
			require.True(t, configured)
			assert.Equal(t, test.wantSize, resolution.Size)
			assert.Equal(t, test.wantTier, resolution.Tier)
			assert.Equal(t, test.wantPrice, resolution.UnitPrice)
		})
	}
}

func TestPolicyResolveFailsClosedForConfiguredUnknownSize(t *testing.T) {
	policy, err := Compile(testConfig())
	require.NoError(t, err)

	_, configured, err := policy.Resolve("gpt-image-2", "auto")
	require.True(t, configured)
	assert.ErrorIs(t, err, ErrUnsupportedSize)

	_, configured, err = policy.Resolve("other-model", "auto")
	require.NoError(t, err)
	assert.False(t, configured)
}

func TestCompileRejectsAmbiguousOrUnsafePolicies(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "missing version", mutate: func(config *Config) { config.Version = "" }},
		{name: "default not priced", mutate: func(config *Config) {
			config.Models["gpt-image-2"] = ModelConfig{DefaultSize: "512x512", Tiers: config.Models["gpt-image-2"].Tiers}
		}},
		{name: "duplicate size", mutate: func(config *Config) {
			model := config.Models["gpt-image-2"]
			tier := model.Tiers["4k"]
			tier.Sizes = append(tier.Sizes, "1024x1024")
			model.Tiers["4k"] = tier
			config.Models["gpt-image-2"] = model
		}},
		{name: "auto size", mutate: func(config *Config) {
			model := config.Models["gpt-image-2"]
			tier := model.Tiers["4k"]
			tier.Sizes = []string{"auto"}
			model.Tiers["4k"] = tier
			config.Models["gpt-image-2"] = model
		}},
		{name: "nan price", mutate: func(config *Config) {
			model := config.Models["gpt-image-2"]
			tier := model.Tiers["1k"]
			tier.UnitPrice = math.NaN()
			model.Tiers["1k"] = tier
			config.Models["gpt-image-2"] = model
		}},
		{name: "zero price", mutate: func(config *Config) {
			model := config.Models["gpt-image-2"]
			tier := model.Tiers["1k"]
			tier.UnitPrice = 0
			model.Tiers["1k"] = tier
			config.Models["gpt-image-2"] = model
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := testConfig()
			test.mutate(&config)
			_, err := Compile(config)
			assert.ErrorIs(t, err, ErrInvalidPolicy)
		})
	}
}

func TestValidateOutboundLocksSizeAndCount(t *testing.T) {
	snapshot := &Snapshot{
		PolicyVersion: "v1", PolicyHash: "hash", Model: "gpt-image-2", Size: "1024x1024", Tier: "1k",
		UnitPrice: 0.67, QuotaPerUnit: 500000, GroupRatio: 1, GroupSpecialRatio: -1, RequestedCount: 2,
	}
	require.NoError(t, ValidateOutbound(snapshot, "1024x1024", 2))
	assert.True(t, errors.Is(ValidateOutbound(snapshot, "2048x2048", 2), ErrOutboundMismatch))
	assert.True(t, errors.Is(ValidateOutbound(snapshot, "1024x1024", 3), ErrOutboundMismatch))
}

func TestCalculateQuotaUsesOneRoundedValueForRequestedAndActualCounts(t *testing.T) {
	snapshot := &Snapshot{
		PolicyVersion: "v1", PolicyHash: "hash", Model: "gpt-image-2", Size: "1024x1024", Tier: "1k",
		UnitPrice: 0.67, QuotaPerUnit: 500000, GroupRatio: 1.000002, RequestedCount: 2,
	}

	requested, err := CalculateQuotaStrict(snapshot, 2)
	require.NoError(t, err)
	assert.Equal(t, 670001, requested)

	actual, clamp, err := CalculateQuota(snapshot, 1)
	require.NoError(t, err)
	assert.Nil(t, clamp)
	assert.Equal(t, 335001, actual)

	overCompleted, err := CalculateQuotaStrict(snapshot, 3)
	require.NoError(t, err)
	assert.Equal(t, 1005002, overCompleted)
}
