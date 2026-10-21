package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	ResellerCurrencyCNY        = "CNY"
	ResellerPaymentFeeRate     = "0.016"
	ResellerPendingPeriodSec   = int64(24 * 60 * 60)
	ResellerMinWithdrawalMinor = int64(10000)
)

const (
	ResellerLedgerKindSettlement         = "consumption_settlement"
	ResellerLedgerKindPendingRelease     = "pending_release"
	ResellerLedgerKindWithdrawal         = "withdrawal"
	ResellerLedgerKindWithdrawalReversal = "withdrawal_reversal"
	ResellerLedgerKindWithdrawalPaid     = "withdrawal_paid"
	ResellerLedgerKindChargeback         = "chargeback"
)

const (
	ResellerWithdrawalRequested = "requested"
	ResellerWithdrawalApproved  = "approved"
	ResellerWithdrawalRejected  = "rejected"
	ResellerWithdrawalFailed    = "failed"
	ResellerWithdrawalPaid      = "paid"
)

var (
	ErrResellerFinanceInvalidInput = errors.New("invalid reseller finance input")
	ErrResellerQuoteConflict       = errors.New("reseller pricing quote conflicts with existing request")
	ErrResellerPriceBelowCost      = errors.New("reseller sale price is below supplier cost")
	ErrResellerWalletNotFound      = errors.New("reseller wallet not found")
	ErrResellerInsufficientFunds   = errors.New("insufficient reseller available funds")
	ErrResellerDebtOutstanding     = errors.New("reseller debt must be cleared before withdrawal")
	ErrResellerWithdrawalConflict  = errors.New("reseller withdrawal is not in a mutable state")
	ErrResellerLedgerConflict      = errors.New("reseller ledger operation conflicts with existing operation")
	ErrResellerWalletInvariant     = errors.New("reseller wallet invariant violated")
	ErrResellerPaymentInFlight     = errors.New("customer has in-flight requests; reconcile them before reversing payment")
)

// PricingQuote is an immutable request-time pricing snapshot. It is never
// recomputed from current settings during settlement.
type PricingQuote struct {
	ID                    int64  `json:"id" gorm:"primaryKey"`
	TenantID              int64  `json:"tenant_id" gorm:"not null;index"`
	CustomerID            int    `json:"customer_id" gorm:"not null;index"`
	RequestID             string `json:"request_id" gorm:"type:varchar(128);not null;uniqueIndex"`
	PriceVersionID        int64  `json:"price_version_id" gorm:"not null;index"`
	Model                 string `json:"model" gorm:"type:varchar(128);not null"`
	Group                 string `json:"group" gorm:"column:group;type:varchar(64);not null"`
	SupplierPriceMinor    int64  `json:"supplier_price_minor" gorm:"not null"`
	SupplierMinorPerQuota string `json:"supplier_minor_per_quota" gorm:"type:varchar(64);not null"`
	SaleMinorPerQuota     string `json:"sale_minor_per_quota" gorm:"type:varchar(64);not null"`
	SupplySnapshot        string `json:"supply_snapshot" gorm:"type:text;not null"`
	SalesMultiplier       string `json:"sales_multiplier" gorm:"type:varchar(64);not null"`
	SaleMinor             int64  `json:"sale_minor" gorm:"not null"`
	PaymentFeeMinor       int64  `json:"payment_fee_minor" gorm:"not null"`
	ProfitMinor           int64  `json:"profit_minor" gorm:"not null"`
	ProfitMinorExact      string `json:"profit_minor_exact" gorm:"type:varchar(128);not null"`
	QuotedQuota           int64  `json:"quoted_quota" gorm:"not null"`
	Currency              string `json:"currency" gorm:"type:varchar(8);not null"`
	PayloadHash           string `json:"payload_hash" gorm:"type:char(64);not null"`
	CreatedAt             int64  `json:"created_at" gorm:"not null"`
}

func (PricingQuote) TableName() string { return "pricing_quotes" }

