package controller

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestShopOrderProjectionDoesNotExposeSecretsOrMisstateFunds(t *testing.T) {
	now := time.Now().Unix()
	orderID, deliveryID, receiptID := int64(1), int64(2), int64(3)
	detail := model.ShopOrderDetail{
		Order:           model.ShopOrder{ShopRecord: model.ShopRecord{ID: orderID, PublicID: "so_test", Version: 1, CreatedAt: now}, TenantID: model.PlatformTenantID, Quantity: 1, PaymentState: "paid", PurchaseState: "closed", DeliveryState: "delivered", EffectiveReceiptID: &receiptID, CurrentDeliveryID: &deliveryID, BuyerInputCiphertext: "buyer-input-secret"},
		Snapshot:        model.ShopOrderSnapshot{Name: "Virtual account", DeliveryType: "manual", Terms: model.ShopProductTerms{DeliveryFields: []model.ShopField{{Key: "password", Label: "Password"}}}},
		CurrentDelivery: &model.ShopDelivery{ShopRecord: model.ShopRecord{ID: deliveryID, PublicID: "sd_test", Version: 1}, OrderID: orderID, DeliveredAt: now, ViewExpiresAt: now + 60, ManualCiphertext: "delivery-secret"},
		Receipts:        []model.ShopPaymentReceipt{{ShopRecord: model.ShopRecord{ID: receiptID}, OrderID: &orderID, ProcessingState: "matched", AmountMinor: 100, Currency: "CNY", CurrencyExponent: 2}},
		Refunds:         []model.ShopRefundCase{{ShopRecord: model.ShopRecord{PublicID: "sr_extra"}, ReceiptID: 99, Kind: "extra", State: "succeeded", AmountMinor: 100, Currency: "CNY", CurrencyExponent: 2}},
		Events:          []model.ShopEvent{{Visibility: "internal", Note: "operator-only-note"}, {Visibility: "public", Note: "Delivered"}},
	}
	out := shopOrderDTO(&detail, false)
	assert.Equal(t, "delivered", out["display_status"], "an extra-payment refund cannot refund the purchased goods")
	assert.Contains(t, out["allowed_actions"], "reveal")
	require.NotNil(t, out["current_delivery"], "current delivery survives truncated history")
	raw, err := common.Marshal(out)
	require.NoError(t, err)
	for _, secret := range []string{"buyer-input-secret", "delivery-secret", "operator-only-note"} {
		assert.NotContains(t, string(raw), secret)
	}
	detail.HasPaymentAdjustment = true
	out = shopOrderDTO(&detail, false)
	assert.NotContains(t, out["allowed_actions"], "reveal")
	assert.False(t, out["current_delivery"].(gin.H)["can_reveal"].(bool))
	detail.Receipts = nil
	out = shopOrderDTO(&detail, false)
	assert.NotContains(t, out["payment_summary"], "received", "missing optional Money must be omitted instead of null")
}

func TestShopStrictBodyRejectsAmbiguousRequestsWithoutEcho(t *testing.T) {
	for _, body := range []string{`{"expected_version":"1","expected_version":"2"}`, `{"expected_version":"1","secret":"private-value"}`, `{"expected_version":"1"} {}`, `[]`} {
		t.Run(body, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			var input struct {
				ExpectedVersion string `json:"expected_version"`
			}
			assert.False(t, shopBind(ctx, &input, 1024))
			assert.Equal(t, 400, recorder.Code)
			assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			assert.NotContains(t, recorder.Body.String(), "private-value")
		})
	}
}

func TestShopPaymentErrorMarksOnlyProvenUncommittedRequest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		marked bool
	}{
		{"creation-rejected", &service.ShopPaymentNotCommittedError{Cause: model.ErrShopVersion}, true},
		{"later-conflict", model.ErrShopVersion, false},
		{"later-not-found", model.ErrShopNotFound, false},
		{"channel-unavailable", service.ErrShopChannelUnavailable, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			shopModelError(ctx, tc.err)
			if tc.marked {
				assert.Contains(t, recorder.Body.String(), `"request_outcome":"not_committed"`)
			} else {
				assert.NotContains(t, recorder.Body.String(), "request_outcome")
			}
			assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
		})
	}
}

