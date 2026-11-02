package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipPaymentCheckRejectsForeignOrMissingIdentity(t *testing.T) {
	for _, userID := range []int{8, 0} {
		t.Run(fmt.Sprintf("user_%d", userID), func(t *testing.T) {
			db, order := membershipEpayFixture(t)
			before, err := model.GetMembershipPaymentCheck(context.Background(), order.ID)
			require.NoError(t, err)
			router := gin.New()
			router.POST("/orders/:trade_no/check", func(c *gin.Context) { c.Set("id", userID) }, CheckMembershipPayment)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/orders/"+order.TradeNo+"/check", strings.NewReader(`{"user_id":7,"paid":true,"amount":9900}`)))
			want := http.StatusNotFound
			if userID == 0 {
				want = http.StatusBadRequest
			}
			assert.Equal(t, want, response.Code)
			assert.NotContains(t, response.Body.String(), order.TradeNo)
			after, err := model.GetMembershipPaymentCheck(context.Background(), order.ID)
			require.NoError(t, err)
			assert.Equal(t, before, after)
			var count int64
			require.NoError(t, db.Model(&model.MembershipPaymentEvidence{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestMembershipPaymentCheckOnlySchedulesOriginalOrder(t *testing.T) {
	db, order := membershipEpayFixture(t)
	router := gin.New()
	router.POST("/orders/:trade_no/check", func(c *gin.Context) { c.Set("id", order.UserID) }, CheckMembershipPayment)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/orders/"+order.TradeNo+"/check", strings.NewReader(`{"user_id":8,"paid":true,"receipt_id":"stolen"}`)))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body struct {
		Data membershipOrderView `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, order.TradeNo, body.Data.TradeNo)
	assert.Equal(t, "checking", body.Data.PaymentState)
	assert.Zero(t, body.Data.PaidAt)
	for _, secret := range []string{"test-secret", "EncryptedSnapshot", "encrypted_snapshot", "merchant", "stolen"} {
		assert.NotContains(t, response.Body.String(), secret)
	}
	check, err := model.GetMembershipPaymentCheck(context.Background(), order.ID)
	require.NoError(t, err)
	assert.Equal(t, "self", check.LastSource)
	assert.Equal(t, order.UserID, check.UserID)
	for _, record := range []any{&model.MembershipPaymentEvidence{}, &model.MembershipPaymentReceipt{}, &model.MembershipAccount{}} {
		var count int64
		require.NoError(t, db.Model(record).Count(&count).Error)
		assert.Zero(t, count)
	}
}

func TestMembershipPaymentCheckCannotClaimSchedulingWhenWorkerDisabled(t *testing.T) {
	_, order := membershipEpayFixture(t)
	t.Setenv("KKAI_MEMBERSHIP_PAYMENT_RECONCILE_ENABLED", "false")
	router := gin.New()
	router.POST("/orders/:trade_no/check", func(c *gin.Context) { c.Set("id", order.UserID) }, CheckMembershipPayment)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/orders/"+order.TradeNo+"/check", nil))
	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
	check, err := model.GetMembershipPaymentCheck(context.Background(), order.ID)
	require.NoError(t, err)
	assert.Equal(t, "checkout", check.LastSource)
}

func TestMembershipPaymentCheckDoesNotClaimSchedulingForCompleteOrder(t *testing.T) {
	db, order := membershipEpayFixture(t)
	require.NoError(t, db.Model(order).Update("status", model.MembershipOrderSuccess).Error)
	router := gin.New()
	router.POST("/orders/:trade_no/check", func(c *gin.Context) { c.Set("id", order.UserID) }, CheckMembershipPayment)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/orders/"+order.TradeNo+"/check", nil))
	assert.Equal(t, http.StatusConflict, response.Code)
	check, err := model.GetMembershipPaymentCheck(context.Background(), order.ID)
	require.NoError(t, err)
	assert.Equal(t, "checkout", check.LastSource)
}

func TestMembershipPaymentProgressDistinguishesReviewFromVerifiedMoney(t *testing.T) {
	db, order := membershipEpayFixture(t)
	router := gin.New()
	router.GET("/orders/:trade_no", func(c *gin.Context) { c.Set("id", order.UserID) }, GetMembershipOrder)
	router.POST("/orders/:trade_no/pay", func(c *gin.Context) { c.Set("id", order.UserID) }, ResumeMembershipPayment)
	require.NoError(t, db.Model(&model.MembershipPaymentCheck{}).Where("order_id = ?", order.ID).Updates(map[string]any{"status": model.MembershipPaymentReview, "last_error": "PAYMENT_SNAPSHOT_INVALID"}).Error)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/orders/"+order.TradeNo, nil))
	require.Equal(t, http.StatusOK, response.Code)
	var body struct {
		Data membershipOrderView `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, "review", body.Data.PaymentState)
	assert.Equal(t, model.MembershipOrderPending, body.Data.Status)
	assert.Zero(t, body.Data.PaidAt)
	assert.False(t, body.Data.CanCheckPayment)

	now := common.GetTimestamp()
	require.NoError(t, db.Model(&model.MembershipPaymentCheck{}).Where("order_id = ?", order.ID).Updates(map[string]any{"status": model.MembershipPaymentPending, "next_check_at": now, "last_error": ""}).Error)
	claim, err := model.ClaimMembershipPaymentCheck(context.Background(), order.ID, now)
	require.NoError(t, err)
	_, err = model.PersistVerifiedMembershipPayment(context.Background(), model.MembershipVerifiedPaymentInput{OrderID: order.ID, ClaimToken: claim.ClaimToken, MerchantID: "merchant", ReceiptID: "paid-original", AmountMinor: order.AmountMinor, Currency: order.Currency, ProviderPaidAt: now})
	require.NoError(t, err)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/orders/"+order.TradeNo, nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, "processing", body.Data.PaymentState)
	assert.False(t, body.Data.CanCheckPayment)
	require.NoError(t, db.Model(&model.MembershipPaymentEvidence{}).Where("order_id = ?", order.ID).Update("validation_error", "provider payment time unavailable").Error)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/orders/"+order.TradeNo, nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, "review", body.Data.PaymentState)
	assert.Zero(t, body.Data.PaidAt)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/orders/"+order.TradeNo+"/pay", nil))
	assert.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	assert.NotContains(t, response.Body.String(), "form_fields")
	var accounts int64
	require.NoError(t, db.Model(&model.MembershipAccount{}).Count(&accounts).Error)
	assert.Zero(t, accounts)
}

func TestMembershipPaymentProgressShowsLegacyCredentialReviewWithoutPaidClaim(t *testing.T) {
	db, order := membershipEpayFixture(t)
	require.NoError(t, db.Where("order_id = ?", order.ID).Delete(&model.MembershipPaymentCheck{}).Error)
	require.NoError(t, model.RecordMembershipLegacyReview(context.Background(), order.ID, model.MembershipLegacyCredentialReview))
	router := gin.New()
	router.GET("/orders/:trade_no", func(c *gin.Context) { c.Set("id", order.UserID) }, GetMembershipOrder)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/orders/"+order.TradeNo, nil))
	require.Equal(t, http.StatusOK, response.Code)
	var body struct {
		Data membershipOrderView `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, "review", body.Data.PaymentState)
	assert.Equal(t, model.MembershipOrderPending, body.Data.Status)
	assert.Zero(t, body.Data.PaidAt)
	assert.Empty(t, body.Data.ExceptionReason)
	// Once original credentials are restored, recovery clears the marker and
	// the order can be checked again without treating it as paid.
	assert.False(t, body.Data.CanCheckPayment)
}
