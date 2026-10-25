package controller

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWalletTopUpDTORequiresReliableClosureAndExplicitAcknowledgement(t *testing.T) {
	now := time.Now().Unix()
	order := &model.TopUp{WalletLifecycleVersion: 1, CreateTime: now, Status: common.TopUpStatusExpired, ClosedAt: now, CloseEvidenceRef: "stripe-session:original", ProviderPaymentState: "unpaid", CloseReason: "timeout", Version: 9007199254740993}
	current := WalletTopUpOrderDTO(order, true)
	assert.Equal(t, "9007199254740993", current.Version)
	assert.Equal(t, "expired", current.DisplayStatus)
	assert.Contains(t, current.AllowedActions, "acknowledge")
	state := walletStateDTO(&model.WalletTopUpState{Flow: &model.WalletTopUpFlow{FlowID: "original", Revision: 3}, Order: order})
	assert.Equal(t, "result", state.Flow.Phase)
	assert.False(t, state.CanCreate)
	history := WalletTopUpOrderDTO(order, false)
	assert.NotContains(t, history.AllowedActions, "acknowledge")
	order.CloseEvidenceRef = ""
	unknown := WalletTopUpOrderDTO(order, true)
	assert.Equal(t, "unavailable", unknown.DisplayStatus)
	assert.NotContains(t, unknown.AllowedActions, "acknowledge")
	legacy := WalletTopUpOrderDTO(&model.TopUp{Status: common.TopUpStatusPending}, false)
	assert.Equal(t, "unavailable", legacy.DisplayStatus)
	assert.Equal(t, "all", legacy.StatusGroup)
	assert.Empty(t, legacy.AllowedActions)
}

func TestWalletCurrentDistinguishesRecoverableOrderFromBoundFlow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state model.WalletTopUpState
		code  string
		phase string
	}{
		{
			name:  "recoverable order without a bound flow",
			state: model.WalletTopUpState{HasRecoverableCandidate: true},
			code:  "RECOVERABLE_ORDER_EXISTS",
			phase: "idle",
		},
		{
			name: "existing bound flow",
			state: model.WalletTopUpState{
				HasRecoverableCandidate: true,
				Flow:                    &model.WalletTopUpFlow{FlowID: "current", Revision: 2},
				Order:                   &model.TopUp{TradeNo: "WT-current", Status: common.TopUpStatusSuccess},
			},
			code:  "CURRENT_FLOW_ACTIVE",
			phase: "result",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := walletStateDTO(&tc.state)
			require.NotNil(t, state.CreateBlockReasonCode)
			assert.Equal(t, tc.code, *state.CreateBlockReasonCode)
			assert.Equal(t, tc.phase, state.Flow.Phase)
			assert.False(t, state.CanCreate)
		})
	}
}

func TestWalletMoneyRejectsInvalidConfiguration(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 0, -1} {
		assert.False(t, walletPositiveFinite(value))
	}
	for _, tc := range []struct {
		currency string
		exponent int
	}{{"IDR", 2}, {"JPY", 0}, {"USD", 2}, {"KWD", 3}} {
		got, err := walletCurrencyExponent(tc.currency)
		require.NoError(t, err)
		assert.Equal(t, tc.exponent, got)
	}
	for _, currency := range []string{"ISK", "UGX", "UNKNOWN"} {
		_, err := walletCurrencyExponent(currency)
		assert.Error(t, err)
	}
}
func TestCreemTestModeNeverBypassesSignature(t *testing.T) {
	old := setting.CreemTestMode
	setting.CreemTestMode = true
	t.Cleanup(func() { setting.CreemTestMode = old })
	assert.False(t, verifyCreemSignature("payload", "", ""))
	assert.True(t, verifyCreemSignature("payload", generateCreemSignature("payload", "frozen-secret"), "frozen-secret"))
	assert.False(t, verifyCreemSignature("tampered", generateCreemSignature("payload", "frozen-secret"), "frozen-secret"))
}