func TestShopMutationsRequirePositiveVersionBeforeDatabaseAccess(t *testing.T) {
	for _, action := range []string{"close-order", "payment-attempt", "buyer-input", "delivery", "replacement", "refund-approve", "receipt-assign", "payment-evidence", "category", "product"} {
		for _, version := range []string{"", "0", "-1", "01", "9223372036854775808"} {
			t.Run(action+"/"+version, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				ctx.Request = httptest.NewRequest(http.MethodPost, "/", nil)
				ctx.Request.Header.Set("Idempotency-Key", "10000000-0000-0000-0000-000000000001")
				_, ok := shopCommand(ctx, nil, action, "existing-resource", version)
				assert.False(t, ok)
				assert.Equal(t, 400, recorder.Code)
				assert.Contains(t, recorder.Body.String(), "SHOP_VALIDATION_FAILED")
			})
		}
	}
}

func TestShopRefundRejectsPublicNoteOutsideBoundRecordProof(t *testing.T) {
	for _, action := range []string{"attempt", "cancel", "verify"} {
		t.Run(action, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"expected_version":"1","public_note":"unbound buyer message"}`))
			ctx.Request.Header.Set("Content-Type", "application/json")
			// No session/proof/database: invalid input must fail before proof consumption.
			ShopRefundAction(action)(ctx)
			assert.Equal(t, 400, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "SHOP_VALIDATION_FAILED")
			assert.NotContains(t, recorder.Body.String(), "unbound buyer message")
		})
	}
}

func TestShopPaymentStateRequiresClosureEvidenceAndPrioritizesPaid(t *testing.T) {
	for _, tc := range []struct {
		create, close, evidence, want string
	}{
		{"prepared", "", "", "creating"},
		{"creating", "", "", "creating"},
		{"ready", "", "", "ready"},
		{"unknown", "", "", "unknown"},
		{"closed", "closed", "", "unknown"},
		{"failed", "closed", "", "unknown"},
		{"ready", "closed", "channel-query", "closed"},
		{"failed", "closed", "channel-query", "failed"},
		{"paid", "closed", "channel-query", "paid"},
	} {
		t.Run(tc.create+"/"+tc.evidence, func(t *testing.T) {
			assert.Equal(t, tc.want, shopPaymentState(model.ShopPaymentAttempt{CreateState: tc.create, CloseState: tc.close, CloseEvidenceRef: tc.evidence}))
		})
	}
}

func TestShopOrderPaymentActionResumesOnlyTheActiveReadyAttempt(t *testing.T) {
	activeID := int64(1)
	detail := model.ShopOrderDetail{Order: model.ShopOrder{PurchaseState: "open", PaymentState: "verifying", ActiveAttemptID: &activeID, ReservationExpiresAt: time.Now().Unix() + 60}}
	for _, tc := range []struct {
		state      string
		hold, want bool
	}{
		{"ready", false, true},
		{"creating", false, false},
		{"unknown", false, false},
		{"ready", true, false},
		{"paid", false, false},
	} {
		t.Run(tc.state, func(t *testing.T) {
			detail.Order.RiskHold = tc.hold
			detail.Attempts = []model.ShopPaymentAttempt{{ShopRecord: model.ShopRecord{ID: 2}, CreateState: "ready"}, {ShopRecord: model.ShopRecord{ID: activeID}, CreateState: tc.state}}
			actions := shopOrderDTO(&detail, false)["allowed_actions"]
			if tc.want {
				assert.Contains(t, actions, "pay")
			} else {
				assert.NotContains(t, actions, "pay")
			}
		})
	}
}

func TestShopOrderProjectionKeepsRequestedInputAndActivePaymentVisible(t *testing.T) {
	activeID := int64(1)
	detail := model.ShopOrderDetail{
		Order: model.ShopOrder{ActiveAttemptID: &activeID, InputCompleteAt: 100, MissingInputKeysJSON: `["region"]`},
		Attempts: []model.ShopPaymentAttempt{
			{ShopRecord: model.ShopRecord{ID: 2}, LastCheckedAt: 300},
			{ShopRecord: model.ShopRecord{ID: activeID}, LastCheckedAt: 200},
		},
	}
	out := shopOrderDTO(&detail, false)
	assert.Equal(t, gin.H{"missing_field_keys": []string{"region"}, "complete": false}, out["buyer_input_status"])
	assert.Equal(t, shopTime(200), out["payment_summary"].(gin.H)["last_checked_at"])
	detail.Order.MissingInputKeysJSON = "[]"
	assert.Equal(t, true, shopOrderDTO(&detail, false)["buyer_input_status"].(gin.H)["complete"])
}

func TestShopCallbackRejectsOversizedOrDuplicateFormsBeforeDatabaseAccess(t *testing.T) {
	for _, tc := range []struct {
		name, method, query, body string
	}{
		{"oversized-query", http.MethodGet, "x=" + strings.Repeat("x", 64<<10), ""},
		{"oversized-body", http.MethodPost, "", "x=" + strings.Repeat("x", 64<<10)},
		{"duplicate-query", http.MethodGet, "out_trade_no=KSa&out_trade_no=KSb", ""},
		{"duplicate-body", http.MethodPost, "", "out_trade_no=KSa&out_trade_no=KSb"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(tc.method, "/?"+tc.query, strings.NewReader(tc.body))
			ctx.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			ShopEpayCallback(ctx)
			assert.Equal(t, 400, recorder.Code)
			assert.Equal(t, "fail", recorder.Body.String())
			assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
		})
	}
}

func shopControllerDB(t *testing.T) *gorm.DB {
	t.Helper()
	t.Setenv("SHOP_SECRET_KEYS_FILE", "")
	t.Setenv("SHOP_FINGERPRINT_KEYS_FILE", "")
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "shop.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous; require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(append(model.ShopSchemaModels(), &model.User{})...))
	require.NoError(t, db.Create(&model.User{Id: 10, Username: "buyer", AffCode: "buyer", TenantID: model.PlatformTenantID, Role: common.RoleCommonUser, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.User{Id: 20, Username: "admin", AffCode: "admin", TenantID: model.PlatformTenantID, Role: common.RoleAdminUser, Status: common.UserStatusEnabled}).Error)
	return db
}

func TestShopRefundActionsPreserveAdjustmentFenceAndFactRecording(t *testing.T) {
	for _, tc := range []struct {
		state, adjustment, attempt string
		want                       []string
	}{
		{"approved", "", "", []string{"register_attempt", "cancel"}},
		{"approved", "failed", "", []string{"register_attempt", "cancel"}},
		{"approved", "pending", "", []string{}},
		{"approved", "succeeded", "", []string{}},
		{"failed", "disputed", "", []string{}},
		{"processing", "pending", "registered", []string{"record_attempt", "verify_attempt"}},
		{"unknown", "disputed", "unknown", []string{"verify_attempt"}},
	} {
		t.Run(tc.state+"/"+tc.adjustment, func(t *testing.T) {
			db := shopControllerDB(t)
			receipt := model.ShopPaymentReceipt{ShopRecord: model.ShopRecord{PublicID: "spr_action", Version: 1}, ReceiptKey: "receipt", AmountMinor: 100, Currency: "CNY", CurrencyExponent: 2}
			require.NoError(t, db.Create(&receipt).Error)
			refund := model.ShopRefundCase{ShopRecord: model.ShopRecord{PublicID: "sr_action", Version: 1}, ReceiptID: receipt.ID, State: tc.state}
			require.NoError(t, db.Create(&refund).Error)
			if tc.adjustment != "" {
				require.NoError(t, db.Create(&model.ShopPaymentAdjustment{ShopRecord: model.ShopRecord{PublicID: "spa_action", Version: 1}, ReceiptID: receipt.ID, AdjustmentKey: "adjustment", State: tc.adjustment, AmountMinor: 100, Currency: "CNY", CurrencyExponent: 2}).Error)
			}
			if tc.attempt != "" {
				require.NoError(t, db.Create(&model.ShopRefundAttempt{ShopRecord: model.ShopRecord{PublicID: "sra_action", Version: 1}, RefundCaseID: refund.ID, State: tc.attempt}).Error)
			}
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			ctx.Set("id", 20)
			ctx.Set("shop_admin", true)
			common.SetContextKey(ctx, constant.ContextKeyTenantID, model.PlatformTenantID)
			shopRespondRefund(ctx, refund.PublicID)
			require.Equal(t, 200, recorder.Code, recorder.Body.String())
			var response struct {
				Data struct {
					AllowedActions []string `json:"allowed_actions"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.ElementsMatch(t, tc.want, response.Data.AllowedActions)
		})
	}
}

