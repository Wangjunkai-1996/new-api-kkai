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
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
