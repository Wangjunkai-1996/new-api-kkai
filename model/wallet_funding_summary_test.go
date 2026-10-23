package model

import (
	"context"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupWalletFundingSummaryDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupWalletPaymentDB(t)
	require.NoError(t, db.AutoMigrate(&KKAIInternalBalanceAdjustment{}, &InvoicePaymentReceipt{}, &SubscriptionOrder{}, &MembershipOrder{}))
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previous
		common.OptionMapRWMutex.Unlock()
	})
	return db
}

func TestWalletFundingSummaryUsesSettledCashEvidence(t *testing.T) {
	db := setupWalletFundingSummaryDB(t)
	now := common.GetTimestamp()
	orders := []TopUp{
		{TradeNo: "large-usd-one", Currency: "USD", CurrencyExponent: 2},
		{TradeNo: "large-usd-two", Currency: "USD", CurrencyExponent: 2},
		{TradeNo: "yen", Currency: "JPY", CurrencyExponent: 0},
		{TradeNo: "different-exponent", Currency: "USD", CurrencyExponent: 3},
		{TradeNo: "channel-adjusted", Currency: "CNY", CurrencyExponent: 2},
		{TradeNo: "free-checkout", Currency: "USD", CurrencyExponent: 2},
		{TradeNo: "legacy-with-proof", WalletLifecycleVersion: -1},
		{TradeNo: "manual-success-without-proof", PaidMinor: 999999, Money: 999999},
		{TradeNo: "pending", Status: common.TopUpStatusPending, Currency: "USD", CurrencyExponent: 2},
		{TradeNo: "review-receipt"},
		{TradeNo: "wrong-merchant"},
		{TradeNo: "subscription-mirror"},
		{TradeNo: "membership-mirror"},
		{TradeNo: "other-user", UserId: 42},
		{TradeNo: "other-tenant", TenantID: 6},
		{TradeNo: "voucher", PaymentProvider: PaymentProviderRedemption},
		{TradeNo: "balance-purchase", PaymentProvider: PaymentProviderBalance},
	}
	for i := range orders {
		order := &orders[i]
		if order.UserId == 0 {
			order.UserId = 41
		}
		if order.Status == "" {
			order.Status = common.TopUpStatusSuccess
		}
		if order.PaymentProvider == "" {
			order.PaymentProvider = PaymentProviderEpay
		}
		order.ProviderAccountRef, order.ProviderEnvironment = "merchant", "prod"
		order.CreateTime, order.CompleteTime = now-10000000, now-1000000
		order.PaidMinor = 100 // The expected price is deliberately unrelated to cash.
		if order.WalletLifecycleVersion == -1 {
			order.WalletLifecycleVersion = 0
		} else {
			order.WalletLifecycleVersion = 1
		}
		require.NoError(t, db.Create(order).Error)
		if i == 6 || i == 7 {
			continue
		}
		amount := int64(123)
		if i < 2 {
			amount = math.MaxInt64
		}
		if i == 5 {
			amount = 0
		}
		receipt := WalletPaymentReceipt{ReceiptKey: order.TradeNo, TopUpID: &order.Id, Provider: order.PaymentProvider, ProviderAccountRef: order.ProviderAccountRef, ProviderEnvironment: order.ProviderEnvironment, TransactionID: order.TradeNo, AmountMinor: amount, Currency: order.Currency, CurrencyExponent: order.CurrencyExponent, PaidAt: now - 1000000, EvidenceRef: order.TradeNo, VerificationStatus: "verified", ProcessingStatus: "credited", PricingVerified: true}
		if i == 5 {
			receipt.SettlementKind = "no_payment_required"
		}
		if i == 9 {
			receipt.ProcessingStatus = "review"
		}
		if i == 10 {
			receipt.ProviderAccountRef = "another-merchant"
		}
		require.NoError(t, db.Create(&receipt).Error)
	}
	// A second authenticated payment is retained for review, not double-counted.
	require.NoError(t, db.Create(&WalletPaymentReceipt{ReceiptKey: "second-payment", TopUpID: &orders[0].Id, AmountMinor: 999, VerificationStatus: "verified", ProcessingStatus: "review"}).Error)
	require.NoError(t, db.Create(&InvoicePaymentReceipt{TopUpID: int64(orders[6].Id), UserID: 41, Provider: PaymentProviderEpay, ReceiptKey: "legacy-proof", AmountMinor: 450, Currency: "CNY", Status: "verified", PaidAt: now - 1000000}).Error)
	require.NoError(t, db.Model(&orders[6]).Update("wallet_lifecycle_version", nil).Error, "the real expansion leaves historical lifecycle columns NULL")
	require.NoError(t, db.Create(&SubscriptionOrder{TradeNo: orders[11].TradeNo}).Error)
	require.NoError(t, db.Create(&MembershipOrder{TradeNo: orders[12].TradeNo}).Error)

	summary, err := GetWalletFundingSummary(context.Background(), WalletTopUpScope{UserID: 41})
	require.NoError(t, err)
	assert.Equal(t, []dto.WalletMoney{
		{AmountMinor: "573", Currency: "CNY", Exponent: 2},
		{AmountMinor: "123", Currency: "JPY", Exponent: 0},
		{AmountMinor: "18446744073709551614", Currency: "USD", Exponent: 2},
		{AmountMinor: "123", Currency: "USD", Exponent: 3},
	}, summary.CashPayments)
	assert.EqualValues(t, 6, summary.ConfirmedPaymentCount)
	assert.EqualValues(t, 3, summary.UnknownPaymentCount)
	assert.False(t, summary.RefundAdjusted)
	assert.Equal(t, "0", summary.RebateCreditedQuota)
	assert.Equal(t, common.CreditEpochLegacy, summary.QuotaEpoch)
	history, err := GetWalletCreditedReceipts([]int{orders[4].Id, orders[5].Id, orders[6].Id, orders[7].Id})
	require.NoError(t, err)
	assert.EqualValues(t, 123, history[orders[4].Id].AmountMinor)
	assert.EqualValues(t, 0, history[orders[5].Id].AmountMinor)
	assert.EqualValues(t, 450, history[orders[6].Id].AmountMinor, "history and lifetime totals use the same legacy proof")
	assert.NotContains(t, history, orders[7].Id)
	require.NoError(t, db.Model(&orders[2]).Update("currency", "US1").Error)
	require.NoError(t, db.Model(&WalletPaymentReceipt{}).Where("receipt_key = ?", orders[2].TradeNo).Update("currency", "US1").Error)
	_, err = GetWalletFundingSummary(context.Background(), WalletTopUpScope{UserID: 41})
	require.ErrorIs(t, err, ErrWalletPaymentEvidence, "malformed receipt currency never reaches the public money contract")
	_, err = GetWalletCreditedReceipts([]int{orders[2].Id})
	require.ErrorIs(t, err, ErrWalletPaymentEvidence)
}

