package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81"
)

func TestWalletPaymentSecretsPreserveOriginalSessionAcrossRotation(t *testing.T) {
	t.Setenv("CRYPTO_SECRET", "")
	t.Setenv("WALLET_PAYMENT_SECRET", strings.Repeat("a", 32))
	t.Setenv("WALLET_PAYMENT_PREVIOUS_SECRETS", "")
	original := WalletGatewaySnapshot{Provider: "stripe", APIKey: "original-merchant-secret", Checkout: nil}
	sealed, err := SealWalletPayment(original, "tenant/user/order/merchant")
	require.NoError(t, err)
	var restored WalletGatewaySnapshot
	require.NoError(t, OpenWalletPayment(sealed, "tenant/user/order/merchant", &restored))
	assert.Equal(t, original.APIKey, restored.APIKey)
	assert.Error(t, OpenWalletPayment(sealed, "other/order/merchant", &restored))
	t.Setenv("WALLET_PAYMENT_SECRET", strings.Repeat("b", 32))
	assert.Error(t, OpenWalletPayment(sealed, "tenant/user/order/merchant", &restored))
	oldKeys, err := common.Marshal([]string{strings.Repeat("a", 32)})
	require.NoError(t, err)
	t.Setenv("WALLET_PAYMENT_PREVIOUS_SECRETS", string(oldKeys))
	require.NoError(t, OpenWalletPayment(sealed, "tenant/user/order/merchant", &restored))
	assert.Equal(t, original.APIKey, restored.APIKey)
}
func TestWalletPaymentSecretsRejectEphemeralDefault(t *testing.T) {
	t.Setenv("WALLET_PAYMENT_SECRET", "")
	t.Setenv("CRYPTO_SECRET", "")
	assert.False(t, WalletPaymentSecretsReady())
	_, err := SealWalletPayment(map[string]string{"session": "secret"}, "order")
	require.ErrorIs(t, err, ErrWalletSecretUnavailable)
}
func TestWalletStripePricingProtectsFrozenPurchase(t *testing.T) {
	order := &model.TopUp{PaidMinor: 1000}
	snapshot := WalletGatewaySnapshot{PriceID: "price_original", Quantity: 2, AllowPromotionCodes: true}
	newSession := func() *stripe.CheckoutSession {
		return &stripe.CheckoutSession{Mode: "payment", PaymentStatus: "paid", AmountSubtotal: 1000, AmountTotal: 800, TotalDetails: &stripe.CheckoutSessionTotalDetails{AmountDiscount: 200}, LineItems: &stripe.LineItemList{Data: []*stripe.LineItem{{Price: &stripe.Price{ID: "price_original"}, Quantity: 2, AmountSubtotal: 1000}}}}
	}
	tests := []struct {
		name   string
		change func(*stripe.CheckoutSession)
		want   bool
	}{
		{"legal original promotion", func(*stripe.CheckoutSession) {}, true},
		{"different product", func(s *stripe.CheckoutSession) { s.LineItems.Data[0].Price.ID = "price_other" }, false},
		{"refunded original payment", func(s *stripe.CheckoutSession) {
			s.PaymentIntent = &stripe.PaymentIntent{LatestCharge: &stripe.Charge{Refunded: true, AmountRefunded: 100}}
		}, false},
		{"different quantity", func(s *stripe.CheckoutSession) { s.LineItems.Data[0].Quantity = 3 }, false},
		{"unexplained underpayment", func(s *stripe.CheckoutSession) { s.AmountTotal = 700 }, false},
		{"unconfigured tax", func(s *stripe.CheckoutSession) { s.TotalDetails.AmountTax = 50; s.AmountTotal = 850 }, false},
		{"legal full promotion", func(s *stripe.CheckoutSession) {
			s.TotalDetails.AmountDiscount = 1000
			s.AmountTotal = 0
			s.PaymentStatus = "no_payment_required"
		}, true},
		{"zero without settled promotion", func(s *stripe.CheckoutSession) {
			s.AmountTotal = 0
			s.TotalDetails.AmountDiscount = 1000
			s.PaymentStatus = "unpaid"
		}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newSession()
			test.change(s)
			assert.Equal(t, test.want, validWalletStripePricing(order, snapshot, s))
		})
	}
}

