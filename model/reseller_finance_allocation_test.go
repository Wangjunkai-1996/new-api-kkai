package model

import (
	"errors"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func settleFinanceFixture(t *testing.T, db *gorm.DB, request string, quota int64, unit string) *ResellerLedgerEntry {
	t.Helper()
	quote, err := CreateResellerPricingQuoteTx(db, ResellerPricingQuoteInput{
		TenantID: 2, CustomerID: 3, PriceVersionID: 1, RequestID: request, Model: "test", Group: "default",
		SupplierMinorPerQuota: unit, SupplierPriceMinor: decimal.RequireFromString(unit).Ceil().IntPart(), SaleMinor: 100, SalesMultiplier: decimal.NewFromInt(2), QuotedQuota: 1, Now: 10,
	})
	require.NoError(t, err)
	var entry *ResellerLedgerEntry
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		entry, err = SettleResellerConsumptionTx(tx, ResellerSettlementInput{TenantID: 2, CustomerID: 3, RequestID: request, QuoteID: quote.ID, ConsumedQuota: quota, Now: 20})
		return err
	}))
	return entry
}

func TestResellerFIFOAllocationConservesCashFeeAndQuota(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	_, err := CreateCustomerPaymentLotTx(db, ResellerPaymentLotInput{TenantID: 2, CustomerID: 3, TopUpID: 1, OperationID: "topup:1", PaidMinor: 101, PaymentFeeMinor: 2, CreditedQuota: 3})
	require.NoError(t, err)
	_, err = CreateCustomerPaymentLotTx(db, ResellerPaymentLotInput{TenantID: 2, CustomerID: 3, TopUpID: 2, OperationID: "topup:2", PaidMinor: 100, PaymentFeeMinor: 2, CreditedQuota: 2})
	require.NoError(t, err)
	// Each request exceeds its reference quote quota; the frozen cost unit is authoritative.
	a := settleFinanceFixture(t, db, "a", 2, "1.2")
	b := settleFinanceFixture(t, db, "b", 3, "1.2")
	assert.Equal(t, int64(201), a.SaleMinor+b.SaleMinor)
	assert.Equal(t, int64(4), a.PaymentFeeMinor+b.PaymentFeeMinor)
	assert.Equal(t, int64(191), a.AmountMinor+b.AmountMinor)
	var allocations []ResellerPaymentAllocation
	require.NoError(t, common.UnmarshalJsonStr(b.PaymentAllocations, &allocations))
	require.Len(t, allocations, 2)
	assert.Equal(t, int64(1), allocations[0].Quota)
	assert.Equal(t, int64(2), allocations[1].Quota)
	var lots []CustomerPaymentLot
	require.NoError(t, db.Order("id").Find(&lots).Error)
	assert.Zero(t, lots[0].RemainingQuota)
	assert.Zero(t, lots[1].RemainingQuota)
	assert.Equal(t, int64(3), lots[0].ConsumedQuota)
	assert.Equal(t, int64(2), lots[1].ConsumedQuota)
}

func TestResellerSubFenConsumptionCarriesLossWithoutCreatingMoney(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	_, err := CreateCustomerPaymentLotTx(db, ResellerPaymentLotInput{TenantID: 2, CustomerID: 3, TopUpID: 1, OperationID: "topup:1", PaidMinor: 2, PaymentFeeMinor: 1, CreditedQuota: 3})
	require.NoError(t, err)
	a := settleFinanceFixture(t, db, "a", 1, "0.2")
	var wallet ResellerWallet
	require.NoError(t, db.First(&wallet, "tenant_id = ?", 2).Error)
	assert.Equal(t, "-0.2", wallet.ExactProfitCarry)
	assert.Zero(t, a.AmountMinor)
	b := settleFinanceFixture(t, db, "b", 1, "0.2")
	c := settleFinanceFixture(t, db, "c", 1, "0.2")
	require.NoError(t, db.First(&wallet, "tenant_id = ?", 2).Error)
	assert.Equal(t, int64(2), a.SaleMinor+b.SaleMinor+c.SaleMinor)
	assert.Equal(t, int64(1), a.PaymentFeeMinor+b.PaymentFeeMinor+c.PaymentFeeMinor)
	assert.Zero(t, wallet.PendingMinor)
	assert.Equal(t, "0.4", wallet.ExactProfitCarry)
}

