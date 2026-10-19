package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func paymentReversalFixture(t *testing.T) (*gorm.DB, User, []CustomerPaymentLot, *ResellerLedgerEntry) {
	t.Helper()
	db := newResellerFinanceTestDB(t)
	require.NoError(t, db.AutoMigrate(&User{}, &TopUp{}, &TenantConsumption{}, &CustomerPaymentReversal{}))
	user := User{Id: 3, TenantID: 2, Username: "refund-customer", AffCode: "refund-customer", Quota: 5}
	require.NoError(t, db.Create(&user).Error)
	lots := make([]CustomerPaymentLot, 0, 2)
	for _, topup := range []TopUp{
		{Id: 1, TenantID: 2, UserId: 3, TradeNo: "payment-1", PaidMinor: 100000, Currency: ResellerCurrencyCNY, PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusSuccess},
		{Id: 2, TenantID: 2, UserId: 3, TradeNo: "payment-2", PaidMinor: 200000, Currency: ResellerCurrencyCNY, PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusSuccess},
	} {
		require.NoError(t, db.Create(&topup).Error)
		lot, err := CreateCustomerPaymentLotTx(db, ResellerPaymentLotInput{TenantID: 2, CustomerID: 3, TopUpID: int64(topup.Id), OperationID: topup.TradeNo, PaidMinor: topup.PaidMinor, PaymentFeeMinor: topup.PaidMinor * 16 / 1000, CreditedQuota: 10})
		require.NoError(t, err)
		lots = append(lots, *lot)
	}
	quote, err := CreateResellerPricingQuoteTx(db, ResellerPricingQuoteInput{TenantID: 2, CustomerID: 3, PriceVersionID: 1, RequestID: "mixed-lots", Model: "text", Group: "default", SupplierMinorPerQuota: "1000", SupplierPriceMinor: 15000, SaleMinor: 200000, SalesMultiplier: decimal.NewFromInt(2), QuotedQuota: 15})
	require.NoError(t, err)
	var settlement *ResellerLedgerEntry
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		settlement, err = SettleResellerConsumptionTx(tx, ResellerSettlementInput{TenantID: 2, CustomerID: 3, RequestID: quote.RequestID, QuoteID: quote.ID, ConsumedQuota: 15, Now: 20})
		return err
	}))
	assert.Equal(t, int64(181800), settlement.AmountMinor)
	return db, user, lots, settlement
}

func TestResellerPaymentReversalRetainsOtherLotProfitAndRemovesUnusedQuota(t *testing.T) {
	db, user, lots, original := paymentReversalFixture(t)
	input := CustomerPaymentReversalInput{TenantID: 2, TopUpID: 1, IdempotencyKey: "refund-1", Kind: "refunded", ActorID: 99, Reason: "confirmed customer refund", PaymentReference: "alipay-refund-1", Now: 30}
	var first *CustomerPaymentReversal
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		first, err = ReverseCustomerPaymentTx(tx, input)
		return err
	}))
	assert.EqualValues(t, "88400", first.ReversedProfitMinorExact)
	assert.Zero(t, first.RemovedQuota)
	var wallet ResellerWallet
	require.NoError(t, db.Where("tenant_id = ?", 2).First(&wallet).Error)
	assert.Equal(t, int64(93400), wallet.PendingMinor)
	assert.Zero(t, wallet.AvailableMinor)
	var retained ResellerLedgerEntry
	require.NoError(t, db.Where("kind = ? AND reversal_of = ?", ResellerLedgerKindSettlement, original.OperationID).First(&retained).Error)
	assert.Equal(t, original.AvailableAt, retained.AvailableAt)
	var allocations []ResellerPaymentAllocation
	require.NoError(t, common.UnmarshalJsonStr(retained.PaymentAllocations, &allocations))
	require.Len(t, allocations, 1)
	assert.Equal(t, lots[1].ID, allocations[0].LotID)
	assert.Equal(t, int64(5), allocations[0].Quota)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		replay, err := ReverseCustomerPaymentTx(tx, input)
		if err == nil {
			assert.Equal(t, first.ID, replay.ID)
		}
		return err
	}))
	input.PaymentReference = "different-proof"
	assert.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := ReverseCustomerPaymentTx(tx, input)
		return err
	}), ErrResellerLedgerConflict)
	input.TopUpID, input.IdempotencyKey, input.PaymentReference = 2, "refund-2", "alipay-refund-2"
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		reversal, err := ReverseCustomerPaymentTx(tx, input)
		if err == nil {
			assert.EqualValues(t, "93400", reversal.ReversedProfitMinorExact)
			assert.Equal(t, int64(5), reversal.RemovedQuota)
		}
		return err
	}))
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Zero(t, user.Quota)
	require.NoError(t, db.First(&lots[1], lots[1].ID).Error)
	assert.Equal(t, int64(5), lots[1].ConsumedQuota)
	assert.Equal(t, int64(5), lots[1].RefundedQuota)
	assert.Zero(t, lots[1].RemainingQuota)
	assert.Equal(t, "refunded", lots[1].RefundStatus)
	require.NoError(t, db.First(&wallet, wallet.ID).Error)
	assert.Zero(t, wallet.PendingMinor)
	assert.Zero(t, wallet.DebtMinor)
	assert.EqualValues(t, "0", wallet.ExactProfitCarry)
}

