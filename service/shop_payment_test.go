package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShopEpayQueryRequiresMatchingMerchantAndPaymentEvidence(t *testing.T) {
	valid := `{"code":1,"pid":"merchant","status":1,"trade_no":"provider-trade","out_trade_no":"KS-order","money":"12.30","endtime":"2026-01-02 12:00:00"}`
	paidAt := time.Date(2026, 1, 2, 4, 0, 0, 0, time.UTC).Unix()
	attempt := &model.ShopPaymentAttempt{ShopRecord: model.ShopRecord{CreatedAt: paidAt - 60}, TradeNo: "KS-order"}
	for _, tc := range []struct {
		name, body                      string
		paid, verified, review, invalid bool
	}{
		{"paid", valid, true, true, false, false},
		{"unpaid", strings.Replace(valid, `"status":1`, `"status":0`, 1), false, false, false, false},
		{"wrong merchant", strings.Replace(valid, `"pid":"merchant"`, `"pid":"other"`, 1), false, false, false, true},
		{"wrong order", strings.Replace(valid, `"out_trade_no":"KS-order"`, `"out_trade_no":"KS-other"`, 1), false, false, false, true},
		{"duplicate status", strings.Replace(valid, `"status":1`, `"status":0,"status":1`, 1), false, false, false, true},
		{"negative amount", strings.Replace(valid, `"12.30"`, `"-12.30"`, 1), false, false, false, true},
		{"overflow amount", strings.Replace(valid, `"12.30"`, `"9223372036854775808.00"`, 1), false, false, false, true},
		{"missing payment time", strings.Replace(valid, `"2026-01-02 12:00:00"`, `""`, 1), true, false, false, false},
		{"refund marker", strings.TrimSuffix(valid, "}") + `,"refund_money":"12.30"}`, true, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api.php", r.URL.Path)
				assert.Equal(t, "merchant", r.URL.Query().Get("pid"))
				assert.Equal(t, "merchant-key", r.URL.Query().Get("key"))
				assert.Equal(t, "KS-order", r.URL.Query().Get("out_trade_no"))
				assert.Empty(t, r.Header.Get("Cookie"))
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			result, err := queryShopEpay(context.Background(), shopEpaySnapshot{APIURL: server.URL, MerchantID: "merchant", Key: "merchant-key", AccountRef: "epay:merchant", Environment: "test"}, attempt, server.Client())
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.paid, result.Paid)
			assert.Equal(t, tc.verified, result.Receipt.PricingVerified)
			assert.Equal(t, tc.review, result.NeedsReview)
			if tc.paid {
				assert.Equal(t, int64(1230), result.Receipt.AmountMinor)
			}
			if tc.verified {
				assert.Equal(t, paidAt, result.Receipt.ProviderPaidAt)
			}
		})
	}
}

func TestShopEpayQueryNeverFollowsRedirectsOrAcceptsOversizeBodies(t *testing.T) {
	for _, redirect := range []bool{true, false} {
		redirected := false
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/redirected" {
				redirected = true
				return
			}
			if redirect {
				http.Redirect(w, r, "/redirected", http.StatusFound)
				return
			}
			_, _ = w.Write([]byte(strings.Repeat("x", (64<<10)+1)))
		}))
		_, err := queryShopEpay(context.Background(), shopEpaySnapshot{APIURL: server.URL}, &model.ShopPaymentAttempt{}, server.Client())
		assert.ErrorIs(t, err, ErrShopChannelUnavailable)
		server.Close()
		assert.False(t, redirected, "merchant credentials must not follow redirects")
	}
}
