package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func membershipPaymentRecoveryFixture(t *testing.T, address string) (*gorm.DB, *model.MembershipOrder) {
	t.Helper()
	if !model.MembershipEnabled() {
		t.Skip("requires KKAI_MEMBERSHIP_ENABLED=true")
	}
	t.Cleanup(func() { require.NoError(t, common.InitNodeRoleFromEnvironment()) })
	t.Setenv("KKAI_NODE_ROLE", "leader")
	t.Setenv("DISABLE_BACKGROUND_TASKS", "false")
	t.Setenv("KKAI_MEMBERSHIP_PAYMENT_RECONCILE_ENABLED", "true")
	t.Setenv("KKAI_MEMBERSHIP_PAYMENT_FULFILL_ENABLED", "true")
	t.Setenv("WALLET_PAYMENT_SECRET", "membership-original-encryption-secret-32")
	require.NoError(t, common.InitNodeRoleFromEnvironment())
	db := walletReceiptRecoveryDB(t)
	mainDB, logDB := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() { common.SetDatabaseTypes(mainDB, logDB) })
	for _, apply := range []func(context.Context, *gorm.DB, kkaimigrate.Options) (*kkaimigrate.Result, error){
		kkaimigrate.ApplyShopExpand, kkaimigrate.ApplyMembershipPaymentExpand,
	} {
		_, err := apply(context.Background(), db, kkaimigrate.Options{Offline: true})
		require.NoError(t, err)
	}
	require.NoError(t, CheckMembershipPaymentRuntimeSchema(context.Background(), db))
	require.NoError(t, db.Create(&model.User{Id: 701, Username: "member-recovery", Group: "default", Quota: 123}).Error)
	catalog, err := model.PublishMembershipCatalog(model.MembershipCatalog{SalesEnabled: true,
		Tiers: []model.MembershipTier{{ID: "gold", Name: "Gold", Rank: 1, Quota: 1000, PriceMinor: 9900, Currency: "CNY", Enabled: true}},
	}, 0, 701)
	require.NoError(t, err)
	skeleton := model.MembershipOrder{UserID: 701, TradeNo: "MEMBER-recovery", Provider: model.PaymentProviderEpay, PaymentMethod: "wxpay", Currency: "CNY"}
	snapshot, err := CreateMembershipEpaySnapshot(&skeleton, address, "merchant-original", "merchant-key-original")
	require.NoError(t, err)
	order, err := model.CreateMembershipOrder(model.MembershipOrderInput{UserID: 701, TierID: "gold", ReleaseID: catalog.ReleaseID,
		TradeNo: skeleton.TradeNo, Provider: skeleton.Provider, PaymentMethod: skeleton.PaymentMethod, ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
		MerchantID: "merchant-original", PaymentSnapshot: snapshot,
	})
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.MembershipPaymentCheck{}).Where("order_id = ?", order.ID).Update("next_check_at", common.GetTimestamp()).Error)
	return db, order
}