// CustomerPaymentLot ties paid CNY to the quota credited by KKAI. It is the
// only supported source for future cash allocation; quota itself is not money.
type CustomerPaymentLot struct {
	ID              int64  `json:"id" gorm:"primaryKey"`
	TenantID        int64  `json:"tenant_id" gorm:"not null;index"`
	CustomerID      int    `json:"customer_id" gorm:"not null;index"`
	TopUpID         int64  `json:"topup_id" gorm:"not null;uniqueIndex"`
	OperationID     string `json:"operation_id" gorm:"type:varchar(128);not null;uniqueIndex"`
	PaidMinor       int64  `json:"paid_minor" gorm:"not null"`
	PaymentFeeMinor int64  `json:"payment_fee_minor" gorm:"not null"`
	CreditedQuota   int64  `json:"credited_quota" gorm:"not null"`
	ConsumedQuota   int64  `json:"consumed_quota" gorm:"not null"`
	RefundedQuota   int64  `json:"refunded_quota" gorm:"not null;default:0"`
	RemainingQuota  int64  `json:"remaining_quota" gorm:"not null"`
	RefundStatus    string `json:"refund_status" gorm:"type:varchar(16);not null"`
	CreatedAt       int64  `json:"created_at" gorm:"not null"`
	UpdatedAt       int64  `json:"updated_at" gorm:"not null"`
	PayloadHash     string `json:"payload_hash" gorm:"type:char(64);not null"`
}

func (CustomerPaymentLot) TableName() string { return "customer_payment_lots" }

// ResellerWallet is a materialized summary. Every mutation has a matching
// append-only ledger entry and is performed under the same database transaction.
type ResellerWallet struct {
	ID               int64                  `json:"id" gorm:"primaryKey"`
	TenantID         int64                  `json:"tenant_id" gorm:"not null;uniqueIndex"`
	PendingMinor     int64                  `json:"pending_minor" gorm:"not null"`
	AvailableMinor   int64                  `json:"available_minor" gorm:"not null"`
	FrozenMinor      int64                  `json:"frozen_minor" gorm:"not null"`
	DebtMinor        int64                  `json:"debt_minor" gorm:"not null"`
	ExactProfitCarry ResellerExactAggregate `json:"exact_profit_carry" gorm:"not null"`
	Version          int64                  `json:"version" gorm:"not null"`
	CreatedAt        int64                  `json:"created_at" gorm:"not null"`
	UpdatedAt        int64                  `json:"updated_at" gorm:"not null"`
}

func (ResellerWallet) TableName() string { return "reseller_wallets" }

// ResellerLedgerEntry is append-only. Bucket deltas describe the wallet
// movement and AmountMinor records the economic amount for audit/reporting.
type ResellerLedgerEntry struct {
	ID                 int64  `json:"id" gorm:"primaryKey"`
	TenantID           int64  `json:"tenant_id" gorm:"not null;index"`
	WalletID           int64  `json:"wallet_id" gorm:"not null;index"`
	OperationID        string `json:"operation_id" gorm:"type:varchar(192);not null;uniqueIndex"`
	Kind               string `json:"kind" gorm:"type:varchar(32);not null;index"`
	AmountMinor        int64  `json:"amount_minor" gorm:"not null"`
	SaleMinor          int64  `json:"sale_minor" gorm:"not null"`
	PaymentFeeMinor    int64  `json:"payment_fee_minor" gorm:"not null"`
	SupplierMinorExact string `json:"supplier_minor_exact" gorm:"type:varchar(128);not null"`
	ExactProfitMinor   string `json:"exact_profit_minor" gorm:"type:varchar(128);not null"`
	PaymentAllocations string `json:"payment_allocations" gorm:"type:text;not null"`
	ActorID            int    `json:"actor_id" gorm:"not null"`
	Reason             string `json:"reason" gorm:"type:text;not null"`
	PendingDelta       int64  `json:"pending_delta" gorm:"not null"`
	AvailableDelta     int64  `json:"available_delta" gorm:"not null"`
	FrozenDelta        int64  `json:"frozen_delta" gorm:"not null"`
	DebtDelta          int64  `json:"debt_delta" gorm:"not null"`
	QuoteID            int64  `json:"quote_id" gorm:"not null;index"`
	SourceReference    string `json:"source_reference" gorm:"type:varchar(192);not null;index"`
	ReversalOf         string `json:"reversal_of" gorm:"type:varchar(192);not null;index"`
	AvailableAt        int64  `json:"available_at" gorm:"not null;index"`
	PayloadHash        string `json:"payload_hash" gorm:"type:char(64);not null"`
	CreatedAt          int64  `json:"created_at" gorm:"not null;index"`
}

