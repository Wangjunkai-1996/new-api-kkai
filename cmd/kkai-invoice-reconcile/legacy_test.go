package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func legacyFixture(t *testing.T) (*gorm.DB, manifest) {
	t.Helper()
	db, input := reconcileFixture(t)
	input.EvidenceSource = "owner_approved_legacy"
	for i := range input.Orders {
		proof := &input.Orders[i]
		proof.ProviderReceiptID, proof.PaidAt = "", 0
		money := fmt.Sprintf("%d.%02d", proof.AmountMinor/100, proof.AmountMinor%100)
		require.NoError(t, db.Model(&model.TopUp{}).Where("trade_no = ?", proof.OrderNo).Update("money", money).Error)
	}
	// Retain one real completion timestamp; the remaining rows have only their
	// historical creation time, which approval must label as estimated.
	require.NoError(t, db.Model(&model.TopUp{}).Where("trade_no = ?", input.Orders[0].OrderNo).Update("complete_time", common.GetTimestamp()-60).Error)
	return db, input
}

func TestLegacyApprovalRequiresExplicitFlagAndRecordsHonestProvenance(t *testing.T) {
	db, input := legacyFixture(t)
	require.NoError(t, db.Model(&model.TopUp{}).Where("trade_no = ?", input.Orders[2].OrderNo).Update("money", "37.50393608").Error)
	preview, err := reconcileLegacy(context.Background(), db, input, 140, false, false)
	require.NoError(t, err)
	assert.Equal(t, "preview", preview.Mode)
	assert.False(t, preview.Orders[0].EstimatedPaidAt)
	assert.True(t, preview.Orders[1].EstimatedPaidAt)
	assert.True(t, preview.Orders[2].AmountRounded)
	_, err = reconcileLegacy(context.Background(), db, input, 140, true, false)
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&model.InvoicePaymentReceipt{}).Count(&count).Error)
	assert.Zero(t, count)
	for range 2 {
		applied, err := reconcileLegacy(context.Background(), db, input, 140, true, true)
		require.NoError(t, err)
		assert.Equal(t, "apply", applied.Mode)
		assert.True(t, applied.Orders[1].EstimatedPaidAt, "replay retains the estimated-time disclosure")
	}
	var receipts []model.InvoicePaymentReceipt
	require.NoError(t, db.Order("top_up_id ASC").Find(&receipts).Error)
	require.Len(t, receipts, 6)
	var total int64
	for i, receipt := range receipts {
		var order model.TopUp
		require.NoError(t, db.First(&order, receipt.TopUpID).Error)
		identity := sha256.Sum256([]byte(fmt.Sprintf("invoice:owner-approved-legacy:v1:0:%d:%s", order.Id, order.TradeNo)))
		assert.Equal(t, hex.EncodeToString(identity[:]), receipt.ReceiptKey)
		assert.Equal(t, "verified", receipt.Status)
		assert.Equal(t, input.Orders[i].AmountMinor, receipt.AmountMinor)
		assert.Equal(t, receipt.PaidAt, order.CompleteTime)
		assert.EqualValues(t, 1333, order.Amount)
		total += receipt.AmountMinor
	}
	assert.EqualValues(t, 26800, total)
	var events []model.WalletPaymentEvent
	require.NoError(t, db.Order("top_up_id ASC").Find(&events).Error)
	require.Len(t, events, 6)
	for i, event := range events {
		assert.Equal(t, "owner_approved_legacy", event.Source)
		assert.Equal(t, "approved", event.Status)
		assert.Equal(t, input.EvidenceSHA256, event.EvidenceHash)
		assert.Contains(t, event.EvidenceRef, input.Operator)
		assert.Nil(t, event.ReceiptID, "no provider receipt identity is invented")
		if i > 0 {
			assert.Equal(t, "HISTORICAL_PAYMENT_TIME_ESTIMATED", event.ReasonCode)
			assert.Contains(t, event.EvidenceRef, "estimated_paid_at:true")
		}
		if i == 2 {
			assert.Contains(t, event.EvidenceRef, "amount_rounded:true")
		}
	}
	var user model.User
	require.NoError(t, db.First(&user, 140).Error)
	assert.EqualValues(t, 55, user.Quota)
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestLegacyApprovalRejectsRisksWithoutPartialWrites(t *testing.T) {
	for _, name := range []string{"owner", "amount", "rounded amount mismatch", "new wallet lifecycle", "unpaid", "refunded", "other provider", "alias", "claimed", "anomalous receipt", "invented provider ID", "invented payment time"} {
		t.Run(name, func(t *testing.T) {
			db, input := legacyFixture(t)
			proof := &input.Orders[3]
			switch name {
			case "owner":
				proof.UserID++
			case "amount":
				proof.AmountMinor++
			case "rounded amount mismatch":
				require.NoError(t, db.Model(&model.TopUp{}).Where("trade_no = ?", proof.OrderNo).Update("money", "37.506").Error)
			case "new wallet lifecycle":
				require.NoError(t, db.Model(&model.TopUp{}).Where("trade_no = ?", proof.OrderNo).Update("wallet_lifecycle_version", 1).Error)
			case "unpaid":
				require.NoError(t, db.Model(&model.TopUp{}).Where("trade_no = ?", proof.OrderNo).Update("status", common.TopUpStatusPending).Error)
			case "refunded":
				require.NoError(t, db.Model(&model.TopUp{}).Where("trade_no = ?", proof.OrderNo).Update("provider_payment_state", "refunded").Error)
			case "other provider":
				require.NoError(t, db.Model(&model.TopUp{}).Where("trade_no = ?", proof.OrderNo).Update("payment_provider", model.PaymentProviderStripe).Error)
			case "alias":
				require.NoError(t, db.Create(&model.SubscriptionOrder{TradeNo: proof.OrderNo}).Error)
			case "claimed":
				key := strings.Repeat("c", 64)
				require.NoError(t, db.Create(&model.InvoiceRequestItem{UserID: 140, OrderNo: proof.OrderNo, PaymentLabel: model.PaymentProviderEpay, CanonicalPaymentKey: key}).Error)
				require.NoError(t, db.Create(&model.InvoiceOrderClaim{CanonicalPaymentKey: key, RequestID: 42}).Error)
			case "anomalous receipt":
				require.NoError(t, db.Create(&model.InvoicePaymentReceipt{TopUpID: 4, UserID: 140, Provider: model.PaymentProviderEpay, ReceiptKey: strings.Repeat("b", 64), AmountMinor: proof.AmountMinor, Currency: "CNY", Status: "exception", PaidAt: common.GetTimestamp() - 60}).Error)
			case "invented provider ID":
				proof.ProviderReceiptID = "not-a-real-platform-transaction"
			case "invented payment time":
				proof.PaidAt = common.GetTimestamp() - 1
			}
			var before []model.TopUp
			require.NoError(t, db.Order("id ASC").Find(&before).Error)
			var receiptsBefore int64
			require.NoError(t, db.Model(&model.InvoicePaymentReceipt{}).Count(&receiptsBefore).Error)
			_, err := reconcileLegacy(context.Background(), db, input, 0, true, true)
			require.Error(t, err)
			var after []model.TopUp
			require.NoError(t, db.Order("id ASC").Find(&after).Error)
			assert.Equal(t, before, after)
			var receiptsAfter, events int64
			require.NoError(t, db.Model(&model.InvoicePaymentReceipt{}).Count(&receiptsAfter).Error)
			require.NoError(t, db.Model(&model.WalletPaymentEvent{}).Count(&events).Error)
			assert.Equal(t, receiptsBefore, receiptsAfter)
			assert.Zero(t, events)
		})
	}
}

