package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupInvoiceReconcileTest(t *testing.T) (*gorm.DB, TopUp) {
	t.Helper()
	db, topUp := setupInvoiceTopUpReceiptTest(t)
	require.NoError(t, db.AutoMigrate(&MembershipOrder{}, &SubscriptionOrder{}))
	require.NoError(t, db.Model(&topUp).Updates(map[string]any{
		"status": common.TopUpStatusSuccess, "paid_minor": 0, "currency": "", "complete_time": 0, "money": 999,
	}).Error)
	require.NoError(t, db.First(&topUp, topUp.Id).Error)
	return db, topUp
}

func TestInvoiceReconcileRestoresOnlyExactEvidenceAndMissingTime(t *testing.T) {
	for _, tc := range []struct {
		name         string
		paidMinor    int64
		currency     string
		completeTime int64
	}{
		{"legacy amount", 0, "", 123},
		{"legacy missing time", 0, "", 0},
		{"exact missing currency", 7500, "", 0},
		{"exact missing time", 7500, "CNY", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, topUp := setupInvoiceReconcileTest(t)
			require.NoError(t, db.Model(&topUp).Updates(map[string]any{
				"paid_minor": tc.paidMinor, "currency": tc.currency, "complete_time": tc.completeTime,
			}).Error)
			receipt := TopUpPaymentReceipt{ProviderReceiptID: "reconciled-receipt", AmountMinor: 7500, Currency: "CNY"}
			startedAt, paidAt := common.GetTimestamp(), common.GetTimestamp()-60
			require.NoError(t, ReconcileInvoiceTopUpPayment(context.Background(), db, 0, topUp.UserId, topUp.TradeNo, receipt, paidAt))
			var proof InvoicePaymentReceipt
			require.NoError(t, db.Where("top_up_id = ?", topUp.Id).First(&proof).Error)
			assert.Equal(t, "verified", proof.Status)
			assert.EqualValues(t, 7500, proof.AmountMinor, "provider evidence, never historical Money, supplies the exact amount")
			assert.Equal(t, "CNY", proof.Currency)
			assert.Equal(t, paidAt, proof.PaidAt)
			assert.GreaterOrEqual(t, proof.CreatedAt, startedAt)
			assert.LessOrEqual(t, proof.CreatedAt, common.GetTimestamp())
			first := proof
			require.NoError(t, ReconcileInvoiceTopUpPayment(context.Background(), db, 0, topUp.UserId, topUp.TradeNo, receipt, paidAt+1))
			require.NoError(t, db.First(&proof, proof.ID).Error)
			assert.Equal(t, first, proof, "replay preserves the original evidence and audit timestamps")
			var stored TopUp
			require.NoError(t, db.First(&stored, topUp.Id).Error)
			assert.EqualValues(t, 7500, stored.PaidMinor)
			assert.Equal(t, "CNY", stored.Currency)
			if tc.completeTime > 0 {
				assert.Equal(t, tc.completeTime, stored.CompleteTime)
			} else {
				assert.Equal(t, paidAt, stored.CompleteTime)
			}
			assert.Equal(t, common.TopUpStatusSuccess, stored.Status)
			assert.Equal(t, topUp.Money, stored.Money)
			assert.Equal(t, topUp.Amount, stored.Amount)
			var user User
			require.NoError(t, db.First(&user, topUp.UserId).Error)
			assert.EqualValues(t, 10, user.Quota)
			var count int64
			require.NoError(t, db.Model(&InvoicePaymentReceipt{}).Count(&count).Error)
			assert.EqualValues(t, 1, count)
			require.NoError(t, db.Model(&KKAIOutboxEvent{}).Count(&count).Error)
			assert.Zero(t, count, "reconciliation creates no credit or rebate event")
			require.NoError(t, db.Model(&InvoiceOrderClaim{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestInvoiceReconcileRestoresMissingVerifiedReceiptTimeWithoutClearingException(t *testing.T) {
	for _, status := range []string{"verified", "exception"} {
		t.Run(status, func(t *testing.T) {
			db, topUp := setupInvoiceReconcileTest(t)
			receipt := TopUpPaymentReceipt{"existing-missing-time", 7500, "CNY"}
			paidAt := common.GetTimestamp() - 60
			require.NoError(t, ReconcileInvoiceTopUpPayment(context.Background(), db, 0, topUp.UserId, topUp.TradeNo, receipt, paidAt))
			var proof InvoicePaymentReceipt
			require.NoError(t, db.Where("top_up_id = ?", topUp.Id).First(&proof).Error)
			originalID, originalCreatedAt := proof.ID, proof.CreatedAt
			require.NoError(t, db.Model(&proof).Updates(map[string]any{"paid_at": 0, "status": status}).Error)
			require.NoError(t, db.Model(&topUp).Update("complete_time", 0).Error)
			err := ReconcileInvoiceTopUpPayment(context.Background(), db, 0, topUp.UserId, topUp.TradeNo, receipt, paidAt)
			if status == "verified" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrInvoicePaymentException)
			}
			require.NoError(t, db.First(&proof, originalID).Error)
			assert.Equal(t, status, proof.Status)
			assert.Equal(t, originalCreatedAt, proof.CreatedAt)
			if status == "verified" {
				assert.Equal(t, paidAt, proof.PaidAt, "successful reconciliation restores usable payment evidence")
				require.NoError(t, ReconcileInvoiceTopUpPayment(context.Background(), db, 0, topUp.UserId, topUp.TradeNo, receipt, paidAt+1))
				require.NoError(t, db.First(&proof, originalID).Error)
				assert.Equal(t, paidAt, proof.PaidAt, "a positive evidence timestamp remains immutable on replay")
			} else {
				assert.Zero(t, proof.PaidAt, "exception evidence must not receive a verified payment timestamp")
			}
			var stored TopUp
			require.NoError(t, db.First(&stored, topUp.Id).Error)
			assert.Equal(t, paidAt, stored.CompleteTime)
			var user User
			require.NoError(t, db.First(&user, topUp.UserId).Error)
			assert.EqualValues(t, 10, user.Quota)
		})
	}
}

func TestInvoiceReconcileConcurrentReplayCreatesOneReceiptWithoutCrediting(t *testing.T) {
	db, topUp := setupInvoiceReconcileTest(t)
	receipt := TopUpPaymentReceipt{ProviderReceiptID: "concurrent-receipt", AmountMinor: 7500, Currency: "CNY"}
	paidAt := common.GetTimestamp() - 60
	start, results := make(chan struct{}), make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			results <- ReconcileInvoiceTopUpPayment(context.Background(), db, 0, topUp.UserId, topUp.TradeNo, receipt, paidAt)
		}()
	}
	close(start)
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	var count int64
	require.NoError(t, db.Model(&InvoicePaymentReceipt{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	var user User
	require.NoError(t, db.First(&user, topUp.UserId).Error)
	assert.EqualValues(t, 10, user.Quota)
	require.NoError(t, db.Model(&KKAIOutboxEvent{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestInvoiceReconcileRejectsWrongOwnerTenantAndPaymentSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name       string
		tenantID   int64
		userOffset int
		updates    map[string]any
		expected   error
	}{
		{"wrong requester", 0, 1, nil, gorm.ErrRecordNotFound},
		{"nonplatform requester", 1, 0, nil, ErrResellerFinanceInvalidInput},
		{"different original tenant", 0, 0, map[string]any{"tenant_id": 1}, gorm.ErrRecordNotFound},
		{"different original owner", 0, 0, map[string]any{"user_id": 9999}, gorm.ErrRecordNotFound},
		{"pending order", 0, 0, map[string]any{"status": common.TopUpStatusPending}, ErrResellerFinanceInvalidInput},
		{"different provider", 0, 0, map[string]any{"payment_provider": PaymentProviderStripe}, ErrResellerFinanceInvalidInput},
		{"negative exact snapshot", 0, 0, map[string]any{"paid_minor": -1, "currency": "CNY"}, ErrResellerFinanceInvalidInput},
		{"mismatched exact amount", 0, 0, map[string]any{"paid_minor": 7499}, ErrResellerFinanceInvalidInput},
		{"different legacy currency", 0, 0, map[string]any{"currency": "USD"}, ErrResellerFinanceInvalidInput},
		{"different exact currency", 0, 0, map[string]any{"paid_minor": 7500, "currency": "USD"}, ErrResellerFinanceInvalidInput},
		{"payment precedes order", 0, 0, map[string]any{"create_time": common.GetTimestamp()}, ErrResellerFinanceInvalidInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, topUp := setupInvoiceReconcileTest(t)
			if tc.updates != nil {
				require.NoError(t, db.Model(&topUp).Updates(tc.updates).Error)
			}
			var before TopUp
			require.NoError(t, db.First(&before, topUp.Id).Error)
			receipt := TopUpPaymentReceipt{ProviderReceiptID: "rejected-receipt", AmountMinor: 7500, Currency: "CNY"}
			err := ReconcileInvoiceTopUpPayment(context.Background(), db, tc.tenantID, 3418+tc.userOffset, topUp.TradeNo, receipt, common.GetTimestamp()-60)
			require.ErrorIs(t, err, tc.expected)
			var after TopUp
			require.NoError(t, db.First(&after, topUp.Id).Error)
			assert.Equal(t, before, after)
			var count int64
			require.NoError(t, db.Model(&InvoicePaymentReceipt{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestInvoiceReconcileRejectsInvalidProviderEvidence(t *testing.T) {
	db, topUp := setupInvoiceReconcileTest(t)
	for _, tc := range []struct {
		name    string
		receipt TopUpPaymentReceipt
		paidAt  int64
	}{
		{"no positive amount", TopUpPaymentReceipt{"id", 0, "CNY"}, common.GetTimestamp() - 60},
		{"wrong currency", TopUpPaymentReceipt{"id", 7500, "USD"}, common.GetTimestamp() - 60},
		{"missing receipt ID", TopUpPaymentReceipt{" ", 7500, "CNY"}, common.GetTimestamp() - 60},
		{"overlong receipt ID", TopUpPaymentReceipt{strings.Repeat("x", 256), 7500, "CNY"}, common.GetTimestamp() - 60},
		{"invalid receipt bytes", TopUpPaymentReceipt{string([]byte{0xff}), 7500, "CNY"}, common.GetTimestamp() - 60},
		{"receipt control characters", TopUpPaymentReceipt{"id\x00", 7500, "CNY"}, common.GetTimestamp() - 60},
		{"missing paid time", TopUpPaymentReceipt{"id", 7500, "CNY"}, 0},
		{"future paid time", TopUpPaymentReceipt{"id", 7500, "CNY"}, common.GetTimestamp() + 3600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ReconcileInvoiceTopUpPayment(context.Background(), db, 0, topUp.UserId, topUp.TradeNo, tc.receipt, tc.paidAt)
			require.ErrorIs(t, err, ErrResellerFinanceInvalidInput)
		})
	}
	var stored TopUp
	require.NoError(t, db.First(&stored, topUp.Id).Error)
	assert.Equal(t, topUp, stored)
	var count int64
	require.NoError(t, db.Model(&InvoicePaymentReceipt{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestInvoiceReconcileRejectsMembershipAndSubscriptionAliases(t *testing.T) {
	for _, source := range []string{"membership", "subscription"} {
		t.Run(source, func(t *testing.T) {
			db, topUp := setupInvoiceReconcileTest(t)
			if source == "membership" {
				require.NoError(t, db.Create(&MembershipOrder{TradeNo: topUp.TradeNo, UserID: 9999}).Error)
			} else {
				require.NoError(t, db.Create(&SubscriptionOrder{TradeNo: topUp.TradeNo, UserId: 9999}).Error)
			}
			err := ReconcileInvoiceTopUpPayment(context.Background(), db, 0, topUp.UserId, topUp.TradeNo, TopUpPaymentReceipt{"alias-receipt", 7500, "CNY"}, common.GetTimestamp()-60)
			require.ErrorIs(t, err, ErrResellerFinanceInvalidInput)
			var stored TopUp
			require.NoError(t, db.First(&stored, topUp.Id).Error)
			assert.Equal(t, topUp, stored)
		})
	}
}

func TestInvoiceReconcileReceiptCannotFundAnotherHistoricalOrder(t *testing.T) {
	db, first := setupInvoiceReconcileTest(t)
	second := first
	second.Id, second.TradeNo = 0, "second-historical-topup"
	require.NoError(t, db.Create(&second).Error)
	receipt := TopUpPaymentReceipt{"shared-provider-receipt", 7500, "CNY"}
	paidAt := common.GetTimestamp() - 60
	require.NoError(t, ReconcileInvoiceTopUpPayment(context.Background(), db, 0, first.UserId, first.TradeNo, receipt, paidAt))
	require.Error(t, ReconcileInvoiceTopUpPayment(context.Background(), db, 0, second.UserId, second.TradeNo, receipt, paidAt))
	var stored TopUp
	require.NoError(t, db.First(&stored, second.Id).Error)
	assert.Equal(t, second, stored, "receipt conflict rolls back snapshot and completion-time repair")
	var count int64
	require.NoError(t, db.Model(&InvoicePaymentReceipt{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestInvoiceReconcileCommitsExceptionAndNeverClearsItOrReleasesClaim(t *testing.T) {
	for _, change := range []struct {
		name    string
		receipt TopUpPaymentReceipt
	}{
		{"additional receipt", TopUpPaymentReceipt{"additional-provider-receipt", 7500, "CNY"}},
		{"existing receipt amount changed", TopUpPaymentReceipt{"original-provider-receipt", 7499, "CNY"}},
	} {
		t.Run(change.name, func(t *testing.T) {
			db, topUp := setupInvoiceReconcileTest(t)
			receipt := TopUpPaymentReceipt{"original-provider-receipt", 7500, "CNY"}
			paidAt := common.GetTimestamp() - 60
			require.NoError(t, ReconcileInvoiceTopUpPayment(context.Background(), db, 0, topUp.UserId, topUp.TradeNo, receipt, paidAt))
			var proof InvoicePaymentReceipt
			require.NoError(t, db.Where("top_up_id = ?", topUp.Id).First(&proof).Error)
			original := proof
			var originalOrder TopUp
			require.NoError(t, db.First(&originalOrder, topUp.Id).Error)
			request := InvoiceRequest{PublicID: "INV-reconcile", UserID: topUp.UserId, Status: InvoiceDelivered, Version: 1,
				CurrentDeliveryVersion: 2, DeliveredAt: paidAt + 10, AmountMinor: 7500, Currency: "CNY", IssuedEvidence: true, EverDelivered: true}
			require.NoError(t, db.Create(&request).Error)
			canonical := sha256.Sum256([]byte("payment:receipt:v1:0:" + proof.ReceiptKey))
			claim := InvoiceOrderClaim{CanonicalPaymentKey: hex.EncodeToString(canonical[:]), RequestID: request.ID}
			require.NoError(t, db.Create(&claim).Error)
			startedAt := common.GetTimestamp()
			require.ErrorIs(t, ReconcileInvoiceTopUpPayment(context.Background(), db, 0, topUp.UserId, topUp.TradeNo, change.receipt, paidAt), ErrInvoicePaymentException)
			require.NoError(t, db.First(&proof, proof.ID).Error)
			assert.Equal(t, "exception", proof.Status)
			assert.NotEmpty(t, proof.ExceptionReceiptKey)
			assert.Equal(t, original.ReceiptKey, proof.ReceiptKey)
			assert.Equal(t, original.AmountMinor, proof.AmountMinor)
			assert.Equal(t, original.Currency, proof.Currency)
			assert.Equal(t, original.PaidAt, proof.PaidAt)
			assert.Equal(t, original.CreatedAt, proof.CreatedAt)
			require.ErrorIs(t, ReconcileInvoiceTopUpPayment(context.Background(), db, 0, topUp.UserId, topUp.TradeNo, receipt, paidAt), ErrInvoicePaymentException)
			require.NoError(t, db.First(&proof, proof.ID).Error)
			assert.Equal(t, "exception", proof.Status)
			var storedClaim InvoiceOrderClaim
			require.NoError(t, db.First(&storedClaim, claim.ID).Error)
			assert.Equal(t, claim, storedClaim)
			var held InvoiceRequest
			require.NoError(t, db.First(&held, request.ID).Error)
			assert.Equal(t, InvoiceFinanceHold, held.Status)
			assert.Equal(t, InvoiceDelivered, held.HoldFromStatus)
			assert.Equal(t, request.Version+1, held.Version)
			assert.Equal(t, request.CurrentDeliveryVersion, held.CurrentDeliveryVersion)
			assert.Equal(t, request.DeliveredAt, held.DeliveredAt)
			assert.True(t, held.IssuedEvidence)
			assert.True(t, held.EverDelivered)
			assert.GreaterOrEqual(t, held.UpdatedAt, startedAt, "financial hold uses current audit time, not historical payment time")
			var events []InvoiceEvent
			require.NoError(t, db.Where("request_id = ?", request.ID).Find(&events).Error)
			require.Len(t, events, 1)
			assert.GreaterOrEqual(t, events[0].CreatedAt, startedAt)
			var user User
			require.NoError(t, db.First(&user, topUp.UserId).Error)
			assert.EqualValues(t, 10, user.Quota)
			var count int64
			require.NoError(t, db.Model(&KKAIOutboxEvent{}).Where("topic = ?", KKAIOutboxTopicTopUpCompleted).Count(&count).Error)
			assert.Zero(t, count)
			var stored TopUp
			require.NoError(t, db.First(&stored, topUp.Id).Error)
			assert.Equal(t, originalOrder, stored, "a conflicting provider payment cannot overwrite the expected snapshot or credit the order")
		})
	}
}

func TestInvoiceReconcileRequiresReceiptSchema(t *testing.T) {
	db, topUp := setupInvoiceReconcileTest(t)
	require.NoError(t, db.Migrator().DropTable(&InvoicePaymentReceipt{}))
	err := ReconcileInvoiceTopUpPayment(context.Background(), db, 0, topUp.UserId, topUp.TradeNo, TopUpPaymentReceipt{"missing-schema", 7500, "CNY"}, common.GetTimestamp()-60)
	require.Error(t, err)
	var stored TopUp
	require.NoError(t, db.First(&stored, topUp.Id).Error)
	assert.Equal(t, topUp, stored)
}