func (ResellerLedgerEntry) TableName() string { return "reseller_ledger_entries" }

type ResellerWithdrawal struct {
	ID                int64  `json:"id" gorm:"primaryKey"`
	TenantID          int64  `json:"tenant_id" gorm:"not null;index;uniqueIndex:idx_reseller_withdrawal_idempotency,priority:1"`
	WalletID          int64  `json:"wallet_id" gorm:"not null;index"`
	IdempotencyKey    string `json:"idempotency_key" gorm:"type:varchar(128);not null;uniqueIndex:idx_reseller_withdrawal_idempotency,priority:2"`
	AmountMinor       int64  `json:"amount_minor" gorm:"not null"`
	FeeMinor          int64  `json:"fee_minor" gorm:"not null"`
	NetMinor          int64  `json:"net_minor" gorm:"not null"`
	Status            string `json:"status" gorm:"type:varchar(16);not null;index"`
	PaymentRef        string `json:"payment_ref" gorm:"type:varchar(192);not null"`
	AlipayAccount     string `json:"alipay_account" gorm:"type:varchar(254);not null"`
	AlipayName        string `json:"alipay_name" gorm:"type:varchar(128);not null"`
	FeePolicySnapshot string `json:"fee_policy_snapshot" gorm:"type:varchar(512);not null"`
	ReviewedBy        int    `json:"reviewed_by" gorm:"not null"`
	ReviewedAt        int64  `json:"reviewed_at" gorm:"not null"`
	PaidAt            int64  `json:"paid_at" gorm:"not null"`
	Timeline          string `json:"timeline" gorm:"type:text;not null"`
	PayloadHash       string `json:"-" gorm:"type:char(64);not null"`
	ReviewNote        string `json:"review_note" gorm:"type:text;not null"`
	CreatedAt         int64  `json:"created_at" gorm:"not null"`
	UpdatedAt         int64  `json:"updated_at" gorm:"not null"`
}

func (ResellerWithdrawal) TableName() string { return "reseller_withdrawals" }

type ResellerPricingQuoteInput struct {
	TenantID              int64
	CustomerID            int
	RequestID             string
	PriceVersionID        int64
	Model                 string
	Group                 string
	SupplierPriceMinor    int64
	SaleMinor             int64
	SaleMinorPerQuota     string
	SupplierMinorPerQuota string
	SupplySnapshot        string
	SalesMultiplier       decimal.Decimal
	QuotedQuota           int64
	Now                   int64
}

type ResellerPaymentLotInput struct {
	TenantID        int64
	CustomerID      int
	TopUpID         int64
	OperationID     string
	PaidMinor       int64
	PaymentFeeMinor int64
	CreditedQuota   int64
	Now             int64
}

type ResellerSettlementInput struct {
	TenantID      int64
	CustomerID    int
	RequestID     string
	QuoteID       int64
	ConsumedQuota int64
	Now           int64
}

type ResellerWithdrawalInput struct {
	TenantID          int64
	IdempotencyKey    string
	AmountMinor       int64
	FeeMinor          int64
	AlipayAccount     string
	AlipayName        string
	FeePolicySnapshot string
	Now               int64
}

type ResellerChargebackInput struct {
	TenantID          int64
	OriginalOperation string
	Reason            string
	ActorID           int
	Now               int64
}

func normalizeFinanceNow(now int64) int64 {
	if now > 0 {
		return now
	}
	return time.Now().Unix()
}

