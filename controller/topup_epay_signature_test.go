package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEpayCallbacksRejectAmbiguousSignaturesBeforeSettlement(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalAddress, originalID, originalKey := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey
	originalMethods := operation_setting.PayMethods
	originalServerAddress, originalTheme, originalDB := system_setting.ServerAddress, common.GetTheme(), model.DB
	t.Cleanup(func() {
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = originalAddress, originalID, originalKey
		operation_setting.PayMethods = originalMethods
		system_setting.ServerAddress = originalServerAddress
		common.SetTheme(originalTheme)
		model.DB = originalDB
	})
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://pay.example.com", "epay_id", "test-key"
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	system_setting.ServerAddress = "https://dashboard.example.com"
	common.SetTheme("default")
	// Rejected signatures and non-success events must never reach settlement.
	model.DB = nil
	require.True(t, isEpayWebhookEnabled())

	t.Run("valid success signature and tampering", func(t *testing.T) {
		client := GetEpayClient()
		require.NotNil(t, client)
		params := epay.GenerateParams(map[string]string{
			"pid": "epay_id", "type": "alipay", "money": "1.00", "name": "充值",
			"out_trade_no": "order", "trade_no": "gateway-order", "trade_status": epay.StatusTradeSuccess,
		}, "test-key")
		verified, err := client.Verify(params)
		require.NoError(t, err)
		require.NotNil(t, verified)
		assert.True(t, verified.VerifyStatus)
		assert.Equal(t, "order", verified.ServiceTradeNo)
		assert.Equal(t, epay.StatusTradeSuccess, verified.TradeStatus)
		params["money"] = "100.00"
		verified, err = client.Verify(params)
		require.NoError(t, err)
		require.NotNil(t, verified)
		assert.False(t, verified.VerifyStatus)
	})

	// These distinct maps shared a valid signing string in v0.0.4. A failed
	// signature comparison alone would not detect the parameter ambiguity.
	ambiguousSign := epay.MD5String("money=1.00&name=item&out_trade_no=other&out_trade_no=order&pid=epay_id&trade_status=TRADE_SUCCESS&type=alipay", "test-key")
	pendingSign := epay.MD5String("money=1.00&name=item%26other&out_trade_no=order&pid=epay_id&trade_status=WAIT_BUYER_PAY&type=alipay", "test-key")
	cases := []struct {
		name, productName, tradeNo, status, sign string
		pending                                  bool
	}{
		{"separator in product name", "item&out_trade_no=other", "order", epay.StatusTradeSuccess, ambiguousSign, false},
		{"separator in order number", "item", "other&out_trade_no=order", epay.StatusTradeSuccess, ambiguousSign, false},
		{"invalid signature", "item", "order", epay.StatusTradeSuccess, "invalid", false},
		{"missing signature", "item", "order", epay.StatusTradeSuccess, "", false},
		{"valid pending with literal percent escape", "item%26other", "order", "WAIT_BUYER_PAY", pendingSign, true},
	}
	endpoints := []struct {
		path    string
		handler gin.HandlerFunc
	}{
		{"/api/user/epay/notify", EpayNotify},
		{"/api/subscription/epay/notify", SubscriptionEpayNotify},
		{"/api/subscription/epay/return", SubscriptionEpayReturn},
	}
	router := gin.New()
	for _, endpoint := range endpoints {
		router.Any(endpoint.path, endpoint.handler)
	}
	for _, endpoint := range endpoints {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			for _, tc := range cases {
				t.Run(endpoint.path+"/"+method+"/"+tc.name, func(t *testing.T) {
					params := url.Values{
						"pid": {"epay_id"}, "type": {"alipay"}, "money": {"1.00"},
						"name": {tc.productName}, "out_trade_no": {tc.tradeNo},
						"trade_status": {tc.status}, "sign_type": {"MD5"},
					}
					if tc.sign != "" {
						params.Set("sign", tc.sign)
					}
					request := httptest.NewRequest(method, endpoint.path+"?"+params.Encode(), nil)
					if method == http.MethodPost {
						request = httptest.NewRequest(method, endpoint.path, strings.NewReader(params.Encode()))
						request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					}
					recorder := httptest.NewRecorder()
					require.NotPanics(t, func() { router.ServeHTTP(recorder, request) })
					if endpoint.path == "/api/subscription/epay/return" {
						assert.Equal(t, http.StatusFound, recorder.Code)
						status := "fail"
						if tc.pending {
							status = "pending"
						}
						assert.Equal(t, "https://dashboard.example.com/wallet?pay="+status, recorder.Header().Get("Location"))
						return
					}
					assert.Equal(t, http.StatusOK, recorder.Code)
					body := "fail"
					if tc.pending && endpoint.path == "/api/user/epay/notify" {
						body = "success"
					}
					assert.Equal(t, body, recorder.Body.String())
				})
			}
		}
	}
}
