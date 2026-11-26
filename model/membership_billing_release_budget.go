package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	membershipPilotCompensationReason    = "pilot_early_no_evidence_compensation"
	membershipStandardCompensationReason = "standard_early_no_evidence_compensation"
	membershipEarlyReleaseHistoryLimit   = 512
)

// Called inside the recovery transaction after it locks the shared control
// and leader rows. Both versions serialize compensation on those same locks,
// before MySQL REPEATABLE READ establishes its first consistent table snapshot.
// The ledger's atomic terminal adjustment is also the durable budget entry.
func membershipStandardReleaseFitsTx(tx *gorm.DB, userID int, quota int64, unit float64, now int64) (bool, error) {
	cost := membershipStandardRiskMicros(quota, unit)
	if cost <= 0 || cost > 6*membershipPilotMoneyScale {
		return false, nil
	}
	remainingGlobal := int64(150 * membershipPilotMoneyScale)
	remainingMember := int64(35 * membershipPilotMoneyScale)
	cutoff := now - 24*60*60
	var history []MembershipBillingRecord
	// Both early policies require admission before age two hours. Give the
	// indexed (state,created_at) range an extra minute for the worker's bounded
	// transaction after admission. Charge by the actual resolution timestamp.
	err := tx.Model(&MembershipBillingRecord{}).Select("user_id", "reserved_quota", "snapshot").
		Where("state = ? AND created_at >= ?", "released", cutoff-membershipRelayCompensationDelay-60).
		Where("billing_scope = ? AND source = ? AND review_resolved_at > ?", MembershipBillingScopeRelay, MembershipSource, cutoff).
		Where("review_reason IN ?", []string{membershipPilotCompensationReason, membershipStandardCompensationReason}).
		Limit(membershipEarlyReleaseHistoryLimit + 1).Find(&history).Error
	if err != nil || len(history) > membershipEarlyReleaseHistoryLimit {
		return false, err
	}
	for _, row := range history {
		var pricing struct {
			QuotaPerUnit float64 `json:"quota_per_unit"`
		}
		if row.UserID <= 0 || row.ReservedQuota <= 0 || common.UnmarshalJsonStr(row.Snapshot, &pricing) != nil {
			return false, nil
		}
		spent := membershipStandardRiskMicros(row.ReservedQuota, pricing.QuotaPerUnit)
		if spent > remainingGlobal || (row.UserID == userID && spent > remainingMember) {
			return false, nil
		}
		remainingGlobal -= spent
		if row.UserID == userID {
			remainingMember -= spent
		}
	}
	return cost <= min(remainingGlobal, remainingMember), nil
}