func decimalCeilInt64(value decimal.Decimal) (int64, error) {
	if value.IsNegative() || value.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return 0, ErrResellerFinanceInvalidInput
	}
	result := value.Ceil().IntPart()
	if result < 0 {
		return 0, ErrResellerFinanceInvalidInput
	}
	return result, nil
}

// MinimumResellerSaleMinor is ceil(cost / 0.984), equivalent to requiring
// sale - ceil(sale * 0.016) >= cost without decimal division precision loss.
func MinimumResellerSaleMinor(supplierMinor int64) (int64, error) {
	if supplierMinor < 0 {
		return 0, ErrResellerFinanceInvalidInput
	}
	numerator := new(big.Int).Mul(big.NewInt(supplierMinor), big.NewInt(125))
	numerator.Add(numerator, big.NewInt(122))
	numerator.Quo(numerator, big.NewInt(123))
	if !numerator.IsInt64() {
		return 0, ErrResellerFinanceInvalidInput
	}
	return numerator.Int64(), nil
}

func financePayloadHash(value any) (string, error) {
	encoded, err := common.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum[:]), nil
}

func resellerWalletForUpdate(tx *gorm.DB, tenantID int64, now int64) (*ResellerWallet, error) {
	if tx == nil || tenantID <= 0 {
		return nil, ErrResellerFinanceInvalidInput
	}
	var wallet ResellerWallet
	err := lockForUpdate(tx).Where("tenant_id = ?", tenantID).First(&wallet).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		wallet = ResellerWallet{TenantID: tenantID, ExactProfitCarry: "0", Version: 1, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&wallet).Error; err != nil {
			return nil, err
		}
		return &wallet, nil
	}
	if err != nil {
		return nil, err
	}
	return &wallet, nil
}

func applyResellerWalletDelta(wallet *ResellerWallet, pending, available, frozen, debt int64) error {
	if wallet == nil {
		return ErrResellerWalletNotFound
	}
	values := []*int64{&wallet.PendingMinor, &wallet.AvailableMinor, &wallet.FrozenMinor, &wallet.DebtMinor}
	deltas := []int64{pending, available, frozen, debt}
	for i, value := range values {
		delta := deltas[i]
		if delta > 0 {
			if *value > math.MaxInt64-delta {
				return ErrResellerWalletInvariant
			}
		} else if delta < 0 {
			if delta == math.MinInt64 || *value < -delta {
				return ErrResellerWalletInvariant
			}
		}
	}
	wallet.PendingMinor += pending
	wallet.AvailableMinor += available
	wallet.FrozenMinor += frozen
	wallet.DebtMinor += debt
	wallet.Version++
	return nil
}

// accrueResellerPendingProfit preserves sub-fen economics across requests and
// payment corrections. Signed carry can never be withdrawn as income.
func accrueResellerPendingProfit(wallet *ResellerWallet, exactProfit *big.Rat) (int64, error) {
	carry, err := parseResellerAggregate(wallet.ExactProfitCarry)
	if err != nil {
		return 0, err
	}
	carry.Add(carry, exactProfit)
	credit := int64(0)
	if carry.Cmp(big.NewRat(1, 1)) >= 0 {
		integer := new(big.Int).Quo(carry.Num(), carry.Denom())
		if !integer.IsInt64() {
			return 0, ErrResellerWalletInvariant
		}
		credit = integer.Int64()
		carry.Sub(carry, big.NewRat(credit, 1))
	}
	wallet.ExactProfitCarry, err = resellerAggregateString(carry)
	if err != nil {
		return 0, err
	}
	if err := applyResellerWalletDelta(wallet, credit, 0, 0, 0); err != nil {
		return 0, err
	}
	return credit, nil
}

