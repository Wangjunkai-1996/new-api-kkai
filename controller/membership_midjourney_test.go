package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func membershipMidjourneyFixture(t *testing.T, response http.HandlerFunc) (*gorm.DB, *gin.Engine, *atomic.Int32) {
	t.Helper()
	db, _ := membershipControllerFixture(t, model.PaymentProviderEpay, "CNY")
	require.NoError(t, db.AutoMigrate(&model.Midjourney{}, &model.UserSubscription{}, &model.Channel{}, &model.Token{}))
	oldRedis, oldMemory := common.RedisEnabled, common.MemoryCacheEnabled
	oldQuotaPerUnit, oldConsumeLog := common.QuotaPerUnit, common.LogConsumeEnabled
	oldPrices := ratio_setting.ModelPrice2JSONString()
	oldGroups := ratio_setting.GroupRatio2JSONString()
	common.RedisEnabled, common.MemoryCacheEnabled = false, false
	common.QuotaPerUnit, common.LogConsumeEnabled = 1000, false
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"mj_imagine":0.1,"swap_face":0.1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	t.Cleanup(func() {
		common.RedisEnabled, common.MemoryCacheEnabled = oldRedis, oldMemory
		common.QuotaPerUnit, common.LogConsumeEnabled = oldQuotaPerUnit, oldConsumeLog
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
	})
	service.InitHttpClient()
	calls := &atomic.Int32{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if response != nil {
			response(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// A real submission response that creates no charge or notification.
		_, _ = w.Write([]byte(`{"code":23,"description":"queue full"}`))
	}))
	t.Cleanup(upstream.Close)
	require.NoError(t, db.Create(&model.Channel{Id: 1, Type: constant.ChannelTypeMidjourney, Status: common.ChannelStatusEnabled, BaseURL: &upstream.URL, Key: "mj-test-key"}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 1, UserId: 7, Key: "mj-test-token", RemainQuota: 1000, Status: common.TokenStatusEnabled}).Error)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(constant.ContextKeyUserId), 7)
		c.Set(string(constant.ContextKeyUserGroup), "default")
		c.Set(string(constant.ContextKeyUsingGroup), "default")
		c.Set(string(constant.ContextKeyOriginalModel), "mj_imagine")
		c.Set(string(constant.ContextKeyChannelBaseUrl), upstream.URL)
		c.Set(string(constant.ContextKeyChannelId), 1)
		c.Set(string(constant.ContextKeyChannelType), constant.ChannelTypeMidjourney)
		c.Set(string(constant.ContextKeyTokenId), 1)
		c.Set(string(constant.ContextKeyTokenKey), "mj-test-token")
		c.Set("relay_mode", relayconstant.Path2RelayModeMidjourney(c.Request.URL.Path))
		c.Next()
	})
	router.POST("/mj/submit/imagine", RelayMidjourney)
	router.POST("/fast/mj/submit/imagine", RelayMidjourney)
	router.POST("/mj/insight-face/swap", RelayMidjourney)
	router.GET("/mj/task/:id/fetch", RelayMidjourney)
	router.POST("/mj/task/list-by-condition", RelayMidjourney)
	return db, router, calls
}

