package main

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/creditmigration"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
	"github.com/QuantumNous/new-api/setting/image_pricing_setting"
)

// checkPricingPlan exercises the real expression/image billing engines using
// the reviewed stored prices. It never opens a database or relay connection.
func checkPricingPlan(plan creditmigration.PricingPlan) (map[string]any, error) {
	if err := creditmigration.ValidatePricingPlan(plan); err != nil {
		return nil, err
	}
	var comparisons []map[string]any
	thresholds := regexp.MustCompile(`len\s*(?:<=|>=|<|>|==)\s*([0-9]+)`)
	for _, item := range plan.Models {
		if item.Path != "billing_setting.billing_expr" && item.Path != "billing_expr" {
			continue
		}
		lengths := map[int]bool{0: true, 1000: true, 1000000: true}
		for _, match := range thresholds.FindAllStringSubmatch(item.OldValue, -1) {
			threshold, _ := strconv.Atoi(match[1])
			for _, length := range []int{threshold - 1, threshold, threshold + 1} {
				lengths[length] = true
			}
		}
		ordered := make([]int, 0, len(lengths))
		for length := range lengths {
			ordered = append(ordered, length)
		}
		sort.Ints(ordered)
		for _, length := range ordered {
			params := billingexpr.TokenParams{P: float64(length), C: 1000, Len: float64(length), CR: 100, CC: 50, CC1h: 25, Img: 30, ImgCR: 10, ImgO: 20, AI: 15, AO: 10}
			old, oldTrace, err := billingexpr.RunExpr(item.OldValue, params)
			if err != nil {
				return nil, fmt.Errorf("source expression %s: %w", item.Key, err)
			}
			next, nextTrace, err := billingexpr.RunExpr(item.TargetValue, params)
			if err != nil || old < 0 || next < 0 || math.Abs(next-old/7) > math.Max(1e-9, math.Abs(old/7)*1e-12) || oldTrace.MatchedTier != nextTrace.MatchedTier || !reflect.DeepEqual(oldTrace.RequestRules, nextTrace.RequestRules) {
				return nil, fmt.Errorf("expression price/tier/request-rule mismatch for %s at len=%d: %v", item.Key, length, err)
			}
			oldQuota, oldClamp := common.QuotaRoundChecked(old / 1e6 * 500000 * 0.4)
			nextQuota, nextClamp := common.QuotaRoundChecked(next / 1e6 * 500000 * 0.2)
			if oldClamp != nil || nextClamp != nil || math.Abs(float64(nextQuota)-float64(oldQuota)/14) > 1 {
				return nil, fmt.Errorf("expression quota conversion mismatch for %s", item.Key)
			}
			comparisons = append(comparisons, map[string]any{"kind": "expression", "model": item.Key, "tokens": params, "tier": oldTrace.MatchedTier, "source_output": old, "target_output": next, "old_quota": oldQuota, "target_quota": nextQuota, "old_rmb": float64(oldQuota) / 500000 * 0.075, "target_rmb": float64(nextQuota) / 500000})
		}
	}
	for _, change := range plan.Options {
		if change.Key != "ImagePricingPolicy" || change.Before == nil {
			continue
		}
		var before, after imagepricing.Config
		if err := common.UnmarshalJsonStr(*change.Before, &before); err != nil {
			return nil, err
		}
		if err := common.UnmarshalJsonStr(change.After, &after); err != nil {
			return nil, err
		}
		if err := image_pricing_setting.UpdateByJSONString(change.After); err != nil {
			return nil, err
		}
		for model, config := range before.Models {
			for tier, price := range config.Tiers {
				for _, count := range []int{1, 4} {
					oldSnapshot := &imagepricing.Snapshot{PolicyVersion: before.Version, PolicyHash: "source-validated", Model: model, Size: price.Sizes[0], Tier: tier, UnitPrice: price.UnitPrice, QuotaPerUnit: 500000, GroupRatio: 1, RequestedCount: count}
					newSnapshot := *oldSnapshot
					newSnapshot.PolicyVersion, newSnapshot.PolicyHash = after.Version, image_pricing_setting.PolicyHash()
					newSnapshot.UnitPrice, newSnapshot.GroupRatio = after.Models[model].Tiers[tier].UnitPrice, 0.5
					oldQuota, err := imagepricing.CalculateQuotaStrict(oldSnapshot, count)
					if err != nil {
						return nil, err
					}
					newQuota, err := imagepricing.CalculateQuotaStrict(&newSnapshot, count)
					if err != nil || math.Abs(float64(newQuota)-float64(oldQuota)/14) > 1 {
						return nil, fmt.Errorf("image quota conversion mismatch for %s/%s", model, tier)
					}
					comparisons = append(comparisons, map[string]any{"kind": "image", "model": model, "tier": tier, "count": count, "old_quota": oldQuota, "target_quota": newQuota, "old_rmb": float64(oldQuota) / 500000 * 0.075, "target_rmb": float64(newQuota) / 500000, "policy_hash": newSnapshot.PolicyHash})
				}
			}
		}
	}
	return map[string]any{"migration_id": plan.MigrationID, "pricing_plan_hash": plan.PlanHash, "status": "passed", "cases": comparisons, "case_count": len(comparisons), "rmb_factor_before_quota_rounding": 20.0 / 21, "scope": "offline actual expression and image billing engine; current prices only; time rule structure preserved"}, nil
}