func membershipPaymentProvider(t *testing.T, status string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	requests := &atomic.Int32{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		assert.Equal(t, "/api.php", r.URL.Path)
		assert.Equal(t, "merchant-original", r.URL.Query().Get("pid"))
		assert.Equal(t, "merchant-key-original", r.URL.Query().Get("key"))
		assert.Equal(t, "MEMBER-recovery", r.URL.Query().Get("out_trade_no"))
		body, err := common.Marshal(map[string]any{"code": 1, "status": status, "pid": "merchant-original", "out_trade_no": "MEMBER-recovery",
			"trade_no": "receipt-original", "money": "99.00", "type": "wxpay2", "endtime": time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02 15:04:05")})
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, err = w.Write(body)
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	return server, requests
}

func TestMembershipPaymentRecoveryWithoutCallbackUsesOriginalSnapshot(t *testing.T) {
	server, requests := membershipPaymentProvider(t, "1")
	db, order := membershipPaymentRecoveryFixture(t, server.URL)
	// Old orders retain their original merchant key after encryption-key rotation.
	t.Setenv("WALLET_PAYMENT_SECRET", "membership-rotated-encryption-secret-32")
	t.Setenv("WALLET_PAYMENT_PREVIOUS_SECRETS", `["membership-original-encryption-secret-32"]`)
	result, err := reconcileMembershipPayment(context.Background(), order.ID, server.Client())
	require.NoError(t, err)
	assert.Equal(t, "complete", result)
	require.NoError(t, db.First(order, order.ID).Error)
	assert.Equal(t, model.MembershipOrderSuccess, order.Status)
	var evidence model.MembershipPaymentEvidence
	require.NoError(t, db.First(&evidence, "order_id = ?", order.ID).Error)
	assert.Equal(t, model.MembershipPaymentMerchantQuery, evidence.Source)
	assert.Equal(t, model.MembershipPaymentComplete, evidence.Status)
	assert.Positive(t, evidence.ProviderPaidAt)
	var account model.MembershipAccount
	require.NoError(t, db.First(&account, order.AccountID).Error)
	assert.EqualValues(t, 1000, account.AmountTotal)
	originalAccount := account
	_, err = model.RetryVerifiedMembershipPayment(context.Background(), evidence.ID)
	require.NoError(t, err)
	require.NoError(t, db.First(&account, order.AccountID).Error)
	assert.Equal(t, originalAccount, account, "a replay must not renew duration or add quota")
	assert.EqualValues(t, 1, requests.Load())
	var check model.MembershipPaymentCheck
	require.NoError(t, db.First(&check, "order_id = ?", order.ID).Error)
	assert.Zero(t, check.CallbackReceivedAt, "no notification is needed for recovery")
	var user model.User
	require.NoError(t, db.First(&user, order.UserID).Error)
	assert.EqualValues(t, 123, user.Quota)
}

func TestMembershipPaymentMissingOldEncryptionKeyRemainsRetryable(t *testing.T) {
	server, requests := membershipPaymentProvider(t, "1")
	db, order := membershipPaymentRecoveryFixture(t, server.URL)
	t.Setenv("WALLET_PAYMENT_SECRET", "membership-rotated-encryption-secret-32")
	t.Setenv("WALLET_PAYMENT_PREVIOUS_SECRETS", "")
	result, err := reconcileMembershipPayment(context.Background(), order.ID, server.Client())
	require.NoError(t, err)
	assert.Equal(t, "pending", result)
	var check model.MembershipPaymentCheck
	require.NoError(t, db.First(&check, "order_id = ?", order.ID).Error)
	assert.Equal(t, model.MembershipPaymentPending, check.Status)
	assert.Equal(t, "PAYMENT_SNAPSHOT_UNAVAILABLE", check.LastError)
	assert.Zero(t, requests.Load())
	t.Setenv("WALLET_PAYMENT_PREVIOUS_SECRETS", `["membership-original-encryption-secret-32"]`)
	require.NoError(t, db.Model(&check).Update("next_check_at", common.GetTimestamp()).Error)
	result, err = reconcileMembershipPayment(context.Background(), order.ID, server.Client())
	require.NoError(t, err)
	assert.Equal(t, "complete", result)
	assert.EqualValues(t, 1, requests.Load())
}

func TestMembershipPaymentTransientHTMLResponseRecoversOnLaterPaidQuery(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			_, err := w.Write([]byte("<html>temporary gateway error</html>"))
			assert.NoError(t, err)
			return
		}
		body, err := common.Marshal(map[string]any{"code": 1, "status": 1, "pid": "merchant-original", "out_trade_no": "MEMBER-recovery",
			"trade_no": "receipt-after-recovery", "money": "99.00", "endtime": time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02 15:04:05")})
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, err = w.Write(body)
		assert.NoError(t, err)
	}))
	defer server.Close()
	db, order := membershipPaymentRecoveryFixture(t, server.URL)
	result, err := reconcileMembershipPayment(context.Background(), order.ID, server.Client())
	require.NoError(t, err)
	assert.Equal(t, "pending", result)
	var check model.MembershipPaymentCheck
	require.NoError(t, db.First(&check, "order_id = ?", order.ID).Error)
	assert.Equal(t, model.MembershipPaymentPending, check.Status)
	assert.Equal(t, "PAYMENT_PROVIDER_UNAVAILABLE", check.LastError)
	assert.Greater(t, check.NextCheckAt, common.GetTimestamp())
	var evidenceCount int64
	require.NoError(t, db.Model(&model.MembershipPaymentEvidence{}).Count(&evidenceCount).Error)
	assert.Zero(t, evidenceCount)
	require.NoError(t, db.Model(&check).Update("next_check_at", common.GetTimestamp()).Error)
	result, err = reconcileMembershipPayment(context.Background(), order.ID, server.Client())
	require.NoError(t, err)
	assert.Equal(t, "complete", result)
	require.NoError(t, db.First(order, order.ID).Error)
	assert.Equal(t, model.MembershipOrderSuccess, order.Status)
	assert.Equal(t, "receipt-after-recovery", order.ReceiptID)
	assert.EqualValues(t, 2, requests.Load())
}

func TestMembershipPaymentSignedCallbackOnlyWakesUnpaidQuery(t *testing.T) {
	server, requests := membershipPaymentProvider(t, "0")
	db, order := membershipPaymentRecoveryFixture(t, server.URL)
	require.NoError(t, db.Model(&model.MembershipPaymentCheck{}).Where("order_id = ?", order.ID).Update("next_check_at", common.GetTimestamp()+300).Error)
	params, err := epay.GenerateParamsWithError(map[string]string{"pid": "merchant-original", "type": "wxpay2", "out_trade_no": order.TradeNo,
		"trade_no": "client-claimed-receipt", "trade_status": epay.StatusTradeSuccess, "money": "99.00", "name": "Gold"}, "merchant-key-original")
	require.NoError(t, err)
	params["money"] = "999.00"
	require.ErrorIs(t, HandleMembershipEpayCallback(context.Background(), params), ErrMembershipPaymentEvidence)
	var check model.MembershipPaymentCheck
	require.NoError(t, db.First(&check, "order_id = ?", order.ID).Error)
	assert.Zero(t, check.CallbackReceivedAt)
	assert.Greater(t, check.NextCheckAt, common.GetTimestamp())
	params["money"] = "99.00"
	require.NoError(t, HandleMembershipEpayCallback(context.Background(), params))
	require.NoError(t, HandleMembershipEpayCallback(context.Background(), params))
	assert.Zero(t, requests.Load(), "callback receipt never performs merchant HTTP inline")
	require.NoError(t, db.First(&check, "order_id = ?", order.ID).Error)
	assert.Positive(t, check.CallbackReceivedAt)
	assert.LessOrEqual(t, check.NextCheckAt, common.GetTimestamp())
	result, err := reconcileMembershipPayment(context.Background(), order.ID, server.Client())
	require.NoError(t, err)
	assert.Equal(t, "pending", result)
	for _, value := range []any{&model.MembershipPaymentEvidence{}, &model.MembershipPaymentReceipt{}, &model.MembershipAccount{}} {
		var count int64
		require.NoError(t, db.Model(value).Count(&count).Error)
		assert.Zero(t, count, "a signed success claim is not payment evidence")
	}
	require.NoError(t, db.First(order, order.ID).Error)
	assert.Equal(t, model.MembershipOrderPending, order.Status)
}

func TestMembershipPaymentEvidenceSurvivesFulfillmentFailure(t *testing.T) {
	server, requests := membershipPaymentProvider(t, "1")
	db, order := membershipPaymentRecoveryFixture(t, server.URL)
	const failAccount = "test:fail_membership_activation"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(failAccount, func(tx *gorm.DB) {
		if tx.Statement.Table == (model.MembershipAccount{}).TableName() {
			tx.AddError(errors.New("injected account write failure"))
		}
	}))
	t.Cleanup(func() { require.NoError(t, db.Callback().Create().Remove(failAccount)) })
	_, err := reconcileMembershipPayment(context.Background(), order.ID, server.Client())
	require.ErrorContains(t, err, "injected account write failure")
	var evidence model.MembershipPaymentEvidence
	require.NoError(t, db.First(&evidence, "order_id = ?", order.ID).Error)
	assert.Equal(t, model.MembershipPaymentPending, evidence.Status)
	assert.Equal(t, "receipt-original", evidence.ReceiptID)
	var receipts int64
	require.NoError(t, db.Model(&model.MembershipPaymentReceipt{}).Count(&receipts).Error)
	assert.Zero(t, receipts, "the fulfillment transaction rolled back independently")
	require.NoError(t, db.First(order, order.ID).Error)
	assert.Equal(t, model.MembershipOrderPending, order.Status)
	require.NoError(t, db.Callback().Create().Remove(failAccount))
	require.NoError(t, db.Model(&evidence).Update("retry_at", common.GetTimestamp()).Error)
	counts, err := RetryDueMembershipPayments(context.Background(), 20)
	require.NoError(t, err)
	assert.Equal(t, 1, counts["complete"])
	counts, err = RetryDueMembershipPayments(context.Background(), 20)
	require.NoError(t, err)
	assert.Zero(t, counts["complete"])
	assert.EqualValues(t, 1, requests.Load(), "fulfillment resumes durable evidence without querying again")
	require.NoError(t, db.First(order, order.ID).Error)
	assert.Equal(t, model.MembershipOrderSuccess, order.Status)
}

