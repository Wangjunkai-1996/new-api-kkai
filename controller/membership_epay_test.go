package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func membershipEpayFixture(t *testing.T) (*gorm.DB, *model.MembershipOrder) {
	t.Helper()
	db, order := membershipControllerFixture(t, model.PaymentProviderEpay, "CNY")
	require.NoError(t, db.Model(order).Update("payment_method", "alipay").Error)
	oldAddress, oldID, oldKey := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey
	oldCallback, oldServer := operation_setting.CustomCallbackAddress, system_setting.ServerAddress
	oldMethods, oldPrice := operation_setting.PayMethods, operation_setting.Price
	oldDiscount, oldGroupRatio := operation_setting.GetPaymentSetting().AmountDiscount, common.TopupGroupRatio2JSONString()
	oldTheme, oldRedis := common.GetTheme(), common.RedisEnabled
	t.Cleanup(func() {
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = oldAddress, oldID, oldKey
		operation_setting.CustomCallbackAddress, system_setting.ServerAddress = oldCallback, oldServer
		operation_setting.PayMethods, operation_setting.Price = oldMethods, oldPrice
		operation_setting.GetPaymentSetting().AmountDiscount = oldDiscount
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(oldGroupRatio))
		common.SetTheme(oldTheme)
		common.RedisEnabled = oldRedis
	})
	confirmPaymentComplianceForTest(t)
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://pay.example.com/gateway/", "merchant", "test-secret"
	operation_setting.CustomCallbackAddress = "https://callback.example.com"
	system_setting.ServerAddress = "https://dashboard.example.com/"
	operation_setting.PayMethods = []map[string]string{{"type": "alipay", "name": "支付宝"}}
	operation_setting.Price = 7.3
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{99: 0.7, 9995: 0.7}
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":0.4}`))
	common.SetTheme("default")
	common.RedisEnabled = false
	return db, order
}