func TestShopOrderReadOwnershipAndNormalResponseSecrecy(t *testing.T) {
	db := shopControllerDB(t)
	snapshot, err := common.Marshal(model.ShopOrderSnapshot{Name: "Test", DeliveryType: "manual"})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ShopOrder{ShopRecord: model.ShopRecord{PublicID: "so_owned", Version: 1, CreatedAt: 100}, TenantID: model.PlatformTenantID, UserID: 10, SnapshotJSON: string(snapshot), PurchaseState: "open", PaymentState: "unpaid", DeliveryState: "not_ready", BuyerInputCiphertext: "must-never-leave-server", Currency: "CNY", Quantity: 1}).Error)
	for _, tc := range []struct {
		name   string
		user   int
		tenant int64
		admin  bool
		status int
	}{{"owner", 10, model.PlatformTenantID, false, 200}, {"another-user", 20, model.PlatformTenantID, false, 404}, {"wrong-tenant", 10, 42, false, 404}, {"admin", 20, model.PlatformTenantID, true, 200}} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			ctx.Set("id", tc.user)
			ctx.Set("shop_admin", tc.admin)
			common.SetContextKey(ctx, constant.ContextKeyTenantID, tc.tenant)
			ctx.Params = gin.Params{{Key: "order_no", Value: "so_owned"}}
			ShopGetOrder(ctx)
			assert.Equal(t, tc.status, recorder.Code)
			assert.NotContains(t, recorder.Body.String(), "must-never-leave-server")
			assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
		})
	}
}

