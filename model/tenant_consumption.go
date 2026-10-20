package model

import (
	"context"
	"errors"
	"math"
	"math/big"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// TenantConsumption persists both the reservation and final usage intent.
// A failed settlement is retried from this row; unknown upstream outcomes keep
// their reservation for operator reconciliation instead of inventing a refund.
type TenantConsumption struct {
	ID            int64  `gorm:"primaryKey"`
	RequestID     string `gorm:"type:varchar(128);not null;uniqueIndex"`
	TenantID      int64  `gorm:"not null;index"`
	CustomerID    int    `gorm:"not null;index"`
	TokenID       int    `gorm:"not null"`
	QuoteID       int64  `gorm:"not null"`
	ReservedQuota int64  `gorm:"not null"`
	ActualQuota   int64  `gorm:"not null"`
	Status        string `gorm:"type:varchar(24);not null;index"`
	ChannelID     int    `gorm:"not null"`
	LastError     string `gorm:"type:text"`
	CreatedAt     int64
	UpdatedAt     int64
}

var ErrTenantConsumptionConflict = errors.New("tenant consumption state conflict")

func ReserveTenantConsumption(ctx context.Context, db *gorm.DB, quoteInput ResellerPricingQuoteInput, tokenID int) (*TenantConsumption, error) {
	if db == nil || tokenID <= 0 || quoteInput.QuotedQuota <= 0 || quoteInput.QuotedQuota > math.MaxInt32 {
		return nil, ErrResellerFinanceInvalidInput
	}
	var record TenantConsumption
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).First(&user, quoteInput.CustomerID).Error; err != nil {
			return err
		}
		if user.TenantID != quoteInput.TenantID || user.Status != common.UserStatusEnabled {
			return ErrTenantMembershipMissing
		}
		err := tx.Where("request_id = ?", quoteInput.RequestID).First(&record).Error
		if err == nil {
			return ErrTenantConsumptionConflict
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if user.Quota < quoteInput.QuotedQuota {
			return ErrResellerInsufficientFunds
		}
		var pending int64
		if err := tx.Model(&TenantConsumption{}).Where("customer_id = ? AND tenant_id = ? AND (quote_id > 0 OR reserved_quota > 0) AND (status = ? OR (status = ? AND last_error <> ''))", user.Id, user.TenantID, "settlement_pending", "reserved").Count(&pending).Error; err != nil {
			return err
		}
		if pending > 0 {
			return ErrTenantConsumptionConflict
		}
		var token Token
		if err := lockForUpdate(tx).First(&token, tokenID).Error; err != nil {
			return err
		}
		if token.UserId != user.Id || token.TenantID != user.TenantID || token.Status != common.TokenStatusEnabled || (token.ExpiredTime != -1 && token.ExpiredTime < time.Now().Unix()) {
			return ErrTenantMembershipMissing
		}
		if !token.UnlimitedQuota && int64(token.RemainQuota) < quoteInput.QuotedQuota {
			return ErrResellerInsufficientFunds
		}
		var wallet ResellerWallet
		if err := tx.Where("tenant_id = ?", user.TenantID).First(&wallet).Error; err != nil {
			return err
		}
		if wallet.DebtMinor > 0 {
			return ErrResellerDebtOutstanding
		}
		// Gross paid funds must cover the immutable supply tariff. Payment fees
		// are accounted as an expense, not an implicit customer price increase.
		var lots []CustomerPaymentLot
		if err := tx.Where("tenant_id = ? AND customer_id = ? AND remaining_quota > 0 AND refund_status = ?", user.TenantID, user.Id, "none").Find(&lots).Error; err != nil {
			return err
		}
		unit, err := parseResellerExact(quoteInput.SupplierMinorPerQuota)
		if err != nil {
			return err
		}
		remaining := decimal.Zero
		for _, lot := range lots {
			if lot.CreditedQuota <= 0 {
				return ErrResellerFinanceInvalidInput
			}
			if big.NewRat(lot.PaidMinor, 1).Cmp(new(big.Rat).Mul(unit, big.NewRat(lot.CreditedQuota, 1))) < 0 {
				return ErrResellerPriceBelowCost
			}
			remaining = remaining.Add(decimal.NewFromInt(lot.RemainingQuota))
		}
		var reserved int64
		if err := tx.Model(&TenantConsumption{}).Where("customer_id = ? AND tenant_id = ? AND status IN ?", user.Id, user.TenantID, []string{"reserved", "refund_pending", "settlement_pending"}).Select("COALESCE(SUM(reserved_quota), 0)").Scan(&reserved).Error; err != nil {
			return err
		}
		// Reservations are already removed from the wallet but still belong to
		// unpaid-lot consumption. Include them when rejecting unbacked credits.
		backedQuota := decimal.NewFromInt(user.Quota).Add(decimal.NewFromInt(reserved))
		if reserved < 0 || remaining.LessThan(backedQuota) {
			return ErrResellerInsufficientFunds
		}
		quote, err := CreateResellerPricingQuoteTx(tx, quoteInput)
		if err != nil {
			return err
		}
		record = TenantConsumption{RequestID: quoteInput.RequestID, TenantID: user.TenantID, CustomerID: user.Id, TokenID: tokenID, QuoteID: quote.ID, ReservedQuota: quoteInput.QuotedQuota, Status: "reserved", CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if err := tx.Model(&user).Update("quota", gorm.Expr("quota - ?", record.ReservedQuota)).Error; err != nil {
			return err
		}
		if !token.UnlimitedQuota {
			return tx.Model(&token).Update("remain_quota", gorm.Expr("remain_quota - ?", record.ReservedQuota)).Error
		}
		return nil
	})
	if err == nil {
		TenantConsumptionAfterCommit(&record)
	}
	return &record, err
}