func TestMembershipPaymentShadowVerificationRequiresExplicitFulfillment(t *testing.T) {
	server, requests := membershipPaymentProvider(t, "1")
	db, order := membershipPaymentRecoveryFixture(t, server.URL)
	t.Setenv("KKAI_MEMBERSHIP_PAYMENT_FULFILL_ENABLED", "false")
	result, err := reconcileMembershipPayment(context.Background(), order.ID, server.Client())
	require.NoError(t, err)
	assert.Equal(t, "processing", result)
	var evidence model.MembershipPaymentEvidence
	require.NoError(t, db.First(&evidence, "order_id = ?", order.ID).Error)
	assert.Equal(t, model.MembershipPaymentPending, evidence.Status)
	_, err = RetryDueMembershipPayments(context.Background(), 20)
	require.Error(t, err)
	var accounts int64
	require.NoError(t, db.Model(&model.MembershipAccount{}).Count(&accounts).Error)
	assert.Zero(t, accounts)
	t.Setenv("KKAI_MEMBERSHIP_PAYMENT_FULFILL_ENABLED", "true")
	t.Setenv("KKAI_MEMBERSHIP_PAYMENT_RECONCILE_ENABLED", "false")
	counts, err := RetryDueMembershipPayments(context.Background(), 20)
	require.NoError(t, err)
	assert.Equal(t, 1, counts["complete"], "querying and fulfillment have independent gates")
	assert.EqualValues(t, 1, requests.Load())
}

