package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func shopPaymentIntegrationDB(t *testing.T) (*gorm.DB, model.ShopOrder, model.ShopCommand) {
	t.Helper()
	shopTestKeys(t)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "payment.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(append(model.ShopSchemaModels(), &model.User{}, &model.KKAIOutboxEvent{}, &model.TopUp{})...))
	oldAddress, oldID, oldKey, oldCallback, oldMethods := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey, operation_setting.CustomCallbackAddress, operation_setting.PayMethods
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://gateway.invalid", "test-merchant", "test-secret"
	operation_setting.CustomCallbackAddress = "https://shop.invalid"
	operation_setting.PayMethods = []map[string]string{{"type": "alipay", "name": "Alipay"}}
	t.Cleanup(func() {
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey, operation_setting.CustomCallbackAddress, operation_setting.PayMethods = oldAddress, oldID, oldKey, oldCallback, oldMethods
	})
	require.NoError(t, db.Create(&model.User{Id: 1, TenantID: model.PlatformTenantID, Username: "payment-buyer", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, Quota: 12345}).Error)
	require.NoError(t, db.Create(&model.ShopSettings{ID: 1, Version: 1, SalesEnabled: true, Provider: "epay", ProviderAccountRef: "epay:test-merchant", Environment: "test", Currency: "CNY", CurrencyExponent: 2, MinAmountMinor: 1, MaxAmountMinor: 10000}).Error)
	product := model.ShopProduct{ShopRecord: model.ShopRecord{PublicID: "sp_fixture", Version: 1}, TenantID: model.PlatformTenantID, DeliveryType: "manual", ManualCapacity: 1, ManualUsed: 1}
	require.NoError(t, db.Create(&product).Error)
	snapshot, err := common.Marshal(model.ShopOrderSnapshot{DeliveryType: "manual", Terms: model.ShopProductTerms{TemplateVersion: 1}})
	require.NoError(t, err)
	now := time.Now().Unix()
	order := model.ShopOrder{ShopRecord: model.ShopRecord{PublicID: "so_fixture", Version: 1, CreatedAt: now - 60}, TenantID: model.PlatformTenantID, UserID: 1, ProductID: product.ID, SnapshotJSON: string(snapshot), Currency: "CNY", CurrencyExponent: 2, AmountMinor: 100, PurchaseState: "open", PaymentState: "unpaid", DeliveryState: "not_ready", ReservationExpiresAt: now + 600, ManualCapacityHeld: true, InputCompleteAt: now}
	require.NoError(t, db.Create(&order).Error)
	command := model.ShopCommand{ActorID: 1, TenantID: model.PlatformTenantID, Key: "payment-fixture-key", RequestHash: strings.Repeat("a", 64), HashKeyVersion: "old", ExpectedVersion: 1, Now: now}
	return db, order, command
}