func activateMidjourneyTestMembership(t *testing.T, quota int64) *model.MembershipAccount {
	t.Helper()
	current, err := model.GetMembershipCatalog()
	require.NoError(t, err)
	catalog, err := model.PublishMembershipCatalog(model.MembershipCatalog{
		SalesEnabled: true,
		Tiers:        []model.MembershipTier{{ID: "gold", Name: "Gold", Rank: 1, Style: "gold", Currency: "USD", PriceMinor: 1000, Quota: quota, Enabled: true, Discounts: map[string]float64{"default": 0.5}}},
		Groups:       []model.MembershipGroup{{Key: "default", Enabled: true, Published: true}},
	}, current.ReleaseID, 7)
	require.NoError(t, err)
	order, err := model.CreateMembershipOrder(model.MembershipOrderInput{UserID: 7, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: model.PaymentProviderEpay, PaymentMethod: "alipay", TradeNo: "mj-membership", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	require.NoError(t, err)
	order, err = model.CompleteMembershipPayment(model.MembershipPaymentInput{TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: "mj-paid", AmountMinor: order.AmountMinor, Currency: order.Currency, OneTime: true, Paid: true})
	require.NoError(t, err)
	require.Equal(t, model.MembershipOrderSuccess, order.Status)
	self, err := model.GetMembershipSelf(7)
	require.NoError(t, err)
	require.NotNil(t, self.Account)
	return self.Account
}

func TestMembershipMidjourneyInsufficientFundingDoesNotSubmit(t *testing.T) {
	if !model.MembershipEnabled() {
		t.Skip("requires KKAI_MEMBERSHIP_ENABLED=true")
	}
	for _, tc := range []struct {
		name, path, body string
	}{
		{"imagine", "/mj/submit/imagine", `{"prompt":"cat"}`},
		{"mode_route", "/fast/mj/submit/imagine", `{"prompt":"cat"}`},
		{"swap_face", "/mj/insight-face/swap", `{"sourceBase64":"source","targetBase64":"target"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, router, calls := membershipMidjourneyFixture(t, nil)
			require.NoError(t, db.Model(&model.User{}).Where("id = ?", 7).Update("quota", 0).Error)
			account := activateMidjourneyTestMembership(t, 10)
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "midjourney_authorization_failed")
			assert.Zero(t, calls.Load())
			var taskCount int64
			require.NoError(t, db.Model(&model.Midjourney{}).Count(&taskCount).Error)
			assert.Zero(t, taskCount)
			var user model.User
			require.NoError(t, db.First(&user, 7).Error)
			assert.Zero(t, user.Quota)
			require.NoError(t, db.First(account, account.ID).Error)
			assert.EqualValues(t, 10, account.Available())
		})
	}
}

func TestMembershipMidjourneySettlesOriginalSourceAfterPolling(t *testing.T) {
	if !model.MembershipEnabled() {
		t.Skip("requires KKAI_MEMBERSHIP_ENABLED=true")
	}
	for _, tc := range []struct {
		name, path, body, terminal string
		memberQuota                int64
		wallet, charge             int
	}{
		{"member_success", "/mj/submit/imagine", `{"prompt":"cat"}`, "SUCCESS", 1000, 0, 50},
		{"member_failure", "/mj/insight-face/swap", `{"sourceBase64":"source","targetBase64":"target"}`, "FAILURE", 1000, 0, 50},
		{"wallet_fallback", "/mj/submit/imagine", `{"prompt":"cat"}`, "SUCCESS", 10, 123, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var polls atomic.Int32
			db, router, calls := membershipMidjourneyFixture(t, func(w http.ResponseWriter, request *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(request.URL.Path, "/list-by-condition") {
					status := tc.terminal
					if polls.Add(1) == 1 && tc.name == "member_success" {
						status = "IN_PROGRESS"
					}
					response, err := common.Marshal([]dto.MidjourneyDto{{MjId: "new-task", Status: status, Progress: "100%", SubmitTime: time.Now().UnixMilli()}})
					assert.NoError(t, err)
					_, _ = w.Write(response)
					return
				}
				_, _ = w.Write([]byte(`{"code":1,"description":"submitted","result":"new-task"}`))
			})
			require.NoError(t, db.Model(&model.User{}).Where("id = ?", 7).Update("quota", tc.wallet).Error)
			var account *model.MembershipAccount
			if tc.memberQuota > 0 {
				account = activateMidjourneyTestMembership(t, tc.memberQuota)
			}
			if tc.name == "member_success" {
				// An older legacy row can refer to the same upstream task. Its
				// local timeout must not turn the new authorization into a refund.
				require.NoError(t, db.Create(&model.Midjourney{UserId: 7, ChannelId: 1, MjId: "new-task", Status: "SUBMITTED", Progress: "0%", SubmitTime: time.Now().Add(-2 * time.Hour).UnixMilli()}).Error)
			}
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), "new-task")
			require.EqualValues(t, 1, calls.Load())
			taskID, err := strconv.Atoi(recorder.Header().Get("X-Midjourney-Task-Id"))
			require.NoError(t, err)
			var task model.Midjourney
			require.NoError(t, db.First(&task, taskID).Error)
			record, err := model.GetMidjourneyMembershipRecord(task.Id)
			require.NoError(t, err)
			require.NotNil(t, record)
			assert.Equal(t, "reserved", record.State)
			assert.EqualValues(t, tc.charge, record.ReservedQuota)
			memberFunded := tc.memberQuota >= int64(tc.charge)
			if memberFunded {
				assert.Equal(t, model.MembershipSource, record.Source)
				assert.Equal(t, account.ID, record.AccountID)
				// Expiry during the render cannot redirect its eventual settlement.
				require.NoError(t, db.Model(account).Update("expires_at", time.Now().Add(-time.Minute).Unix()).Error)
			} else {
				assert.Equal(t, model.MembershipWalletSource, record.Source)
			}
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"mj_imagine":9,"swap_face":9}`))
			runMidjourneyTaskUpdateOnce(context.Background(), nil)
			if tc.name == "member_success" {
				require.NoError(t, db.First(&task, task.Id).Error)
				assert.Equal(t, "IN_PROGRESS", task.Status)
				assert.NotEqual(t, "100%", task.Progress)
				pending, err := model.GetMidjourneyMembershipRecord(task.Id)
				require.NoError(t, err)
				assert.Equal(t, "reserved", pending.State)
			}
			runMidjourneyTaskUpdateOnce(context.Background(), nil)
			runMidjourneyTaskUpdateOnce(context.Background(), nil)
			require.NoError(t, db.First(&task, task.Id).Error)
			assert.Equal(t, tc.terminal, task.Status)
			assert.Equal(t, "100%", task.Progress)
			record, err = model.GetMidjourneyMembershipRecord(task.Id)
			require.NoError(t, err)
			expectedCharge := tc.charge
			if tc.terminal == "FAILURE" {
				expectedCharge = 0
				assert.Equal(t, "released", record.State)
			} else {
				assert.Equal(t, "settled", record.State)
			}
			assert.EqualValues(t, expectedCharge, record.ChargedQuota)
			var user model.User
			require.NoError(t, db.First(&user, 7).Error)
			expectedWallet := tc.wallet
			if !memberFunded {
				expectedWallet -= expectedCharge
			}
			assert.EqualValues(t, expectedWallet, user.Quota)
			assert.EqualValues(t, expectedCharge, user.UsedQuota)
			var token model.Token
			require.NoError(t, db.First(&token, 1).Error)
			assert.Equal(t, 1000-expectedCharge, token.RemainQuota)
			assert.Equal(t, expectedCharge, token.UsedQuota)
			if account != nil {
				require.NoError(t, db.First(account, account.ID).Error)
				assert.Zero(t, account.AmountReserved)
				if memberFunded {
					assert.EqualValues(t, expectedCharge, account.AmountUsed)
				} else {
					assert.Zero(t, account.AmountUsed)
				}
			}
		})
	}
}

