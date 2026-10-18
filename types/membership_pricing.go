package types

import (
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
)

// MembershipPricing contains only billing facts. Request bodies, headers and
// credential values must never be copied into the durable funding ledger.
type MembershipPricing struct {
	MaxInputTokens          int                          `json:"max_input_tokens,omitempty"`
	ViolationFeeBeforeGroup float64                      `json:"violation_fee_before_group,omitempty"`
	MaxOutputTokens         int                          `json:"max_output_tokens,omitempty"`
	OutputCopies            int                          `json:"output_copies,omitempty"`
	ToolCallLimits          map[string]int               `json:"tool_call_limits,omitempty"`
	RequiresOutputBound     bool                         `json:"requires_output_bound,omitempty"`
	Version                 int                          `json:"version"`
	GroupKey                string                       `json:"group_key"`
	ModelName               string                       `json:"model_name"`
	TierID                  string                       `json:"tier_id,omitempty"`
	ReleaseID               int64                        `json:"release_id,omitempty"`
	AccountID               int64                        `json:"account_id,omitempty"`
	AccountRevision         int64                        `json:"account_revision,omitempty"`
	BaseRatio               float64                      `json:"base_ratio"`
	WalletRatio             float64                      `json:"wallet_ratio"`
	Discount                float64                      `json:"discount"`
	Price                   PriceData                    `json:"price"`
	OtherRatios             map[string]float64           `json:"other_ratios,omitempty"`
	QuotaPerUnit            float64                      `json:"quota_per_unit"`
	ToolPrices              map[string]float64           `json:"tool_prices,omitempty"`
	GeminiAudioInputPrice   float64                      `json:"gemini_audio_input_price"`
	Tiered                  *billingexpr.BillingSnapshot `json:"tiered,omitempty"`
	Image                   *imagepricing.Snapshot       `json:"image,omitempty"`
	MaximumBeforeGroup      float64                      `json:"maximum_before_group"`
}
