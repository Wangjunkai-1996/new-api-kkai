package model

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceAdditionalMembershipPaymentHoldsDeliveredInvoiceAtomically(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	order, _ := buyMembershipTest(t, user.Id, catalog, "gold", "invoice-paid")
	require.NoError(t, DB.AutoMigrate(&InvoiceRequest{}, &InvoiceRequestItem{}, &InvoiceOrderClaim{},
		&InvoiceAttachment{}, &InvoiceDelivery{}, &InvoiceDispute{}, &InvoiceEvent{},
		&InvoiceEventRead{}, &InvoiceOperation{}, &InvoiceProfile{}, &InvoicePaymentReceipt{}))
	request := InvoiceRequest{
		PublicID: "INV-payment-safety", UserID: user.Id, Status: InvoiceDelivered, Version: 4,
		CurrentDeliveryVersion: 2, EverDelivered: true, IssuedEvidence: true,
		DeliveredAt: 123, AmountMinor: 1000, Currency: "CNY",
	}
	require.NoError(t, DB.Create(&request).Error)
	canonical := sha256.Sum256([]byte("payment:v1:0:" + order.ActualProvider + ":" + order.TradeNo))
	claim := InvoiceOrderClaim{CanonicalPaymentKey: hex.EncodeToString(canonical[:]), RequestID: request.ID}
	require.NoError(t, DB.Create(&claim).Error)
	input := MembershipPaymentInput{
		TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: "additional-verified",
		AmountMinor: order.AmountMinor, Currency: order.Currency, Paid: true, OneTime: true,
	}
	result, err := CompleteMembershipPayment(input)
	require.NoError(t, err)
	assert.Len(t, result.Payments, 2)
	assert.NotEmpty(t, result.ExceptionReason)
	var held InvoiceRequest
	require.NoError(t, DB.First(&held, request.ID).Error)
	assert.Equal(t, InvoiceFinanceHold, held.Status)
	assert.Equal(t, InvoiceDelivered, held.HoldFromStatus)
	assert.EqualValues(t, 5, held.Version)
	assert.EqualValues(t, 2, held.CurrentDeliveryVersion)
	assert.EqualValues(t, 123, held.DeliveredAt)
	assert.True(t, held.EverDelivered)
	assert.True(t, held.IssuedEvidence)
	var keptClaim InvoiceOrderClaim
	require.NoError(t, DB.First(&keptClaim, claim.ID).Error)
	assert.Equal(t, request.ID, keptClaim.RequestID)
	var events []InvoiceEvent
	require.NoError(t, DB.Where("request_id = ?", request.ID).Find(&events).Error)
	require.Len(t, events, 1)
	assert.Equal(t, "hold", events[0].Kind)
	assert.Zero(t, events[0].ActorID)
	assert.True(t, events[0].ActorAdmin)
	assert.Empty(t, events[0].DescriptionCipher)
	var notice KKAIOutboxEvent
	require.NoError(t, DB.Where("event_key = ?", "invoice:event:"+events[0].PublicID).First(&notice).Error)
	assert.Equal(t, "invoice.notification.v1", notice.Topic)
	assert.Equal(t, KKAIOutboxStatusPending, notice.Status)
	var payload struct {
		EventID int64 `json:"event_id"`
	}
	require.NoError(t, common.UnmarshalJsonStr(notice.Payload, &payload))
	assert.Equal(t, events[0].ID, payload.EventID)
	// Replaying the callback and another verified receipt cannot overwrite the
	// original hold stage or duplicate the state change and notification.
	_, err = CompleteMembershipPayment(input)
	require.NoError(t, err)
	input.ReceiptID = "third-verified"
	_, err = CompleteMembershipPayment(input)
	require.NoError(t, err)
	require.NoError(t, DB.First(&held, request.ID).Error)
	assert.EqualValues(t, 5, held.Version)
	assert.Equal(t, InvoiceDelivered, held.HoldFromStatus)
	var count int64
	require.NoError(t, DB.Model(&InvoiceEvent{}).Where("request_id = ?", request.ID).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestInvoiceMembershipReceiptAmountChangeHoldsEveryLinkedClaim(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	order, _ := buyMembershipTest(t, user.Id, catalog, "gold", "invoice-amount-changed")
	require.NoError(t, DB.AutoMigrate(&InvoiceRequest{}, &InvoiceRequestItem{}, &InvoiceOrderClaim{},
		&InvoiceAttachment{}, &InvoiceDelivery{}, &InvoiceDispute{}, &InvoiceEvent{},
		&InvoiceEventRead{}, &InvoiceOperation{}, &InvoiceProfile{}, &InvoicePaymentReceipt{}))
	for _, id := range []string{"receipt-claim-one", "receipt-claim-two"} {
		request := InvoiceRequest{PublicID: "INV-" + id, UserID: user.Id, Status: InvoiceDelivered, Version: 4,
			CurrentDeliveryVersion: 2, EverDelivered: true, IssuedEvidence: true, DeliveredAt: 123, AmountMinor: 1000, Currency: "CNY"}
		require.NoError(t, DB.Create(&request).Error)
		canonical := sha256.Sum256([]byte(id))
		key := hex.EncodeToString(canonical[:])
		require.NoError(t, DB.Create(&InvoiceOrderClaim{CanonicalPaymentKey: key, RequestID: request.ID}).Error)
		require.NoError(t, DB.Create(&InvoiceRequestItem{UserID: user.Id, RequestID: request.ID, CanonicalPaymentKey: key, OrderNo: order.TradeNo, PaymentLabel: order.ActualProvider}).Error)
	}
	_, err := CompleteMembershipPayment(MembershipPaymentInput{TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: order.ReceiptID,
		AmountMinor: order.AmountMinor - 1, Currency: order.Currency, Paid: true, OneTime: true})
	require.NoError(t, err)
	var held []InvoiceRequest
	require.NoError(t, DB.Order("id ASC").Find(&held).Error)
	require.Len(t, held, 2)
	for _, request := range held {
		assert.Equal(t, InvoiceFinanceHold, request.Status)
		assert.Equal(t, InvoiceDelivered, request.HoldFromStatus)
		assert.EqualValues(t, 5, request.Version)
		assert.EqualValues(t, 2, request.CurrentDeliveryVersion)
	}
	var proof MembershipPaymentReceipt
	require.NoError(t, DB.Where("receipt_key = ?", *order.ReceiptKey).First(&proof).Error)
	assert.Equal(t, MembershipOrderException, proof.Status)
	assert.Equal(t, order.AmountMinor, proof.AmountMinor)
	_, err = CompleteMembershipPayment(MembershipPaymentInput{TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: order.ReceiptID,
		AmountMinor: order.AmountMinor, Currency: order.Currency, Paid: true, OneTime: true})
	require.NoError(t, err)
	require.NoError(t, DB.First(&proof, proof.ID).Error)
	assert.Equal(t, MembershipOrderException, proof.Status)
	var events int64
	require.NoError(t, DB.Model(&InvoiceEvent{}).Count(&events).Error)
	assert.EqualValues(t, 2, events, "a replay does not duplicate either hold event")
}

func TestInvoiceAdditionalMembershipPaymentSupportsUnexpandedSchemaAndRejectsPartialSchema(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	order, _ := buyMembershipTest(t, user.Id, catalog, "gold", "invoice-legacy")
	input := MembershipPaymentInput{
		TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: "legacy-extra",
		AmountMinor: order.AmountMinor, Currency: order.Currency, Paid: true, OneTime: true,
	}
	result, err := CompleteMembershipPayment(input)
	require.NoError(t, err, "an optional invoice expansion must not block legacy callbacks")
	assert.Len(t, result.Payments, 2)
	require.NoError(t, DB.AutoMigrate(&InvoiceRequest{}))
	input.ReceiptID = "partial-extra"
	_, err = CompleteMembershipPayment(input)
	require.Error(t, err, "a partial schema must not silently commit without invoice protection")
	var count int64
	require.NoError(t, DB.Model(&MembershipPaymentReceipt{}).Where("receipt_id = ?", input.ReceiptID).Count(&count).Error)
	assert.Zero(t, count, "receipt and invoice state protection share the owning transaction")
}