func TestMembershipMidjourneyPreservesLegacySubmission(t *testing.T) {
	for _, tc := range []struct {
		name   string
		legacy bool
	}{{"wallet_only", false}, {"legacy_subscription", true}} {
		t.Run(tc.name, func(t *testing.T) {
			db, router, calls := membershipMidjourneyFixture(t, nil)
			if model.MembershipEnabled() && tc.legacy {
				require.NoError(t, db.Create(&model.UserSubscription{UserId: 7, Status: "active", EndTime: time.Now().Add(time.Hour).Unix()}).Error)
			} else if !model.MembershipEnabled() {
				// Disabled installations must not require the membership tables.
				require.NoError(t, db.Migrator().DropTable(&model.MembershipCatalogState{}))
			}
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", strings.NewReader(`{"prompt":"cat"}`))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)
			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "queue full")
			assert.EqualValues(t, 1, calls.Load())
			if model.MembershipEnabled() {
				var count int64
				require.NoError(t, db.Model(&model.MembershipBillingRecord{}).Count(&count).Error)
				assert.Zero(t, count)
			}
		})
	}
}

func TestMembershipMidjourneyWalletWithoutAccountSettlesOnLegacyPath(t *testing.T) {
	if !model.MembershipEnabled() {
		t.Skip("requires KKAI_MEMBERSHIP_ENABLED=true")
	}
	db, router, calls := membershipMidjourneyFixture(t, func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(request.URL.Path, "/list-by-condition") {
			_, _ = w.Write([]byte(`[{"id":"wallet-task","status":"SUCCESS","progress":"100%"}]`))
			return
		}
		_, _ = w.Write([]byte(`{"code":1,"description":"submitted","result":"wallet-task"}`))
	})
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 7).Update("quota", 123).Error)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/fast/mj/submit/imagine", strings.NewReader(`{"prompt":"cat"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.EqualValues(t, 1, calls.Load())
	var task model.Midjourney
	require.NoError(t, db.Where("mj_id = ?", "wallet-task").First(&task).Error)
	record, err := model.GetMidjourneyMembershipRecord(task.Id)
	require.NoError(t, err)
	assert.Nil(t, record)
	runMidjourneyTaskUpdateOnce(context.Background(), nil)
	require.NoError(t, db.First(&task, task.Id).Error)
	assert.Equal(t, "SUCCESS", task.Status)
	var user model.User
	require.NoError(t, db.First(&user, 7).Error)
	assert.EqualValues(t, 23, user.Quota)
	assert.EqualValues(t, 100, user.UsedQuota)
}