func TestShopBuyerInputHTTPRequiresLiveOwnedSession(t *testing.T) {
	db := shopControllerDB(t)
	require.NoError(t, db.AutoMigrate(&model.UserSession{}))
	require.NoError(t, db.Model(&model.User{}).Where("id IN ?", []int{10, 20}).Update("auth_version", 1).Error)
	previousRedis, previousSecure := common.RedisEnabled, common.SessionCookieSecure
	common.RedisEnabled, common.SessionCookieSecure = false, true
	t.Cleanup(func() { common.RedisEnabled, common.SessionCookieSecure = previousRedis, previousSecure })
	for purpose, value := range map[string]string{"SECRET": "s", "FINGERPRINT": "f"} {
		encoded, err := common.Marshal(map[string]string{"test": base64.StdEncoding.EncodeToString([]byte(strings.Repeat(value, 32)))})
		require.NoError(t, err)
		t.Setenv("SHOP_"+purpose+"_ACTIVE_KEY_ID", "test")
		t.Setenv("SHOP_"+purpose+"_KEYS", string(encoded))
	}

	terms := model.ShopProductTerms{TemplateVersion: 1, BuyerInputSchema: []model.ShopField{{Key: "contact_email", Label: "Contact email"}}}
	secret, _, err := service.EncryptShopBuyerInput("so_private", terms, map[string]string{"contact_email": "buyer@example.invalid"})
	require.NoError(t, err)
	snapshot, err := common.Marshal(model.ShopOrderSnapshot{Name: "Test", DeliveryType: "manual", Terms: terms})
	require.NoError(t, err)
	product := model.ShopProduct{ShopRecord: model.ShopRecord{PublicID: "sp_private", Version: 1}}
	require.NoError(t, db.Create(&product).Error)
	order := model.ShopOrder{ShopRecord: model.ShopRecord{PublicID: "so_private", Version: 1}, ProductID: product.ID, UserID: 10, SnapshotJSON: string(snapshot), BuyerInputCiphertext: secret.Ciphertext, BuyerInputKeyVersion: secret.KeyVersion, PurchaseState: "open", PaymentState: "unpaid", DeliveryState: "not_ready", Currency: "CNY", Quantity: 1}
	require.NoError(t, db.Create(&order).Error)
	tokens := map[string]string{}
	for _, fixture := range []struct {
		name, status string
		userID       int
	}{{"buyer", model.UserSessionStatusActive, 10}, {"admin", model.UserSessionStatusActive, 20}, {"revoked", model.UserSessionStatusRevoked, 10}} {
		session := model.UserSession{SID: "shop-http-" + fixture.name, UserID: fixture.userID, Version: 1, UserAuthVersion: 1, Status: fixture.status, RefreshHash: "unused-test-hash", LoginMethod: "password", ExpiresAt: time.Now().Add(time.Hour).Unix()}
		require.NoError(t, db.Create(&session).Error)
		token, _, err := service.IssueAccessToken(service.AuthIdentity{UserID: fixture.userID, SessionID: session.SID, UserAuthVersion: 1, SessionVersion: 1})
		require.NoError(t, err)
		tokens[fixture.name] = token
	}

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		// Fixture for trusted Host resolution; identity still comes from the real
		// authentication middleware and persisted login session.
		tenantID := model.PlatformTenantID
		if c.Request.Host == "tenant.shop.invalid" {
			tenantID = 42
		}
		common.SetContextKey(c, constant.ContextKeyTenantContext, model.TenantContext{TenantID: tenantID, Hostname: c.Request.Host})
		common.SetContextKey(c, constant.ContextKeyTenantID, tenantID)
		c.Next()
	})
	buyer := engine.Group("/api/shop", middleware.ShopMainSiteGuard(), middleware.UserAuth(), middleware.ShopSessionGuard(false))
	buyer.GET("/orders/:order_no", ShopGetOrder)
	buyer.GET("/orders/:order_no/buyer-input", ShopGetBuyerInput)
	buyer.PUT("/orders/:order_no/buyer-input", ShopUpdateBuyerInput)
	admin := engine.Group("/api/admin/shop", middleware.ShopMainSiteGuard(), middleware.SensitiveResponseAudit(), middleware.AdminAuth(), middleware.ShopSessionGuard(true))
	admin.Use(func(c *gin.Context) { c.Set("shop_admin", true); c.Next() })
	admin.GET("/orders/:order_no/buyer-input", ShopGetBuyerInput)
	for _, tc := range []struct {
		name, token, method, prefix, suffix, host, origin string
		status                                            int
		private                                           bool
	}{
		{name: "owner", token: "buyer", status: 200, private: true},
		{name: "other-user", token: "admin", status: 404},
		{name: "anonymous", status: 401},
		{name: "revoked", token: "revoked", status: 401},
		{name: "other-site", token: "buyer", host: "tenant.shop.invalid", status: 403},
		{name: "admin", token: "admin", prefix: "/api/admin/shop", status: 200, private: true},
		{name: "non-admin", token: "buyer", prefix: "/api/admin/shop", status: 403},
		{name: "normal-order", token: "buyer", suffix: "order-only", status: 200},
		{name: "foreign-origin", token: "buyer", method: http.MethodPut, origin: "https://foreign.invalid", status: 403},
		{name: "missing-origin", token: "buyer", method: http.MethodPut, status: 403},
		{name: "same-origin-invalid-body", token: "buyer", method: http.MethodPut, origin: "https://shop.invalid", status: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.method == "" {
				tc.method = http.MethodGet
			}
			if tc.prefix == "" {
				tc.prefix = "/api/shop"
			}
			if tc.host == "" {
				tc.host = "shop.invalid"
			}
			path := tc.prefix + "/orders/so_private"
			if tc.suffix != "order-only" {
				path += "/buyer-input"
			}
			request := httptest.NewRequest(tc.method, "https://"+tc.host+path, strings.NewReader(`{}`))
			request.Header.Set("Content-Type", "application/json")
			if tc.origin != "" {
				request.Header.Set("Origin", tc.origin)
			}
			if tc.token != "" {
				request.Header.Set("Authorization", "Bearer "+tokens[tc.token])
			}
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			assert.Equal(t, "no-referrer", recorder.Header().Get("Referrer-Policy"))
			assert.NotContains(t, recorder.Body.String(), secret.Ciphertext)
			if tc.private {
				assert.Contains(t, recorder.Body.String(), "buyer@example.invalid")
			} else {
				assert.NotContains(t, recorder.Body.String(), "buyer@example.invalid")
			}
		})
	}
	var events []model.ShopEvent
	require.NoError(t, db.Where("order_id = ? AND kind = ?", order.ID, "buyer_input_read").Find(&events).Error)
	assert.Len(t, events, 2, "only the authorized buyer and admin may audit a private read")
}

