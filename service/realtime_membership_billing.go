package service

import (
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
)

// PrepareRealtimeMembershipBudget bounds one manually started upstream response,
// including its entire context. Local counts do not bound retained audio/history.
func PrepareRealtimeMembershipBudget(info *relaycommon.RelayInfo, limits model.MembershipModelLimit, outputLimit int) error {
	if info == nil || limits.MaxInputTokens <= 0 || limits.MaxInputTokens > math.MaxInt32 || limits.MaxOutputTokens <= 0 || limits.MaxOutputTokens > math.MaxInt32 || strings.TrimSpace(limits.Evidence) == "" || outputLimit <= 0 || int64(outputLimit) > limits.MaxOutputTokens {
		return fmt.Errorf("configure verified aggregate input/output limits for this realtime model and use a finite max_output_tokens")
	}
	price := info.PriceData
	if len(price.OtherRatios()) != 0 {
		return fmt.Errorf("realtime membership pricing does not support additional request multipliers")
	}
	input, output := float64(limits.MaxInputTokens), float64(limits.MaxOutputTokens)
	maximum := 0.0
	if snapshot := info.TieredBillingSnapshot; snapshot != nil {
		variables := billingexpr.UsedVarsByHash(snapshot.ExprString, snapshot.ExprHash)
		for _, probe := range []string{"param", "header", "hour", "minute", "weekday", "month", "day"} {
			if variables[probe] {
				return fmt.Errorf("realtime membership pricing does not support request/header/time predicates; configure a token-only expression")
			}
		}
		frozen, err := billingexpr.FreezeRequestPricing(snapshot.ExprString, billingexpr.RequestInput{})
		if err != nil {
			return err
		}
		upper := *snapshot
		upper.ExprString, upper.ExprHash = frozen, billingexpr.ExprHashString(frozen)
		if upper.DisplayExprString == "" {
			upper.DisplayExprString = snapshot.ExprString
		}
		upper.GroupRatio = 1
		maximum, err = billingexpr.MaximumTokenQuota(&upper, billingexpr.TokenParams{P: input, C: output, Len: input, CR: input, AI: input, AO: output}, 1)
		if err != nil {
			return err
		}
		upper.GroupRatio = price.GroupRatioInfo.GroupRatio
		info.TieredBillingSnapshot = &upper
	} else if price.UsePrice {
		maximum = price.ModelPrice * common.QuotaPerUnit
	} else {
		for _, ratio := range []float64{price.ModelRatio, price.CompletionRatio, price.AudioRatio, price.AudioCompletionRatio} {
			if ratio < 0 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
				return model.ErrMembershipInvalidInput
			}
		}
		maximum = (input*max(1, price.AudioRatio) + output*max(price.CompletionRatio, price.AudioRatio*price.AudioCompletionRatio)) * price.ModelRatio
	}
	fee := 0.0
	if settings := model_setting.GetGrokSettings(); settings != nil && settings.ViolationDeductionEnabled {
		fee = settings.ViolationDeductionAmount * common.QuotaPerUnit
		if fee < 0 || math.IsNaN(fee) || math.IsInf(fee, 0) {
			return model.ErrMembershipInvalidInput
		}
	}
	maximum += fee
	if maximum < 0 || math.IsNaN(maximum) || math.IsInf(maximum, 0) {
		return model.ErrMembershipInvalidInput
	}
	info.MembershipPricing = &types.MembershipPricing{MaximumBeforeGroup: maximum, MaxInputTokens: int(limits.MaxInputTokens), MaxOutputTokens: outputLimit, OutputCopies: 1, QuotaPerUnit: common.QuotaPerUnit, ViolationFeeBeforeGroup: fee}
	return nil
}
