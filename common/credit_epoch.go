package common

import (
	"errors"
	"math/big"
	"slices"
)

const (
	CreditEpochOption = "KKAIUSDCreditCutover"
	CreditEpochLegacy = "legacy_075"
	CreditEpochUSD    = "usd_credit_v1"
)

var ErrCreditEpochInvalid = errors.New("invalid credit currency epoch; billing is disabled")

// CreditEpochCutover is committed with balances and pricing during the offline
// migration. IDs, rather than timestamps, identify immutable legacy records.
type CreditEpochCutover struct {
	Version                 int              `json:"version"`
	MigrationID             string           `json:"migration_id"`
	SourceEpoch             string           `json:"source_epoch"`
	TargetEpoch             string           `json:"target_epoch"`
	Numerator               int64            `json:"numerator"`
	Denominator             int64            `json:"denominator"`
	PlanHash                string           `json:"plan_hash"`
	PricingPlanHash         string           `json:"pricing_plan_hash"`
	LegacyMaxIDs            map[string]int64 `json:"legacy_max_ids"`
	AppliedAt               int64            `json:"applied_at"`
	LegacyTopUpPolicy       string           `json:"legacy_topup_policy"`
	LegacyUsedRedemptionIDs []int64          `json:"legacy_used_redemption_ids"`
}

func ParseCreditEpochCutover(raw string) (*CreditEpochCutover, error) {
	if raw == "" {
		return nil, nil
	}
	var epoch CreditEpochCutover
	if err := UnmarshalJsonStr(raw, &epoch); err != nil {
		return nil, ErrCreditEpochInvalid
	}
	if epoch.Version != 1 || epoch.MigrationID == "" || epoch.SourceEpoch != CreditEpochLegacy ||
		epoch.TargetEpoch != CreditEpochUSD || epoch.Numerator != 3 || epoch.Denominator != 40 ||
		epoch.PlanHash == "" || epoch.PricingPlanHash == "" || epoch.AppliedAt <= 0 || epoch.LegacyMaxIDs == nil || epoch.LegacyTopUpPolicy != "hold" {
		return nil, ErrCreditEpochInvalid
	}
	for _, id := range epoch.LegacyMaxIDs {
		if id < 0 {
			return nil, ErrCreditEpochInvalid
		}
	}
	for index, id := range epoch.LegacyUsedRedemptionIDs {
		if id <= 0 || id > epoch.LegacyMaxIDs["redemptions"] || (index > 0 && epoch.LegacyUsedRedemptionIDs[index-1] >= id) {
			return nil, ErrCreditEpochInvalid
		}
	}
	return &epoch, nil
}

func (epoch *CreditEpochCutover) IsLegacyUsedRedemption(id int64) bool {
	if epoch == nil {
		return false
	}
	_, found := slices.BinarySearch(epoch.LegacyUsedRedemptionIDs, id)
	return found
}

func CurrentCreditEpoch() (*CreditEpochCutover, error) {
	OptionMapRWMutex.RLock()
	raw := OptionMap[CreditEpochOption]
	OptionMapRWMutex.RUnlock()
	return ParseCreditEpochCutover(raw)
}

func (epoch *CreditEpochCutover) IsLegacy(table string, id int64) bool {
	return epoch != nil && id > 0 && id <= epoch.LegacyMaxIDs[table]
}

// HistoricalCreditQuota converts a response projection without changing the
// immutable stored amount. A missing boundary must not label old funds as USD.
func HistoricalCreditQuota(table string, id int64, quota int) (int, error) {
	epoch, err := CurrentCreditEpoch()
	if err != nil {
		return 0, err
	}
	if epoch == nil {
		return quota, nil
	}
	if _, exists := epoch.LegacyMaxIDs[table]; !exists || id <= 0 {
		return 0, ErrCreditEpochInvalid
	}
	if epoch.IsLegacy(table, id) {
		return int(LegacyCreditToUSD(int64(quota))), nil
	}
	return quota, nil
}

// LegacyCreditToUSD uses exact 3/40 arithmetic with half-away-from-zero
// rounding. The result cannot overflow int64 because the scale is below one.
func LegacyCreditToUSD(value int64) int64 {
	n := big.NewInt(value)
	negative := n.Sign() < 0
	n.Abs(n).Mul(n, big.NewInt(3))
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(n, big.NewInt(40), r)
	if r.Int64() >= 20 {
		q.Add(q, big.NewInt(1))
	}
	if negative {
		q.Neg(q)
	}
	return q.Int64()
}
