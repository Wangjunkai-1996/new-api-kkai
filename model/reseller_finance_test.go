package model

import (
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newResellerFinanceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:reseller-finance-%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&PricingQuote{}, &CustomerPaymentLot{}, &ResellerWallet{}, &ResellerLedgerEntry{}, &ResellerWithdrawal{}))
	return db
}

func TestResellerQuoteFreezesIndependentRetailAndSupplyPrices(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	input := ResellerPricingQuoteInput{
		TenantID: 42, CustomerID: 7, RequestID: "absolute-price", PriceVersionID: 3,
		Model: "kimi", Group: "default", SupplierPriceMinor: 400, SaleMinor: 500,
		SupplierMinorPerQuota: "4", SalesMultiplier: decimal.RequireFromString("0.5"), QuotedQuota: 100, Now: 1000,
	}
	quote, err := CreateResellerPricingQuoteTx(db, input)
	require.NoError(t, err)
	assert.Equal(t, int64(500), quote.SaleMinor)
	assert.Equal(t, int64(8), quote.PaymentFeeMinor)
	assert.Equal(t, int64(92), quote.ProfitMinor)
	input.Now++
	repeated, err := CreateResellerPricingQuoteTx(db, input)
	require.NoError(t, err)
	assert.Equal(t, quote.ID, repeated.ID)
	assert.Equal(t, quote.CreatedAt, repeated.CreatedAt)
	input.SaleMinor++
	_, err = CreateResellerPricingQuoteTx(db, input)
	assert.ErrorIs(t, err, ErrResellerQuoteConflict)
	input.RequestID = "below-cost"
	input.SaleMinor = 399
	_, err = CreateResellerPricingQuoteTx(db, input)
	assert.ErrorIs(t, err, ErrResellerPriceBelowCost)
}