func TestWalletQuoteUsesStablePaymentKey(t *testing.T) {
	t.Setenv("WALLET_PAYMENT_SECRET", strings.Repeat("q", 32))
	t.Setenv("WALLET_PAYMENT_PREVIOUS_SECRETS", "")
	old := common.CryptoSecret
	t.Cleanup(func() { common.CryptoSecret = old })
	common.CryptoSecret = "ephemeral-process-one"
	token, err := SignWalletQuote(map[string]string{"order": "frozen"})
	require.NoError(t, err)
	common.CryptoSecret = "ephemeral-process-two"
	var claims map[string]string
	require.NoError(t, VerifyWalletQuote(token, &claims))
	assert.Equal(t, "frozen", claims["order"])
	assert.Error(t, VerifyWalletQuote(token+"modified", &claims))
}
func TestWalletPaymentMinorRejectsUnboundedAndFractionalMoney(t *testing.T) {
	for _, tc := range []struct {
		amount   string
		exponent int
		want     int64
		valid    bool
	}{{"100.25", 2, 10025, true}, {"100", 0, 100, true}, {"1.234", 3, 1234, true}, {"92233720368547758.07", 2, 9223372036854775807, true}, {"92233720368547758.08", 2, 0, false}, {"1.001", 2, 0, false}, {"NaN", 2, 0, false}, {"1e99", 2, 0, false}, {"-1", 2, 0, false}} {
		t.Run(tc.amount, func(t *testing.T) {
			got, err := WalletPaymentMinor(tc.amount, tc.exponent)
			if tc.valid {
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestWalletEpayUnpaidRemainsPayableAfterManyChecks(t *testing.T) {
	db := walletReceiptRecoveryDB(t)
	t.Setenv("WALLET_PAYMENT_SECRET", strings.Repeat("e", 32))
	t.Setenv("WALLET_PAYMENT_PREVIOUS_SECRETS", "")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "WT-unpaid", r.URL.Query().Get("out_trade_no"))
		_, _ = w.Write([]byte(`{"code":1,"status":0,"pid":"merchant","out_trade_no":"WT-unpaid","trade_no":"provider-order","money":"10.00","currency":"CNY"}`))
	}))
	defer server.Close()
	oldClient := http.DefaultClient
	http.DefaultClient = server.Client()
	t.Cleanup(func() { http.DefaultClient = oldClient })
	now := time.Now().Unix()
	order := model.TopUp{UserId: 29, TradeNo: "WT-unpaid", Status: "pending", CreateTime: now - 100, WalletLifecycleVersion: 1, PaymentProvider: "epay", PaymentMethod: "alipay", ProviderAccountRef: "original-merchant", ProviderEnvironment: "prod", PaidMinor: 1000, Currency: "CNY", CurrencyExponent: 2, CreateState: "ready", CheckAttempts: 20, PayDeadlineAt: now + 3600}
	snapshot := WalletGatewaySnapshot{Provider: "epay", APIURL: server.URL, MerchantID: "merchant", APIKey: "original-key", Environment: "prod", CanResume: true}
	var err error
	order.CheckoutPayload, err = SealWalletPayment(snapshot, WalletPaymentScope(&order))
	require.NoError(t, err)
	require.NoError(t, db.Create(&order).Error)
	classification, err := ReconcileWalletTopUp(context.Background(), order.TradeNo)
	require.NoError(t, err)
	assert.Equal(t, "retry_failed", classification)
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.Equal(t, "pending", order.Status)
	assert.Equal(t, "unpaid", order.ProviderPaymentState)
	assert.Empty(t, order.ReasonCode)
	assert.Empty(t, order.CloseEvidenceRef)
	assert.Greater(t, order.NextCheckAt, now)
}

func TestWalletEndedEpayStopsPollingButLatePaymentStillSettles(t *testing.T) {
	db := walletReceiptRecoveryDB(t)
	t.Setenv("WALLET_PAYMENT_SECRET", strings.Repeat("e", 32))
	t.Setenv("WALLET_PAYMENT_PREVIOUS_SECRETS", "")
	t.Setenv("TOPUP_REBATE_ACTIVE_FROM_ID", "1")
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedis })
	paid, queries := false, 0
	now := time.Now().Unix()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries++
		assert.Equal(t, "WT-ended-epay", r.URL.Query().Get("out_trade_no"))
		assert.Equal(t, "merchant", r.URL.Query().Get("pid"))
		assert.Equal(t, "original-key", r.URL.Query().Get("key"))
		status := 0
		if paid {
			status = 1
		}
		body, err := common.Marshal(map[string]any{"code": 1, "status": status, "pid": "merchant", "out_trade_no": "WT-ended-epay", "trade_no": "provider-late-payment", "money": "10.00", "currency": "CNY", "endtime": time.Unix(now, 0).Format("2006-01-02 15:04:05")})
		require.NoError(t, err)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	oldClient := http.DefaultClient
	http.DefaultClient = server.Client()
	t.Cleanup(func() { http.DefaultClient = oldClient })
	user := model.User{Id: 63, Username: "late-wallet", AffCode: "late-wallet", Group: "default", Quota: 10}
	require.NoError(t, db.Create(&user).Error)
	order := model.TopUp{UserId: user.Id, TradeNo: "WT-ended-epay", Status: "pending", CreateTime: now - 3600, WalletLifecycleVersion: 1, PaymentProvider: "epay", PaymentMethod: "alipay", ProviderAccountRef: "original-merchant", ProviderEnvironment: "prod", PaidMinor: 1000, Currency: "CNY", CurrencyExponent: 2, CreditedQuota: 500, PricingMode: "fixed", CreateState: "ready", CheckAttempts: 8, LocalEndedAt: now - 1800, LocalEndReason: "user_ended"}
	snapshot := WalletGatewaySnapshot{Provider: "epay", APIURL: server.URL, MerchantID: "merchant", APIKey: "original-key", Environment: "prod"}
	var err error
	order.CheckoutPayload, err = SealWalletPayment(snapshot, WalletPaymentScope(&order))
	require.NoError(t, err)
	require.NoError(t, db.Create(&order).Error)
	_, err = ReconcileWalletTopUp(context.Background(), order.TradeNo)
	require.NoError(t, err)
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.Zero(t, order.NextCheckAt)
	assert.Equal(t, "pending", order.Status)
	assert.Empty(t, order.CloseEvidenceRef)
	// A trusted callback can schedule this original order again after regular
	// polling stops. The next authenticated query may still prove late funds.
	paid = true
	_, err = model.ScheduleWalletTopUpCheck(model.WalletTopUpScope{UserID: order.UserId}, order.TradeNo)
	require.NoError(t, err)
	category, err := ReconcileWalletTopUp(context.Background(), order.TradeNo)
	require.NoError(t, err)
	assert.Equal(t, "credited", category)
	assert.Equal(t, 2, queries)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.EqualValues(t, 510, user.Quota)
	category, err = ReconcileWalletTopUp(context.Background(), order.TradeNo)
	require.NoError(t, err)
	assert.Equal(t, "credited", category)
	assert.Equal(t, 2, queries)
}

