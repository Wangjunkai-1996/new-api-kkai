package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// holdMembershipInvoiceForPaymentExceptionTx runs while the source membership
// payment is locked. It never locks another payment, and uses current reads so
// a MySQL repeatable-read snapshot cannot hide a claim committed while this
// callback was waiting for that source payment lock.
func holdMembershipInvoiceForPaymentExceptionTx(tx *gorm.DB, order MembershipOrder, now int64) error {
	keys := []string{}
	if order.ReceiptKey != nil {
		receipt := sha256.Sum256([]byte(*order.ReceiptKey))
		canonical := sha256.Sum256([]byte("payment:receipt:v1:0:" + hex.EncodeToString(receipt[:])))
		keys = append(keys, hex.EncodeToString(canonical[:]))
	}
	return holdInvoiceForPaymentExceptionTx(tx, order.ActualProvider, order.TradeNo, order.UserID, now, keys...)
}

func holdInvoiceForPaymentExceptionTx(tx *gorm.DB, provider, tradeNo string, userID int, now int64, receiptKeys ...string) error {
	tables, err := tx.Migrator().GetTables()
	if err != nil {
		return err
	}
	present := make(map[string]bool, len(tables))
	hasInvoiceSchema := false
	for _, table := range tables {
		present[table] = true
		hasInvoiceSchema = hasInvoiceSchema || strings.HasPrefix(table, "invoice_")
	}
	if !hasInvoiceSchema {
		return nil // The additive invoice expansion is optional for existing payments.
	}
	for _, table := range []string{
		"invoice_requests", "invoice_request_items", "invoice_order_claims",
		"invoice_attachments", "invoice_deliveries", "invoice_disputes",
		"invoice_events", "invoice_event_reads", "invoice_operations", "invoice_profiles", "invoice_payment_receipts", "kkai_outbox",
	} {
		if !present[table] {
			return fmt.Errorf("invoice payment safety schema is incomplete")
		}
	}
	canonical := sha256.Sum256([]byte("payment:v1:0:" + provider + ":" + tradeNo))
	keys := append([]string{hex.EncodeToString(canonical[:])}, receiptKeys...)
	// The immutable item retains the original receipt-derived claim identity even
	// when the source payment now carries another receipt or an anomaly. Keep the
	// old merchant-trade key above for requests created by earlier invoice code.
	var items []InvoiceRequestItem
	if err := lockForUpdate(tx).Where("tenant_id = ? AND user_id = ? AND order_no = ? AND payment_label = ?", PlatformTenantID, userID, tradeNo, provider).Order("id ASC").Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		keys = append(keys, item.CanonicalPaymentKey)
	}
	var claims []InvoiceOrderClaim
	if err := lockForUpdate(tx).Where("tenant_id = ? AND canonical_payment_key IN ?", PlatformTenantID, keys).Order("canonical_payment_key ASC").Find(&claims).Error; err != nil {
		return err
	}
	requestIDs := make([]int64, 0, len(claims))
	for _, claim := range claims {
		requestIDs = append(requestIDs, claim.RequestID)
	}
	slices.Sort(requestIDs)
	for _, requestID := range slices.Compact(requestIDs) {
		if err := holdInvoiceRequestForPaymentExceptionTx(tx, requestID, now); err != nil {
			return err
		}
	}
	return nil
}

func holdInvoiceRequestForPaymentExceptionTx(tx *gorm.DB, requestID int64, now int64) error {
	var request InvoiceRequest
	if err := lockForUpdate(tx).Where("id = ? AND tenant_id = ?", requestID, PlatformTenantID).First(&request).Error; err != nil {
		return err
	}
	if request.Status == InvoiceRejected || request.Status == InvoiceFinanceHold {
		return nil
	}
	switch request.Status {
	case InvoicePendingReview, InvoiceApprovedForIssue, InvoiceDelivered, InvoiceConfirmed, InvoiceDisputed, InvoiceAwaitingReconfirmation:
	default:
		return fmt.Errorf("invoice payment safety state is invalid")
	}
	if request.Version <= 0 || request.Version == math.MaxInt64 {
		return fmt.Errorf("invoice payment safety version is invalid")
	}
	if err := tx.Model(&request).Updates(map[string]any{
		"status": InvoiceFinanceHold, "hold_from_status": request.Status,
		"version": request.Version + 1, "updated_at": now,
	}).Error; err != nil {
		return err
	}
	publicID, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return err
	}
	event := InvoiceEvent{
		PublicID: "ie_" + publicID, TenantID: request.TenantID, RequestID: request.ID,
		UserID: request.UserID, ActorID: 0, ActorAdmin: true, Kind: "hold", Status: InvoiceFinanceHold,
		DeliveryVersion: request.CurrentDeliveryVersion, CreatedAt: now,
	}
	if err := tx.Create(&event).Error; err != nil {
		return err
	}
	payload, err := common.Marshal(map[string]int64{"event_id": event.ID})
	if err != nil {
		return err
	}
	return tx.Create(&KKAIOutboxEvent{
		EventKey: "invoice:event:" + event.PublicID, Topic: "invoice.notification.v1",
		AggregateID: strconv.FormatInt(request.ID, 10), Payload: string(payload),
		Status: KKAIOutboxStatusPending, AvailableAt: now, CreatedAt: now,
	}).Error
}
