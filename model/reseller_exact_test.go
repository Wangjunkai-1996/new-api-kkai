package model

import (
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestResellerExactThreeMultiplierDoesNotLoseOneFen(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	sale, supply, err := ResellerQuotaTariff(decimal.NewFromInt(1), decimal.NewFromInt(500000), decimal.NewFromInt(1), decimal.NewFromInt(3))
	require.NoError(t, err)
	assert.Equal(t, "0.0002", sale)
	assert.Equal(t, "1/15000", supply)
	quote, err := CreateResellerPricingQuoteTx(db, ResellerPricingQuoteInput{TenantID: 42, CustomerID: 7,
		RequestID: "three-times", PriceVersionID: 1, Model: "text", Group: "default", SupplierPriceMinor: 20000,
		SupplierMinorPerQuota: supply, SaleMinorPerQuota: sale, SaleMinor: 60000, QuotedQuota: 300000000, SalesMultiplier: decimal.NewFromInt(3)})
	require.NoError(t, err)
	assert.Equal(t, "39040", quote.ProfitMinorExact)
	_, err = CreateCustomerPaymentLotTx(db, ResellerPaymentLotInput{TenantID: 42, CustomerID: 7, TopUpID: 1,
		OperationID: "three-times-paid", PaidMinor: 100000, PaymentFeeMinor: 1600, CreditedQuota: 500000000})
	require.NoError(t, err)
	entry, err := SettleResellerConsumptionTx(db, ResellerSettlementInput{TenantID: 42, CustomerID: 7,
		RequestID: quote.RequestID, QuoteID: quote.ID, ConsumedQuota: 300000000, Now: 100})
	require.NoError(t, err)
	assert.Equal(t, "20000", entry.SupplierMinorExact)
	assert.Equal(t, "39040", entry.ExactProfitMinor)
	assert.EqualValues(t, 39040, entry.AmountMinor)
	var wallet ResellerWallet
	require.NoError(t, db.Where("tenant_id = ?", 42).First(&wallet).Error)
	assert.EqualValues(t, 39040, wallet.PendingMinor)
	assert.EqualValues(t, "0", wallet.ExactProfitCarry)
}

func TestResellerExactFractionAccruesAndReversesWithoutRounding(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	_, err := CreateCustomerPaymentLotTx(db, ResellerPaymentLotInput{TenantID: 42, CustomerID: 7, TopUpID: 1,
		OperationID: "fraction-paid", PaidMinor: 6, PaymentFeeMinor: 1, CreditedQuota: 3})
	require.NoError(t, err)
	var entries []*ResellerLedgerEntry
	for i, expected := range []struct {
		credit int64
		profit string
		carry  string
	}{{1, "5/3", "2/3"}, {2, "5/3", "1/3"}, {1, "2/3", "0"}} {
		quote, err := CreateResellerPricingQuoteTx(db, ResellerPricingQuoteInput{TenantID: 42, CustomerID: 7,
			RequestID: fmt.Sprintf("third-%d", i), PriceVersionID: 1, Model: "text", Group: "default", SupplierPriceMinor: 1,
			SupplierMinorPerQuota: "1/3", SaleMinorPerQuota: "2", SaleMinor: 2, QuotedQuota: 1, SalesMultiplier: decimal.NewFromInt(3)})
		require.NoError(t, err)
		entry, err := SettleResellerConsumptionTx(db, ResellerSettlementInput{TenantID: 42, CustomerID: 7,
			RequestID: quote.RequestID, QuoteID: quote.ID, ConsumedQuota: 1, Now: 100})
		require.NoError(t, err)
		assert.Equal(t, expected.credit, entry.AmountMinor)
		assert.Equal(t, "1/3", entry.SupplierMinorExact)
		assert.Equal(t, expected.profit, entry.ExactProfitMinor)
		var wallet ResellerWallet
		require.NoError(t, db.Where("tenant_id = ?", 42).First(&wallet).Error)
		assert.EqualValues(t, expected.carry, wallet.ExactProfitCarry)
		entries = append(entries, entry)
	}
	for _, entry := range entries {
		_, err := ReverseResellerSettlementTx(db, ResellerChargebackInput{TenantID: 42, OriginalOperation: entry.OperationID,
			ActorID: 1, Reason: "confirmed synthetic reversal", Now: 200})
		require.NoError(t, err)
	}
	var wallet ResellerWallet
	require.NoError(t, db.Where("tenant_id = ?", 42).First(&wallet).Error)
	assert.Zero(t, wallet.PendingMinor)
	assert.EqualValues(t, "0", wallet.ExactProfitCarry)
}

func TestResellerExactMultiplePriceVersionsPersistAndReverseLargeAggregate(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	require.NoError(t, db.AutoMigrate(&User{}, &TopUp{}, &TenantConsumption{}, &CustomerPaymentReversal{}))
	require.NoError(t, db.Create(&User{Id: 7, TenantID: 42, Username: "aggregate-customer", AffCode: "aggregate-customer", Quota: 500000000}).Error)
	require.NoError(t, db.Create(&TopUp{Id: 1, TenantID: 42, UserId: 7, TradeNo: "aggregate-paid", PaidMinor: 100000,
		Currency: ResellerCurrencyCNY, PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusSuccess}).Error)
	_, err := CreateCustomerPaymentLotTx(db, ResellerPaymentLotInput{TenantID: 42, CustomerID: 7, TopUpID: 1,
		OperationID: "aggregate-paid", PaidMinor: 100000, PaymentFeeMinor: 1600, CreditedQuota: 500000000})
	require.NoError(t, err)
	multipliers := []string{"2", "1.030000000000000003", "1.070000000000000011", "1.090000000000000013", "1.130000000000000017"}
	quotas := []int64{200000000, 1030, 1070, 1090, 1130}
	expectedProfit := new(big.Rat)
	for index, value := range multipliers {
		multiplier := decimal.RequireFromString(value)
		sale, supply, err := ResellerQuotaTariff(decimal.NewFromInt(1), decimal.NewFromInt(500000), decimal.NewFromInt(1), multiplier)
		require.NoError(t, err)
		saleMinor, err := ResellerQuotedCharge(sale, quotas[index])
		require.NoError(t, err)
		supplyMinor, err := ResellerQuotedCharge(supply, quotas[index])
		require.NoError(t, err)
		quote, err := CreateResellerPricingQuoteTx(db, ResellerPricingQuoteInput{TenantID: 42, CustomerID: 7,
			RequestID: fmt.Sprintf("aggregate-%d", index), PriceVersionID: int64(index + 1), Model: "text", Group: "default",
			SupplierPriceMinor: supplyMinor, SupplierMinorPerQuota: supply, SaleMinorPerQuota: sale,
			SaleMinor: saleMinor, QuotedQuota: quotas[index], SalesMultiplier: multiplier})
		require.NoError(t, err)
		entry, err := SettleResellerConsumptionTx(db, ResellerSettlementInput{TenantID: 42, CustomerID: 7,
			RequestID: quote.RequestID, QuoteID: quote.ID, ConsumedQuota: quotas[index], Now: 100})
		require.NoError(t, err)
		profit, err := parseResellerExact(entry.ExactProfitMinor)
		require.NoError(t, err)
		expectedProfit.Add(expectedProfit, profit)
	}
	var wallet ResellerWallet
	require.NoError(t, db.Where("tenant_id = ?", 42).First(&wallet).Error)
	assert.EqualValues(t, 19360, wallet.PendingMinor)
	assert.Greater(t, len(wallet.ExactProfitCarry), 128)
	carry, err := parseResellerAggregate(wallet.ExactProfitCarry)
	require.NoError(t, err)
	assert.Zero(t, carry.Cmp(new(big.Rat).Sub(expectedProfit, big.NewRat(19360, 1))))
	_, err = ReleaseResellerPendingTx(db, 42, 100+ResellerPendingPeriodSec, 0)
	require.NoError(t, err)
	withdrawalInput := ResellerWithdrawalInput{TenantID: 42, IdempotencyKey: "aggregate-withdrawal", AmountMinor: 19360,
		AlipayAccount: "local@example.invalid", AlipayName: "local", Now: 200000}
	_, err = RequestResellerWithdrawalTx(db, withdrawalInput)
	assert.ErrorIs(t, err, ErrResellerInsufficientFunds)
	withdrawalInput.AmountMinor = 15000
	withdrawal, err := RequestResellerWithdrawalTx(db, withdrawalInput)
	require.NoError(t, err)
	_, err = ReviewResellerWithdrawalTx(db, ResellerWithdrawalReviewInput{WithdrawalID: withdrawal.ID, ReviewerID: 1, Status: ResellerWithdrawalApproved, Now: 200001})
	require.NoError(t, err)
	_, err = ReviewResellerWithdrawalTx(db, ResellerWithdrawalReviewInput{WithdrawalID: withdrawal.ID, ReviewerID: 1,
		Status: ResellerWithdrawalPaid, PaymentRef: "confirmed-local", Now: 200002})
	require.NoError(t, err)
	input := CustomerPaymentReversalInput{TenantID: 42, TopUpID: 1, IdempotencyKey: "aggregate-reverse", Kind: "chargeback",
		ActorID: 1, Reason: "confirmed synthetic rejection", PaymentReference: "local proof", Now: 200003}
	var reversal *CustomerPaymentReversal
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		reversal, err = ReverseCustomerPaymentTx(tx, input)
		return err
	}))
	assert.Greater(t, len(reversal.ReversedProfitMinorExact), 128)
	reversed, err := parseResellerAggregate(reversal.ReversedProfitMinorExact)
	require.NoError(t, err)
	assert.Zero(t, reversed.Cmp(expectedProfit))
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		repeated, err := ReverseCustomerPaymentTx(tx, input)
		if err == nil {
			assert.Equal(t, reversal.ID, repeated.ID)
		}
		return err
	}))
	require.NoError(t, db.First(&wallet, wallet.ID).Error)
	assert.Zero(t, wallet.PendingMinor)
	assert.Zero(t, wallet.AvailableMinor)
	assert.Zero(t, wallet.FrozenMinor)
	assert.EqualValues(t, 15000, wallet.DebtMinor)
	assert.EqualValues(t, "0", wallet.ExactProfitCarry)
}