func TestWalletEndedUnknownCreationNeverReplaysGatewayCreate(t *testing.T) {
	db := walletReceiptRecoveryDB(t)
	t.Setenv("WALLET_PAYMENT_SECRET", strings.Repeat("u", 32))
	order := model.TopUp{UserId: 64, TradeNo: "WT-ended-unknown", Status: "pending", CreateTime: time.Now().Unix() - 3600, WalletLifecycleVersion: 1, PaymentProvider: "stripe", ProviderAccountRef: "original-merchant", ProviderEnvironment: "test", CreateState: "unknown"}
	snapshot := WalletGatewaySnapshot{Provider: "stripe", APIKey: "sk_original", Environment: "test", PriceID: "price_original", Quantity: 1}
	var err error
	order.CheckoutPayload, err = SealWalletPayment(snapshot, WalletPaymentScope(&order))
	require.NoError(t, err)
	require.NoError(t, db.Create(&order).Error)
	walletStripeTestBackend(t, func(http.ResponseWriter, *http.Request) {
		t.Error("ended attempts must not create or recover a payable gateway session")
	})
	category, err := ReconcileWalletTopUp(context.Background(), order.TradeNo)
	require.NoError(t, err)
	assert.Equal(t, "closed", category)
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.Zero(t, order.NextCheckAt)
	assert.Empty(t, order.ProviderSessionID)
	assert.Empty(t, order.CloseEvidenceRef)
	// A late receipt promotes the financial responsibility to credit review,
	// but cannot authorize replaying checkout creation for the ended attempt.
	require.NoError(t, db.Model(&order).Updates(map[string]any{"status": "credit_review", "provider_payment_state": "paid"}).Error)
	category, err = ReconcileWalletTopUp(context.Background(), order.TradeNo)
	require.NoError(t, err)
	assert.Equal(t, "review_required", category)
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.Equal(t, "credit_review", order.Status)
	assert.Empty(t, order.ProviderSessionID)
}

func TestWalletUnmatchedAuthenticatedReceiptPersistsOperationsHold(t *testing.T) {
	db := walletReceiptRecoveryDB(t)
	receipt := model.WalletPaymentReceipt{Provider: "stripe", ProviderAccountRef: "acct_original", ProviderEnvironment: "test", TransactionID: "pi_unknown", AmountMinor: 9900, Currency: "USD", CurrencyExponent: 2, EvidenceSource: "verified_callback", EvidenceRef: "stripe-event:unknown"}
	require.NoError(t, SaveUnmatchedWalletPaymentReceipt(context.Background(), receipt))
	require.NoError(t, SaveUnmatchedWalletPaymentReceipt(context.Background(), receipt))
	var saved []model.WalletPaymentReceipt
	require.NoError(t, db.Find(&saved).Error)
	require.Len(t, saved, 1)
	assert.Nil(t, saved[0].TopUpID)
	assert.Equal(t, "UNMATCHED_PAYMENT", saved[0].ExceptionReason)
	assert.Equal(t, "payment-operations", saved[0].ReviewOwner)
	assert.Equal(t, "review", saved[0].ProcessingStatus)
	assert.Zero(t, saved[0].NextRetryAt)
}
