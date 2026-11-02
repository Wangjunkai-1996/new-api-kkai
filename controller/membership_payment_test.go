package controller

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/webhook"
	"gorm.io/gorm"
)

func membershipControllerFixture(t *testing.T, provider, currency string) (*gorm.DB, *model.MembershipOrder) {
	t.Helper()
	previousDB := model.DB
	mainDB, logDB := common.MainDatabaseType(), common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	require.NoError(t, db.AutoMigrate(model.MembershipModels()...))
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetDatabaseTypes(mainDB, logDB)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Create(&model.User{Id: 7, Username: "member", Group: "default", Status: 1, Quota: 123}).Error)
	catalog, err := model.PublishMembershipCatalog(model.MembershipCatalog{
		Tiers: []model.MembershipTier{{ID: "gold", Name: "Gold", Rank: 1, Quota: 1000, PriceMinor: 9900, Currency: currency, Enabled: true}},
	}, 0, 7)
	require.NoError(t, err)
	terms, err := common.Marshal(model.MembershipOrderTerms{Tier: catalog.Tiers[0], ReleaseID: catalog.ReleaseID, DurationSeconds: model.MembershipDurationSeconds, Action: "purchase"})
	require.NoError(t, err)
	order := &model.MembershipOrder{TradeNo: "MEMBER-test", UserID: 7, TierID: "gold", ReleaseID: catalog.ReleaseID, AmountMinor: 9900, Currency: currency, Provider: provider, PaymentMethod: provider, Terms: string(terms), Status: model.MembershipOrderPending, ExpiresAt: time.Now().Add(time.Hour).Unix()}
	require.NoError(t, db.Create(order).Error)
	return db, order
}