// ReserveTenantFreeConsumption retains the same durable request identity and
// statistics as paid requests without inventing a charge, payment lot or profit.
func ReserveTenantFreeConsumption(ctx context.Context, db *gorm.DB, tenantID int64, customerID, tokenID int, requestID string) (*TenantConsumption, error) {
	if db == nil || tenantID <= 0 || customerID <= 0 || tokenID <= 0 || requestID == "" || len(requestID) > 128 {
		return nil, ErrResellerFinanceInvalidInput
	}
	var record TenantConsumption
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Where("id = ? AND tenant_id = ? AND status = ?", customerID, tenantID, common.UserStatusEnabled).First(&user).Error; err != nil {
			return err
		}
		var token Token
		if err := lockForUpdate(tx).Where("id = ? AND tenant_id = ? AND user_id = ? AND status = ?", tokenID, tenantID, customerID, common.TokenStatusEnabled).First(&token).Error; err != nil {
			return err
		}
		if token.ExpiredTime != -1 && token.ExpiredTime < time.Now().Unix() {
			return ErrTenantMembershipMissing
		}
		var existing TenantConsumption
		if err := tx.Where("request_id = ?", requestID).First(&existing).Error; err == nil {
			return ErrTenantConsumptionConflict
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		record = TenantConsumption{RequestID: requestID, TenantID: tenantID, CustomerID: customerID, TokenID: tokenID, Status: "reserved", CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
		return tx.Create(&record).Error
	})
	return &record, err
}

// RecordTenantConsumptionResult commits usage before attempting settlement.
// Retrying this method cannot turn a successful request into a refund.
func RecordTenantConsumptionResult(db *gorm.DB, requestID string, actualQuota int64, channelID int, refund bool) error {
	if db == nil || requestID == "" || actualQuota < 0 || actualQuota > math.MaxInt32 || (refund && actualQuota != 0) {
		return ErrResellerFinanceInvalidInput
	}
	status := "settlement_pending"
	if refund {
		status = "refund_pending"
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var row TenantConsumption
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&row).Error; err != nil {
			return err
		}
		if row.QuoteID == 0 && actualQuota > 0 {
			return ErrResellerFinanceInvalidInput
		}
		if row.Status != "reserved" {
			if row.ActualQuota == actualQuota && ((refund && (row.Status == "refunded" || row.Status == status)) || (!refund && (row.Status == "settled" || row.Status == status))) {
				return nil
			}
			return ErrTenantConsumptionConflict
		}
		return tx.Model(&row).Updates(map[string]any{"status": status, "actual_quota": actualQuota, "channel_id": channelID, "updated_at": time.Now().Unix()}).Error
	})
}

// MarkTenantConsumptionUncertain retains the reservation after a request may
// have reached upstream. An operator must reconcile usage before resolving it.
func MarkTenantConsumptionUncertain(db *gorm.DB, requestID string, channelID int) error {
	if db == nil || requestID == "" {
		return ErrResellerFinanceInvalidInput
	}
	return db.Model(&TenantConsumption{}).Where("request_id = ? AND status = ?", requestID, "reserved").Updates(map[string]any{"channel_id": channelID, "last_error": "upstream_final_usage_unknown", "updated_at": time.Now().Unix()}).Error
}

