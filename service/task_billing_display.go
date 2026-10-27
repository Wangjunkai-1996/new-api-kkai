package service

import (
	"encoding/base64"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
)

// TaskBillingForDisplay reads frozen terms only. It never settles, refunds or
// substitutes current catalog prices for missing historical facts.
func TaskBillingForDisplay(task *model.Task) *dto.TaskBilling {
	if task == nil {
		return nil
	}
	result := &dto.TaskBilling{}
	switch state := task.PrivateData.BillingState; state {
	case model.TaskBillingStatePending, model.TaskBillingStateReserved, model.TaskBillingStateDispatching,
		model.TaskBillingStateAccepted, model.TaskBillingStateAmbiguous, model.TaskBillingStateRefunded, model.TaskBillingStateCompleted:
		result.State = state
	}
	switch source := task.PrivateData.BillingSource; source {
	case model.TaskBillingSourceWallet, model.TaskBillingSourceSubscription, model.MembershipSource:
		result.BillingSource = source
	}
	epoch, err := common.CurrentCreditEpoch()
	if err != nil || epoch == nil || task.ID <= 0 {
		if result.State == "" && result.BillingSource == "" {
			return nil
		}
		return result
	}
	if _, exists := epoch.LegacyMaxIDs["tasks"]; !exists {
		return result
	}
	if epoch.IsLegacy("tasks", task.ID) {
		result.PricingEpoch = common.CreditEpochLegacy
		return result
	}
	result.PricingEpoch = common.CreditEpochUSD
	bc := task.PrivateData.BillingContext
	if bc == nil {
		return result
	}
	// Historical non-pointer fields cannot distinguish an absent price from 0.
	// A positive frozen value is evidence; never invent a free unit price.
	if bc.ModelPrice > 0 {
		result.ModelPrice = publicTaskBillingNumber(bc.ModelPrice)
	}
	if bc.ModelRatio > 0 {
		result.ModelRatio = publicTaskBillingNumber(bc.ModelRatio)
	}
	if bc.GroupRatio > 0 {
		result.GroupRatio = publicTaskBillingNumber(bc.GroupRatio)
	}
	if accounting := task.PrivateData.AccountingContext; accounting != nil && accounting.HasUserGroupRatio {
		result.UserGroupRatio = publicTaskBillingNumber(accounting.UserGroupRatio)
	}
	for key, value := range bc.OtherRatios {
		if value <= 0 {
			continue
		}
		number := publicTaskBillingNumber(value)
		switch key {
		case "seconds":
			result.Seconds = number
		case "duration":
			result.Duration = number
		case "size":
			result.Size = number
		case "resolution":
			result.Resolution = number
		case "image_count":
			result.ImageCount = number
		}
	}
	if bc.PerCallBilling {
		result.BillingUnit = "request"
		if result.Seconds != nil {
			result.BillingUnit = "second"
		}
	}
	if snapshot := bc.TieredSnapshot; snapshot != nil {
		result.BillingMode = "tiered_expr"
		expression := snapshot.ExprString
		if snapshot.DisplayExprString != "" && task.TenantID <= model.PlatformTenantID {
			expression = snapshot.DisplayExprString
		}
		result.ExprB64 = base64.StdEncoding.EncodeToString([]byte(expression))
		result.MatchedTier = snapshot.EstimatedTier
		used := billingexpr.UsedUsageKeys(snapshot.ExprString)
		// Only audited numeric billing facts are public, even if an expression
		// references arbitrary strings or nested objects in a plugin snapshot.
		for _, key := range []string{"seconds", "duration", "image_count", "input_tokens", "output_tokens", "total_tokens", "audio_seconds"} {
			if !used[key] {
				continue
			}
			var value float64
			switch raw := snapshot.UsageFacts[key].(type) {
			case float64:
				value = raw
			case int:
				value = float64(raw)
			case int64:
				value = float64(raw)
			default:
				continue
			}
			if publicTaskBillingNumber(value) == nil {
				continue
			}
			if result.UsageFacts == nil {
				result.UsageFacts = make(map[string]float64)
			}
			result.UsageFacts[key] = value
			if key == "image_count" {
				result.ImageCount = publicTaskBillingNumber(value)
			}
		}
	}
	pricing := task.PrivateData.MembershipPricing
	if task.PrivateData.MembershipBillingRequestID == "" || pricing == nil {
		return result
	}
	if task.PrivateData.BillingSource == model.MembershipWalletSource && pricing.AccountID > 0 {
		fallback := true
		result.MembershipWalletFallback = &fallback
	}
	if task.PrivateData.BillingSource != model.MembershipSource {
		return result
	}
	result.MembershipTierName = pricing.TierName
	result.MembershipTierStyle = pricing.TierStyle
	result.MembershipDiscount = publicTaskBillingNumber(pricing.Discount)
	result.MembershipWalletRatio = publicTaskBillingNumber(pricing.WalletRatio)
	candidate := pricing.WalletRatio
	if pricing.Discount < 1 {
		candidate = pricing.BaseRatio * pricing.Discount
	}
	result.MembershipCandidateRatio = publicTaskBillingNumber(candidate)
	if pricing.WalletRatio > 0 {
		result.MembershipSavings = publicTaskBillingNumber(max(0, (pricing.WalletRatio-pricing.Price.GroupRatioInfo.GroupRatio)/pricing.WalletRatio))
	}
	return result
}

func publicTaskBillingNumber(value float64) *float64 {
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		common.SysError("invalid frozen task billing display value omitted")
		return nil
	}
	return &value
}
