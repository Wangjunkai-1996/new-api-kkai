package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type WalletTopUpHistoryQuery struct {
	Scope        WalletTopUpScope
	AllUsers     bool
	QueryScope   string
	StatusGroup  string
	Keyword      string
	ExactTradeNo string
	Offset       int
	Limit        int
}
type WalletTopUpHistoryResult struct {
	Orders     []*TopUp
	Total      int64
	Counts     map[string]int64
	QueryScope string
}

// Site-abandoned attempts are closed for user actions, independently from the
// remote payment facts. Confirmed receipts retain their accounting review.
const walletTopUpGroupSQL = `CASE
 WHEN status = 'success' THEN 'completed'
 WHEN status = 'credit_review' OR provider_payment_state = 'paid' THEN 'review'
 WHEN COALESCE(local_ended_at,0) > 0 THEN 'closed'
 WHEN status IN ('cancelled','expired','failed') AND COALESCE(closed_at,0) > 0 AND COALESCE(close_evidence_ref,'') <> '' AND (COALESCE(provider_payment_state,'') IN ('unpaid','failed') OR COALESCE(create_state,'') = 'not_created') THEN 'closed'
 WHEN COALESCE(create_time,0) <= 0 THEN 'all'
 WHEN create_time <= ? OR (COALESCE(pay_deadline_at,0) > 0 AND pay_deadline_at <= ?) OR (COALESCE(provider_expires_at,0) > 0 AND provider_expires_at <= ?) THEN 'closed'
 WHEN status = 'review_required' AND COALESCE(review_owner,'') <> '' THEN 'review'
 WHEN status = 'pending' AND wallet_lifecycle_version = 1 AND (create_state IN ('creating','unknown') OR COALESCE(close_requested_at,0) > 0 OR provider_payment_state IN ('processing','payment_processing')) THEN 'processing'
 WHEN status = 'pending' AND wallet_lifecycle_version = 1 AND create_state = 'ready' AND COALESCE(provider_session_id,'') <> '' THEN 'pending'
 ELSE 'all' END`

func QueryWalletTopUps(input WalletTopUpHistoryQuery) (*WalletTopUpHistoryResult, error) {
	if !WalletPaymentSchemaAvailable() {
		return nil, ErrWalletPaymentSchema
	}
	if !input.AllUsers {
		if err := validateWalletScope(input.Scope); err != nil {
			return nil, err
		}
	}
	if input.Offset < 0 || input.Limit < 1 || input.Limit > 100 || len(input.Keyword) > 255 || len(input.ExactTradeNo) > 255 || input.Keyword != "" && input.ExactTradeNo != "" {
		return nil, ErrResellerFinanceInvalidInput
	}
	if input.StatusGroup == "" {
		input.StatusGroup = "all"
	}
	switch input.StatusGroup {
	case "all", "pending", "processing", "review", "completed", "closed":
	default:
		return nil, ErrResellerFinanceInvalidInput
	}
	if input.QueryScope == "" {
		if input.AllUsers {
			input.QueryScope = "legacy_admin"
		} else {
			input.QueryScope = "recent"
		}
	}
	switch input.QueryScope {
	case "wallet", "recent", "unfinished", "legacy_admin":
	default:
		return nil, ErrResellerFinanceInvalidInput
	}
	result := &WalletTopUpHistoryResult{Orders: []*TopUp{}, Counts: map[string]int64{"all": 0, "pending": 0, "processing": 0, "review": 0, "completed": 0, "closed": 0}, QueryScope: input.QueryScope}
	now := common.GetTimestamp()
	err := DB.Transaction(func(tx *gorm.DB) error {
		q := WalletTopUpOnlyQuery(tx.Model(&TopUp{}))
		if input.AllUsers {
			if input.Scope.UserID > 0 {
				q = q.Where("user_id = ?", input.Scope.UserID)
			}
			if input.Scope.TenantID >= 0 {
				q = q.Where("tenant_id = ?", input.Scope.TenantID)
			}
		} else {
			q = walletScopeQuery(q, input.Scope)
		}
		if input.ExactTradeNo != "" {
			q = q.Where("trade_no = ?", input.ExactTradeNo)
			result.QueryScope = "exact"
		} else {
			switch input.QueryScope {
			case "recent":
				q = q.Where("create_time >= ?", now-topUpQueryWindowSeconds)
			case "wallet":
				q = q.Where("(create_time >= ? OR "+WalletTopUpHistoryRetainedSQL+")", now-topUpQueryWindowSeconds)
			case "unfinished":
				q = q.Where(WalletTopUpUnfinishedSQL, now-WalletTopUpAttemptTTLSeconds, now, now)
			}
			// Indexed prefix lookup preserves the old keyword affordance without an
			// unbounded leading-wildcard scan of all historical payment identities.
			if input.Keyword != "" {
				escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(input.Keyword)
				q = q.Where("trade_no LIKE ? ESCAPE '!'", escaped+"%")
			}
		}
		type countRow struct {
			StatusGroup string
			Count       int64
		}
		var rows []countRow
		if err := q.Session(&gorm.Session{}).Select(walletTopUpGroupSQL+" AS status_group, COUNT(*) AS count", now-WalletTopUpAttemptTTLSeconds, now, now).Group("status_group").Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			result.Counts["all"] += row.Count
			if row.StatusGroup != "all" {
				result.Counts[row.StatusGroup] = row.Count
			}
		}
		result.Total = result.Counts[input.StatusGroup]
		if input.StatusGroup != "all" {
			q = q.Where("("+walletTopUpGroupSQL+") = ?", now-WalletTopUpAttemptTTLSeconds, now, now, input.StatusGroup)
		}
		return q.Session(&gorm.Session{}).Order("id DESC").Offset(input.Offset).Limit(input.Limit).Find(&result.Orders).Error
	})
	return result, err
}
