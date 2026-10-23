package model

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupInvoiceTopUpReceiptTest(t *testing.T) (*gorm.DB, TopUp) {
	t.Helper()
	db := setupTopUpFinalizeTestDB(t)
	topUp := seedTopUpFinalizeFixture(t, db)
	require.NoError(t, db.Model(&topUp).Updates(map[string]any{"paid_minor": 7500, "currency": "CNY"}).Error)
	require.NoError(t, db.AutoMigrate(&InvoicePaymentReceipt{}, &InvoiceRequest{}, &InvoiceRequestItem{}, &InvoiceOrderClaim{},
		&InvoiceAttachment{}, &InvoiceDelivery{}, &InvoiceDispute{}, &InvoiceEvent{}, &InvoiceEventRead{}, &InvoiceOperation{}, &InvoiceProfile{}))
	return db, topUp
}

func TestInvoiceTopUpReceiptRecordsOnlyVerifiedCallbackAndReplaysOnce(t *testing.T) {
	db, topUp := setupInvoiceTopUpReceiptTest(t)
	_, err := RechargeEpayVerified(topUp.TradeNo, "alipay", "74.99", "provider-one")
	require.ErrorIs(t, err, ErrResellerFinanceInvalidInput)
	var pending TopUp
	require.NoError(t, db.First(&pending, topUp.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, pending.Status)
	var count int64
	require.NoError(t, db.Model(&InvoicePaymentReceipt{}).Count(&count).Error)
	assert.Zero(t, count)
	var user User
	require.NoError(t, db.First(&user, topUp.UserId).Error)
	assert.EqualValues(t, 10, user.Quota)

	completed, err := RechargeEpayVerified(topUp.TradeNo, "alipay", "75.00", "provider-one")
	require.NoError(t, err)
	assert.False(t, completed.AlreadyCompleted)
	var proof InvoicePaymentReceipt
	require.NoError(t, db.Where("top_up_id = ?", topUp.Id).First(&proof).Error)
	assert.Equal(t, "verified", proof.Status)
	assert.EqualValues(t, 7500, proof.AmountMinor)
	assert.Equal(t, "CNY", proof.Currency)
	assert.Equal(t, topUp.UserId, proof.UserID)
	assert.Positive(t, proof.PaidAt)
	assert.Len(t, proof.ReceiptKey, 64)
	assert.Empty(t, proof.ExceptionReceiptKey)
	firstID, firstPaidAt := proof.ID, proof.PaidAt
	replay, err := RechargeEpayVerified(topUp.TradeNo, "alipay", "75.00", "provider-one")
	require.NoError(t, err)
	assert.True(t, replay.AlreadyCompleted)
	require.NoError(t, db.First(&proof, firstID).Error)
	assert.Equal(t, firstPaidAt, proof.PaidAt)
	require.NoError(t, db.Model(&InvoicePaymentReceipt{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, db.First(&user, topUp.UserId).Error)
	assert.Equal(t, 10+completed.QuotaDelta, user.Quota)
	require.NoError(t, db.Model(&KKAIOutboxEvent{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestInvoiceTopUpReceiptCannotFundTwoOrders(t *testing.T) {
	db, topUp := setupInvoiceTopUpReceiptTest(t)
	first, err := RechargeEpayVerified(topUp.TradeNo, "alipay", "75.00", "provider-shared")
	require.NoError(t, err)
	second := topUp
	second.Id, second.TradeNo = 843, "trade-843"
	second.PaidMinor, second.Currency = 7500, "CNY"
	require.NoError(t, db.Create(&second).Error)
	_, err = RechargeEpayVerified(second.TradeNo, "alipay", "75.00", "provider-shared")
	require.Error(t, err)
	require.NoError(t, db.First(&second, second.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, second.Status)
	var user User
	require.NoError(t, db.First(&user, topUp.UserId).Error)
	assert.Equal(t, 10+first.QuotaDelta, user.Quota)
	var count int64
	require.NoError(t, db.Model(&InvoicePaymentReceipt{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestInvoiceTopUpManualCompletionAndLegacyEstimateDoNotCreateEvidence(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy amount", true: "manual completion"}[manual], func(t *testing.T) {
			db, topUp := setupInvoiceTopUpReceiptTest(t)
			var err error
			if manual {
				err = ManualCompleteTopUp(topUp.TradeNo, "127.0.0.1")
			} else {
				require.NoError(t, db.Model(&topUp).Updates(map[string]any{"paid_minor": 0, "currency": ""}).Error)
				_, err = RechargeEpayVerified(topUp.TradeNo, "alipay", "75.00", "legacy-receipt")
			}
			require.NoError(t, err)
			var stored TopUp
			require.NoError(t, db.First(&stored, topUp.Id).Error)
			assert.Equal(t, common.TopUpStatusSuccess, stored.Status)
			var count int64
			require.NoError(t, db.Model(&InvoicePaymentReceipt{}).Count(&count).Error)
			assert.Zero(t, count)
			if manual {
				var customer User
				require.NoError(t, db.First(&customer, topUp.UserId).Error)
				quotaBefore := customer.Quota
				result, err := RechargeEpayVerified(topUp.TradeNo, "alipay", "75.00", "late-real-receipt")
				require.NoError(t, err)
				assert.True(t, result.AlreadyCompleted)
				require.NoError(t, db.Model(&InvoicePaymentReceipt{}).Count(&count).Error)
				assert.EqualValues(t, 1, count, "a later actual signed callback can supply missing evidence")
				require.NoError(t, db.First(&customer, topUp.UserId).Error)
				assert.Equal(t, quotaBefore, customer.Quota)
			}
		})
	}
}

func TestInvoiceTopUpAdditionalSignedPaymentHoldsDeliveredInvoice(t *testing.T) {
	for _, tc := range []struct{ name, receiptID, amount string }{
		{"additional receipt", "provider-two", "75.00"},
		{"additional receipt and amount", "provider-two", "74.99"},
		{"same receipt altered amount", "provider-one", "74.99"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, topUp := setupInvoiceTopUpReceiptTest(t)
			completed, err := RechargeEpayVerified(topUp.TradeNo, "alipay", "75.00", "provider-one")
			require.NoError(t, err)
			request := InvoiceRequest{PublicID: "INV-topup-safety", UserID: topUp.UserId, Status: InvoiceDelivered, Version: 4,
				CurrentDeliveryVersion: 2, EverDelivered: true, IssuedEvidence: true, DeliveredAt: 123, AmountMinor: 7500, Currency: "CNY"}
			require.NoError(t, db.Create(&request).Error)
			receiptKey := sha256.Sum256([]byte("epay:provider-one"))
			key := sha256.Sum256([]byte("payment:receipt:v1:0:" + hex.EncodeToString(receiptKey[:])))
			claim := InvoiceOrderClaim{CanonicalPaymentKey: hex.EncodeToString(key[:]), RequestID: request.ID}
			require.NoError(t, db.Create(&claim).Error)
			result, err := RechargeEpayVerified(topUp.TradeNo, "alipay", tc.amount, tc.receiptID)
			require.NoError(t, err)
			assert.True(t, result.AlreadyCompleted)
			var proof InvoicePaymentReceipt
			require.NoError(t, db.Where("top_up_id = ?", topUp.Id).First(&proof).Error)
			assert.Equal(t, "exception", proof.Status)
			assert.EqualValues(t, 7500, proof.AmountMinor, "retain original verified evidence")
			assert.NotEmpty(t, proof.ExceptionReceiptKey)
			_, err = RechargeEpayVerified(topUp.TradeNo, "alipay", "75.00", "provider-one")
			require.NoError(t, err)
			require.NoError(t, db.First(&proof, proof.ID).Error)
			assert.Equal(t, "exception", proof.Status, "an original replay cannot erase an anomaly")
			var held InvoiceRequest
			require.NoError(t, db.First(&held, request.ID).Error)
			assert.Equal(t, InvoiceFinanceHold, held.Status)
			assert.Equal(t, InvoiceDelivered, held.HoldFromStatus)
			assert.EqualValues(t, 5, held.Version)
			assert.EqualValues(t, 2, held.CurrentDeliveryVersion)
			assert.EqualValues(t, 123, held.DeliveredAt)
			assert.True(t, held.EverDelivered)
			assert.True(t, held.IssuedEvidence)
			var keptClaim InvoiceOrderClaim
			require.NoError(t, db.First(&keptClaim, claim.ID).Error)
			assert.Equal(t, request.ID, keptClaim.RequestID)
			var user User
			require.NoError(t, db.First(&user, topUp.UserId).Error)
			assert.Equal(t, 10+completed.QuotaDelta, user.Quota)
			var events []InvoiceEvent
			require.NoError(t, db.Where("request_id = ?", request.ID).Find(&events).Error)
			require.Len(t, events, 1)
			assert.Equal(t, "hold", events[0].Kind)
			var notices int64
			require.NoError(t, db.Model(&KKAIOutboxEvent{}).Where("topic = ?", "invoice.notification.v1").Count(&notices).Error)
			assert.EqualValues(t, 1, notices)
		})
	}
}

func TestInvoiceTopUpReceiptSupportsUnexpandedSchemaAndRejectsPartialSchema(t *testing.T) {
	db := setupTopUpFinalizeTestDB(t)
	topUp := seedTopUpFinalizeFixture(t, db)
	require.NoError(t, db.Model(&topUp).Updates(map[string]any{"paid_minor": 7500, "currency": "CNY"}).Error)
	_, err := RechargeEpayVerified(topUp.TradeNo, "alipay", "75.00", "unexpanded")
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&InvoiceRequest{}))
	_, err = RechargeEpayVerified(topUp.TradeNo, "alipay", "75.00", "partial")
	require.Error(t, err, "completed replay must not mask missing invoice protection")
}
