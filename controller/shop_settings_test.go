package controller

import (
	"encoding/base64"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShopSettingsGETUsesCurrentMerchantWithoutPersistingOrLeakingKey(t *testing.T) {
	db := shopControllerDB(t)
	previousID, previousKey := operation_setting.EpayId, operation_setting.EpayKey
	t.Cleanup(func() {
		operation_setting.EpayId, operation_setting.EpayKey = previousID, previousKey
	})
	operation_setting.EpayId, operation_setting.EpayKey = "current-merchant", "secret-key-must-not-leak"

	read := func() map[string]any {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/admin/shop/settings", nil)
		ctx.Set("id", 20)
		ctx.Set("shop_admin", true)
		common.SetContextKey(ctx, constant.ContextKeyTenantID, model.PlatformTenantID)
		GetShopSettings(ctx)
		require.Equal(t, http.StatusOK, recorder.Code)
		assert.NotContains(t, recorder.Body.String(), "secret-key-must-not-leak")
		var response struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		return response.Data
	}

	defaults := read()
	assert.Equal(t, "0", defaults["version"])
	assert.Equal(t, false, defaults["new_orders_enabled"])
	assert.Equal(t, "epay:current-merchant", defaults["provider_account_ref"])
	assert.Equal(t, "CNY", defaults["currency"])
	var count int64
	require.NoError(t, db.Model(&model.ShopSettings{}).Count(&count).Error)
	assert.Zero(t, count)

	operation_setting.EpayId = ""
	assert.Equal(t, "", read()["provider_account_ref"], "an absent merchant must not create a stranded persisted identity")
	operation_setting.EpayId = "current-merchant"
	stored := model.ShopSettings{ID: 1, Version: 9, SalesEnabled: false, Provider: "epay", ProviderAccountRef: "epay:historic-merchant", Environment: "test", Currency: "CNY", CurrencyExponent: 2, MinAmountMinor: 17, MaxAmountMinor: 12345, ReservationSeconds: 540, MaxUnpaidOrders: 7, RetentionPolicyJSON: `{"secret_after_delivery_seconds":1234}`, ReminderPolicyJSON: `{"payment_unknown_seconds":4321}`}
	require.NoError(t, db.Create(&stored).Error)
	previous := read()
	assert.Equal(t, "9", previous["version"])
	assert.Equal(t, "epay:historic-merchant", previous["provider_account_ref"])
	assert.Equal(t, "test", previous["environment"])
	assert.Equal(t, float64(7), previous["pending_order_limit"])
}

func TestShopSettingsDefaultsCanBeSavedWithoutManualOperatingConfiguration(t *testing.T) {
	db := shopControllerDB(t)
	previousID := operation_setting.EpayId
	t.Cleanup(func() { operation_setting.EpayId = previousID })
	operation_setting.EpayId = "current-merchant"
	t.Setenv("SHOP_FINGERPRINT_ACTIVE_KEY_ID", "test")
	keys, err := common.Marshal(map[string]string{"test": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))})
	require.NoError(t, err)
	t.Setenv("SHOP_FINGERPRINT_KEYS", string(keys))
	defaults, err := model.GetShopSettings(db)
	require.NoError(t, err)
	data, err := shopSettingsDTO(defaults)
	require.NoError(t, err)
	payload := gin.H{"reason": "Initialize shop from defaults"}
	for _, name := range []string{"new_orders_enabled", "provider", "provider_account_ref", "environment", "currency", "reservation_minutes", "pending_order_limit", "min_amount_minor", "max_amount_minor", "retention_policy", "reminder_policy"} {
		payload[name] = data[name]
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPatch, "/api/admin/shop/settings", strings.NewReader(string(body)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set("Idempotency-Key", "10000000-0000-0000-0000-000000000002")
	ctx.Set("id", 20)
	ctx.Set("shop_admin", true)
	common.SetContextKey(ctx, constant.ContextKeyTenantID, model.PlatformTenantID)
	UpdateShopSettings(ctx)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var stored model.ShopSettings
	require.NoError(t, db.First(&stored, 1).Error)
	assert.Equal(t, int64(1), stored.Version)
	assert.False(t, stored.SalesEnabled)
	assert.Equal(t, "epay:current-merchant", stored.ProviderAccountRef)
	assert.Equal(t, defaults.ReservationSeconds, stored.ReservationSeconds)
	assert.Equal(t, defaults.MaxUnpaidOrders, stored.MaxUnpaidOrders)
	assert.JSONEq(t, defaults.RetentionPolicyJSON, stored.RetentionPolicyJSON)
	assert.JSONEq(t, defaults.ReminderPolicyJSON, stored.ReminderPolicyJSON)
}

func TestShopSettingsRejectsVersionOverflowWithoutMutation(t *testing.T) {
	db := shopControllerDB(t)
	t.Setenv("SHOP_FINGERPRINT_ACTIVE_KEY_ID", "test")
	keys, err := common.Marshal(map[string]string{"test": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))})
	require.NoError(t, err)
	t.Setenv("SHOP_FINGERPRINT_KEYS", string(keys))
	require.NoError(t, db.Create(&model.ShopSettings{ID: 1, Version: math.MaxInt64, PolicyVersion: math.MaxInt64}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"expected_version":"9223372036854775807","new_orders_enabled":false,"provider":"epay","provider_account_ref":"test-account","environment":"test","currency":"CNY","reservation_minutes":30,"pending_order_limit":3,"min_amount_minor":"1","max_amount_minor":"10000","retention_policy":{"secret_after_delivery_seconds":60,"buyer_input_after_closure_seconds":60,"evidence_after_settlement_seconds":60,"audit_after_settlement_seconds":60,"idempotency_after_closure_seconds":60},"reminder_policy":{"payment_unknown_seconds":60,"delivery_exception_seconds":60,"refund_verification_seconds":60},"reason":"Update settings"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set("Idempotency-Key", "10000000-0000-0000-0000-000000000001")
	ctx.Set("id", 20)
	ctx.Set("shop_admin", true)
	common.SetContextKey(ctx, constant.ContextKeyTenantID, model.PlatformTenantID)
	UpdateShopSettings(ctx)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "SHOP_VALIDATION_FAILED")
	var settings model.ShopSettings
	require.NoError(t, db.First(&settings, 1).Error)
	assert.Equal(t, int64(math.MaxInt64), settings.Version)
	assert.Equal(t, int64(math.MaxInt64), settings.PolicyVersion)
	var commits int64
	require.NoError(t, db.Model(&model.ShopIdempotencyKey{}).Count(&commits).Error)
	assert.Zero(t, commits)
}
