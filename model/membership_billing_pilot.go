package model

import (
	"errors"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	membershipPilotHold         = "MembershipBillingPilotHoldSpent"
	membershipPilotEarlyRelease = "MembershipBillingPilotEarlyReleaseSpent"
	membershipPilotMoneyScale   = 1_000_000 // Micro-dollars.
)

var errMembershipPilotBudget = errors.New("invalid membership billing pilot budget")

func reconcileMembershipPilotHoldTx(tx *gorm.DB, record *MembershipBillingRecord, actual, charged int64) error {
	if record.Scope != MembershipBillingScopeRelay || record.Source != MembershipSource {
		return nil
	}
	full := gjson.Get(record.Snapshot, "full_reserved_quota").Int()
	if full <= record.ReservedQuota {
		return nil
	}
	held := full - record.ReservedQuota
	loss := min(actual, full) - charged
	if loss == held {
		return nil
	}
	unit := gjson.Get(record.Snapshot, "quota_per_unit").Float()
	ok, err := adjustMembershipPilotBudgetTx(tx, membershipPilotHold, record.UserID, loss, held, unit)
	if err != nil {
		return err
	}
	if !ok {
		return errMembershipPilotBudget
	}
	return nil
}

func MembershipPilotHoldEnabled(userID int) bool {
	return membershipPilotUserAllowed("MembershipPilotHoldUserIDs", "KKAI_MEMBERSHIP_PILOT_HOLD_USER_IDS", userID)
}

func MembershipPilotEarlyReleaseEnabled(userID int) bool {
	return membershipPilotUserAllowed("MembershipPilotEarlyReleaseUserIDs", "KKAI_MEMBERSHIP_PILOT_EARLY_RELEASE_USER_IDS", userID)
}

func membershipPilotUserAllowed(option, env string, userID int) bool {
	if userID <= 0 {
		return false
	}
	common.OptionMapRWMutex.RLock()
	raw, configured := common.OptionMap[option]
	common.OptionMapRWMutex.RUnlock()
	if !configured {
		raw = os.Getenv(env)
	}
	if raw == "" {
		return false
	}
	id := strconv.Itoa(userID)
	for value := range strings.SplitSeq(raw, ",") {
		if strings.TrimSpace(value) == id {
			return true
		}
	}
	return false
}

// The owning funds transaction locks one row per policy. Hold reductions
// reserve their full potential loss. Early releases spend their face value.
func spendMembershipPilotBudgetTx(tx *gorm.DB, kind string, userID int, quota int64, quotaPerUnit float64) (bool, error) {
	return adjustMembershipPilotBudgetTx(tx, kind, userID, quota, 0, quotaPerUnit)
}

// previouslyHeld is set only when completing a reduced hold. Replace that
// request's original risk allocation with its incurred loss.
func adjustMembershipPilotBudgetTx(tx *gorm.DB, kind string, userID int, quota, previouslyHeld int64, quotaPerUnit float64) (bool, error) {
	var requestLimit, userLimit int64
	switch kind {
	case membershipPilotHold:
		requestLimit, userLimit = 2*membershipPilotMoneyScale, 20*membershipPilotMoneyScale
	case membershipPilotEarlyRelease:
		requestLimit, userLimit = 6*membershipPilotMoneyScale, 35*membershipPilotMoneyScale
	default:
		return false, errMembershipPilotBudget
	}
	const totalLimit = 100 * membershipPilotMoneyScale

	if tx == nil || userID <= 0 || quota < 0 || (quota == 0 && previouslyHeld == 0) ||
		quotaPerUnit <= 0 || math.IsNaN(quotaPerUnit) || math.IsInf(quotaPerUnit, 0) {
		return false, nil
	}
	unit := decimal.NewFromFloat(quotaPerUnit)
	roundUp := func(amount int64) decimal.Decimal {
		cost, remainder := decimal.New(amount, 6).QuoRem(unit, 0)
		if !remainder.IsZero() {
			cost = cost.Add(decimal.NewFromInt(1))
		}
		return cost
	}
	cost := roundUp(quota)
	if cost.GreaterThan(decimal.NewFromInt(requestLimit)) {
		return false, nil
	}
	micros := cost.IntPart()

	option := Option{Key: kind, Value: "{}"}
	// This also acquires the SQLite write lock before reading the counter.
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&option).Error; err != nil {
		return false, err
	}
	if err := lockForUpdate(tx).First(&option).Error; err != nil {
		return false, err
	}
	var spentByUser map[int]int64
	if err := common.UnmarshalJsonStr(option.Value, &spentByUser); err != nil || spentByUser == nil {
		return false, errMembershipPilotBudget
	}
	var total int64
	for id, spent := range spentByUser {
		if id <= 0 || spent <= 0 || spent > userLimit || spent > totalLimit-total {
			return false, errMembershipPilotBudget
		}
		total += spent
	}
	if previouslyHeld > 0 {
		held := roundUp(previouslyHeld)
		if kind != membershipPilotHold || quota > previouslyHeld || held.GreaterThan(decimal.NewFromInt(spentByUser[userID])) {
			return false, errMembershipPilotBudget
		}
		micros -= held.IntPart()
	} else if micros > userLimit-spentByUser[userID] || micros > totalLimit-total {
		return false, nil
	}
	spentByUser[userID] += micros
	if spentByUser[userID] == 0 {
		delete(spentByUser, userID)
	}
	value, err := common.Marshal(spentByUser)
	if err != nil {
		return false, err
	}
	if err := tx.Model(&option).Update("value", string(value)).Error; err != nil {
		return false, err
	}
	return true, nil
}