func TestResellerMixedLotReversalRetainsExactFraction(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	require.NoError(t, db.AutoMigrate(&User{}, &TopUp{}, &TenantConsumption{}, &CustomerPaymentReversal{}))
	require.NoError(t, db.Create(&User{Id: 3, TenantID: 2, Username: "fraction-customer", AffCode: "fraction-customer", Quota: 1}).Error)
	for _, id := range []int{1, 2} {
		topup := TopUp{Id: id, TenantID: 2, UserId: 3, TradeNo: fmt.Sprintf("third-lot-%d", id), PaidMinor: 2,
			Currency: ResellerCurrencyCNY, PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusSuccess}
		require.NoError(t, db.Create(&topup).Error)
		_, err := CreateCustomerPaymentLotTx(db, ResellerPaymentLotInput{TenantID: 2, CustomerID: 3, TopUpID: int64(id),
			OperationID: topup.TradeNo, PaidMinor: 2, PaymentFeeMinor: 1, CreditedQuota: 3})
		require.NoError(t, err)
	}
	quote, err := CreateResellerPricingQuoteTx(db, ResellerPricingQuoteInput{TenantID: 2, CustomerID: 3,
		RequestID: "mixed-thirds", PriceVersionID: 1, Model: "text", Group: "default", SupplierPriceMinor: 2,
		SupplierMinorPerQuota: "1/3", SaleMinor: 3, QuotedQuota: 5, SalesMultiplier: decimal.NewFromInt(2)})
	require.NoError(t, err)
	entry, err := SettleResellerConsumptionTx(db, ResellerSettlementInput{TenantID: 2, CustomerID: 3,
		RequestID: quote.RequestID, QuoteID: quote.ID, ConsumedQuota: 5, Now: 100})
	require.NoError(t, err)
	assert.Equal(t, "5/3", entry.SupplierMinorExact)
	assert.Equal(t, "1/3", entry.ExactProfitMinor)
	for _, expected := range []struct {
		id            int64
		profit, carry string
	}{{1, "0", "1/3"}, {2, "1/3", "0"}} {
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			reversal, err := ReverseCustomerPaymentTx(tx, CustomerPaymentReversalInput{TenantID: 2, TopUpID: expected.id,
				IdempotencyKey: fmt.Sprintf("third-refund-%d", expected.id), Kind: "refunded", ActorID: 1,
				Reason: "confirmed synthetic refund", PaymentReference: "local proof", Now: 200})
			if err == nil {
				assert.EqualValues(t, expected.profit, reversal.ReversedProfitMinorExact)
			}
			return err
		}))
		var wallet ResellerWallet
		require.NoError(t, db.Where("tenant_id = ?", 2).First(&wallet).Error)
		assert.Zero(t, wallet.PendingMinor)
		assert.EqualValues(t, expected.carry, wallet.ExactProfitCarry)
	}
}