func TestMembershipEpayVerifiedMoneyAndDuplicateCallbacks(t *testing.T) {
	for _, tc := range []struct {
		name, money, status, checkStatus string
		actualMinor                      int64
	}{
		{"exact", "99.00", model.MembershipOrderSuccess, model.MembershipPaymentComplete, 9900},
		{"underpaid", "9.90", model.MembershipOrderException, model.MembershipPaymentReview, 990},
		{"fractional_minor", "99.001", model.MembershipOrderPending, model.MembershipPaymentPending, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var order *model.MembershipOrder
			var queryBody []byte
			var queries atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				queries.Add(1)
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api.php", r.URL.Path)
				assert.Equal(t, "order", r.URL.Query().Get("act"))
				assert.Equal(t, "merchant", r.URL.Query().Get("pid"))
				assert.Equal(t, "test-secret", r.URL.Query().Get("key"))
				assert.Equal(t, order.TradeNo, r.URL.Query().Get("out_trade_no"))
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write(queryBody)
				assert.NoError(t, err)
			}))
			defer server.Close()
			db, order := membershipEpayFixture(t, server.URL)
			paidAt := time.Now().Unix()
			queryBody, err := common.Marshal(map[string]any{
				"code": 1, "status": 1, "pid": "merchant", "type": "alipay",
				"out_trade_no": order.TradeNo, "trade_no": "provider-one", "money": tc.money, "currency": "CNY",
				"endtime": time.Unix(paidAt, 0).In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02 15:04:05"),
			})
			require.NoError(t, err)
			// Signed callback claims cannot replace authenticated merchant-query evidence.
			params, err := epay.GenerateParamsWithError(map[string]string{"pid": "merchant", "type": "alipay", "out_trade_no": order.TradeNo, "trade_no": "callback-hint", "trade_status": epay.StatusTradeSuccess, "money": "99.00", "name": "Gold"}, "test-secret")
			require.NoError(t, err)
			values := url.Values{}
			for key, value := range params {
				values.Set(key, value)
			}
			router := gin.New()
			router.POST("/notify", MembershipEpayNotify)
			for i := 0; i < 2; i++ {
				recorder := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(values.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				router.ServeHTTP(recorder, req)
				assert.Equal(t, http.StatusOK, recorder.Code)
				assert.Equal(t, "success", recorder.Body.String())
			}
			require.NoError(t, db.First(order, order.ID).Error)
			assert.Equal(t, model.MembershipOrderPending, order.Status)
			assert.Zero(t, order.PaidAt)
			assert.Zero(t, order.AccountID)
			assert.Zero(t, queries.Load())
			for _, table := range []any{&model.MembershipPaymentEvidence{}, &model.MembershipPaymentReceipt{}, &model.MembershipAccount{}} {
				var count int64
				require.NoError(t, db.Model(table).Count(&count).Error)
				assert.Zero(t, count, "callback must only wake a merchant query")
			}
			check, err := model.GetMembershipPaymentCheck(context.Background(), order.ID)
			require.NoError(t, err)
			assert.Equal(t, model.MembershipPaymentPending, check.Status)
			assert.Equal(t, "callback", check.LastSource)
			assert.Positive(t, check.CallbackReceivedAt)
			assert.LessOrEqual(t, check.NextCheckAt, time.Now().Unix())

			oldClient := http.DefaultClient
			http.DefaultClient = server.Client()
			t.Cleanup(func() { http.DefaultClient = oldClient })
			result, err := service.ReconcileMembershipPayment(context.Background(), order.ID)
			require.NoError(t, err)
			assert.Equal(t, tc.checkStatus, result)
			assert.EqualValues(t, 1, queries.Load())
			require.NoError(t, db.First(order, order.ID).Error)
			assert.Equal(t, tc.status, order.Status)
			settledPaidAt := order.PaidAt
			var granted model.MembershipAccount
			if tc.status == model.MembershipOrderSuccess {
				require.NoError(t, db.First(&granted, order.AccountID).Error)
				assert.Equal(t, order.UserID, granted.UserID)
				assert.Equal(t, model.MembershipActive, granted.Status)
				assert.EqualValues(t, 1000, granted.AmountTotal)
				assert.EqualValues(t, 1000, granted.Available())
				assert.Equal(t, model.MembershipDurationSeconds, granted.ExpiresAt-granted.StartedAt)
			} else if tc.status == model.MembershipOrderException {
				assert.Equal(t, "payment amount or currency mismatch", order.ExceptionReason)
			}

			for i := 0; i < 2; i++ {
				recorder := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(values.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				router.ServeHTTP(recorder, req)
				assert.Equal(t, http.StatusOK, recorder.Code)
				assert.Equal(t, "success", recorder.Body.String())
			}
			require.NoError(t, db.First(order, order.ID).Error)
			assert.Equal(t, tc.status, order.Status)
			assert.Equal(t, settledPaidAt, order.PaidAt)
			assert.Equal(t, tc.actualMinor, order.ActualAmountMinor)
			assert.EqualValues(t, 1, queries.Load())
			check, err = model.GetMembershipPaymentCheck(context.Background(), order.ID)
			require.NoError(t, err)
			assert.Equal(t, tc.checkStatus, check.Status)

			var evidenceCount, receiptCount, accountCount int64
			require.NoError(t, db.Model(&model.MembershipPaymentEvidence{}).Count(&evidenceCount).Error)
			require.NoError(t, db.Model(&model.MembershipPaymentReceipt{}).Count(&receiptCount).Error)
			require.NoError(t, db.Model(&model.MembershipAccount{}).Count(&accountCount).Error)
			if tc.actualMinor == 0 {
				assert.Zero(t, evidenceCount)
				assert.Zero(t, receiptCount)
				assert.Zero(t, order.PaidAt)
				assert.Empty(t, order.ReceiptID)
				assert.Equal(t, "PAYMENT_QUERY_RESPONSE_INVALID", check.LastError)
				assert.Greater(t, check.NextCheckAt, time.Now().Unix(), "malformed merchant replies remain retryable")
			} else {
				assert.EqualValues(t, 1, evidenceCount)
				assert.EqualValues(t, 1, receiptCount)
				assert.Positive(t, order.PaidAt)
				assert.Equal(t, "provider-one", order.ReceiptID)
				var evidence model.MembershipPaymentEvidence
				require.NoError(t, db.First(&evidence, "order_id = ?", order.ID).Error)
				assert.Equal(t, model.MembershipPaymentMerchantQuery, evidence.Source)
				assert.Equal(t, "provider-one", evidence.ReceiptID)
				assert.Equal(t, tc.actualMinor, evidence.AmountMinor)
				assert.Equal(t, paidAt, evidence.ProviderPaidAt)
			}
			if tc.status == model.MembershipOrderSuccess {
				assert.EqualValues(t, 1, accountCount)
				var account model.MembershipAccount
				require.NoError(t, db.First(&account, order.AccountID).Error)
				assert.Equal(t, granted.ID, account.ID)
				assert.Equal(t, granted.AmountTotal, account.AmountTotal)
				assert.Equal(t, granted.Available(), account.Available())
				assert.Equal(t, granted.ExpiresAt, account.ExpiresAt)
				assert.Equal(t, granted.Revision, account.Revision)
			} else {
				assert.Zero(t, accountCount)
				assert.Zero(t, order.AccountID)
			}
			var user model.User
			require.NoError(t, db.First(&user, 7).Error)
			assert.Equal(t, "default", user.Group)
			assert.EqualValues(t, 123, user.Quota)
		})
	}
}