func createResellerLedgerEntry(tx *gorm.DB, entry *ResellerLedgerEntry, payload any) (*ResellerLedgerEntry, error) {
	if tx == nil || entry == nil || strings.TrimSpace(entry.OperationID) == "" || strings.TrimSpace(entry.Kind) == "" {
		return nil, ErrResellerFinanceInvalidInput
	}
	hash, err := financePayloadHash(payload)
	if err != nil {
		return nil, err
	}
	entry.PayloadHash = hash
	var existing ResellerLedgerEntry
	if err := lockForUpdate(tx).Where("operation_id = ?", entry.OperationID).First(&existing).Error; err == nil {
		if existing.PayloadHash != entry.PayloadHash {
			return nil, ErrResellerLedgerConflict
		}
		return &existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := tx.Create(entry).Error; err != nil {
		return nil, err
	}
	return entry, nil
}

func CreateResellerPricingQuoteTx(tx *gorm.DB, input ResellerPricingQuoteInput) (*PricingQuote, error) {
	if tx == nil || input.TenantID <= 0 || input.CustomerID <= 0 || strings.TrimSpace(input.RequestID) == "" || len(input.RequestID) > 128 || input.PriceVersionID < 0 || strings.TrimSpace(input.Model) == "" || len(input.Model) > 128 || strings.TrimSpace(input.Group) == "" || len(input.Group) > 64 || input.SupplierPriceMinor < 0 || input.QuotedQuota <= 0 || len(input.SupplySnapshot) > 65536 || input.SupplierMinorPerQuota == "" {
		return nil, ErrResellerFinanceInvalidInput
	}
	unit, err := parseResellerExact(input.SupplierMinorPerQuota)
	if err != nil || unit.Sign() < 0 {
		return nil, ErrResellerFinanceInvalidInput
	}
	if !input.SalesMultiplier.IsPositive() || input.SalesMultiplier.Exponent() < -32 || input.SalesMultiplier.Exponent() > 6 || input.SalesMultiplier.GreaterThan(decimal.NewFromInt(1000000)) || input.SaleMinor < 0 {
		return nil, ErrResellerFinanceInvalidInput
	}
	sale := input.SaleMinor
	fee, err := decimalCeilInt64(decimal.NewFromInt(sale).Mul(decimal.RequireFromString(ResellerPaymentFeeRate)))
	if err != nil {
		return nil, err
	}
	retailUnit := big.NewRat(sale, input.QuotedQuota)
	if input.SaleMinorPerQuota != "" {
		retailUnit, err = parseResellerExact(input.SaleMinorPerQuota)
		if err != nil || retailUnit.Sign() < 0 {
			return nil, ErrResellerFinanceInvalidInput
		}
	}
	supplyEstimate, err := resellerExactCeilInt64(new(big.Rat).Mul(unit, big.NewRat(input.QuotedQuota, 1)))
	if err != nil || supplyEstimate != input.SupplierPriceMinor {
		return nil, ErrResellerFinanceInvalidInput
	}
	if input.SaleMinorPerQuota != "" {
		saleEstimate, err := resellerExactCeilInt64(new(big.Rat).Mul(retailUnit, big.NewRat(input.QuotedQuota, 1)))
		if err != nil || saleEstimate != sale {
			return nil, ErrResellerFinanceInvalidInput
		}
	}
	// Gross customer funds cover supply. Payment fees remain an actual expense
	// and may produce a negative profit when inheriting the platform price.
	netRate := new(big.Rat).Sub(big.NewRat(1, 1), decimal.RequireFromString(ResellerPaymentFeeRate).Rat())
	netUnit := new(big.Rat).Mul(retailUnit, netRate)
	if retailUnit.Cmp(unit) < 0 {
		return nil, ErrResellerPriceBelowCost
	}
	exactProfit := new(big.Rat).Mul(new(big.Rat).Sub(netUnit, unit), big.NewRat(input.QuotedQuota, 1))
	profitInteger := new(big.Int).Div(exactProfit.Num(), exactProfit.Denom())
	if !profitInteger.IsInt64() {
		return nil, ErrResellerFinanceInvalidInput
	}
	profit := profitInteger.Int64()
	profitText, err := resellerExactString(exactProfit, 128)
	if err != nil {
		return nil, err
	}
	unitText, err := resellerExactString(unit, 64)
	if err != nil {
		return nil, err
	}
	retailText, err := resellerExactString(retailUnit, 64)
	if err != nil {
		return nil, err
	}
	now := normalizeFinanceNow(input.Now)
	// Time is not part of an idempotent request's financial identity.
	payload := input
	payload.Now = 0
	payload.SupplierMinorPerQuota = unitText
	payload.SaleMinorPerQuota = retailText
	hash, err := financePayloadHash(payload)
	if err != nil {
		return nil, err
	}
	var existing PricingQuote
	if err := lockForUpdate(tx).Where("request_id = ?", input.RequestID).First(&existing).Error; err == nil {
		if existing.PayloadHash != hash {
			return nil, ErrResellerQuoteConflict
		}
		return &existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	quote := &PricingQuote{TenantID: input.TenantID, CustomerID: input.CustomerID, RequestID: input.RequestID, PriceVersionID: input.PriceVersionID, Model: input.Model, Group: input.Group, SupplierPriceMinor: input.SupplierPriceMinor, SupplierMinorPerQuota: unitText, SaleMinorPerQuota: retailText, SupplySnapshot: input.SupplySnapshot, SalesMultiplier: input.SalesMultiplier.String(), SaleMinor: sale, PaymentFeeMinor: fee, ProfitMinor: profit, ProfitMinorExact: profitText, QuotedQuota: input.QuotedQuota, Currency: ResellerCurrencyCNY, PayloadHash: hash, CreatedAt: now}
	if err := tx.Create(quote).Error; err != nil {
		return nil, err
	}
	return quote, nil
}

// Decimal inputs have bounded exponents to avoid allocating unbounded integers.
func parseResellerDecimal(value string) (decimal.Decimal, error) {
	if value == "" {
		value = "0"
	}
	if len(value) > 128 {
		return decimal.Zero, ErrResellerFinanceInvalidInput
	}
	d, err := decimal.NewFromString(value)
	if err != nil || d.Exponent() < -32 || d.Exponent() > 18 || d.Abs().GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return decimal.Zero, ErrResellerFinanceInvalidInput
	}
	return d, nil
}

func CreateCustomerPaymentLotTx(tx *gorm.DB, input ResellerPaymentLotInput) (*CustomerPaymentLot, error) {
	if tx == nil || input.TenantID <= 0 || input.CustomerID <= 0 || input.TopUpID <= 0 || strings.TrimSpace(input.OperationID) == "" || len(input.OperationID) > 128 || input.PaidMinor <= 0 || input.PaymentFeeMinor < 0 || input.PaymentFeeMinor > input.PaidMinor || input.CreditedQuota <= 0 {
		return nil, ErrResellerFinanceInvalidInput
	}
	now := normalizeFinanceNow(input.Now)
	payload := struct {
		TenantID, CustomerID, TopUpID int64
		OperationID                   string
		PaidMinor, PaymentFeeMinor    int64
		CreditedQuota                 int64
	}{input.TenantID, int64(input.CustomerID), input.TopUpID, input.OperationID, input.PaidMinor, input.PaymentFeeMinor, input.CreditedQuota}
	hash, err := financePayloadHash(payload)
	if err != nil {
		return nil, err
	}
	var existing CustomerPaymentLot
	if err := lockForUpdate(tx).Where("operation_id = ?", input.OperationID).First(&existing).Error; err == nil {
		if existing.PayloadHash != hash {
			return nil, ErrResellerLedgerConflict
		}
		return &existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	lot := &CustomerPaymentLot{TenantID: input.TenantID, CustomerID: input.CustomerID, TopUpID: input.TopUpID, OperationID: input.OperationID, PaidMinor: input.PaidMinor, PaymentFeeMinor: input.PaymentFeeMinor, CreditedQuota: input.CreditedQuota, RemainingQuota: input.CreditedQuota, RefundStatus: "none", PayloadHash: hash, CreatedAt: now, UpdatedAt: now}
	if err := tx.Create(lot).Error; err != nil {
		return nil, err
	}
	return lot, nil
}