func TestShopPaymentRejectionDistinguishesCommittedAttempt(t *testing.T) {
	db, order, command := shopPaymentIntegrationDB(t)
	stale := command
	stale.ExpectedVersion++
	_, err := CreateShopPayment(context.Background(), db, stale, order.PublicID, "alipay")
	require.ErrorIs(t, err, model.ErrShopVersion)
	var rejection *ShopPaymentNotCommittedError
	require.ErrorAs(t, err, &rejection)
	var count int64
	require.NoError(t, db.Model(&model.ShopPaymentAttempt{}).Count(&count).Error)
	assert.Zero(t, count)
	// A failure while saving checkout occurs after the attempt transaction committed.
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:reject-checkout-save", func(tx *gorm.DB) {
		if tx.Statement.Table == "shop_payment_attempts" {
			tx.AddError(model.ErrShopVersion)
		}
	}))
	_, err = CreateShopPayment(context.Background(), db, command, order.PublicID, "alipay")
	require.ErrorIs(t, err, model.ErrShopVersion)
	assert.False(t, errors.As(err, &rejection), "the same HTTP conflict must not authorize a new request key after commit")
	require.NoError(t, db.Callback().Update().Remove("test:reject-checkout-save"))
	require.NoError(t, db.Model(&model.ShopPaymentAttempt{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	replayed, err := CreateShopPayment(context.Background(), db, command, order.PublicID, "alipay")
	require.NoError(t, err)
	assert.Equal(t, "creating", replayed.Attempt.CreateState)
	require.NoError(t, db.Model(&model.ShopPaymentAttempt{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestShopSignedCallbackAndQueryNeverCreditRecharge(t *testing.T) {
	db, order, command := shopPaymentIntegrationDB(t)
	var tradeNo string
	paidTime := time.Unix(command.Now, 0).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")
	gateway := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-merchant", r.URL.Query().Get("pid"))
		assert.Equal(t, tradeNo, r.URL.Query().Get("out_trade_no"))
		raw, err := common.Marshal(map[string]any{"code": 1, "pid": "test-merchant", "status": 1, "trade_no": "provider-trade", "out_trade_no": tradeNo, "money": "1.00", "endtime": paidTime})
		if !assert.NoError(t, err) {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	defer gateway.Close()
	operation_setting.PayAddress = gateway.URL
	oldClient := http.DefaultClient
	http.DefaultClient = gateway.Client()
	t.Cleanup(func() { http.DefaultClient = oldClient })
	result, err := CreateShopPayment(context.Background(), db, command, order.PublicID, "alipay")
	require.NoError(t, err)
	require.NotNil(t, result.Checkout)
	tradeNo = result.Attempt.TradeNo
	params, err := epay.GenerateParamsWithError(map[string]string{"pid": "test-merchant", "trade_no": "provider-trade", "out_trade_no": tradeNo, "money": "1.00", "trade_status": epay.StatusTradeSuccess, "type": "alipay"}, "test-secret")
	require.NoError(t, err)
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	tampered := values.Encode()
	invalid, err := url.ParseQuery(tampered)
	require.NoError(t, err)
	invalid.Set("money", "0.01")
	accepted, err := HandleShopEpayCallback(context.Background(), db, invalid)
	assert.False(t, accepted)
	require.Error(t, err)
	for range 2 {
		accepted, err = HandleShopEpayCallback(context.Background(), db, values)
		require.NoError(t, err)
		require.True(t, accepted)
	}
	var receipt model.ShopPaymentReceipt
	require.NoError(t, db.First(&receipt).Error)
	_, err = model.MatchShopPaymentReceipt(db, receipt.ID, command.Now)
	require.NoError(t, err)
	require.NoError(t, db.First(&order, order.ID).Error)
	assert.NotEqual(t, "paid", order.PaymentState, "callback without reliable paid time requires original-channel query")
	_, err = model.MatchShopPaymentReceipt(db, receipt.ID, command.Now)
	require.NoError(t, err)
	var count int64
	require.NoError(t, db.Model(&model.ShopEvent{}).Where("order_id = ? AND kind = ? AND visibility = ?", order.ID, "payment_received", "public").Count(&count).Error)
	assert.Zero(t, count, "review and its retry must not announce confirmed payment")
	require.NoError(t, ReconcileShopPayment(context.Background(), db, result.Attempt.PublicID))
	_, err = model.MatchShopPaymentReceipt(db, receipt.ID, command.Now)
	require.NoError(t, err)
	_, err = model.MatchShopPaymentReceipt(db, receipt.ID, command.Now)
	require.NoError(t, err)
	require.NoError(t, db.First(&order, order.ID).Error)
	assert.Equal(t, "paid", order.PaymentState)
	require.NoError(t, db.Model(&model.ShopEvent{}).Where("order_id = ? AND receipt_id = ? AND kind = ? AND visibility = ?", order.ID, receipt.ID, "payment_received", "public").Count(&count).Error)
	assert.Equal(t, int64(1), count, "callback, query and matching retries represent one received payment")
	require.NoError(t, db.Model(&model.ShopPaymentReceipt{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", model.ShopTopicFulfillment).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	require.NoError(t, db.Model(&model.TopUp{}).Count(&count).Error)
	assert.Zero(t, count)
	var buyer model.User
	require.NoError(t, db.First(&buyer, 1).Error)
	assert.Equal(t, int64(12345), buyer.Quota)
}