func TestMembershipStripeRequiresOneTimeAndExactCurrency(t *testing.T) {
	for _, tc := range []struct{ name, mode, currency, status string }{{"valid", "payment", "usd", model.MembershipOrderSuccess}, {"recurring", "subscription", "usd", model.MembershipOrderException}, {"wrong_currency", "payment", "eur", model.MembershipOrderException}} {
		t.Run(tc.name, func(t *testing.T) {
			db, order := membershipControllerFixture(t, model.PaymentProviderStripe, "USD")
			event := stripe.Event{Type: stripe.EventTypeCheckoutSessionCompleted, Data: &stripe.EventData{Object: map[string]any{"client_reference_id": order.TradeNo, "payment_status": "paid", "mode": tc.mode, "currency": tc.currency, "amount_total": "9900", "payment_intent": "pi_test"}}}
			require.NoError(t, handleMembershipStripeEvent(event))
			require.NoError(t, handleMembershipStripeEvent(event))
			require.NoError(t, db.First(order, order.ID).Error)
			assert.Equal(t, tc.status, order.Status)
		})
	}
}

func TestMembershipStripeWebhookVerifiesSignatureAndGrantsOnce(t *testing.T) {
	if !model.MembershipEnabled() {
		t.Skip("requires KKAI_MEMBERSHIP_ENABLED=true")
	}
	db, order := membershipControllerFixture(t, model.PaymentProviderStripe, "USD")
	oldKey, oldSecret, oldPrice := setting.StripeApiSecret, setting.StripeWebhookSecret, setting.StripePriceId
	t.Cleanup(func() {
		setting.StripeApiSecret, setting.StripeWebhookSecret, setting.StripePriceId = oldKey, oldSecret, oldPrice
	})
	setting.StripeApiSecret, setting.StripeWebhookSecret, setting.StripePriceId = "sk_test_membership", "whsec_membership_local", ""
	// A membership receipt remains routable without enabling ordinary top-ups.
	require.False(t, isStripeTopUpEnabled())
	payload, err := common.Marshal(map[string]any{
		"id": "evt_membership_local", "object": "event", "api_version": stripe.APIVersion,
		"type": stripe.EventTypeCheckoutSessionCompleted,
		"data": map[string]any{"object": map[string]any{
			"id": "cs_membership_local", "object": "checkout.session",
			"client_reference_id": order.TradeNo, "status": "complete", "payment_status": "paid",
			"mode": "payment", "currency": "usd", "amount_total": 9900, "payment_intent": "pi_membership_local",
		}},
	})
	require.NoError(t, err)
	router := gin.New()
	router.POST("/api/stripe/webhook", StripeWebhook)
	for _, tc := range []struct {
		name, secret, orderStatus string
		httpStatus                int
		grants                    int64
	}{
		{"invalid_signature", "whsec_wrong", model.MembershipOrderPending, http.StatusBadRequest, 0},
		{"verified_payment", setting.StripeWebhookSecret, model.MembershipOrderSuccess, http.StatusOK, 1},
		{"duplicate_payment", setting.StripeWebhookSecret, model.MembershipOrderSuccess, http.StatusOK, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: payload, Secret: tc.secret})
			request := httptest.NewRequest(http.MethodPost, "/api/stripe/webhook", bytes.NewReader(payload))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Stripe-Signature", signed.Header)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, tc.httpStatus, response.Code, "%s", response.Body.String())
			require.NoError(t, db.First(order, order.ID).Error)
			assert.Equal(t, tc.orderStatus, order.Status)
			for _, table := range []any{&model.MembershipAccount{}, &model.MembershipPaymentReceipt{}} {
				var count int64
				require.NoError(t, db.Model(table).Count(&count).Error)
				assert.Equal(t, tc.grants, count)
			}
			if tc.grants == 0 {
				assert.Zero(t, order.PaidAt)
				assert.Zero(t, order.AccountID)
			} else {
				var account model.MembershipAccount
				require.NoError(t, db.First(&account, order.AccountID).Error)
				assert.Equal(t, order.UserID, account.UserID)
				assert.EqualValues(t, 1000, account.AmountTotal)
				assert.EqualValues(t, 1000, account.Available())
				assert.Equal(t, model.MembershipActive, account.Status)
				assert.Equal(t, "pi_membership_local", order.ReceiptID)
			}
			var user model.User
			require.NoError(t, db.First(&user, order.UserID).Error)
			assert.EqualValues(t, 123, user.Quota)
		})
	}
}

