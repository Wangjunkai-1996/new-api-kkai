package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestInvoiceEpayEvidenceRequiresAuthenticatedExactSuccessfulReceipt(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	oldAddress, oldID, oldKey, oldMethods := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey, operation_setting.PayMethods
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	t.Cleanup(func() {
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey, operation_setting.PayMethods = oldAddress, oldID, oldKey, oldMethods
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.RedisEnabled = oldRedisEnabled
		common.SetDatabaseTypes(oldMainType, oldLogType)
	})
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://pay.example.com", "epay_id", "test-key"
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "invoice-epay.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.KKAIOutboxEvent{}, &model.InvoicePaymentReceipt{}, &model.Log{}))
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, raw.Close()) })
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.Create(&model.User{Id: 3418, Username: "invoice-customer", AffCode: "invoice-customer", Quota: 10}).Error)
	require.NoError(t, db.Create(&model.TopUp{Id: 842, UserId: 3418, TradeNo: "invoice-epay-order", Amount: 75, Money: 75, PaidMinor: 7500, Currency: "CNY", PaymentMethod: "alipay", PaymentProvider: model.PaymentProviderEpay, Status: common.TopUpStatusPending}).Error)
	router := gin.New()
	router.GET("/api/user/epay/notify", EpayNotify)
	for _, tc := range []struct {
		name, amount, receiptID, signature, response string
	}{
		{"invalid signature", "75.00", "provider-receipt", "invalid", "fail"},
		{"signed missing receipt", "75.00", "", "", "fail"},
		{"signed underpayment", "74.99", "provider-receipt", "", "fail"},
		{"signed exact payment", "75.00", "provider-receipt", "", "success"},
		{"signed replay", "75.00", "provider-receipt", "", "success"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := epay.GenerateParams(map[string]string{"pid": "epay_id", "type": "alipay", "money": tc.amount, "name": "充值", "out_trade_no": "invoice-epay-order", "trade_no": tc.receiptID, "trade_status": epay.StatusTradeSuccess}, "test-key")
			if tc.signature != "" {
				params["sign"] = tc.signature
			}
			values := url.Values{}
			for key, value := range params {
				values.Set(key, value)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/user/epay/notify?"+values.Encode(), nil))
			assert.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, tc.response, response.Body.String())
			var count int64
			require.NoError(t, db.Model(&model.InvoicePaymentReceipt{}).Count(&count).Error)
			var topUp model.TopUp
			require.NoError(t, db.First(&topUp, 842).Error)
			var customer model.User
			require.NoError(t, db.First(&customer, 3418).Error)
			if tc.response == "success" {
				assert.EqualValues(t, 1, count)
				assert.Equal(t, common.TopUpStatusSuccess, topUp.Status)
				assert.EqualValues(t, 37_500_010, customer.Quota)
			} else {
				assert.Zero(t, count)
				assert.Equal(t, common.TopUpStatusPending, topUp.Status)
				assert.EqualValues(t, 10, customer.Quota)
			}
		})
	}
}