func TestResellerSettlementIsIdempotentAndPendingReleaseMovesFunds(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	quote, err := CreateResellerPricingQuoteTx(db, ResellerPricingQuoteInput{
		TenantID: 42, CustomerID: 7, RequestID: "req-1", PriceVersionID: 3,
		Model: "kimi", Group: "default", SupplierPriceMinor: 10000, SaleMinor: 11000, SupplierMinorPerQuota: "100",
		SalesMultiplier: decimal.RequireFromString("1.1"), QuotedQuota: 100, Now: 1_000,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(11000), quote.SaleMinor)
	assert.Equal(t, int64(176), quote.PaymentFeeMinor)
	assert.Equal(t, int64(824), quote.ProfitMinor)
	lot, err := CreateCustomerPaymentLotTx(db, ResellerPaymentLotInput{
		TenantID: 42, CustomerID: 7, TopUpID: 11, OperationID: "topup-1", PaidMinor: 11000, PaymentFeeMinor: 176,
		CreditedQuota: 100, Now: 1_000,
	})
	require.NoError(t, err)

	entry, err := SettleResellerConsumptionTx(db, ResellerSettlementInput{TenantID: 42, CustomerID: 7, RequestID: "req-1", QuoteID: quote.ID, ConsumedQuota: 100, Now: 1_001})
	require.NoError(t, err)
	assert.Equal(t, int64(824), entry.AmountMinor)
	_, err = CreateResellerPricingQuoteTx(db, ResellerPricingQuoteInput{TenantID: 42, CustomerID: 7, RequestID: "req-1", PriceVersionID: 3, Model: "kimi", Group: "default", SupplierPriceMinor: 10000, SaleMinor: 11000, SupplierMinorPerQuota: "100", SalesMultiplier: decimal.RequireFromString("1.2"), QuotedQuota: 100, Now: 1_001})
	require.ErrorIs(t, err, ErrResellerQuoteConflict)
	var wallet ResellerWallet
	require.NoError(t, db.First(&wallet, "tenant_id = ?", 42).Error)
	assert.Equal(t, int64(824), wallet.PendingMinor)
	assert.Equal(t, int64(0), wallet.AvailableMinor)

	// A retry returns the existing operation and does not consume the lot or add money.
	retry, err := SettleResellerConsumptionTx(db, ResellerSettlementInput{TenantID: 42, CustomerID: 7, RequestID: "req-1", QuoteID: quote.ID, ConsumedQuota: 100, Now: 1_002})
	require.NoError(t, err)
	assert.Equal(t, entry.ID, retry.ID)
	var lotAfter CustomerPaymentLot
	require.NoError(t, db.First(&lotAfter, lot.ID).Error)
	assert.Equal(t, int64(100), lotAfter.ConsumedQuota)
	assert.Equal(t, int64(0), lotAfter.RemainingQuota)

	released, err := ReleaseResellerPendingTx(db, 42, 1_001+ResellerPendingPeriodSec, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, released)
	require.NoError(t, db.First(&wallet, "tenant_id = ?", 42).Error)
	assert.Equal(t, int64(0), wallet.PendingMinor)
	assert.Equal(t, int64(824), wallet.AvailableMinor)
	released, err = ReleaseResellerPendingTx(db, 42, 1_001+ResellerPendingPeriodSec+1, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, released)
}

func TestResellerWithdrawalFreezesAvailableFundsAndIsIdempotent(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	quote, err := CreateResellerPricingQuoteTx(db, ResellerPricingQuoteInput{
		TenantID: 9, CustomerID: 3, RequestID: "req-withdraw", PriceVersionID: 1,
		Model: "kimi", Group: "default", SupplierPriceMinor: 100000, SaleMinor: 120000, SupplierMinorPerQuota: "10000",
		SalesMultiplier: decimal.RequireFromString("1.2"), QuotedQuota: 10, Now: 100,
	})
	require.NoError(t, err)
	_, err = SettleResellerConsumptionTx(db, ResellerSettlementInput{TenantID: 9, CustomerID: 3, RequestID: quote.RequestID, QuoteID: quote.ID, ConsumedQuota: quote.QuotedQuota, Now: 101})
	require.Error(t, err)
	_, err = CreateCustomerPaymentLotTx(db, ResellerPaymentLotInput{TenantID: 9, CustomerID: 3, TopUpID: 12, OperationID: "topup-withdraw", PaidMinor: 120000, PaymentFeeMinor: 1920, CreditedQuota: 10, Now: 100})
	require.NoError(t, err)
	_, err = SettleResellerConsumptionTx(db, ResellerSettlementInput{TenantID: 9, CustomerID: 3, RequestID: quote.RequestID, QuoteID: quote.ID, ConsumedQuota: quote.QuotedQuota, Now: 101})
	require.NoError(t, err)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := ReleaseResellerPendingTx(tx, 9, 101+ResellerPendingPeriodSec, 0)
		return err
	}))

	withdrawal, err := RequestResellerWithdrawalTx(db, ResellerWithdrawalInput{TenantID: 9, IdempotencyKey: "wd-1", AmountMinor: 10000, FeeMinor: 100, AlipayAccount: "agent@example.com", AlipayName: "Agent", FeePolicySnapshot: "flat:100", Now: 200})
	require.NoError(t, err)
	retry, err := RequestResellerWithdrawalTx(db, ResellerWithdrawalInput{TenantID: 9, IdempotencyKey: "wd-1", AmountMinor: 10000, FeeMinor: 100, AlipayAccount: "agent@example.com", AlipayName: "Agent", FeePolicySnapshot: "flat:100", Now: 201})
	require.NoError(t, err)
	assert.Equal(t, withdrawal.ID, retry.ID)

	var wallet ResellerWallet
	require.NoError(t, db.First(&wallet, "tenant_id = ?", 9).Error)
	assert.Equal(t, int64(8080), wallet.AvailableMinor)
	assert.Equal(t, int64(10000), wallet.FrozenMinor)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return reviewWithdrawalTestTx(tx, withdrawal.ID, ResellerWithdrawalApproved, "", "", 202)
	}))
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return reviewWithdrawalTestTx(tx, withdrawal.ID, ResellerWithdrawalPaid, "alipay-1", "", 203)
	}))
	require.NoError(t, db.First(&wallet, "tenant_id = ?", 9).Error)
	assert.Equal(t, int64(0), wallet.FrozenMinor)
	var paid ResellerWithdrawal
	require.NoError(t, db.First(&paid, withdrawal.ID).Error)
	assert.Equal(t, ResellerWithdrawalPaid, paid.Status)
}

