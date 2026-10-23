package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81"
)

func walletStripeTestBackend(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	original := stripe.GetBackend(stripe.APIBackend)
	server := httptest.NewServer(handler)
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{URL: stripe.String(server.URL), HTTPClient: server.Client(), MaxNetworkRetries: stripe.Int64(0)}))
	t.Cleanup(func() { stripe.SetBackend(stripe.APIBackend, original); server.Close() })
}
func TestWalletStripeGatewayKeepsOriginalCheckoutAndIdempotency(t *testing.T) {
	globalKey := stripe.Key
	requests := 0
	walletStripeTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/checkout/sessions", r.URL.Path)
		assert.Equal(t, "Bearer sk_test_original_account", r.Header.Get("Authorization"))
		assert.Equal(t, "wallet-topup-WT-original-order", r.Header.Get("Idempotency-Key"))
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "WT-original-order", r.Form.Get("client_reference_id"))
		assert.Equal(t, "payment", r.Form.Get("mode"))
		assert.Equal(t, "price_original", r.Form.Get("line_items[0][price]"))
		assert.Equal(t, "3", r.Form.Get("line_items[0][quantity]"))
		assert.Equal(t, "cus_original", r.Form.Get("customer"))
		assert.Equal(t, "true", r.Form.Get("allow_promotion_codes"))
		assert.Equal(t, "https://wallet.example.test/wallet", r.Form.Get("success_url"))
		assert.Equal(t, r.Form.Get("success_url"), r.Form.Get("cancel_url"))
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"id":"cs_original","url":"https://checkout.stripe.com/c/pay/original","client_reference_id":"WT-original-order","expires_at":1800000000}`))
		assert.NoError(t, err)
	})
	order := &model.TopUp{TradeNo: "WT-original-order"}
	snapshot := WalletGatewaySnapshot{Provider: "stripe", APIKey: "sk_test_original_account", PriceID: "price_original", Quantity: 3, CustomerID: "cus_original", AllowPromotionCodes: true, ReturnURL: "https://wallet.example.test/wallet"}
	first, err := CreateWalletGatewaySession(context.Background(), order, snapshot)
	require.NoError(t, err)
	assert.Equal(t, "cs_original", first.SessionID)
	assert.Equal(t, "cs_original", first.OrderID)
	assert.EqualValues(t, 1800000000, first.ExpiresAt)
	require.NotNil(t, first.Checkout)
	assert.Equal(t, "https://checkout.stripe.com/c/pay/original", first.Checkout.URL)
	replayed, err := CreateWalletGatewaySession(context.Background(), order, snapshot)
	require.NoError(t, err)
	assert.Equal(t, first, replayed)
	assert.Equal(t, 2, requests)
	assert.Equal(t, globalKey, stripe.Key, "per-order credentials must not mutate the SDK's global key")
}
func TestWalletStripeQueryRejectsDifferentOriginalPaymentIdentity(t *testing.T) {
	for _, test := range []struct {
		name, key string
		value     any
	}{
		{"original", "", nil},
		{"different session", "id", "cs_other"},
		{"different order", "client_reference_id", "WT-other"},
		{"different currency", "currency", "eur"},
		{"different environment", "livemode", true},
		{"subscription mirror", "mode", "subscription"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := map[string]any{"id": "cs_original", "client_reference_id": "WT-original", "currency": "usd", "livemode": false, "mode": "payment", "status": "open", "payment_status": "unpaid"}
			if test.key != "" {
				data[test.key] = test.value
			}
			walletStripeTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/checkout/sessions/cs_original", r.URL.Path)
				assert.Equal(t, "Bearer sk_original", r.Header.Get("Authorization"))
				encoded, err := common.Marshal(data)
				require.NoError(t, err)
				w.Header().Set("Content-Type", "application/json")
				_, err = w.Write(encoded)
				assert.NoError(t, err)
			})
			order := &model.TopUp{TradeNo: "WT-original", ProviderSessionID: "cs_original", Currency: "USD", ProviderEnvironment: "test"}
			remote, err := RetrieveWalletStripeSession(context.Background(), order, WalletGatewaySnapshot{APIKey: "sk_original"})
			if test.key == "" {
				require.NoError(t, err)
				require.NotNil(t, remote)
			} else {
				require.ErrorIs(t, err, model.ErrWalletPaymentEvidence)
				assert.Nil(t, remote)
			}
		})
	}
}