func TestShopCommittedRefundReplayChecksActorAndBodyBeforeProof(t *testing.T) {
	db := shopControllerDB(t)
	t.Setenv("SHOP_FINGERPRINT_ACTIVE_KEY_ID", "test")
	encoded, err := common.Marshal(map[string]string{"test": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))})
	require.NoError(t, err)
	t.Setenv("SHOP_FINGERPRINT_KEYS", string(encoded))
	receipt := model.ShopPaymentReceipt{ShopRecord: model.ShopRecord{PublicID: "spr_test", Version: 1}, ReceiptKey: "receipt", AmountMinor: 100, Currency: "CNY", CurrencyExponent: 2, ProcessingState: "review"}
	require.NoError(t, db.Create(&receipt).Error)
	evidence := model.ShopPaymentEvidence{ShopRecord: model.ShopRecord{PublicID: "spe_test", Version: 1}, ReceiptID: receipt.ID}
	require.NoError(t, db.Create(&evidence).Error)
	body := `{"expected_version":"1","kind":"mismatch","reason":"refund","evidence_refs":["spe_test"]}`
	key := "10000000-0000-0000-0000-000000000001"
	var input struct {
		ExpectedVersion string   `json:"expected_version"`
		Kind            string   `json:"kind"`
		Reason          string   `json:"reason"`
		EvidenceRefs    []string `json:"evidence_refs"`
	}
	require.NoError(t, common.Unmarshal([]byte(body), &input))
	hash, version, err := service.ShopRequestHash(input, "refund-approve:spr_test", "")
	require.NoError(t, err)
	refund, err := model.ApproveShopRefund(db, model.ShopRefundApproveInput{Command: model.ShopCommand{ActorID: 20, TenantID: model.PlatformTenantID, Admin: true, Key: key, RequestHash: hash, HashKeyVersion: version, ExpectedVersion: 1, Now: 100}, ReceiptPublicID: receipt.PublicID, Kind: "mismatch", Reason: "refund", EvidenceIDs: []string{evidence.PublicID}})
	require.NoError(t, err)

	request := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(r)
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Request.Header.Set("Idempotency-Key", key)
		c.Set("id", 20)
		c.Set("shop_admin", true)
		common.SetContextKey(c, constant.ContextKeyTenantID, model.PlatformTenantID)
		c.Params = gin.Params{{Key: "receipt_id", Value: "spr_test"}}
		ShopApproveRefund(c)
		return r
	}
	result := request(body)
	assert.Equal(t, 200, result.Code, result.Body.String())
	assert.Contains(t, result.Body.String(), refund.PublicID, "committed metadata replay needs no new operation proof")
	result = request(strings.Replace(body, `"refund"`, `"changed"`, 1))
	assert.Equal(t, 409, result.Code)
	assert.Contains(t, result.Body.String(), "SHOP_IDEMPOTENCY_CONFLICT")
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 20).Update("status", common.UserStatusDisabled).Error)
	result = request(body)
	assert.Equal(t, 404, result.Code, "disabled actor cannot replay a former authorized operation")
}