func TestResellerSettlementScalesPartialQuotaAndRejectsCrossTenantLot(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	quote, err := CreateResellerPricingQuoteTx(db, ResellerPricingQuoteInput{
		TenantID: 20, CustomerID: 5, RequestID: "req-partial", PriceVersionID: 1,
		Model: "kimi", Group: "default", SupplierPriceMinor: 100000, SaleMinor: 120000, SupplierMinorPerQuota: "1000",
		SalesMultiplier: decimal.RequireFromString("1.2"), QuotedQuota: 100, Now: 1,
	})
	require.NoError(t, err)
	_, err = CreateCustomerPaymentLotTx(db, ResellerPaymentLotInput{TenantID: 21, CustomerID: 5, TopUpID: 22, OperationID: "lot-other-tenant", PaidMinor: 120000, PaymentFeeMinor: 1920, CreditedQuota: 100, Now: 1})
	require.NoError(t, err)
	_, err = SettleResellerConsumptionTx(db, ResellerSettlementInput{TenantID: 20, CustomerID: 5, RequestID: quote.RequestID, QuoteID: quote.ID, ConsumedQuota: 50, Now: 2})
	require.Error(t, err)

	_, err = CreateCustomerPaymentLotTx(db, ResellerPaymentLotInput{TenantID: 20, CustomerID: 5, TopUpID: 23, OperationID: "lot-same-tenant", PaidMinor: 120000, PaymentFeeMinor: 1920, CreditedQuota: 100, Now: 1})
	require.NoError(t, err)
	entry, err := SettleResellerConsumptionTx(db, ResellerSettlementInput{TenantID: 20, CustomerID: 5, RequestID: quote.RequestID, QuoteID: quote.ID, ConsumedQuota: 50, Now: 2})
	require.NoError(t, err)
	// sale=60,000; fee=960; supplier=50,000, profit=9,040.
	assert.Equal(t, int64(9040), entry.AmountMinor)
}

func TestResellerPendingReleaseRejectsWalletUnderflow(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	quote, err := CreateResellerPricingQuoteTx(db, ResellerPricingQuoteInput{TenantID: 30, CustomerID: 6, RequestID: "req-underflow", PriceVersionID: 1, Model: "kimi", Group: "default", SupplierPriceMinor: 1000, SaleMinor: 11000, SupplierMinorPerQuota: "100", SalesMultiplier: decimal.RequireFromString("1.1"), QuotedQuota: 10, Now: 1})
	require.NoError(t, err)
	_, err = CreateCustomerPaymentLotTx(db, ResellerPaymentLotInput{TenantID: 30, CustomerID: 6, TopUpID: 31, OperationID: "lot-underflow", PaidMinor: 11000, PaymentFeeMinor: 176, CreditedQuota: 10, Now: 1})
	require.NoError(t, err)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := SettleResellerConsumptionTx(tx, ResellerSettlementInput{TenantID: 30, CustomerID: 6, RequestID: quote.RequestID, QuoteID: quote.ID, ConsumedQuota: 10, Now: 2})
		return err
	}))
	require.NoError(t, db.Model(&ResellerWallet{}).Where("tenant_id = ?", 30).Update("pending_minor", 0).Error)
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := ReleaseResellerPendingTx(tx, 30, 2+ResellerPendingPeriodSec, 0)
		return err
	}), ErrResellerWalletInvariant)
}

