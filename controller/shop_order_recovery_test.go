package controller

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestShopCommittedOrderSurvivesAuthorizationChangeBeforeResponse(t *testing.T) {
	db := shopControllerDB(t)
	for purpose, value := range map[string]string{"SECRET": "s", "FINGERPRINT": "f"} {
		encoded, err := common.Marshal(map[string]string{"test": base64.StdEncoding.EncodeToString([]byte(strings.Repeat(value, 32)))})
		require.NoError(t, err)
		t.Setenv("SHOP_"+purpose+"_ACTIVE_KEY_ID", "test")
		t.Setenv("SHOP_"+purpose+"_KEYS", string(encoded))
	}
	terms := model.ShopProductTerms{
		TemplateVersion: 1, DeliveryFields: []model.ShopField{{Key: "account", Label: "Account"}}, UsageConditions: "Personal use",
		DeliveryPolicy: model.ShopDeliveryPolicy{Mode: "manual", PublicTerms: "After payment", SLA: &model.ShopSLA{Clock: "elapsed", DurationMinutes: 60, Timezone: "UTC"}},
		ViewPolicy:     model.ShopViewPolicy{DurationSeconds: 600, FirstStart: "delivered_at", ReplacementStart: "delivered_at", ExpiredAction: "deny_reveal", PublicTerms: "Ten minutes"},
		WarrantyPolicy: model.ShopPeriodPolicy{Mode: "none", PublicTerms: "No continuing warranty"}, EntitlementPolicy: model.ShopPeriodPolicy{Mode: "described", PublicTerms: "One account"},
		RefundPolicy: model.ShopRefundPolicy{GoodsRefundMode: "full_only", Execution: "manual_original_channel", PublicTerms: "Review required"},
	}
	encoded, err := common.Marshal(terms)
	require.NoError(t, err)
	product := model.ShopProduct{ShopRecord: model.ShopRecord{PublicID: "sp_recovery", Version: 1}, TenantID: model.PlatformTenantID, Name: "Account", Status: "published", DeliveryType: "manual", PriceMinor: 100, Currency: "CNY", CurrencyExponent: 2, TermsVersion: 1, TermsJSON: string(encoded), ManualCapacity: 2}
	require.NoError(t, model.ValidateShopProduct(&product, true))
	require.NoError(t, db.Create(&product).Error)
	require.NoError(t, db.Create(&model.ShopSettings{ID: 1, Version: 1, SalesEnabled: true, Currency: "CNY", CurrencyExponent: 2, MinAmountMinor: 1, MaxAmountMinor: 10000, ReservationSeconds: 300, MaxUnpaidOrders: 2, RetentionPolicyJSON: "{}"}).Error)

	remembered, injected := false, false
	require.NoError(t, db.Callback().Create().After("gorm:create").Register("shop:test_order_remembered", func(tx *gorm.DB) {
		if tx.Statement.Table == "shop_idempotency_keys" && tx.Error == nil {
			remembered = true
		}
	}))
	t.Cleanup(func() { assert.NoError(t, db.Callback().Create().Remove("shop:test_order_remembered")) })
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("shop:test_disable_after_commit", func(tx *gorm.DB) {
		if !remembered || injected || tx.Statement.Table != "users" {
			return
		}
		if _, inTransaction := tx.Statement.ConnPool.(gorm.TxCommitter); inTransaction {
			return
		}
		injected = true
		var count int64
		require.NoError(t, db.Model(&model.ShopOrder{}).Count(&count).Error)
		require.Equal(t, int64(1), count, "another connection can see the committed order before authorization changes")
		require.NoError(t, db.Model(&model.User{}).Where("id = ?", 10).Update("status", common.UserStatusDisabled).Error)
	}))
	t.Cleanup(func() { assert.NoError(t, db.Callback().Query().Remove("shop:test_disable_after_commit")) })

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		// The request has already authenticated when this response-time race occurs.
		c.Set("id", 10)
		common.SetContextKey(c, constant.ContextKeyTenantID, model.PlatformTenantID)
	})
	engine.POST("/orders", ShopCreateOrder)
	engine.GET("/orders/:order_no", ShopGetOrder)
	body := `{"product_id":"sp_recovery","product_version":"1","terms_version":"1","accepted_terms":true,"buyer_input":{}}`
	key := "10000000-0000-0000-0000-000000000001"
	request := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", key)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.True(t, injected)
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), "SHOP_REQUEST_UNRESOLVED")
	assert.NotContains(t, response.Body.String(), "request_outcome")
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))

	var order model.ShopOrder
	require.NoError(t, db.First(&order).Error)
	assert.NotContains(t, response.Body.String(), order.PublicID)
	read := httptest.NewRecorder()
	engine.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/orders/"+order.PublicID, nil))
	assert.Equal(t, http.StatusNotFound, read.Code, "ordinary reads still hide inaccessible orders")
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 10).Update("status", common.UserStatusEnabled).Error)
	replay := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(body))
	replay.Header = request.Header.Clone()
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, replay)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), order.PublicID)
	var count int64
	require.NoError(t, db.Model(&model.ShopOrder{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	require.NoError(t, db.First(&product, product.ID).Error)
	assert.Equal(t, int64(1), product.ManualUsed)
}
