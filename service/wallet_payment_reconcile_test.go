package service

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalletStripeClosureRequiresAuthenticatedRecheckAndPaymentWins(t *testing.T) {
	for _, tc := range []struct {
		name, reason, remoteStatus, paymentStatus, category, localStatus string
	}{
		{"deadline closes original session", "", "expired", "unpaid", "closed", "expired"},
		{"user cancellation closes original session", "user_cancel", "expired", "unpaid", "closed", "cancelled"},
		{"successful expire response alone is insufficient", "user_cancel", "open", "unpaid", "retry_failed", "pending"},
		{"payment wins cancellation race", "user_cancel", "complete", "paid", "credited", "success"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := walletReceiptRecoveryDB(t)
			t.Setenv("WALLET_PAYMENT_SECRET", strings.Repeat("s", 32))
			t.Setenv("TOPUP_REBATE_ACTIVE_FROM_ID", "1")
			user := model.User{Id: 41, Username: "stripe-user", AffCode: "stripe-user", Group: "default", Quota: 10}
			require.NoError(t, db.Create(&user).Error)
			snapshot := WalletGatewaySnapshot{Provider: "stripe", APIKey: "sk_original", Environment: "test", PriceID: "price_original", Quantity: 1}
			order := &model.TopUp{TradeNo: "WT-stripe-close", PaidMinor: 100, Currency: "USD", CurrencyExponent: 2,
				CreditedQuota: 500, PricingMode: "fixed", PaymentProvider: "stripe", PaymentMethod: "stripe",
				ProviderAccountRef: "original-merchant", ProviderEnvironment: "test"}
			sealed, err := SealWalletPayment(snapshot, WalletPaymentScope(&model.TopUp{TradeNo: order.TradeNo, UserId: user.Id, PaymentProvider: order.PaymentProvider, ProviderAccountRef: order.ProviderAccountRef}))
			require.NoError(t, err)
			order.CheckoutPayload = sealed
			scope := model.WalletTopUpScope{UserID: user.Id}
			order, flow, _, err := model.CreateWalletTopUp(scope, strings.Repeat("a", 64), strings.Repeat("b", 64), 0, order)
			require.NoError(t, err)
			expiresAt := common.GetTimestamp() + 3600
			if tc.reason == "" {
				expiresAt = common.GetTimestamp() - 1
			}
			order, err = model.SaveWalletTopUpSession(order.TradeNo, order.CreateClaimToken, "cs_original", "cs_original", sealed, expiresAt)
			require.NoError(t, err)
			if tc.reason != "" {
				order, err = model.RequestWalletTopUpCancel(scope, order.TradeNo, order.Version, tc.reason)
				require.NoError(t, err)
			}
			queries, closes := 0, 0
			walletStripeTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "Bearer sk_original", r.Header.Get("Authorization"))
				if r.Method == http.MethodPost {
					closes++
					assert.Equal(t, "/v1/checkout/sessions/cs_original/expire", r.URL.Path)
					assert.Equal(t, "wallet-close-"+order.TradeNo, r.Header.Get("Idempotency-Key"))
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"id":"cs_original","status":"expired"}`))
					return
				}
				queries++
				assert.Equal(t, "/v1/checkout/sessions/cs_original", r.URL.Path)
				status, payment := "open", "unpaid"
				if closes > 0 {
					status, payment = tc.remoteStatus, tc.paymentStatus
				}
				response := map[string]any{"id": "cs_original", "client_reference_id": order.TradeNo, "mode": "payment", "currency": "usd", "livemode": false,
					"status": status, "payment_status": payment, "amount_total": 100, "amount_subtotal": 100,
					"line_items":     map[string]any{"has_more": false, "data": []any{map[string]any{"price": map[string]any{"id": "price_original"}, "quantity": 1, "amount_subtotal": 100}}},
					"payment_intent": map[string]any{"id": "pi_original", "latest_charge": map[string]any{"id": "ch_original", "paid": true, "captured": true, "balance_transaction": map[string]any{"id": "txn_original", "created": common.GetTimestamp()}}}}
				encoded, err := common.Marshal(response)
				require.NoError(t, err)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(encoded)
			})
			category, err := ReconcileWalletTopUp(context.Background(), order.TradeNo)
			require.NoError(t, err)
			assert.Equal(t, tc.category, category)
			assert.Equal(t, 1, closes)
			assert.Equal(t, 2, queries)
			stored, err := model.FindTopUpByTradeNo(order.TradeNo)
			require.NoError(t, err)
			assert.Equal(t, tc.localStatus, stored.Status)
			if tc.category == "closed" {
				assert.Equal(t, "stripe-session:cs_original", stored.CloseEvidenceRef)
				assert.Positive(t, stored.ClosedAt)
			}
			if tc.category == "retry_failed" {
				assert.Zero(t, stored.ClosedAt)
				_, err = model.AcknowledgeWalletTopUp(scope, flow.FlowID, flow.Revision, order.TradeNo)
				assert.ErrorIs(t, err, model.ErrTopUpStatusInvalid)
			}
			require.NoError(t, db.First(&user, user.Id).Error)
			expected := int64(10)
			if tc.category == "credited" {
				expected = 510
				category, err = ReconcileWalletTopUp(context.Background(), order.TradeNo)
				require.NoError(t, err)
				assert.Equal(t, "credited", category)
				assert.Equal(t, 2, queries)
			}
			assert.Equal(t, expected, user.Quota)
		})
	}
}

func TestWalletReconcileReportsPersistedReceiptReview(t *testing.T) {
	for _, tc := range []struct {
		name, provider, createState, remoteStatus string
		unavailable, locallyEnded                 bool
		queries                                   int
	}{
		{name: "negative closure", provider: "stripe", createState: "ready", remoteStatus: "expired", queries: 1},
		{name: "negative unpaid", provider: "stripe", createState: "ready", remoteStatus: "open", queries: 1},
		{name: "provider unavailable", provider: "stripe", createState: "ready", unavailable: true, queries: 1},
		{name: "ended uncertain creation", provider: "stripe", createState: "unknown", locallyEnded: true},
		{name: "uncertain creation retry", provider: "epay", createState: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := walletReceiptRecoveryDB(t)
			t.Setenv("WALLET_PAYMENT_SECRET", strings.Repeat("s", 32))
			user := model.User{Id: 41, Username: "receipt-review", AffCode: "receipt-review", Group: "default", Quota: 10}
			require.NoError(t, db.Create(&user).Error)
			now := common.GetTimestamp()
			order := model.TopUp{UserId: user.Id, TradeNo: "WT-receipt-review", Status: common.TopUpStatusPending, CreateTime: now - 60,
				WalletLifecycleVersion: 1, CreateState: tc.createState, PaymentProvider: tc.provider, PaymentMethod: tc.provider,
				ProviderAccountRef: "original-merchant", ProviderEnvironment: "test", ProviderSessionID: "cs_original", ProviderPaymentState: "unknown",
				PaidMinor: 100, Currency: "USD", CurrencyExponent: 2, PricingMode: "fixed", CreditedQuota: 500}
			if tc.locallyEnded {
				order.LocalEndedAt = now - 1
				order.LocalEndReason = "user_ended"
			}
			snapshot := WalletGatewaySnapshot{Provider: tc.provider, APIKey: "sk_original", Environment: "test"}
			sealed, err := SealWalletPayment(snapshot, WalletPaymentScope(&order))
			require.NoError(t, err)
			order.CheckoutPayload = sealed
			require.NoError(t, db.Create(&order).Error)
			// Model the crash window after authentic paid evidence commits but
			// before the order status or user balance is updated.
			_, err = model.SaveWalletPaymentReceipt(&model.WalletPaymentReceipt{
				Provider: order.PaymentProvider, ProviderAccountRef: order.ProviderAccountRef, ProviderEnvironment: order.ProviderEnvironment,
				TransactionID: "tx-original", TopUpID: &order.Id, AmountMinor: 100, Currency: "USD", CurrencyExponent: 2,
				PaidAt: now - 10, EvidenceSource: "authenticated_query", EvidenceRef: "original-paid-proof", VerificationStatus: "verified",
			})
			require.NoError(t, err)
			queries := 0
			walletStripeTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
				queries++
				assert.Equal(t, http.MethodGet, r.Method)
				if tc.unavailable {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte(`{"error":{"message":"unavailable"}}`))
					return
				}
				encoded, err := common.Marshal(map[string]any{"id": "cs_original", "client_reference_id": order.TradeNo, "mode": "payment", "currency": "usd", "livemode": false,
					"status": tc.remoteStatus, "payment_status": "unpaid"})
				require.NoError(t, err)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(encoded)
			})
			category, err := ReconcileWalletTopUp(context.Background(), order.TradeNo)
			require.NoError(t, err)
			assert.Equal(t, "review_required", category)
			assert.Equal(t, tc.queries, queries)
			require.NoError(t, db.First(&order, order.Id).Error)
			assert.Equal(t, "credit_review", order.Status)
			assert.Equal(t, "unknown", order.ProviderPaymentState)
			assert.Zero(t, order.ClosedAt)
			assert.Empty(t, order.CloseEvidenceRef)
			assert.Empty(t, order.CheckClaimToken)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.EqualValues(t, 10, user.Quota)
		})
	}
}