func TestResellerPaymentChargebackAfterPayoutCreatesOnlyAttributableDebt(t *testing.T) {
	db, _, _, original := paymentReversalFixture(t)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := ReleaseResellerPendingTx(tx, 2, original.AvailableAt, 0)
		return err
	}))
	var withdrawal *ResellerWithdrawal
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		withdrawal, err = RequestResellerWithdrawalTx(tx, ResellerWithdrawalInput{TenantID: 2, IdempotencyKey: "withdraw", AmountMinor: 180000, AlipayAccount: "owner", AlipayName: "Owner", Now: original.AvailableAt + 1})
		return err
	}))
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return reviewWithdrawalTestTx(tx, withdrawal.ID, ResellerWithdrawalApproved, "", "", original.AvailableAt+2)
	}))
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return reviewWithdrawalTestTx(tx, withdrawal.ID, ResellerWithdrawalPaid, "alipay-paid", "", original.AvailableAt+3)
	}))
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := ReverseCustomerPaymentTx(tx, CustomerPaymentReversalInput{TenantID: 2, TopUpID: 1, IdempotencyKey: "chargeback", Kind: "chargeback", ActorID: 99, Reason: "provider confirmed", PaymentReference: "provider-proof", Now: original.AvailableAt + 4})
		return err
	}))
	var wallet ResellerWallet
	require.NoError(t, db.Where("tenant_id = ?", 2).First(&wallet).Error)
	assert.Zero(t, wallet.AvailableMinor)
	assert.Zero(t, wallet.PendingMinor)
	assert.Equal(t, int64(86600), wallet.DebtMinor)
	assert.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := RequestResellerWithdrawalTx(tx, ResellerWithdrawalInput{TenantID: 2, IdempotencyKey: "blocked", AmountMinor: 10000, AlipayAccount: "owner", AlipayName: "Owner"})
		return err
	}), ErrResellerDebtOutstanding)
}

func TestResellerPaymentReversalWaitsForInflightRequestsWithoutMutations(t *testing.T) {
	for _, status := range []string{"reserved", "settlement_pending", "refund_pending"} {
		t.Run(status, func(t *testing.T) {
			db, user, lots, _ := paymentReversalFixture(t)
			record := TenantConsumption{TenantID: 2, CustomerID: 3, RequestID: "in-flight", Status: status, ReservedQuota: 2}
			require.NoError(t, db.Create(&record).Error)
			assert.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
				_, err := ReverseCustomerPaymentTx(tx, CustomerPaymentReversalInput{TenantID: 2, TopUpID: 2, IdempotencyKey: "refund", Kind: "refunded", ActorID: 99, Reason: "customer refund", PaymentReference: "proof"})
				return err
			}), ErrResellerPaymentInFlight)
			var count int64
			require.NoError(t, db.Model(&CustomerPaymentReversal{}).Count(&count).Error)
			assert.Zero(t, count)
			require.NoError(t, db.First(&lots[1], lots[1].ID).Error)
			assert.Equal(t, "none", lots[1].RefundStatus)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, int64(5), user.Quota)
		})
	}
}

