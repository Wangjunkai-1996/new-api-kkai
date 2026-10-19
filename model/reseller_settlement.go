package model

import (
	"errors"
	"math"
	"math/big"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// A lot allocation retains the exact source of every settled CNY fen.
type ResellerPaymentAllocation struct {
	LotID     int64 `json:"lot_id"`
	Quota     int64 `json:"quota"`
	SaleMinor int64 `json:"sale_minor"`
	FeeMinor  int64 `json:"fee_minor"`
}

// Cumulative division makes the final consumption receive the remainder; the
// sum of allocations always equals the original paid amount and payment fee.
func resellerLotCumulative(total, consumed, credited int64) int64 {
	value := new(big.Int).Mul(big.NewInt(total), big.NewInt(consumed))
	return value.Quo(value, big.NewInt(credited)).Int64()
}

func allocateResellerPaymentLotsTx(tx *gorm.DB, input ResellerSettlementInput, unit decimal.Decimal, now int64) ([]ResellerPaymentAllocation, int64, int64, error) {
	var lots []CustomerPaymentLot
	if err := lockForUpdate(tx).Where("tenant_id = ? AND customer_id = ? AND remaining_quota > 0 AND refund_status = ?", input.TenantID, input.CustomerID, "none").Order("id ASC").Find(&lots).Error; err != nil {
		return nil, 0, 0, err
	}
	remaining, sale, fee := input.ConsumedQuota, int64(0), int64(0)
	allocations := make([]ResellerPaymentAllocation, 0)
	for i := range lots {
		if remaining == 0 {
			break
		}
		lot := &lots[i]
		if lot.CreditedQuota <= 0 || lot.PaidMinor <= 0 || lot.PaymentFeeMinor < 0 || lot.PaymentFeeMinor > lot.PaidMinor || lot.ConsumedQuota < 0 || lot.ConsumedQuota > lot.CreditedQuota || lot.RemainingQuota != lot.CreditedQuota-lot.ConsumedQuota {
			return nil, 0, 0, ErrResellerWalletInvariant
		}
		// Compare products, not rounded per-request fen, to reject genuinely lossy prices.
		if decimal.NewFromInt(lot.PaidMinor - lot.PaymentFeeMinor).LessThan(unit.Mul(decimal.NewFromInt(lot.CreditedQuota))) {
			return nil, 0, 0, ErrResellerPriceBelowCost
		}
		consumed := min(remaining, lot.RemainingQuota)
		after := lot.ConsumedQuota + consumed
		cash := resellerLotCumulative(lot.PaidMinor, after, lot.CreditedQuota) - resellerLotCumulative(lot.PaidMinor, lot.ConsumedQuota, lot.CreditedQuota)
		paymentFee := resellerLotCumulative(lot.PaymentFeeMinor, after, lot.CreditedQuota) - resellerLotCumulative(lot.PaymentFeeMinor, lot.ConsumedQuota, lot.CreditedQuota)
		if cash < 0 || paymentFee < 0 || sale > math.MaxInt64-cash || fee > math.MaxInt64-paymentFee {
			return nil, 0, 0, ErrResellerWalletInvariant
		}
		sale += cash
		fee += paymentFee
		allocations = append(allocations, ResellerPaymentAllocation{LotID: lot.ID, Quota: consumed, SaleMinor: cash, FeeMinor: paymentFee})
		lot.ConsumedQuota = after
		lot.RemainingQuota -= consumed
		lot.UpdatedAt = now
		if err := tx.Save(lot).Error; err != nil {
			return nil, 0, 0, err
		}
		remaining -= consumed
	}
	if remaining != 0 {
		return nil, 0, 0, ErrResellerInsufficientFunds
	}
	return allocations, sale, fee, nil
}

// SettleResellerConsumptionTx participates in the caller's quota transaction.
// Lock order for every wallet mutation is wallet -> quote/settlement -> lots.
func SettleResellerConsumptionTx(tx *gorm.DB, input ResellerSettlementInput) (*ResellerLedgerEntry, error) {
	if tx == nil || input.TenantID <= 0 || input.CustomerID <= 0 || strings.TrimSpace(input.RequestID) == "" || len(input.RequestID) > 128 || input.QuoteID <= 0 || input.ConsumedQuota <= 0 || input.ConsumedQuota > math.MaxInt32 {
		return nil, ErrResellerFinanceInvalidInput
	}
	now := normalizeFinanceNow(input.Now)
	if now > math.MaxInt64-ResellerPendingPeriodSec {
		return nil, ErrResellerFinanceInvalidInput
	}
	wallet, err := resellerWalletForUpdate(tx, input.TenantID, now)
	if err != nil {
		return nil, err
	}
	var quote PricingQuote
	if err := lockForUpdate(tx).Where("id = ? AND tenant_id = ? AND customer_id = ? AND request_id = ?", input.QuoteID, input.TenantID, input.CustomerID, input.RequestID).First(&quote).Error; err != nil {
		return nil, err
	}
	unit, err := parseResellerDecimal(quote.SupplierMinorPerQuota)
	if err != nil || unit.IsNegative() {
		return nil, ErrResellerFinanceInvalidInput
	}
	payload := input
	payload.Now = 0
	hash, err := financePayloadHash(payload)
	if err != nil {
		return nil, err
	}
	operationID := "settlement:" + input.RequestID
	var existing ResellerLedgerEntry
	if err := lockForUpdate(tx).Where("operation_id = ?", operationID).First(&existing).Error; err == nil {
		if existing.PayloadHash != hash {
			return nil, ErrResellerLedgerConflict
		}
		return &existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	allocations, sale, fee, err := allocateResellerPaymentLotsTx(tx, input, unit, now)
	if err != nil {
		return nil, err
	}
	supply := unit.Mul(decimal.NewFromInt(input.ConsumedQuota))
	if supply.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return nil, ErrResellerFinanceInvalidInput
	}
	exactProfit := decimal.NewFromInt(sale).Sub(decimal.NewFromInt(fee)).Sub(supply)
	credit, err := accrueResellerPendingProfit(wallet, exactProfit)
	if err != nil {
		return nil, err
	}
	wallet.UpdatedAt = now
	if err := tx.Save(wallet).Error; err != nil {
		return nil, err
	}
	encoded, err := common.Marshal(allocations)
	if err != nil {
		return nil, err
	}
	entry := &ResellerLedgerEntry{TenantID: input.TenantID, WalletID: wallet.ID, OperationID: operationID, Kind: ResellerLedgerKindSettlement, AmountMinor: credit, PendingDelta: credit, QuoteID: quote.ID, SourceReference: input.RequestID, AvailableAt: now + ResellerPendingPeriodSec, CreatedAt: now, PayloadHash: hash, SaleMinor: sale, PaymentFeeMinor: fee, SupplierMinorExact: supply.String(), ExactProfitMinor: exactProfit.String(), PaymentAllocations: string(encoded)}
	if err := tx.Create(entry).Error; err != nil {
		return nil, err
	}
	return entry, nil
}

func ReleaseResellerPendingTx(tx *gorm.DB, tenantID int64, now int64, limit int) (int, error) {
	if tx == nil || tenantID <= 0 || limit < 0 {
		return 0, ErrResellerFinanceInvalidInput
	}
	now = normalizeFinanceNow(now)
	wallet, err := resellerWalletForUpdate(tx, tenantID, now)
	if err != nil {
		return 0, err
	}
	processed := tx.Model(&ResellerLedgerEntry{}).Select("reversal_of").Where("tenant_id = ? AND kind IN ?", tenantID, []string{ResellerLedgerKindPendingRelease, ResellerLedgerKindChargeback})
	query := lockForUpdate(tx).Where("tenant_id = ? AND kind = ? AND available_at <= ? AND operation_id NOT IN (?)", tenantID, ResellerLedgerKindSettlement, now, processed).Order("id ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	var settlements []ResellerLedgerEntry
	if err := query.Find(&settlements).Error; err != nil {
		return 0, err
	}
	released := 0
	for _, settlement := range settlements {
		releaseOp := "release:" + settlement.OperationID
		var existing ResellerLedgerEntry
		if err := lockForUpdate(tx).Where("operation_id = ?", releaseOp).First(&existing).Error; err == nil {
			continue
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return released, err
		}
		amount := settlement.AmountMinor
		if amount > wallet.PendingMinor {
			return released, ErrResellerWalletInvariant
		}
		if amount < 0 {
			return released, ErrResellerWalletInvariant
		}
		debtRepaid := min(amount, wallet.DebtMinor)
		if err := applyResellerWalletDelta(wallet, -amount, amount-debtRepaid, 0, -debtRepaid); err != nil {
			return released, err
		}
		wallet.UpdatedAt = now
		if err := tx.Save(wallet).Error; err != nil {
			return released, err
		}
		entry := &ResellerLedgerEntry{TenantID: tenantID, WalletID: wallet.ID, OperationID: releaseOp, Kind: ResellerLedgerKindPendingRelease, AmountMinor: amount, PendingDelta: -amount, AvailableDelta: amount - debtRepaid, DebtDelta: -debtRepaid, QuoteID: settlement.QuoteID, SourceReference: settlement.SourceReference, ReversalOf: settlement.OperationID, CreatedAt: now}
		if _, err := createResellerLedgerEntry(tx, entry, struct {
			Original string
			Amount   int64
		}{settlement.OperationID, amount}); err != nil {
			return released, err
		}
		released++
	}
	return released, nil
}

// ReverseResellerSettlementTx appends a chargeback entry. Once profit has
// become available, the available balance is consumed first and any shortfall
// becomes debt; debt is never clamped away.
func ReverseResellerSettlementTx(tx *gorm.DB, input ResellerChargebackInput) (*ResellerLedgerEntry, error) {
	if tx == nil || input.TenantID <= 0 || input.ActorID <= 0 || strings.TrimSpace(input.OriginalOperation) == "" || len(input.OriginalOperation) > 192 || strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 4096 {
		return nil, ErrResellerFinanceInvalidInput
	}
	now := normalizeFinanceNow(input.Now)
	wallet, err := resellerWalletForUpdate(tx, input.TenantID, now)
	if err != nil {
		return nil, err
	}
	var original ResellerLedgerEntry
	if err := lockForUpdate(tx).Where("tenant_id = ? AND operation_id = ? AND kind = ?", input.TenantID, input.OriginalOperation, ResellerLedgerKindSettlement).First(&original).Error; err != nil {
		return nil, err
	}
	operationID := "chargeback:" + input.OriginalOperation
	payload := struct {
		TenantID int64
		Original string
		Reason   string
		ActorID  int
	}{input.TenantID, input.OriginalOperation, input.Reason, input.ActorID}
	hash, err := financePayloadHash(payload)
	if err != nil {
		return nil, err
	}
	var existing ResellerLedgerEntry
	if err := lockForUpdate(tx).Where("operation_id = ?", operationID).First(&existing).Error; err == nil {
		if existing.PayloadHash != hash {
			return nil, ErrResellerLedgerConflict
		}
		return &existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var release ResellerLedgerEntry
	released := false
	if err := lockForUpdate(tx).Where("operation_id = ?", "release:"+input.OriginalOperation).First(&release).Error; err == nil {
		released = true
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	pendingDelta, availableDelta, debtDelta := int64(0), int64(0), int64(0)
	if released {
		available := original.AmountMinor
		if available > wallet.AvailableMinor {
			available = wallet.AvailableMinor
		}
		availableDelta = -available
		debtDelta = original.AmountMinor - available
	} else {
		if original.AmountMinor > wallet.PendingMinor {
			return nil, ErrResellerWalletInvariant
		}
		pendingDelta = -original.AmountMinor
	}
	if err := applyResellerWalletDelta(wallet, pendingDelta, availableDelta, 0, debtDelta); err != nil {
		return nil, err
	}
	carry, err := parseResellerDecimal(wallet.ExactProfitCarry)
	if err != nil {
		return nil, err
	}
	exact, err := parseResellerDecimal(original.ExactProfitMinor)
	if err != nil {
		return nil, err
	}
	wallet.ExactProfitCarry = carry.Sub(exact).Add(decimal.NewFromInt(original.AmountMinor)).String()
	wallet.UpdatedAt = now
	if err := tx.Save(wallet).Error; err != nil {
		return nil, err
	}
	supplier, err := parseResellerDecimal(original.SupplierMinorExact)
	if err != nil {
		return nil, err
	}
	entry := &ResellerLedgerEntry{TenantID: input.TenantID, WalletID: wallet.ID, OperationID: operationID, Kind: ResellerLedgerKindChargeback, AmountMinor: -original.AmountMinor, PendingDelta: pendingDelta, AvailableDelta: availableDelta, DebtDelta: debtDelta, QuoteID: original.QuoteID, SourceReference: original.SourceReference, ReversalOf: original.OperationID, CreatedAt: now, PayloadHash: hash, ActorID: input.ActorID, Reason: input.Reason, SaleMinor: -original.SaleMinor, PaymentFeeMinor: -original.PaymentFeeMinor, SupplierMinorExact: supplier.Neg().String(), ExactProfitMinor: exact.Neg().String()}
	if err := tx.Create(entry).Error; err != nil {
		return nil, err
	}
	return entry, nil
}