func TestMembershipMidjourneyKeepsHistoricalTaskReadsAvailable(t *testing.T) {
	db, router, calls := membershipMidjourneyFixture(t, nil)
	require.NoError(t, db.Create(&model.Midjourney{UserId: 7, MjId: "old-task", Status: "SUCCESS", Progress: "100%"}).Error)
	// Reading an old task remains possible even when new billing is unavailable.
	require.NoError(t, db.Migrator().DropTable(&model.MembershipCatalogState{}))
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/mj/task/old-task/fetch", ""},
		{http.MethodPost, "/mj/task/list-by-condition", `{"ids":["old-task"]}`},
	} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, req)
		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "old-task")
		assert.Contains(t, recorder.Body.String(), "SUCCESS")
	}
	assert.Zero(t, calls.Load())
}

func TestMembershipMidjourneyBillingCheckFailsClosed(t *testing.T) {
	if !model.MembershipEnabled() {
		t.Skip("requires KKAI_MEMBERSHIP_ENABLED=true")
	}
	db, router, calls := membershipMidjourneyFixture(t, nil)
	require.NoError(t, db.Migrator().DropTable(&model.MembershipCatalogState{}))
	// A fresh handle checks the unprepared schema before accepting new billing.
	model.DB = db.Session(&gorm.Session{NewDB: true})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", strings.NewReader(`{"prompt":"cat"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "membership_billing_unavailable")
	assert.Zero(t, calls.Load())
}

func TestMembershipMidjourneyUnknownResponseKeepsOriginalReservation(t *testing.T) {
	if !model.MembershipEnabled() {
		t.Skip("requires KKAI_MEMBERSHIP_ENABLED=true")
	}
	db, router, calls := membershipMidjourneyFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":`))
	})
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 7).Update("quota", 0).Error)
	account := activateMidjourneyTestMembership(t, 1000)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", strings.NewReader(`{"prompt":"cat"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusBadGateway, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "submission_outcome_unknown")
	assert.NotEmpty(t, recorder.Header().Get("X-Midjourney-Task-Id"))
	runMidjourneyTaskUpdateOnce(context.Background(), nil)
	assert.EqualValues(t, 1, calls.Load(), "an ambiguous submission must never be retried or refunded by polling")
	var task model.Midjourney
	require.NoError(t, db.First(&task).Error)
	assert.Empty(t, task.MjId)
	assert.NotEqual(t, "FAILURE", task.Status)
	record, err := model.GetMidjourneyMembershipRecord(task.Id)
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.Equal(t, "reserved", record.State)
	assert.Equal(t, model.MembershipSource, record.Source)
	assert.EqualValues(t, 50, record.ReservedQuota)
	require.NoError(t, db.First(account, account.ID).Error)
	assert.EqualValues(t, 50, account.AmountReserved)
	assert.Zero(t, account.AmountUsed)
	var user model.User
	require.NoError(t, db.First(&user, 7).Error)
	assert.Zero(t, user.Quota)
	reviews, err := model.ListMembershipBillingReviews(false, 100)
	require.NoError(t, err)
	require.Len(t, reviews, 1)
	assert.Equal(t, record.RequestID, reviews[0].RequestID)
}