func TestResellerReversedPendingSettlementIsNeverReleased(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	_, err := CreateCustomerPaymentLotTx(db, ResellerPaymentLotInput{TenantID: 2, CustomerID: 3, TopUpID: 1, OperationID: "topup:1", PaidMinor: 100, PaymentFeeMinor: 2, CreditedQuota: 1})
	require.NoError(t, err)
	entry := settleFinanceFixture(t, db, "reversed", 1, "10")
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := ReverseResellerSettlementTx(tx, ResellerChargebackInput{TenantID: 2, OriginalOperation: entry.OperationID, Reason: "chargeback", ActorID: 99, Now: 30})
		return err
	}))
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		count, err := ReleaseResellerPendingTx(tx, 2, entry.AvailableAt, 1)
		assert.Zero(t, count)
		return err
	}))
	var wallet ResellerWallet
	require.NoError(t, db.First(&wallet, "tenant_id = ?", 2).Error)
	assert.Zero(t, wallet.PendingMinor)
	assert.Zero(t, wallet.AvailableMinor)
}

func TestResellerLotCumulativeSupportsInt64WithoutOverflow(t *testing.T) {
	assert.Equal(t, int64(math.MaxInt64), resellerLotCumulative(math.MaxInt64, math.MaxInt64, math.MaxInt64))
	assert.Equal(t, int64(33), resellerLotCumulative(100, 1, 3))
	assert.Equal(t, int64(66), resellerLotCumulative(100, 2, 3))
}

func TestMinimumResellerSaleMinorIncludesRoundedFee(t *testing.T) {
	for _, test := range []struct{ cost, sale int64 }{{0, 0}, {1, 2}, {10000, 10163}} {
		sale, err := MinimumResellerSaleMinor(test.cost)
		require.NoError(t, err)
		assert.Equal(t, test.sale, sale)
		fee, err := decimalCeilInt64(decimal.NewFromInt(sale).Mul(decimal.RequireFromString(ResellerPaymentFeeRate)))
		require.NoError(t, err)
		assert.GreaterOrEqual(t, sale-fee, test.cost)
	}
	_, err := MinimumResellerSaleMinor(math.MaxInt64)
	assert.Error(t, err)
}

func TestResellerWithdrawalAuditAndPaymentReplayConflicts(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	require.NoError(t, db.Create(&ResellerWallet{TenantID: 2, AvailableMinor: 20000, Version: 1}).Error)
	input := ResellerWithdrawalInput{TenantID: 2, IdempotencyKey: "wd-review", AmountMinor: 10000, FeeMinor: 100, FeePolicySnapshot: "fixed:100", AlipayAccount: "owner@example.com", AlipayName: "Owner", Now: 1}
	withdrawal, err := RequestResellerWithdrawalTx(db, input)
	require.NoError(t, err)
	changed := input
	changed.AlipayAccount = "other@example.com"
	_, err = RequestResellerWithdrawalTx(db, changed)
	assert.ErrorIs(t, err, ErrResellerLedgerConflict)
	_, err = ReviewResellerWithdrawalTx(db, ResellerWithdrawalReviewInput{WithdrawalID: withdrawal.ID, Status: ResellerWithdrawalApproved})
	assert.ErrorIs(t, err, ErrResellerFinanceInvalidInput)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return reviewWithdrawalTestTx(tx, withdrawal.ID, ResellerWithdrawalApproved, "", "", 2)
	}))
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return reviewWithdrawalTestTx(tx, withdrawal.ID, ResellerWithdrawalPaid, "alipay-proof", "", 3)
	}))
	require.NoError(t, db.First(withdrawal, withdrawal.ID).Error)
	assert.Equal(t, 99, withdrawal.ReviewedBy)
	assert.Equal(t, int64(3), withdrawal.PaidAt)
	var events []ResellerWithdrawalEvent
	require.NoError(t, common.UnmarshalJsonStr(withdrawal.Timeline, &events))
	require.Len(t, events, 3)
	assert.Equal(t, "fixed:100", withdrawal.FeePolicySnapshot)
	assert.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
		return reviewWithdrawalTestTx(tx, withdrawal.ID, ResellerWithdrawalPaid, "different-proof", "", 4)
	}), ErrResellerLedgerConflict)
}

func TestResellerQuoteAcceptsProfitableSubFenTariff(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	input := ResellerPricingQuoteInput{
		TenantID: 2, CustomerID: 3, PriceVersionID: 1, RequestID: "sub-fen", Model: "text", Group: "default",
		SupplierMinorPerQuota: "0.0001", SaleMinorPerQuota: "0.0002", SupplierPriceMinor: 1, SaleMinor: 1,
		SalesMultiplier: decimal.NewFromInt(2), QuotedQuota: 10,
	}
	quote, err := CreateResellerPricingQuoteTx(db, input)
	require.NoError(t, err)
	assert.Equal(t, "0.0002", quote.SaleMinorPerQuota)
	assert.Equal(t, "0.000968", quote.ProfitMinorExact)
	assert.Zero(t, quote.ProfitMinor)
	input.RequestID = "below-cost-sub-fen"
	input.SaleMinorPerQuota = "0.0001"
	_, err = CreateResellerPricingQuoteTx(db, input)
	assert.ErrorIs(t, err, ErrResellerPriceBelowCost)
}

