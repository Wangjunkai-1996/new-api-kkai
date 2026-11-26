package model

import (
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const membershipStandardHoldAdmission = "MembershipStandardHoldAdmission"
const membershipStandardHoldScanLimit = 512

func tryMembershipStandardHoldAdmissionTx(tx *gorm.DB) (bool, error) {
	if tx.Dialector.Name() == "postgres" {
		// PostgreSQL keeps the two-int advisory namespace distinct from the
		// bigint keys used by the schema and email locks ("KKAI", "HOLD").
		var acquired bool
		// A non-default REPEATABLE READ/SERIALIZABLE role may already have an
		// old read view. It can still authorize the original amount.
		err := tx.Raw("SELECT current_setting('transaction_isolation') IN ('read committed', 'read uncommitted') AND pg_try_advisory_xact_lock(?, ?)", 0x4b4b4149, 0x484f4c44).Scan(&acquired).Error
		return acquired, err
	}
	anchor := Option{Key: membershipStandardHoldAdmission}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&anchor).Error; err != nil {
		return false, err
	}
	err := lockForUpdate(tx).First(&anchor).Error
	return err == nil, err
}

// Reserve the missing portion of each quote against the in-flight limits.
// Terminal rows leave the indexed set even when an older binary settles them.
// The admission lock serializes new standard holds. Ordinary snapshot reads
// may include settlements in progress, which only overstates exposure.
func membershipStandardHoldFitsTx(tx *gorm.DB, userID int, newRisk int64) (bool, error) {
	const globalLimit = 400 * membershipPilotMoneyScale
	const memberLimit = 100 * membershipPilotMoneyScale
	globalRemaining, memberRemaining := int64(globalLimit), int64(memberLimit)
	// New snapshots begin with the two numeric policy fields. The prefix below
	// includes both even at their maximum lengths. Legacy pilot snapshots need
	// their frozen unit from the JSON body. Ordinary prices carry no exposure.
	const projection = `user_id, reserved_quota, CASE
		WHEN substr(snapshot, 1, 32) = '{"standard_full_reserved_quota":' THEN substr(snapshot, 1, 128)
		WHEN snapshot LIKE '%"full_reserved_quota"%' OR snapshot LIKE '%"standard_full_reserved_quota"%' THEN snapshot
		ELSE '' END AS snapshot`
	var records []MembershipBillingRecord
	err := tx.Model(&MembershipBillingRecord{}).
		Select(projection).
		Where("state = ? AND billing_scope = ? AND source = ?", "reserved", MembershipBillingScopeRelay, MembershipSource).
		Limit(membershipStandardHoldScanLimit + 1).Find(&records).Error
	if err != nil || len(records) > membershipStandardHoldScanLimit {
		return false, err
	}
	for _, record := range records {
		if record.Snapshot == "" {
			continue
		}
		var full, micros int64
		if strings.Contains(record.Snapshot, `"standard_full_reserved_quota"`) {
			var err error
			full, err = strconv.ParseInt(gjson.Get(record.Snapshot, "standard_full_reserved_quota").Raw, 10, 64)
			if err != nil {
				return false, nil
			}
			micros, err = strconv.ParseInt(gjson.Get(record.Snapshot, "standard_hold_risk_micros").Raw, 10, 64)
			if err != nil {
				return false, nil
			}
		} else {
			var legacy struct {
				FullReservedQuota int64   `json:"full_reserved_quota"`
				QuotaPerUnit      float64 `json:"quota_per_unit"`
			}
			if err := common.UnmarshalJsonStr(record.Snapshot, &legacy); err != nil {
				return false, nil
			}
			full = legacy.FullReservedQuota
			micros = membershipStandardRiskMicros(full-record.ReservedQuota, legacy.QuotaPerUnit)
		}
		if full <= record.ReservedQuota || full > math.MaxInt32 || record.ReservedQuota < 1 || micros < 1 || micros > 2*membershipPilotMoneyScale {
			return false, nil
		}
		globalRemaining -= micros
		if record.UserID == userID {
			memberRemaining -= micros
		}
		if globalRemaining <= 0 || memberRemaining <= 0 {
			return false, nil
		}
	}
	return newRisk > 0 && newRisk <= min(globalRemaining, memberRemaining), nil
}

func membershipStandardRiskMicros(amount int64, quotaPerUnit float64) int64 {
	if amount < 0 || quotaPerUnit <= 0 || math.IsNaN(quotaPerUnit) || math.IsInf(quotaPerUnit, 0) {
		return math.MaxInt64
	}
	cost, remainder := decimal.New(amount, 6).QuoRem(decimal.NewFromFloat(quotaPerUnit), 0)
	if !remainder.IsZero() {
		cost = cost.Add(decimal.NewFromInt(1))
	}
	// A damaged price snapshot must exceed the limit without overflowing int64.
	if cost.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return math.MaxInt64
	}
	return cost.IntPart()
}