func TestMembershipOrderCannotBeReadByAnotherUser(t *testing.T) {
	_, order := membershipControllerFixture(t, model.PaymentProviderStripe, "USD")
	router := gin.New()
	router.GET("/orders/:trade_no", func(c *gin.Context) { c.Set("id", 8); c.Next() }, GetMembershipOrder)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/orders/"+order.TradeNo, nil))
	assert.Equal(t, http.StatusNotFound, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "9900")
}

func TestMembershipStripeCheckoutUsesFrozenOneTimePrice(t *testing.T) {
	_, order := membershipControllerFixture(t, model.PaymentProviderStripe, "USD")
	oldBackend := stripe.GetBackend(stripe.APIBackend)
	oldKey, oldAddress := setting.StripeApiSecret, system_setting.ServerAddress
	t.Cleanup(func() {
		stripe.SetBackend(stripe.APIBackend, oldBackend)
		setting.StripeApiSecret = oldKey
		system_setting.ServerAddress = oldAddress
	})
	setting.StripeApiSecret = "sk_test_membership"
	system_setting.ServerAddress = "https://dashboard.example.com"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "payment", r.Form.Get("mode"))
		assert.Equal(t, "9900", r.Form.Get("line_items[0][price_data][unit_amount]"))
		assert.Equal(t, "usd", r.Form.Get("line_items[0][price_data][currency]"))
		assert.Equal(t, "false", r.Form.Get("allow_promotion_codes"))
		assert.Equal(t, order.TradeNo, r.Header.Get("Idempotency-Key"))
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"id":"cs_one","url":"https://checkout.stripe.com/c/pay/test"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{URL: stripe.String(server.URL), HTTPClient: server.Client()}))
	checkout, err := obtainMembershipCheckout(context.Background(), order)
	require.NoError(t, err)
	assert.Equal(t, "https://checkout.stripe.com/c/pay/test", checkout.PaymentURL)
	reloaded, err := model.GetMembershipOrder(order.TradeNo, order.UserID)
	require.NoError(t, err)
	reused, err := obtainMembershipCheckout(context.Background(), reloaded)
	require.NoError(t, err)
	assert.Equal(t, checkout, reused)
	assert.Equal(t, 1, requests, "retry must reuse the durable checkout without contacting the provider")
	assert.Equal(t, "ready", reloaded.CheckoutState)
}

func TestMembershipUncertainCheckoutCannotCreateAnotherProviderSession(t *testing.T) {
	_, order := membershipControllerFixture(t, model.PaymentProviderStripe, "USD")
	oldBackend := stripe.GetBackend(stripe.APIBackend)
	t.Cleanup(func() { stripe.SetBackend(stripe.APIBackend, oldBackend) })
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"id":"cs_unknown"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{URL: stripe.String(server.URL), HTTPClient: server.Client()}))
	_, err := obtainMembershipCheckout(context.Background(), order)
	require.Error(t, err)
	_, err = obtainMembershipCheckout(context.Background(), order)
	require.ErrorIs(t, err, model.ErrMembershipConflict)
	assert.Equal(t, 1, requests)
	stored, err := model.GetMembershipOrder(order.TradeNo, order.UserID)
	require.NoError(t, err)
	assert.Equal(t, model.MembershipOrderPending, stored.Status)
	assert.Equal(t, "creating", stored.CheckoutState)
}