func TestResellerWithdrawalIdempotencyIsScopedToTenant(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	for _, tenantID := range []int64{2, 3} {
		require.NoError(t, db.Create(&ResellerWallet{TenantID: tenantID, AvailableMinor: 10000, Version: 1}).Error)
		input := ResellerWithdrawalInput{TenantID: tenantID, IdempotencyKey: "same-browser-key", AmountMinor: 10000, AlipayAccount: "owner@example.com", AlipayName: "Owner", FeePolicySnapshot: "fixed:0", Now: 1}
		var first, replay *ResellerWithdrawal
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			var err error
			first, err = RequestResellerWithdrawalTx(tx, input)
			return err
		}))
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			var err error
			replay, err = RequestResellerWithdrawalTx(tx, input)
			return err
		}))
		assert.Equal(t, first.ID, replay.ID)
		assert.Equal(t, tenantID, replay.TenantID)
	}
	var count int64
	require.NoError(t, db.Model(&ResellerWithdrawal{}).Count(&count).Error)
	assert.Equal(t, int64(2), count)
	require.NoError(t, db.Model(&ResellerLedgerEntry{}).Where("kind = ?", ResellerLedgerKindWithdrawal).Count(&count).Error)
	assert.Equal(t, int64(2), count)
}

func TestResellerUnknownPayoutRetainsFrozenFundsUntilConfirmedFailure(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	require.NoError(t, db.Create(&ResellerWallet{TenantID: 2, AvailableMinor: 10000, Version: 1}).Error)
	var withdrawal *ResellerWithdrawal
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		withdrawal, err = RequestResellerWithdrawalTx(tx, ResellerWithdrawalInput{TenantID: 2, IdempotencyKey: "unknown-payout", AmountMinor: 10000, AlipayAccount: "owner@example.com", AlipayName: "Owner", FeePolicySnapshot: "fixed:0", Now: 1})
		return err
	}))
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return reviewWithdrawalTestTx(tx, withdrawal.ID, ResellerWithdrawalApproved, "", "", 2)
	}))
	for _, status := range []string{"unknown", ResellerWithdrawalPaid, ResellerWithdrawalFailed} {
		assert.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
			return reviewWithdrawalTestTx(tx, withdrawal.ID, status, "", "", 3)
		}), ErrResellerFinanceInvalidInput)
	}
	var wallet ResellerWallet
	require.NoError(t, db.Where("tenant_id = ?", 2).First(&wallet).Error)
	assert.Zero(t, wallet.AvailableMinor)
	assert.Equal(t, int64(10000), wallet.FrozenMinor)
	require.NoError(t, db.First(withdrawal, withdrawal.ID).Error)
	assert.Equal(t, ResellerWithdrawalApproved, withdrawal.Status)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return reviewWithdrawalTestTx(tx, withdrawal.ID, ResellerWithdrawalFailed, "", "manual reconciliation confirmed no payment", 4)
	}))
	require.NoError(t, db.First(&wallet, wallet.ID).Error)
	assert.Equal(t, int64(10000), wallet.AvailableMinor)
	assert.Zero(t, wallet.FrozenMinor)
}

func TestResellerConcurrentWithdrawalsCannotOverspendWallet(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	// SQLite serializes writers; keep its in-memory connection alive and let
	// two callers contend for the same transaction boundary without sleeps.
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.Create(&ResellerWallet{TenantID: 2, AvailableMinor: 10000, Version: 1}).Error)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, key := range []string{"request-a", "request-b"} {
		go func(key string) {
			<-start
			results <- db.Transaction(func(tx *gorm.DB) error {
				_, err := RequestResellerWithdrawalTx(tx, ResellerWithdrawalInput{TenantID: 2, IdempotencyKey: key, AmountMinor: 10000, AlipayAccount: "owner@example.com", AlipayName: "Owner", FeePolicySnapshot: "fixed:0"})
				return err
			})
		}(key)
	}
	close(start)
	a, b := <-results, <-results
	assert.True(t, (a == nil && errors.Is(b, ErrResellerInsufficientFunds)) || (b == nil && errors.Is(a, ErrResellerInsufficientFunds)))
	var wallet ResellerWallet
	require.NoError(t, db.Where("tenant_id = ?", 2).First(&wallet).Error)
	assert.Zero(t, wallet.AvailableMinor)
	assert.Equal(t, int64(10000), wallet.FrozenMinor)
	var count int64
	require.NoError(t, db.Model(&ResellerWithdrawal{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}