func TestWalletFundingSummaryLegacyProofCannotCrossOwnersOrReplaceLifecycleEvidence(t *testing.T) {
	db := setupWalletFundingSummaryDB(t)
	now := common.GetTimestamp()
	orders := []TopUp{
		{UserId: 41, TradeNo: "wrong-proof-owner", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusSuccess},
		{UserId: 41, TradeNo: "wrong-proof-tenant", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusSuccess},
		{UserId: 41, TradeNo: "new-lifecycle", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusSuccess, WalletLifecycleVersion: 1},
		{UserId: 41, TradeNo: "exception-proof", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusSuccess},
	}
	require.NoError(t, db.Create(&orders).Error)
	for i, order := range orders {
		proof := InvoicePaymentReceipt{TopUpID: int64(order.Id), UserID: 41, Provider: PaymentProviderEpay, ReceiptKey: order.TradeNo, AmountMinor: 999, Currency: "CNY", Status: "verified", PaidAt: now}
		if i == 0 {
			proof.UserID = 42
		}
		if i == 1 {
			proof.TenantID = 6
		}
		if i == 3 {
			proof.Status = "exception"
		}
		require.NoError(t, db.Create(&proof).Error)
	}
	summary, err := GetWalletFundingSummary(context.Background(), WalletTopUpScope{UserID: 41})
	require.NoError(t, err)
	assert.Empty(t, summary.CashPayments)
	assert.Zero(t, summary.ConfirmedPaymentCount)
	assert.EqualValues(t, len(orders), summary.UnknownPaymentCount)
}