func TestResellerChargebackCreatesDebtAfterFundsWereWithdrawn(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	quote, err := CreateResellerPricingQuoteTx(db, ResellerPricingQuoteInput{TenantID: 40, CustomerID: 8, RequestID: "req-chargeback", PriceVersionID: 1, Model: "kimi", Group: "default", SupplierPriceMinor: 100000, SaleMinor: 120000, SupplierMinorPerQuota: "10000", SalesMultiplier: decimal.RequireFromString("1.2"), QuotedQuota: 10, Now: 1})
	require.NoError(t, err)
	_, err = CreateCustomerPaymentLotTx(db, ResellerPaymentLotInput{TenantID: 40, CustomerID: 8, TopUpID: 41, OperationID: "lot-chargeback", PaidMinor: 120000, PaymentFeeMinor: 1920, CreditedQuota: 10, Now: 1})
	require.NoError(t, err)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := SettleResellerConsumptionTx(tx, ResellerSettlementInput{TenantID: 40, CustomerID: 8, RequestID: quote.RequestID, QuoteID: quote.ID, ConsumedQuota: 10, Now: 2})
		return err
	}))
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := ReleaseResellerPendingTx(tx, 40, 2+ResellerPendingPeriodSec, 0)
		return err
	}))
	withdrawal, err := RequestResellerWithdrawalTx(db, ResellerWithdrawalInput{TenantID: 40, IdempotencyKey: "wd-chargeback", AmountMinor: 10000, FeeMinor: 100, AlipayAccount: "agent@example.com", AlipayName: "Agent", FeePolicySnapshot: "flat:100", Now: 3})
	require.NoError(t, err)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return reviewWithdrawalTestTx(tx, withdrawal.ID, ResellerWithdrawalApproved, "", "", 4)
	}))
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return reviewWithdrawalTestTx(tx, withdrawal.ID, ResellerWithdrawalPaid, "alipay-chargeback", "", 5)
	}))

	entry, err := ReverseResellerSettlementTx(db, ResellerChargebackInput{TenantID: 40, OriginalOperation: "settlement:req-chargeback", Reason: "客户拒付", ActorID: 99, Now: 6})
	require.NoError(t, err)
	assert.Equal(t, int64(10000), entry.DebtDelta)
	var wallet ResellerWallet
	require.NoError(t, db.First(&wallet, "tenant_id = ?", 40).Error)
	assert.Equal(t, int64(10000), wallet.DebtMinor)
	assert.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := RequestResellerWithdrawalTx(tx, ResellerWithdrawalInput{TenantID: 40, IdempotencyKey: "wd-blocked", AmountMinor: 10000, FeeMinor: 100, AlipayAccount: "agent@example.com", AlipayName: "Agent", FeePolicySnapshot: "flat:100", Now: 7})
		return err
	}), ErrResellerDebtOutstanding)

	retry, err := ReverseResellerSettlementTx(db, ResellerChargebackInput{TenantID: 40, OriginalOperation: "settlement:req-chargeback", Reason: "客户拒付", ActorID: 99, Now: 8})
	require.NoError(t, err)
	assert.Equal(t, entry.ID, retry.ID)
	_, err = ReverseResellerSettlementTx(db, ResellerChargebackInput{TenantID: 40, OriginalOperation: "settlement:req-chargeback", Reason: "different correction", ActorID: 99, Now: 9})
	assert.ErrorIs(t, err, ErrResellerLedgerConflict)
	require.NoError(t, db.First(&wallet, wallet.ID).Error)
	assert.Equal(t, int64(10000), wallet.DebtMinor)
}

func TestResellerWithdrawalRejectRestoresAvailableFunds(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	wallet := &ResellerWallet{TenantID: 12, AvailableMinor: 12000, Version: 1, CreatedAt: 1, UpdatedAt: 1}
	require.NoError(t, db.Create(wallet).Error)
	withdrawal, err := RequestResellerWithdrawalTx(db, ResellerWithdrawalInput{TenantID: 12, IdempotencyKey: "wd-reject", AmountMinor: 10000, FeeMinor: 100, AlipayAccount: "agent@example.com", AlipayName: "Agent", FeePolicySnapshot: "flat:100", Now: 2})
	require.NoError(t, err)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return reviewWithdrawalTestTx(tx, withdrawal.ID, ResellerWithdrawalRejected, "", "人工审核未通过", 3)
	}))
	var updated ResellerWallet
	require.NoError(t, db.First(&updated, wallet.ID).Error)
	assert.Equal(t, int64(12000), updated.AvailableMinor)
	assert.Equal(t, int64(0), updated.FrozenMinor)
	var persisted ResellerWithdrawal
	require.NoError(t, db.First(&persisted, withdrawal.ID).Error)
	assert.Equal(t, ResellerWithdrawalRejected, persisted.Status)
}

func reviewWithdrawalTestTx(tx *gorm.DB, id int64, status, ref, note string, now int64) error {
	_, err := ReviewResellerWithdrawalTx(tx, ResellerWithdrawalReviewInput{WithdrawalID: id, ReviewerID: 99, Status: status, PaymentRef: ref, Note: note, Now: now})
	return err
}