func TestLegacyApprovalSupportsExplicitMultiUserManifestAndPreservesRealReceipt(t *testing.T) {
	db, input := legacyFixture(t)
	require.NoError(t, db.Create(&model.User{Id: 141, Username: "second-customer", AffCode: "second", Quota: 77}).Error)
	last := &input.Orders[5]
	last.UserID = 141
	require.NoError(t, db.Model(&model.TopUp{}).Where("trade_no = ?", last.OrderNo).Update("user_id", 141).Error)
	existing := model.InvoicePaymentReceipt{TopUpID: 1, UserID: 140, Provider: model.PaymentProviderEpay, ReceiptKey: strings.Repeat("d", 64), AmountMinor: input.Orders[0].AmountMinor, Currency: "CNY", Status: "verified", PaidAt: common.GetTimestamp() - 60, CreatedAt: common.GetTimestamp() - 30}
	require.NoError(t, db.Create(&existing).Error)
	_, err := reconcileLegacy(context.Background(), db, input, 140, true, true)
	require.Error(t, err, "optional single-user restriction must reject mixed owners")
	result, err := reconcileLegacy(context.Background(), db, input, 0, true, true)
	require.NoError(t, err)
	assert.Equal(t, "preserved_verified", result.Orders[0].Status)
	var stored model.InvoicePaymentReceipt
	require.NoError(t, db.First(&stored, existing.ID).Error)
	assert.Equal(t, existing, stored)
	assert.Equal(t, 141, result.Orders[5].UserID)
	var user model.User
	require.NoError(t, db.First(&user, 141).Error)
	assert.EqualValues(t, 77, user.Quota)
}