func TestMembershipPaymentWorkersRejectServingAndDisabledRoles(t *testing.T) {
	server, requests := membershipPaymentProvider(t, "1")
	db, order := membershipPaymentRecoveryFixture(t, server.URL)
	for _, tc := range []struct{ role, disabled, reconcile string }{{"serving", "false", "true"}, {"leader", "true", "true"}, {"leader", "false", "false"}} {
		t.Run(tc.role+tc.disabled+tc.reconcile, func(t *testing.T) {
			t.Setenv("KKAI_NODE_ROLE", tc.role)
			t.Setenv("DISABLE_BACKGROUND_TASKS", tc.disabled)
			t.Setenv("KKAI_MEMBERSHIP_PAYMENT_RECONCILE_ENABLED", tc.reconcile)
			require.NoError(t, common.InitNodeRoleFromEnvironment())
			_, err := reconcileMembershipPayment(context.Background(), order.ID, server.Client())
			require.Error(t, err)
			var check model.MembershipPaymentCheck
			require.NoError(t, db.First(&check, "order_id = ?", order.ID).Error)
			assert.Zero(t, check.Attempts)
		})
	}
	assert.Zero(t, requests.Load())
}

func TestMembershipPaymentRuntimeSchemaRejectsUnreceiptedTables(t *testing.T) {
	db := walletReceiptRecoveryDB(t)
	require.NoError(t, db.AutoMigrate(model.MembershipPaymentModels()...))
	require.Error(t, CheckMembershipPaymentRuntimeSchema(context.Background(), db))
	assert.False(t, model.MembershipPaymentSchemaAvailable())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, CheckMembershipPaymentRuntimeSchema(ctx, db), context.Canceled)
}