func TestResellerExactTariffRejectsInvalidAndOversizedValues(t *testing.T) {
	for _, value := range []string{"1/0", "1/-3", "2/6", "+1/3", "1.0/3", strings.Repeat("1", 129), "1e1000000000"} {
		t.Run(value[:min(len(value), 20)], func(t *testing.T) {
			_, err := parseResellerExact(value)
			assert.Error(t, err)
		})
	}
	for _, value := range []string{"0.005", "-0.2", "1/3", "-1/3"} {
		t.Run(value, func(t *testing.T) {
			ratio, err := parseResellerExact(value)
			require.NoError(t, err)
			text, err := resellerExactString(ratio, 128)
			require.NoError(t, err)
			assert.Equal(t, value, text)
		})
	}
	ratio, err := parseResellerExact("1/300")
	require.NoError(t, err)
	_, err = resellerExactString(ratio, 4)
	assert.ErrorIs(t, err, ErrResellerFinanceInvalidInput)
}

func TestResellerWithdrawalReservesNegativeFraction(t *testing.T) {
	db := newResellerFinanceTestDB(t)
	wallet := ResellerWallet{TenantID: 42, AvailableMinor: 10000, ExactProfitCarry: "-1/3", Version: 1}
	require.NoError(t, db.Create(&wallet).Error)
	input := ResellerWithdrawalInput{TenantID: 42, IdempotencyKey: "fraction-reserve", AmountMinor: 10000,
		AlipayAccount: "local@example.invalid", AlipayName: "local", Now: 100}
	_, err := RequestResellerWithdrawalTx(db, input)
	assert.ErrorIs(t, err, ErrResellerInsufficientFunds)
	require.NoError(t, db.Model(&wallet).Update("available_minor", 10001).Error)
	withdrawal, err := RequestResellerWithdrawalTx(db, input)
	require.NoError(t, err)
	_, err = ReviewResellerWithdrawalTx(db, ResellerWithdrawalReviewInput{WithdrawalID: withdrawal.ID, ReviewerID: 1, Status: ResellerWithdrawalApproved, Now: 101})
	require.NoError(t, err)
	require.NoError(t, db.Model(&wallet).Update("available_minor", 0).Error)
	_, err = ReviewResellerWithdrawalTx(db, ResellerWithdrawalReviewInput{WithdrawalID: withdrawal.ID, ReviewerID: 1,
		Status: ResellerWithdrawalPaid, PaymentRef: "confirmed-local", Now: 102})
	assert.ErrorIs(t, err, ErrResellerInsufficientFunds)
}