func TestResellerConcurrentPaymentReversalAppliesOnce(t *testing.T) {
	db, user, _, _ := paymentReversalFixture(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	input := CustomerPaymentReversalInput{TenantID: 2, TopUpID: 2, IdempotencyKey: "concurrent", Kind: "refunded", ActorID: 99, Reason: "customer refund", PaymentReference: "proof", Now: 30}
	start, results := make(chan struct{}), make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			results <- db.Transaction(func(tx *gorm.DB) error { _, err := ReverseCustomerPaymentTx(tx, input); return err })
		}()
	}
	close(start)
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	var count int64
	require.NoError(t, db.Model(&CustomerPaymentReversal{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Zero(t, user.Quota)
	var wallet ResellerWallet
	require.NoError(t, db.Where("tenant_id = ?", 2).First(&wallet).Error)
	assert.Equal(t, int64(88400), wallet.PendingMinor)
}

func TestResellerPaymentReversalRollsBackWhenLedgerCannotBeWritten(t *testing.T) {
	db, user, lots, _ := paymentReversalFixture(t)
	input := CustomerPaymentReversalInput{TenantID: 2, TopUpID: 2, IdempotencyKey: "rollback", Kind: "refunded", ActorID: 99, Reason: "customer refund", PaymentReference: "proof", Now: 30}
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail-payment-reversal-ledger", func(tx *gorm.DB) {
		if tx.Statement.Table == "reseller_ledger_entries" {
			tx.AddError(ErrResellerWalletInvariant)
		}
	}))
	t.Cleanup(func() { require.NoError(t, db.Callback().Create().Remove("fail-payment-reversal-ledger")) })
	assert.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := ReverseCustomerPaymentTx(tx, input)
		return err
	}), ErrResellerWalletInvariant)
	var count int64
	require.NoError(t, db.Model(&CustomerPaymentReversal{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, int64(5), user.Quota)
	require.NoError(t, db.First(&lots[1], lots[1].ID).Error)
	assert.Equal(t, int64(5), lots[1].RemainingQuota)
	assert.Equal(t, "none", lots[1].RefundStatus)
	var wallet ResellerWallet
	require.NoError(t, db.Where("tenant_id = ?", 2).First(&wallet).Error)
	assert.Equal(t, int64(181800), wallet.PendingMinor)
}

func TestResellerPaymentReversalRejectsOtherTenantReceipt(t *testing.T) {
	db, _, _, _ := paymentReversalFixture(t)
	assert.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := ReverseCustomerPaymentTx(tx, CustomerPaymentReversalInput{TenantID: 3, TopUpID: 2, IdempotencyKey: "other", Kind: "chargeback", ActorID: 99, Reason: "provider confirmed", PaymentReference: "proof"})
		return err
	}), gorm.ErrRecordNotFound)
	var count int64
	require.NoError(t, db.Model(&CustomerPaymentReversal{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestResellerMixedLotPaymentReversalPreservesSubFenCarry(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	require.NoError(t, db.AutoMigrate(&User{}, &TopUp{}, &TenantConsumption{}, &CustomerPaymentReversal{}))
	user := User{Id: 3, TenantID: 2, Username: "fractional-customer", AffCode: "fractional-customer", Quota: 1}
	require.NoError(t, db.Create(&user).Error)
	for _, id := range []int{1, 2} {
		topup := TopUp{Id: id, TenantID: 2, UserId: 3, TradeNo: fmt.Sprintf("fractional-%d", id), PaidMinor: 2, Currency: ResellerCurrencyCNY, PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusSuccess}
		require.NoError(t, db.Create(&topup).Error)
		_, err := CreateCustomerPaymentLotTx(db, ResellerPaymentLotInput{TenantID: 2, CustomerID: 3, TopUpID: int64(id), OperationID: topup.TradeNo, PaidMinor: 2, PaymentFeeMinor: 1, CreditedQuota: 3})
		require.NoError(t, err)
	}
	quote, err := CreateResellerPricingQuoteTx(db, ResellerPricingQuoteInput{TenantID: 2, CustomerID: 3, PriceVersionID: 1, RequestID: "fractional", Model: "text", Group: "default", SupplierMinorPerQuota: "0.2", SupplierPriceMinor: 1, SaleMinor: 3, SalesMultiplier: decimal.NewFromInt(2), QuotedQuota: 5})
	require.NoError(t, err)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := SettleResellerConsumptionTx(tx, ResellerSettlementInput{TenantID: 2, CustomerID: 3, RequestID: quote.RequestID, QuoteID: quote.ID, ConsumedQuota: 5, Now: 20})
		return err
	}))
	for _, test := range []struct {
		topup         int64
		profit, carry string
	}{{1, "0.4", "0.6"}, {2, "0.6", "0"}} {
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			reversal, err := ReverseCustomerPaymentTx(tx, CustomerPaymentReversalInput{TenantID: 2, TopUpID: test.topup, IdempotencyKey: fmt.Sprintf("refund-%d", test.topup), Kind: "refunded", ActorID: 99, Reason: "confirmed refund", PaymentReference: "proof", Now: 30})
			if err == nil {
				assert.EqualValues(t, test.profit, reversal.ReversedProfitMinorExact)
			}
			return err
		}))
		var wallet ResellerWallet
		require.NoError(t, db.Where("tenant_id = ?", 2).First(&wallet).Error)
		assert.Zero(t, wallet.PendingMinor)
		assert.EqualValues(t, test.carry, wallet.ExactProfitCarry)
	}
}