func TestWalletOrderActionsRequireRecoverableOriginalCheckout(t *testing.T) {
	t.Setenv("WALLET_PAYMENT_SECRET", strings.Repeat("c", 32))
	t.Setenv("WALLET_PAYMENT_PREVIOUS_SECRETS", "")
	order := &model.TopUp{UserId: 3, TradeNo: "WT-actions", CreateTime: time.Now().Unix(), WalletLifecycleVersion: 1, Status: "pending", CreateState: "ready", PaymentProvider: "stripe", ProviderAccountRef: "merchant", ProviderSessionID: "cs_original"}
	snapshot := service.WalletGatewaySnapshot{Provider: "stripe", CanResume: true, Checkout: &dto.WalletCheckout{Kind: "url", URL: "https://checkout.stripe.com/c/original"}}
	var err error
	order.CheckoutPayload, err = service.SealWalletPayment(snapshot, service.WalletPaymentScope(order))
	require.NoError(t, err)
	actions := WalletTopUpOrderDTO(order, false).AllowedActions
	assert.Contains(t, actions, "resume")
	assert.Contains(t, actions, "cancel")
	snapshot.TokenExpiresAt = time.Now().Unix() - 1
	order.CheckoutPayload, err = service.SealWalletPayment(snapshot, service.WalletPaymentScope(order))
	require.NoError(t, err)
	assert.NotContains(t, WalletTopUpOrderDTO(order, true).AllowedActions, "resume")
	t.Setenv("WALLET_PAYMENT_SECRET", strings.Repeat("x", 32))
	actions = WalletTopUpOrderDTO(order, true).AllowedActions
	assert.NotContains(t, actions, "resume")
	assert.NotContains(t, actions, "cancel")
}

func TestWalletLocalEndResultPreservesProviderFactsAndMoneyPriority(t *testing.T) {
	now := time.Now().Unix()
	for _, tc := range []struct {
		name, status, providerState, localReason, wantStatus, wantGroup string
		created, ended                                                  int64
	}{
		{"unknown creation time", "pending", "unpaid", "", "unavailable", "all", 0, 0},
		{"old legacy pending", "pending", "unpaid", "", "timed_out", "closed", now - 3600, 0},
		{"user ended", "pending", "unpaid", "user_ended", "ended", "closed", now, now},
		{"received money requires credit review", "credit_review", "paid", "user_ended", "credit_review", "review", now - 3600, now - 1800},
		{"paid fact before status update", "pending", "paid", "user_ended", "credit_review", "review", now - 3600, now - 1800},
		{"late credit wins", "success", "paid", "user_ended", "success", "completed", now - 3600, now - 1800},
	} {
		t.Run(tc.name, func(t *testing.T) {
			order := &model.TopUp{Status: tc.status, CreateTime: tc.created, LocalEndedAt: tc.ended, LocalEndReason: tc.localReason, ProviderPaymentState: tc.providerState}
			current := WalletTopUpOrderDTO(order, true)
			assert.Equal(t, tc.wantStatus, current.DisplayStatus)
			assert.Equal(t, tc.wantGroup, current.StatusGroup)
			assert.Nil(t, current.ClosedAt)
			assert.Nil(t, current.CloseReason)
			assert.NotContains(t, current.AllowedActions, "resume")
			assert.NotContains(t, current.AllowedActions, "cancel")
			assert.NotContains(t, current.AllowedActions, "end")
			if tc.wantGroup == "closed" {
				assert.Equal(t, []string{"acknowledge"}, current.AllowedActions)
				assert.Empty(t, WalletTopUpOrderDTO(order, false).AllowedActions)
			}
		})
	}
}