func TestMembershipEpayCheckoutKeepsFrozenPriceAndSignedForm(t *testing.T) {
	if !model.MembershipEnabled() {
		t.Skip("requires KKAI_MEMBERSHIP_ENABLED=true")
	}
	db, fixtureOrder := membershipEpayFixture(t)
	require.NoError(t, db.Delete(fixtureOrder).Error)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}))
	catalog, err := model.PublishMembershipCatalog(model.MembershipCatalog{SalesEnabled: true, Tiers: []model.MembershipTier{{ID: "gold", Name: "黄金 & Gold + 50% = x", Rank: 1, Style: "gold", Currency: "CNY", PriceMinor: 9995, Quota: 1000, Enabled: true}}}, fixtureOrder.ReleaseID, 7)
	require.NoError(t, err)
	order, err := model.CreateMembershipOrder(model.MembershipOrderInput{UserID: 7, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: model.PaymentProviderEpay, PaymentMethod: "alipay", TradeNo: "MEMBER-fixed-price", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	require.NoError(t, err)
	// Change the live price before the first form is generated. Both payment
	// amount and benefit terms still belong to the order's original release.
	catalog.Tiers[0].PriceMinor, catalog.Tiers[0].Quota, catalog.Tiers[0].Name = 19990, 2000, "黄金新价"
	_, err = model.PublishMembershipCatalog(*catalog, catalog.ReleaseID, 7)
	require.NoError(t, err)
	assert.Equal(t, 0.4, common.GetTopupGroupRatio("default"))
	assert.Equal(t, 0.7, operation_setting.GetPaymentSetting().AmountDiscount[99])

	router := gin.New()
	router.POST("/api/membership/orders/:trade_no/pay", func(c *gin.Context) { c.Set("id", 7); c.Next() }, ResumeMembershipPayment)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/membership/orders/"+order.TradeNo+"/pay", nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result struct {
		Success bool `json:"success"`
		Data    struct {
			membershipCheckout
			Order membershipOrderView `json:"order"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	require.True(t, result.Success, response.Body.String())
	assert.EqualValues(t, 9995, result.Data.Order.AmountMinor)
	assert.EqualValues(t, 9995, result.Data.Order.PriceMinor)
	assert.EqualValues(t, 1000, result.Data.Order.Quota)
	assert.Equal(t, "黄金 & Gold + 50% = x", result.Data.Order.TierName)
	assert.Equal(t, "https://pay.example.com/gateway/submit.php", result.Data.SubmitURL)
	assert.Empty(t, result.Data.PaymentURL)
	fields := result.Data.FormFields
	assert.Equal(t, "99.95", fields["money"], "membership price must bypass wallet exchange rates and recharge discounts")
	assert.Equal(t, "Membership: 黄金 ＆ Gold + 50% = x", fields["name"])
	assert.Equal(t, "merchant", fields["pid"])
	assert.Equal(t, "alipay", fields["type"])
	assert.Equal(t, order.TradeNo, fields["out_trade_no"])
	assert.Equal(t, "https://callback.example.com/api/membership/epay/notify", fields["notify_url"])
	assert.Equal(t, "https://dashboard.example.com/membership?order="+order.TradeNo, fields["return_url"])
	assert.Equal(t, "MD5", fields["sign_type"])
	client := GetEpayClient()
	require.NotNil(t, client)
	verified, err := client.Verify(fields)
	require.NoError(t, err)
	assert.True(t, verified.VerifyStatus)
	assert.Equal(t, "99.95", verified.Money)
	assert.Equal(t, order.TradeNo, verified.ServiceTradeNo)

	// Replaying a ready checkout uses its durable signed form even if current
	// payment configuration is temporarily broken; it creates no second order.
	operation_setting.EpayKey = ""
	replayed := httptest.NewRecorder()
	router.ServeHTTP(replayed, httptest.NewRequest(http.MethodPost, "/api/membership/orders/"+order.TradeNo+"/pay", nil))
	require.Equal(t, http.StatusOK, replayed.Code, replayed.Body.String())
	assert.JSONEq(t, response.Body.String(), replayed.Body.String())
	require.NoError(t, db.First(order, order.ID).Error)
	assert.Equal(t, "ready", order.CheckoutState)
	assert.EqualValues(t, 9995, order.AmountMinor)
	var count int64
	require.NoError(t, db.Model(&model.MembershipOrder{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&model.TopUp{}).Count(&count).Error)
	assert.Zero(t, count)
	var user model.User
	require.NoError(t, db.First(&user, 7).Error)
	assert.EqualValues(t, 123, user.Quota)
}

func TestMembershipEpayLocalCheckoutFailureCanRetry(t *testing.T) {
	for _, scenario := range []string{"invalid_callback", "checkout_save_failed"} {
		t.Run(scenario, func(t *testing.T) {
			db, order := membershipEpayFixture(t)
			failure := errors.New("checkout persistence failed")
			if scenario == "invalid_callback" {
				operation_setting.CustomCallbackAddress = "https://callback.example.com/?a=1&b=2"
			} else {
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register("epay-checkout-save-failure", func(tx *gorm.DB) {
					updates, ok := tx.Statement.Dest.(map[string]any)
					if tx.Statement.Table == "kkai_membership_orders" && ok && updates["checkout_state"] == "ready" {
						tx.AddError(failure)
					}
				}))
				t.Cleanup(func() { _ = db.Callback().Update().Remove("epay-checkout-save-failure") })
			}
			_, err := obtainMembershipCheckout(context.Background(), order)
			if scenario == "invalid_callback" {
				require.ErrorIs(t, err, epay.ErrInvalidParams)
				operation_setting.CustomCallbackAddress = "https://callback.example.com"
			} else {
				require.ErrorIs(t, err, failure)
				require.NoError(t, db.Callback().Update().Remove("epay-checkout-save-failure"))
			}
			require.NoError(t, db.First(order, order.ID).Error)
			assert.Equal(t, model.MembershipOrderPending, order.Status)
			assert.Empty(t, order.CheckoutState)
			assert.Empty(t, order.CheckoutData)
			assert.Zero(t, order.CheckoutStartedAt)
			checkout, err := obtainMembershipCheckout(context.Background(), order)
			require.NoError(t, err)
			assert.Equal(t, "99.00", checkout.FormFields["money"])
			assert.Equal(t, "ready", order.CheckoutState)
			// A late cleanup for an earlier failure must not erase a ready form.
			require.NoError(t, model.ReleaseMembershipEpayCheckout(order.TradeNo, order.CheckoutStartedAt))
			require.NoError(t, db.First(order, order.ID).Error)
			assert.Equal(t, "ready", order.CheckoutState)
		})
	}
}

func TestMembershipEpayCallbackRejectsForeignMerchantAndTampering(t *testing.T) {
	for _, scenario := range []string{"foreign_merchant", "tampered_signature", "wrong_payment_method"} {
		t.Run(scenario, func(t *testing.T) {
			db, order := membershipEpayFixture(t)
			params := map[string]string{"pid": "merchant", "type": "alipay", "out_trade_no": order.TradeNo, "trade_no": "provider-one", "trade_status": epay.StatusTradeSuccess, "money": "99.00", "name": "Gold"}
			if scenario == "foreign_merchant" {
				params["pid"] = "another-merchant"
			}
			if scenario == "wrong_payment_method" {
				params["type"] = "wxpay"
			}
			params, err := epay.GenerateParamsWithError(params, "test-secret")
			require.NoError(t, err)
			if scenario == "tampered_signature" {
				params["money"] = "99.01"
			}
			values := url.Values{}
			for key, value := range params {
				values.Set(key, value)
			}
			router := gin.New()
			router.Any("/notify", MembershipEpayNotify)
			request := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(values.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if scenario == "foreign_merchant" {
				request = httptest.NewRequest(http.MethodGet, "/notify?"+values.Encode(), nil)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code)
			require.NoError(t, db.First(order, order.ID).Error)
			if scenario == "wrong_payment_method" {
				assert.Equal(t, "success", response.Body.String())
				assert.Equal(t, model.MembershipOrderException, order.Status)
				assert.Equal(t, "payment method mismatch", order.ExceptionReason)
			} else {
				assert.Equal(t, "fail", response.Body.String())
				assert.Equal(t, model.MembershipOrderPending, order.Status)
				assert.Zero(t, order.PaidAt)
				var receipts int64
				require.NoError(t, db.Model(&model.MembershipPaymentReceipt{}).Count(&receipts).Error)
				assert.Zero(t, receipts)
			}
			var grants int64
			require.NoError(t, db.Model(&model.MembershipAccount{}).Count(&grants).Error)
			assert.Zero(t, grants)
		})
	}
}
