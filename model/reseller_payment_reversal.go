package model

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// CustomerPaymentReversal records a confirmed, manual full-payment refund or
// chargeback. It never calls a payment provider or erases the original receipt.
type CustomerPaymentReversal struct {
	ID                       int64  `json:"id" gorm:"primaryKey"`
	TenantID                 int64  `json:"tenant_id" gorm:"not null;index;uniqueIndex:idx_customer_payment_reversal_idempotency,priority:1"`
	IdempotencyKey           string `json:"idempotency_key" gorm:"type:varchar(128);not null;uniqueIndex:idx_customer_payment_reversal_idempotency,priority:2"`
	PaymentLotID             int64  `json:"payment_lot_id" gorm:"not null;uniqueIndex"`
	TopUpID                  int64  `json:"topup_id" gorm:"not null;uniqueIndex"`
	CustomerID               int    `json:"customer_id" gorm:"not null;index"`
	Kind                     string `json:"kind" gorm:"type:varchar(16);not null"`
	PaidMinor                int64  `json:"paid_minor" gorm:"not null"`
	PaymentFeeMinor          int64  `json:"payment_fee_minor" gorm:"not null"`
	RemovedQuota             int64  `json:"removed_quota" gorm:"not null"`
	ReversedProfitMinorExact string `json:"reversed_profit_minor_exact" gorm:"type:varchar(128);not null"`
	LedgerEntryIDs           string `json:"ledger_entry_ids" gorm:"type:text;not null"`
	ActorID                  int    `json:"actor_id" gorm:"not null"`
	Reason                   string `json:"reason" gorm:"type:text;not null"`
	PaymentReference         string `json:"payment_reference" gorm:"type:varchar(192);not null"`
	PayloadHash              string `json:"-" gorm:"type:char(64);not null"`
	CreatedAt                int64  `json:"created_at" gorm:"not null"`
}

func (CustomerPaymentReversal) TableName() string { return "customer_payment_reversals" }

type CustomerPaymentReversalInput struct {
	TenantID         int64
	TopUpID          int64
	IdempotencyKey   string
	Kind             string
	ActorID          int
	Reason           string
	PaymentReference string
	Now              int64
}