func TestWalletExpiredAttemptsRejectEveryManualRecoveryRoute(t *testing.T) {
	db := walletCallbackDB(t)
	now := time.Now().Unix()
	order := model.TopUp{UserId: 61, TradeNo: "WT-expired-attempt", Status: "pending", CreateTime: now - 3600, WalletLifecycleVersion: 1, CreateState: "ready", Version: 3, PaymentProvider: "stripe", ProviderSessionID: "cs_original", NextCheckAt: now + 600}
	require.NoError(t, db.Create(&order).Error)
	require.NoError(t, db.Create(&model.WalletTopUpFlow{UserID: order.UserId, TopUpID: &order.Id, FlowID: "original-flow", Revision: 2}).Error)
	for _, tc := range []struct {
		name, body, code string
		handler          gin.HandlerFunc
	}{
		{"resume", `{"flow_id":"original-flow","flow_revision":"2"}`, "PAYMENT_NOT_RESUMABLE", ResumeWalletTopUpOrder},
		{"cancel", `{"expected_order_version":"3"}`, "PAYMENT_ATTEMPT_ENDED", CancelWalletTopUpOrder},
		{"end again", `{"expected_order_version":"3"}`, "PAYMENT_ATTEMPT_ENDED", EndWalletTopUpOrder},
		{"self reconcile", `{}`, "PAYMENT_ATTEMPT_ENDED", ReconcileWalletTopUpOrder},
		{"admin reconcile", `{"reason":"verify expired order"}`, "PAYMENT_ATTEMPT_ENDED", AdminReconcileWalletTopUpOrder},
		{"old manual complete", `{"trade_no":"WT-expired-attempt"}`, "PAYMENT_ATTEMPT_ENDED", AdminCompleteTopUp},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Set("id", order.UserId)
			ctx.Params = gin.Params{{Key: "trade_no", Value: order.TradeNo}}
			ctx.Request = httptest.NewRequest(http.MethodPost, "/wallet/operation", strings.NewReader(tc.body))
			tc.handler(ctx)
			assert.Equal(t, http.StatusConflict, response.Code)
			var result struct {
				Code string `json:"code"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			assert.Equal(t, tc.code, result.Code)
		})
	}
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.Equal(t, "pending", order.Status)
	assert.Equal(t, now+600, order.NextCheckAt)
	assert.Empty(t, order.CloseEvidenceRef)
}

func TestWalletEndReturnsResultBeforeExactAcknowledgement(t *testing.T) {
	db := walletCallbackDB(t)
	order := model.TopUp{UserId: 62, TradeNo: "WT-local-end", Status: "pending", CreateTime: time.Now().Unix(), WalletLifecycleVersion: 1, CreateState: "ready", Version: 3, PaymentProvider: "epay"}
	require.NoError(t, db.Create(&order).Error)
	flow := model.WalletTopUpFlow{UserID: order.UserId, TopUpID: &order.Id, FlowID: "flow-a", Revision: 2}
	require.NoError(t, db.Create(&flow).Error)
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Set("id", order.UserId)
	ctx.Params = gin.Params{{Key: "trade_no", Value: order.TradeNo}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/wallet/end", strings.NewReader(`{"expected_order_version":"3"}`))
	EndWalletTopUpOrder(ctx)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result struct {
		Data struct {
			Wallet dto.WalletState `json:"wallet"`
			Order  dto.WalletOrder `json:"order"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.Equal(t, "result", result.Data.Wallet.Flow.Phase)
	assert.False(t, result.Data.Wallet.CanCreate)
	assert.Equal(t, "ended", result.Data.Order.DisplayStatus)
	assert.Equal(t, []string{"acknowledge"}, result.Data.Order.AllowedActions)
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.Equal(t, "pending", order.Status)
	assert.Empty(t, order.CloseEvidenceRef)
	state, err := model.AcknowledgeWalletTopUp(model.WalletTopUpScope{UserID: order.UserId}, flow.FlowID, flow.Revision, order.TradeNo)
	require.NoError(t, err)
	assert.Nil(t, state.Order)
	assert.Nil(t, walletStateDTO(state).Flow.Order)
	// Ending a different owned historical attempt returns the latest current
	// flow and cannot replace it with the historical result.
	current := model.TopUp{UserId: order.UserId, TradeNo: "WT-current-b", Status: "pending", CreateTime: time.Now().Unix(), WalletLifecycleVersion: 1, Version: 1}
	historical := model.TopUp{UserId: order.UserId, TradeNo: "WT-historical-c", Status: "pending", CreateTime: time.Now().Unix(), WalletLifecycleVersion: 1, Version: 1}
	require.NoError(t, db.Create(&current).Error)
	require.NoError(t, db.Create(&historical).Error)
	require.NoError(t, db.Model(&flow).Updates(map[string]any{"top_up_id": current.Id, "flow_id": "flow-b", "revision": 4}).Error)
	response = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(response)
	ctx.Set("id", order.UserId)
	ctx.Params = gin.Params{{Key: "trade_no", Value: historical.TradeNo}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/wallet/end", strings.NewReader(`{"expected_order_version":"1"}`))
	EndWalletTopUpOrder(ctx)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	require.NotNil(t, result.Data.Wallet.Flow.Order)
	assert.Equal(t, current.TradeNo, result.Data.Wallet.Flow.Order.TradeNo)
	assert.Equal(t, historical.TradeNo, result.Data.Order.TradeNo)
	assert.Equal(t, "ended", result.Data.Order.DisplayStatus)
	assert.Empty(t, result.Data.Order.AllowedActions)
}

func TestWalletLegacyHistoryHidesEndedActionsWithoutChangingMirroredPurchases(t *testing.T) {
	db := walletCallbackDB(t)
	require.NoError(t, db.AutoMigrate(&model.SubscriptionOrder{}))
	now := time.Now().Unix()
	orders := []*model.TopUp{
		{TradeNo: "old-wallet", Status: "pending", CreateTime: now - 3600},
		{TradeNo: "unknown-time", Status: "pending"},
		{TradeNo: "paid-review", Status: "credit_review", ProviderPaymentState: "paid", CreateTime: now - 3600},
		{TradeNo: "successful", Status: "success", CreateTime: now - 3600},
		{TradeNo: "member-mirror", Status: "pending", CreateTime: now - 3600},
		{TradeNo: "subscription-mirror", Status: "pending", CreateTime: now - 3600},
	}
	require.NoError(t, db.Create(&orders).Error)
	require.NoError(t, db.Create(&model.MembershipOrder{TradeNo: "member-mirror"}).Error)
	require.NoError(t, db.Create(&model.SubscriptionOrder{TradeNo: "subscription-mirror"}).Error)
	rows, err := walletLegacyHistory(orders)
	require.NoError(t, err)
	encoded, err := common.Marshal(rows)
	require.NoError(t, err)
	var projected []struct {
		Status        string `json:"status"`
		DisplayStatus string `json:"display_status"`
	}
	require.NoError(t, common.Unmarshal(encoded, &projected))
	require.Len(t, projected, len(orders))
	for i, expected := range []string{"expired", "unavailable", "credit_review", "success", "pending", "pending"} {
		assert.Equal(t, expected, projected[i].Status)
	}
	assert.Equal(t, "timed_out", projected[0].DisplayStatus)
	assert.Equal(t, "unavailable", projected[1].DisplayStatus)
	stored, err := model.FindTopUpByTradeNo("old-wallet")
	require.NoError(t, err)
	assert.Equal(t, "pending", stored.Status)
	assert.Zero(t, stored.ClosedAt)
	assert.Empty(t, stored.CloseEvidenceRef)
}

func walletCallbackDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "wallet.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	previous := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		require.NoError(t, sqlDB.Close())
	})
	for _, ddl := range []string{
		"CREATE TABLE users(id INTEGER PRIMARY KEY, telegram_id TEXT, username VARCHAR(20), email VARCHAR(50))",
		"CREATE UNIQUE INDEX uni_users_username ON users(username)",
		"CREATE TABLE tokens(id INTEGER PRIMARY KEY)",
		"CREATE TABLE passkey_credentials(id INTEGER PRIMARY KEY)",
		"CREATE TABLE options(key VARCHAR(255) PRIMARY KEY, value TEXT)",
		"CREATE TABLE midjourneys(id INTEGER PRIMARY KEY, mj_id TEXT, channel_id BIGINT, quota BIGINT, status TEXT)",
		"CREATE TABLE top_ups(id INTEGER PRIMARY KEY, user_id INTEGER, trade_no VARCHAR(255), status VARCHAR(32), create_time INTEGER)",
		"CREATE TABLE user_oauth_bindings(id INTEGER PRIMARY KEY)",
		"CREATE TABLE logs(id INTEGER PRIMARY KEY)",
		"CREATE TABLE quota_data(id INTEGER PRIMARY KEY)",
		"CREATE TABLE tasks(id INTEGER PRIMARY KEY)",
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	ctx := context.Background()
	for _, apply := range []func(context.Context, *gorm.DB, kkaimigrate.Options) (*kkaimigrate.Result, error){
		kkaimigrate.ApplyLegacyBaseline, kkaimigrate.ApplyOutboxEventKeyCompatibility,
		kkaimigrate.ApplyVideoStudioExpand, kkaimigrate.ApplyVideoSampleCategoryExpand,
		kkaimigrate.ApplyImageStudioExpand, kkaimigrate.ApplyAuthenticationExpand,
		kkaimigrate.ApplyRC41ArchitectureExpand, kkaimigrate.ApplyImageAsyncExpand,
		kkaimigrate.ApplyMembershipExpand, kkaimigrate.ApplyTenantOnlineExpansion,
		kkaimigrate.FinalizeTenantOnlineSchema, kkaimigrate.ApplyResellerApplicationExpand,
		kkaimigrate.ApplyRequestTicketExpand, kkaimigrate.ApplyInvoiceExpand,
		kkaimigrate.ApplyWalletPaymentExpand,
	} {
		_, err := apply(ctx, db, kkaimigrate.Options{Offline: true})
		require.NoError(t, err)
	}
	for _, value := range []any{&model.User{}, &model.TopUp{}} {
		statement := &gorm.Statement{DB: db}
		require.NoError(t, statement.Parse(value))
		columns, err := db.Migrator().ColumnTypes(value)
		require.NoError(t, err)
		present := make(map[string]bool, len(columns))
		for _, column := range columns {
			present[column.Name()] = true
		}
		for _, field := range statement.Schema.Fields {
			if field.DBName != "" && !field.IgnoreMigration && !present[field.DBName] {
				require.NoError(t, db.Migrator().AddColumn(value, field.Name))
			}
		}
	}
	require.NoError(t, kkaimigrate.CheckWalletPaymentSchema(ctx, db))
	return db
}

func TestWalletEpayUnknownOrderCallbackPersistsOnlyAuthenticatedPaidFact(t *testing.T) {
	db := walletCallbackDB(t)
	oldAddress, oldPID, oldKey := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey
	t.Cleanup(func() {
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = oldAddress, oldPID, oldKey
	})
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://merchant.example.test", "123", "original-secret"
	// Receiving a previously created payment is independent of new purchases.
	paymentSetting := operation_setting.GetPaymentSetting()
	oldConfirmed := paymentSetting.ComplianceConfirmed
	paymentSetting.ComplianceConfirmed = false
	t.Cleanup(func() { paymentSetting.ComplianceConfirmed = oldConfirmed })
	invoke := func(signatureKey string) string {
		params := epay.GenerateParams(map[string]string{"pid": "123", "type": "alipay", "money": "10.00", "name": "Wallet recharge", "out_trade_no": "WT-unmatched", "trade_no": "provider-actual-transaction", "trade_status": epay.StatusTradeSuccess}, signatureKey)
		form := url.Values{}
		for key, value := range params {
			form.Set(key, value)
		}
		response := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(response)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/epay/notify", strings.NewReader(form.Encode()))
		ctx.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		EpayNotify(ctx)
		return response.Body.String()
	}
	assert.Equal(t, "fail", invoke("wrong-secret"))
	var count int64
	require.NoError(t, db.Model(&model.WalletPaymentReceipt{}).Count(&count).Error)
	assert.Zero(t, count)
	assert.Equal(t, "success", invoke("original-secret"))
	assert.Equal(t, "success", invoke("original-secret"))
	var receipts []model.WalletPaymentReceipt
	require.NoError(t, db.Find(&receipts).Error)
	require.Len(t, receipts, 1)
	assert.Nil(t, receipts[0].TopUpID)
	assert.Equal(t, "review", receipts[0].ProcessingStatus)
	assert.Equal(t, "UNMATCHED_PAYMENT", receipts[0].ExceptionReason)
	assert.Equal(t, "payment-operations", receipts[0].ReviewOwner)
	assert.EqualValues(t, 1000, receipts[0].AmountMinor)
	require.NoError(t, db.Model(&model.TopUp{}).Count(&count).Error)
	assert.Zero(t, count)
	// Complete physical tables without the canonical migration receipts still
	// cannot ACK a verified callback and silently lose the payment.
	unreceipted, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "unreceipted.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := unreceipted.DB()
	require.NoError(t, err)
	defer sqlDB.Close()
	require.NoError(t, unreceipted.AutoMigrate(&model.TopUp{}, &model.WalletTopUpFlow{}, &model.WalletPaymentReceipt{}, &model.WalletPaymentEvent{}, &model.WalletTopUpRun{}))
	model.DB = unreceipted
	assert.True(t, model.WalletPaymentSchemaAvailable())
	assert.Equal(t, "fail", invoke("original-secret"))
	require.NoError(t, unreceipted.Model(&model.WalletPaymentReceipt{}).Count(&count).Error)
	assert.Zero(t, count)
	model.DB = db
}

// A transport interface is the deterministic boundary for callback timing:
// callbacks perform no HTTP; only the independently invoked reconciler does.
type walletCallbackTransport func(*http.Request) (*http.Response, error)

func (transport walletCallbackTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}
func TestWalletEpayPaidCallbackPersistsBeforeAckWithoutMerchantHTTP(t *testing.T) {
	previousRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedis })
	db := walletCallbackDB(t)
	t.Setenv("WALLET_PAYMENT_SECRET", strings.Repeat("e", 32))
	t.Setenv("WALLET_PAYMENT_PREVIOUS_SECRETS", "")
	t.Setenv("TOPUP_REBATE_ACTIVE_FROM_ID", "1")
	user := model.User{Id: 51, Username: "epay-callback", AffCode: "epay-callback", Group: "default", Quota: 10}
	require.NoError(t, db.Create(&user).Error)
	now := common.GetTimestamp()
	order := model.TopUp{UserId: user.Id, TradeNo: "WT-fast-callback", Status: "pending", CreateTime: now - 120, WalletLifecycleVersion: 1, PaymentProvider: "epay", PaymentMethod: "alipay", ProviderAccountRef: "original-account", ProviderEnvironment: "prod", PaidMinor: 1000, Currency: "CNY", CurrencyExponent: 2, PricingMode: "fixed", CreditedQuota: 500, CreateState: "ready"}
	snapshot := service.WalletGatewaySnapshot{Provider: "epay", APIURL: "https://original-merchant.example.test", MerchantID: "original-merchant", APIKey: "original-secret", Environment: "prod"}
	var err error
	order.CheckoutPayload, err = service.SealWalletPayment(snapshot, service.WalletPaymentScope(&order))
	require.NoError(t, err)
	require.NoError(t, db.Create(&order).Error)
	requests := 0
	oldClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = oldClient })
	http.DefaultClient = &http.Client{Transport: walletCallbackTransport(func(request *http.Request) (*http.Response, error) {
		requests++
		assert.Equal(t, "original-merchant.example.test", request.URL.Host)
		assert.Equal(t, "original-merchant", request.URL.Query().Get("pid"))
		assert.Equal(t, "original-secret", request.URL.Query().Get("key"))
		body, err := common.Marshal(map[string]any{"code": 1, "status": 1, "pid": "original-merchant", "out_trade_no": order.TradeNo, "trade_no": "provider-fast-payment", "money": "10.00", "currency": "CNY", "endtime": time.Unix(now-10, 0).In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02 15:04:05")})
		require.NoError(t, err)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})}
	invoke := func() string {
		params := epay.GenerateParams(map[string]string{"pid": "original-merchant", "type": "alipay", "money": "10.00", "name": "Wallet recharge", "out_trade_no": order.TradeNo, "trade_no": "provider-fast-payment", "trade_status": epay.StatusTradeSuccess}, "original-secret")
		form := url.Values{}
		for key, value := range params {
			form.Set(key, value)
		}
		response := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(response)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/epay/notify", strings.NewReader(form.Encode()))
		ctx.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		EpayNotify(ctx)
		return response.Body.String()
	}
	assert.Equal(t, "success", invoke())
	assert.Equal(t, "success", invoke())
	assert.Zero(t, requests, "merchant latency must not delay payment ACK")
	var receipts []model.WalletPaymentReceipt
	require.NoError(t, db.Find(&receipts).Error)
	require.Len(t, receipts, 1)
	assert.Equal(t, "review", receipts[0].ProcessingStatus)
	assert.Zero(t, receipts[0].PaidAt)
	assert.Equal(t, "PAYMENT_PROOF_INCOMPLETE", receipts[0].ExceptionReason)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.EqualValues(t, 10, user.Quota)
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.GreaterOrEqual(t, order.NextCheckAt, now)
	classification, err := service.ReconcileWalletTopUp(context.Background(), order.TradeNo)
	require.NoError(t, err)
	assert.Equal(t, "credited", classification)
	assert.Equal(t, 1, requests)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.EqualValues(t, 510, user.Quota)
	require.NoError(t, db.First(&receipts[0], receipts[0].ID).Error)
	assert.Equal(t, "credited", receipts[0].ProcessingStatus)
	assert.Equal(t, now-10, receipts[0].PaidAt)
	require.NoError(t, db.Exec("CREATE TRIGGER reject_wallet_receipt BEFORE INSERT ON wallet_payment_receipts BEGIN SELECT RAISE(FAIL, 'receipt unavailable'); END").Error)
	assert.Equal(t, "fail", invoke(), "failed durable storage must never ACK a paid fact")
	assert.Equal(t, 1, requests)
}