func TestWalletFundingSummaryNormalizesActualRebateCredits(t *testing.T) {
	db := setupWalletFundingSummaryDB(t)
	legacyActual, delayedActual, reversalActual, zeroActual := int64(100), int64(8), int64(-8), int64(0)
	rows := []KKAIInternalBalanceAdjustment{
		{ID: 1, OperationID: "legacy-wallet", UserID: 41, Delta: 999, WalletDelta: &legacyActual, Reason: KKAIBalanceAdjustmentReasonCredit},
		{ID: 2, OperationID: "legacy-delta", UserID: 41, Delta: 100, Reason: KKAIBalanceAdjustmentReasonCredit},
		{ID: 3, OperationID: "delayed-legacy", UserID: 41, Delta: 100, WalletDelta: &delayedActual, SourceEpoch: common.CreditEpochLegacy, Reason: KKAIBalanceAdjustmentReasonCredit},
		{ID: 4, OperationID: "delayed-reversal", UserID: 41, Delta: -100, WalletDelta: &reversalActual, SourceEpoch: common.CreditEpochLegacy, Reason: KKAIBalanceAdjustmentReasonReversal},
		{ID: 5, OperationID: "rounded-zero", UserID: 41, Delta: 1, WalletDelta: &zeroActual, SourceEpoch: common.CreditEpochLegacy, Reason: KKAIBalanceAdjustmentReasonCredit},
		{ID: 6, OperationID: "unrelated-user", UserID: 42, Delta: 999, Reason: KKAIBalanceAdjustmentReasonCredit},
		{ID: 7, OperationID: "other-adjustment", UserID: 41, Delta: 999, Reason: "admin_adjustment"},
	}
	require.NoError(t, db.Create(&rows).Error)
	setTestCreditEpoch(t, map[string]int64{"kkai_internal_balance_adjustments": 2})
	summary, err := GetWalletFundingSummary(context.Background(), WalletTopUpScope{UserID: 41})
	require.NoError(t, err)
	assert.Equal(t, "16", summary.RebateCreditedQuota, "round each legacy entry; delayed payouts are already current wallet units")
	assert.Equal(t, common.CreditEpochUSD, summary.QuotaEpoch)
	assert.Empty(t, summary.CashPayments)

	require.NoError(t, db.Model(&rows[2]).Update("wallet_delta", nil).Error)
	_, err = GetWalletFundingSummary(context.Background(), WalletTopUpScope{UserID: 41})
	require.ErrorIs(t, err, common.ErrCreditEpochInvalid)
}

func TestWalletFundingSummaryRejectsCrossTenantAndIncompleteEpoch(t *testing.T) {
	db := setupWalletFundingSummaryDB(t)
	_, err := GetWalletFundingSummary(context.Background(), WalletTopUpScope{TenantID: 6, UserID: 41})
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, err = GetWalletFundingSummary(context.Background(), WalletTopUpScope{UserID: 0})
	require.ErrorIs(t, err, ErrResellerFinanceInvalidInput)
	setTestCreditEpoch(t, map[string]int64{})
	_, err = GetWalletFundingSummary(context.Background(), WalletTopUpScope{UserID: 41})
	require.ErrorIs(t, err, common.ErrCreditEpochInvalid)
	var count int64
	require.NoError(t, db.Model(&TopUp{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestWalletHistoryAllTimePreservesRecentAndExactScopes(t *testing.T) {
	db := setupWalletFundingSummaryDB(t)
	now := common.GetTimestamp()
	orders := []TopUp{
		{UserId: 41, TradeNo: "recent-success", CreateTime: now, Status: common.TopUpStatusSuccess},
		{UserId: 41, TradeNo: "old-success", CreateTime: now - topUpQueryWindowSeconds - 1, Status: common.TopUpStatusSuccess},
		{UserId: 41, TradeNo: "other-tenant", TenantID: 6, CreateTime: now, Status: common.TopUpStatusSuccess},
	}
	require.NoError(t, db.Create(&orders).Error)
	for _, tc := range []struct {
		scope, exact, resolved string
		count                  int64
	}{
		{"", "", "recent", 1},
		{"all", "", "all", 2},
		{"unfinished", "", "unfinished", 0},
		{"recent", "old-success", "exact", 1},
		{"all", "other-tenant", "exact", 0},
	} {
		result, err := QueryWalletTopUps(WalletTopUpHistoryQuery{Scope: WalletTopUpScope{UserID: 41}, QueryScope: tc.scope, ExactTradeNo: tc.exact, Limit: 10})
		require.NoError(t, err)
		assert.Equal(t, tc.count, result.Total, tc.scope+tc.exact)
		assert.Equal(t, tc.resolved, result.QueryScope)
	}
}