func SettleTenantConsumption(ctx context.Context, db *gorm.DB, requestID string) error {
	var row TenantConsumption
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&row).Error; err != nil {
			return err
		}
		if row.QuoteID == 0 && (row.ActualQuota != 0 || row.ReservedQuota != 0) {
			return ErrResellerFinanceInvalidInput
		}
		if row.Status == "settled" || row.Status == "refunded" {
			return nil
		}
		if row.Status != "settlement_pending" && row.Status != "refund_pending" {
			return ErrTenantConsumptionConflict
		}
		var user User
		if err := lockForUpdate(tx).Where("id = ? AND tenant_id = ?", row.CustomerID, row.TenantID).First(&user).Error; err != nil {
			return err
		}
		var token Token
		if err := lockForUpdate(tx).Unscoped().Where("id = ? AND tenant_id = ? AND user_id = ?", row.TokenID, row.TenantID, row.CustomerID).First(&token).Error; err != nil {
			return err
		}
		delta := row.ActualQuota - row.ReservedQuota
		if delta > user.Quota || (!token.UnlimitedQuota && delta > int64(token.RemainQuota)) {
			return ErrResellerInsufficientFunds
		}
		if user.UsedQuota > math.MaxInt64-row.ActualQuota || int64(token.UsedQuota) > math.MaxInt64-row.ActualQuota {
			return ErrResellerFinanceInvalidInput
		}
		if row.ActualQuota > 0 {
			if _, err := SettleResellerConsumptionTx(tx, ResellerSettlementInput{TenantID: row.TenantID, CustomerID: row.CustomerID, RequestID: row.RequestID, QuoteID: row.QuoteID, ConsumedQuota: row.ActualQuota}); err != nil {
				return err
			}
		}
		updates := map[string]any{"quota": gorm.Expr("quota - ?", delta), "used_quota": gorm.Expr("used_quota + ?", row.ActualQuota)}
		if row.Status != "refund_pending" {
			updates["request_count"] = gorm.Expr("request_count + 1")
		}
		if err := tx.Model(&user).Updates(updates).Error; err != nil {
			return err
		}
		tokenUpdates := map[string]any{"used_quota": gorm.Expr("used_quota + ?", row.ActualQuota), "accessed_time": time.Now().Unix()}
		if !token.UnlimitedQuota {
			tokenUpdates["remain_quota"] = gorm.Expr("remain_quota - ?", delta)
		}
		if err := tx.Unscoped().Model(&token).Updates(tokenUpdates).Error; err != nil {
			return err
		}
		if row.ChannelID > 0 && row.ActualQuota > 0 {
			if err := tx.Model(&Channel{}).Where("id = ?", row.ChannelID).Update("used_quota", gorm.Expr("used_quota + ?", row.ActualQuota)).Error; err != nil {
				return err
			}
		}
		status := "settled"
		if row.Status == "refund_pending" {
			status = "refunded"
		}
		return tx.Model(&row).Updates(map[string]any{"status": status, "last_error": "", "updated_at": time.Now().Unix()}).Error
	})
	if err != nil {
		db.Model(&TenantConsumption{}).Where("request_id = ? AND status IN ?", requestID, []string{"settlement_pending", "refund_pending"}).Updates(map[string]any{"last_error": err.Error(), "updated_at": time.Now().Unix()})
		return err
	}
	TenantConsumptionAfterCommit(&row)
	return nil
}

func TenantConsumptionAfterCommit(row *TenantConsumption) {
	if row == nil {
		return
	}
	if err := InvalidateUserCache(row.CustomerID); err != nil {
		common.SysError("tenant user cache invalidation: " + err.Error())
	}
	var token Token
	if err := DB.Unscoped().Select("key").First(&token, row.TokenID).Error; err == nil {
		if err := invalidateTokenCacheForMutation(token.Key); err != nil {
			common.SysError("tenant token cache invalidation: " + err.Error())
		}
	}
}

func IncreaseTenantConsumptionReservation(db *gorm.DB, requestID string, target int64) error {
	if target < 0 || target > math.MaxInt32 {
		return ErrResellerFinanceInvalidInput
	}
	var row TenantConsumption
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&row).Error; err != nil {
			return err
		}
		if row.Status != "reserved" {
			return ErrTenantConsumptionConflict
		}
		if row.QuoteID == 0 && target > 0 {
			return ErrResellerFinanceInvalidInput
		}
		delta := target - row.ReservedQuota
		if delta <= 0 {
			return nil
		}
		var user User
		if err := lockForUpdate(tx).Where("id = ? AND tenant_id = ?", row.CustomerID, row.TenantID).First(&user).Error; err != nil {
			return err
		}
		var token Token
		if err := lockForUpdate(tx).Where("id = ? AND tenant_id = ? AND user_id = ?", row.TokenID, row.TenantID, row.CustomerID).First(&token).Error; err != nil {
			return err
		}
		if user.Quota < delta || (!token.UnlimitedQuota && int64(token.RemainQuota) < delta) {
			return ErrResellerInsufficientFunds
		}
		if err := tx.Model(&user).Update("quota", gorm.Expr("quota - ?", delta)).Error; err != nil {
			return err
		}
		if !token.UnlimitedQuota {
			if err := tx.Model(&token).Update("remain_quota", gorm.Expr("remain_quota - ?", delta)).Error; err != nil {
				return err
			}
		}
		return tx.Model(&row).Updates(map[string]any{"reserved_quota": target, "updated_at": time.Now().Unix()}).Error
	})
	if err == nil {
		TenantConsumptionAfterCommit(&row)
	}
	return err
}