func ReverseCustomerPaymentTx(tx *gorm.DB, input CustomerPaymentReversalInput) (*CustomerPaymentReversal, error) {
	if tx == nil || input.TenantID <= 0 || input.TopUpID <= 0 || input.ActorID <= 0 || strings.TrimSpace(input.IdempotencyKey) == "" || len(input.IdempotencyKey) > 128 || strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 2048 || strings.TrimSpace(input.PaymentReference) == "" || len(input.PaymentReference) > 192 || (input.Kind != "refunded" && input.Kind != "chargeback") {
		return nil, ErrResellerFinanceInvalidInput
	}
	payload := input
	payload.Now = 0
	hash, err := financePayloadHash(payload)
	if err != nil {
		return nil, err
	}
	now := normalizeFinanceNow(input.Now)
	// Same order as top-up finalization, then settlement's user -> wallet -> lot.
	// Never lock consumption rows while holding the user: settlement locks those
	// rows first. A plain in-flight check is safe because reservations lock user.
	var topup TopUp
	if err := lockForUpdate(tx).Where("id = ? AND tenant_id = ?", input.TopUpID, input.TenantID).First(&topup).Error; err != nil {
		return nil, err
	}
	var user User
	if err := lockForUpdate(tx).Unscoped().Where("id = ? AND tenant_id = ?", topup.UserId, input.TenantID).First(&user).Error; err != nil {
		return nil, err
	}
	wallet, err := resellerWalletForUpdate(tx, input.TenantID, now)
	if err != nil {
		return nil, err
	}
	var existing CustomerPaymentReversal
	if err := tx.Where("tenant_id = ? AND idempotency_key = ?", input.TenantID, input.IdempotencyKey).First(&existing).Error; err == nil {
		if existing.PayloadHash != hash {
			return nil, ErrResellerLedgerConflict
		}
		return &existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var lot CustomerPaymentLot
	if err := lockForUpdate(tx).Where("tenant_id = ? AND top_up_id = ? AND customer_id = ?", input.TenantID, input.TopUpID, user.Id).First(&lot).Error; err != nil {
		return nil, err
	}
	if lot.RefundStatus != "none" {
		return nil, ErrResellerLedgerConflict
	}
	if topup.Status != common.TopUpStatusSuccess || lot.CreditedQuota <= 0 || lot.ConsumedQuota < 0 || lot.RefundedQuota != 0 || lot.RemainingQuota < 0 || lot.ConsumedQuota > lot.CreditedQuota || lot.RemainingQuota != lot.CreditedQuota-lot.ConsumedQuota {
		return nil, ErrResellerWalletInvariant
	}
	var inFlight int64
	if err := tx.Model(&TenantConsumption{}).Where("tenant_id = ? AND customer_id = ? AND status IN ?", input.TenantID, user.Id, []string{"reserved", "settlement_pending", "refund_pending"}).Count(&inFlight).Error; err != nil {
		return nil, err
	}
	if inFlight != 0 {
		return nil, ErrResellerPaymentInFlight
	}
	if user.Quota < lot.RemainingQuota {
		return nil, ErrResellerWalletInvariant
	}
	reversal := &CustomerPaymentReversal{TenantID: input.TenantID, IdempotencyKey: input.IdempotencyKey, PaymentLotID: lot.ID, TopUpID: input.TopUpID, CustomerID: user.Id, Kind: input.Kind, PaidMinor: lot.PaidMinor, PaymentFeeMinor: lot.PaymentFeeMinor, RemovedQuota: lot.RemainingQuota, ActorID: input.ActorID, Reason: input.Reason, PaymentReference: input.PaymentReference, PayloadHash: hash, CreatedAt: now}
	if err := tx.Create(reversal).Error; err != nil {
		return nil, err
	}
	profit, entries, err := reversePaymentLotProfitsTx(tx, wallet, lot, reversal)
	if err != nil {
		return nil, err
	}
	encoded, err := common.Marshal(entries)
	if err != nil {
		return nil, err
	}
	reversal.ReversedProfitMinorExact, reversal.LedgerEntryIDs = profit.String(), string(encoded)
	if err := tx.Save(reversal).Error; err != nil {
		return nil, err
	}
	lot.RefundedQuota, lot.RemainingQuota, lot.RefundStatus, lot.UpdatedAt = lot.RemainingQuota, 0, input.Kind, now
	if err := tx.Save(&lot).Error; err != nil {
		return nil, err
	}
	if err := tx.Unscoped().Model(&user).Update("quota", gorm.Expr("quota - ?", reversal.RemovedQuota)).Error; err != nil {
		return nil, err
	}
	return reversal, nil
}

// Reverse a mixed-lot settlement then retain only its unaffected allocations.
// This uses its frozen supplier tariff and original release time, and preserves
// exact carry even when one request received a previous request's fractional fen.
func reversePaymentLotProfitsTx(tx *gorm.DB, wallet *ResellerWallet, lot CustomerPaymentLot, reversal *CustomerPaymentReversal) (decimal.Decimal, []int64, error) {
	quotes := tx.Model(&PricingQuote{}).Select("id").Where("tenant_id = ? AND customer_id = ?", lot.TenantID, lot.CustomerID)
	reversed := tx.Model(&ResellerLedgerEntry{}).Select("reversal_of").Where("tenant_id = ? AND kind = ?", lot.TenantID, ResellerLedgerKindChargeback)
	var settlements []ResellerLedgerEntry
	if err := tx.Where("tenant_id = ? AND kind = ? AND quote_id IN (?) AND operation_id NOT IN (?)", lot.TenantID, ResellerLedgerKindSettlement, quotes, reversed).Order("id").Find(&settlements).Error; err != nil {
		return decimal.Zero, nil, err
	}
	total, entryIDs := decimal.Zero, make([]int64, 0)
	for _, original := range settlements {
		var allocations []ResellerPaymentAllocation
		if err := common.UnmarshalJsonStr(original.PaymentAllocations, &allocations); err != nil {
			return decimal.Zero, nil, err
		}
		kept := make([]ResellerPaymentAllocation, 0, len(allocations))
		var removedQuota, removedSale, removedFee int64
		var allocatedQuota, allocatedSale, allocatedFee int64
		for _, allocation := range allocations {
			if allocation.Quota <= 0 || allocation.Quota > math.MaxInt32 || allocation.SaleMinor < 0 || allocation.FeeMinor < 0 || allocatedQuota > math.MaxInt32-allocation.Quota || allocatedSale > math.MaxInt64-allocation.SaleMinor || allocatedFee > math.MaxInt64-allocation.FeeMinor {
				return decimal.Zero, nil, ErrResellerWalletInvariant
			}
			allocatedQuota += allocation.Quota
			allocatedSale += allocation.SaleMinor
			allocatedFee += allocation.FeeMinor
			if allocation.LotID == lot.ID {
				removedQuota += allocation.Quota
				removedSale += allocation.SaleMinor
				removedFee += allocation.FeeMinor
			} else {
				kept = append(kept, allocation)
			}
		}
		if removedQuota == 0 {
			continue
		}
		if allocatedQuota <= 0 || allocatedSale != original.SaleMinor || allocatedFee != original.PaymentFeeMinor || removedQuota > lot.ConsumedQuota {
			return decimal.Zero, nil, ErrResellerWalletInvariant
		}
		var quote PricingQuote
		if err := tx.First(&quote, original.QuoteID).Error; err != nil {
			return decimal.Zero, nil, err
		}
		unit, err := parseResellerDecimal(quote.SupplierMinorPerQuota)
		if err != nil {
			return decimal.Zero, nil, err
		}
		supplier, err := parseResellerDecimal(original.SupplierMinorExact)
		if err != nil || !supplier.Equal(unit.Mul(decimal.NewFromInt(allocatedQuota))) {
			return decimal.Zero, nil, ErrResellerWalletInvariant
		}
		originalProfit, err := parseResellerDecimal(original.ExactProfitMinor)
		if err != nil || !originalProfit.Equal(decimal.NewFromInt(allocatedSale-allocatedFee).Sub(supplier)) {
			return decimal.Zero, nil, ErrResellerWalletInvariant
		}
		removedProfit := decimal.NewFromInt(removedSale - removedFee).Sub(unit.Mul(decimal.NewFromInt(removedQuota)))
		total = total.Add(removedProfit)
		entry, err := ReverseResellerSettlementTx(tx, ResellerChargebackInput{TenantID: lot.TenantID, OriginalOperation: original.OperationID, ActorID: reversal.ActorID, Reason: fmt.Sprintf("payment %s %d: %s", reversal.Kind, reversal.TopUpID, reversal.Reason), Now: reversal.CreatedAt})
		if err != nil {
			return decimal.Zero, nil, err
		}
		entryIDs = append(entryIDs, entry.ID)
		if len(kept) == 0 {
			continue
		}
		if err := tx.First(wallet, wallet.ID).Error; err != nil {
			return decimal.Zero, nil, err
		}
		profit := originalProfit.Sub(removedProfit)
		credit, err := accrueResellerPendingProfit(wallet, profit)
		if err != nil {
			return decimal.Zero, nil, err
		}
		wallet.UpdatedAt = reversal.CreatedAt
		if err := tx.Save(wallet).Error; err != nil {
			return decimal.Zero, nil, err
		}
		encoded, err := common.Marshal(kept)
		if err != nil {
			return decimal.Zero, nil, err
		}
		retained := &ResellerLedgerEntry{TenantID: lot.TenantID, WalletID: wallet.ID, OperationID: fmt.Sprintf("payment-retained:%d:%d", reversal.ID, original.ID), Kind: ResellerLedgerKindSettlement, AmountMinor: credit, PendingDelta: credit, QuoteID: original.QuoteID, SourceReference: original.SourceReference, ReversalOf: original.OperationID, AvailableAt: original.AvailableAt, CreatedAt: reversal.CreatedAt, SaleMinor: original.SaleMinor - removedSale, PaymentFeeMinor: original.PaymentFeeMinor - removedFee, SupplierMinorExact: supplier.Sub(unit.Mul(decimal.NewFromInt(removedQuota))).String(), ExactProfitMinor: profit.String(), PaymentAllocations: string(encoded), ActorID: reversal.ActorID, Reason: "retained allocations after payment reversal"}
		if _, err := createResellerLedgerEntry(tx, retained, struct{ ReversalID, OriginalID int64 }{reversal.ID, original.ID}); err != nil {
			return decimal.Zero, nil, err
		}
		entryIDs = append(entryIDs, retained.ID)
	}
	// Due retained profits restore their prior liquidity and repay any debt
	// caused by the full reversal before this transaction commits.
	if _, err := ReleaseResellerPendingTx(tx, lot.TenantID, reversal.CreatedAt, 0); err != nil {
		return decimal.Zero, nil, err
	}
	return total, entryIDs, nil
}
