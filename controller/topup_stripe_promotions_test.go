package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81"
)

func TestStripeTopUpPromotionCodesIgnoreHistoricalMembershipFlag(t *testing.T) {
	db, _ := membershipControllerFixture(t, model.PaymentProviderStripe, "USD")
	require.NoError(t, db.Model(&model.MembershipCatalogState{}).Where("id = ?", 1).Update("topup_discounts_disabled", true).Error)
	enableWalletLegacyPayments(t)
	t.Setenv("WALLET_PAYMENT_SECRET", strings.Repeat("s", 32))
	oldBackend, oldKey := stripe.GetBackend(stripe.APIBackend), stripe.Key
	oldPromotionCodes, oldMinimum := setting.StripePromotionCodesEnabled, setting.StripeMinTopUp
	oldQuota, oldDisplayType := common.QuotaPerUnit, operation_setting.GetQuotaDisplayType()
	oldRatio, oldAddress := common.TopupGroupRatio2JSONString(), system_setting.ServerAddress
	t.Cleanup(func() {
		stripe.SetBackend(stripe.APIBackend, oldBackend)
		stripe.Key = oldKey
		setting.StripePromotionCodesEnabled, setting.StripeMinTopUp = oldPromotionCodes, oldMinimum
		common.QuotaPerUnit = oldQuota
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(oldRatio))
		system_setting.ServerAddress = oldAddress
	})
	setting.StripeMinTopUp = 1
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1}`))
	system_setting.ServerAddress = "https://dashboard.example.com"
	checkoutPromotionCodes := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/checkout/sessions":
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			checkoutPromotionCodes <- r.PostForm.Get("allow_promotion_codes")
			_, _ = w.Write([]byte(`{"id":"cs_topup","url":"https://checkout.stripe.com/c/pay/test"}`))
		case "GET /v1/prices/price_legacy":
			_, _ = w.Write([]byte(`{"id":"price_legacy","active":true,"unit_amount":100,"currency":"usd","billing_scheme":"per_unit","livemode":false}`))
		case "GET /v1/account":
			_, _ = w.Write([]byte(`{"id":"acct_topup"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{URL: stripe.String(server.URL), HTTPClient: server.Client(), MaxNetworkRetries: stripe.Int64(0)}))
	for _, tc := range []struct {
		name, pricingMode string
		enabled           bool
	}{
		{"enabled", "channel_adjusted", true},
		{"disabled", "fixed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setting.StripePromotionCodesEnabled = tc.enabled
			link, err := genStripeLink("topup-promotion-test", "", "", 10, "https://dashboard.example.com/wallet", "https://dashboard.example.com/wallet")
			require.NoError(t, err)
			assert.Equal(t, "https://checkout.stripe.com/c/pay/test", link)
			select {
			case enabled := <-checkoutPromotionCodes:
				assert.Equal(t, strconv.FormatBool(tc.enabled), enabled)
			default:
				t.Fatal("Stripe checkout request was not recorded")
			}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/wallet/quote", nil)
			ctx.Set("id", 7)
			quote, snapshot, err := walletQuote(ctx, dto.WalletQuoteRequest{PaymentProvider: model.PaymentProviderStripe, PaymentMethod: model.PaymentMethodStripe, Amount: "10"})
			require.NoError(t, err)
			assert.Equal(t, tc.enabled, snapshot.AllowPromotionCodes)
			assert.Equal(t, tc.pricingMode, quote.PricingMode)
		})
	}
}
