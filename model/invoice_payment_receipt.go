package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type TopUpPaymentReceipt struct {
	ProviderReceiptID string
	AmountMinor       int64
	Currency          string
}

var ErrInvoicePaymentException = errors.New("invoice payment requires financial review")

// ReconcileInvoiceTopUpPayment records independently verified provider evidence
// for an already successful order. It never credits or completes a payment.
func ReconcileInvoiceTopUpPayment(ctx context.Context, db *gorm.DB, tenantID int64, userID int, tradeNo string, receipt TopUpPaymentReceipt, paidAt int64) error {
	now := common.GetTimestamp()
	if ctx == nil || db == nil || tenantID != PlatformTenantID || userID <= 0 || strings.TrimSpace(tradeNo) == "" || len(tradeNo) > 255 || !utf8.ValidString(tradeNo) || strings.ContainsAny(tradeNo, "\x00\r\n") || receipt.AmountMinor <= 0 || receipt.Currency != ResellerCurrencyCNY || strings.TrimSpace(receipt.ProviderReceiptID) == "" || len(receipt.ProviderReceiptID) > 255 || !utf8.ValidString(receipt.ProviderReceiptID) || strings.ContainsAny(receipt.ProviderReceiptID, "\x00\r\n") || paidAt <= 0 || paidAt > now {
		return ErrResellerFinanceInvalidInput
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		paymentException := false
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var topUp TopUp
			if err := lockForUpdate(tx).Where("tenant_id = ? AND user_id = ? AND trade_no = ?", tenantID, userID, tradeNo).First(&topUp).Error; err != nil {
				return err
			}
			if topUp.Status != common.TopUpStatusSuccess || topUp.PaymentProvider != PaymentProviderEpay || (topUp.Currency != "" && topUp.Currency != ResellerCurrencyCNY) || topUp.PaidMinor < 0 || (topUp.CreateTime > 0 && paidAt < topUp.CreateTime) {
				return ErrResellerFinanceInvalidInput
			}
			var membership MembershipOrder
			// Trade identities are immutable. Do not invert the membership payment
			// lock order by locking its order after this top-up mirror.
			member := tx.Select("id").Where("trade_no = ?", tradeNo).Limit(1).Find(&membership)
			if member.Error != nil {
				return member.Error
			}
			if member.RowsAffected != 0 {
				return ErrResellerFinanceInvalidInput
			}
			var subscription SubscriptionOrder
			legacy := tx.Select("id").Where("trade_no = ?", tradeNo).Limit(1).Find(&subscription)
			if legacy.Error != nil {
				return legacy.Error
			}
			if legacy.RowsAffected != 0 {
				return ErrResellerFinanceInvalidInput
			}
			// Unlike callbacks, explicit reconciliation requires the invoice schema;
			// its absence must not become a successful no-op.
			var previous InvoicePaymentReceipt
			if err := lockForUpdate(tx).Where("top_up_id = ?", topUp.Id).Limit(1).Find(&previous).Error; err != nil {
				return err
			}
			// Existing evidence conflicts must reach the recorder so their invoice
			// hold survives the transaction instead of leaving old proof usable.
			if previous.ID == 0 && topUp.PaidMinor > 0 && topUp.PaidMinor != receipt.AmountMinor {
				return ErrResellerFinanceInvalidInput
			}
			updates := map[string]any{}
			if topUp.PaidMinor == 0 {
				topUp.PaidMinor = receipt.AmountMinor
				updates["paid_minor"] = topUp.PaidMinor
			}
			if topUp.Currency == "" {
				topUp.Currency = receipt.Currency
				updates["currency"] = topUp.Currency
			}
			if topUp.CompleteTime <= 0 {
				topUp.CompleteTime = paidAt
				updates["complete_time"] = paidAt
			}
			if len(updates) != 0 {
				if err := tx.Model(&topUp).Updates(updates).Error; err != nil {
					return err
				}
			}
			if err := recordInvoiceTopUpReceiptTx(tx, topUp, receipt, now); err != nil {
				return err
			}
			var proof InvoicePaymentReceipt
			if err := tx.Where("top_up_id = ?", topUp.Id).First(&proof).Error; err != nil {
				return err
			}
			if proof.Status == "verified" && (previous.ID == 0 || proof.PaidAt <= 0) {
				if err := tx.Model(&proof).Update("paid_at", paidAt).Error; err != nil {
					return err
				}
			}
			paymentException = proof.Status != "verified"
			return nil // Commit any newly detected exception before reporting it.
		})
		if err == nil {
			if paymentException {
				return ErrInvoicePaymentException
			}
			return nil
		}
		lastErr = err
		if !isSQLiteBusyError(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 5 * time.Millisecond):
		}
	}
	return lastErr
}

// recordInvoiceTopUpReceiptTx runs under the original payment row lock, before
// success/replay returns. Checkout estimates and manual completions never call it.
func recordInvoiceTopUpReceiptTx(tx *gorm.DB, topUp TopUp, receipt TopUpPaymentReceipt, now int64) error {
	if topUp.TenantID != PlatformTenantID {
		return nil // Tenant payments keep their existing financial validation path.
	}
	if topUp.PaymentProvider != PaymentProviderEpay || receipt.AmountMinor <= 0 || receipt.Currency != ResellerCurrencyCNY || strings.TrimSpace(receipt.ProviderReceiptID) == "" || len(receipt.ProviderReceiptID) > 255 || !utf8.ValidString(receipt.ProviderReceiptID) || strings.ContainsAny(receipt.ProviderReceiptID, "\x00\r\n") {
		return ErrResellerFinanceInvalidInput
	}
	matchesExpected := receipt.AmountMinor == topUp.PaidMinor && topUp.Currency == receipt.Currency
	if topUp.Status == common.TopUpStatusPending && topUp.PaidMinor > 0 && !matchesExpected {
		return ErrResellerFinanceInvalidInput
	}
	// Older orders lack an exact expected amount. Continue their existing payment
	// workflow, but do not turn a floating-point estimate into invoice evidence.
	if topUp.PaidMinor <= 0 {
		return nil
	}
	tables, err := tx.Migrator().GetTables()
	if err != nil {
		return err
	}
	present, hasInvoiceSchema := false, false
	for _, table := range tables {
		present = present || table == "invoice_payment_receipts"
		hasInvoiceSchema = hasInvoiceSchema || strings.HasPrefix(table, "invoice_")
	}
	if !present {
		if hasInvoiceSchema {
			return fmt.Errorf("invoice payment safety schema is incomplete")
		}
		return nil // Invoice expansion is optional until its controlled migration.
	}
	key := sha256.Sum256([]byte(topUp.PaymentProvider + ":" + receipt.ProviderReceiptID))
	proof := InvoicePaymentReceipt{TenantID: topUp.TenantID, UserID: topUp.UserId, TopUpID: int64(topUp.Id), Provider: topUp.PaymentProvider, ReceiptKey: hex.EncodeToString(key[:]), AmountMinor: receipt.AmountMinor, Currency: receipt.Currency, Status: "verified", PaidAt: now, CreatedAt: now}
	if !matchesExpected {
		proof.Status = "exception"
		proof.ExceptionReceiptKey = proof.ReceiptKey
	}
	canonical := sha256.Sum256([]byte("payment:receipt:v1:0:" + proof.ReceiptKey))
	var existing InvoicePaymentReceipt
	err = lockForUpdate(tx).Where("top_up_id = ?", topUp.Id).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := tx.Create(&proof).Error; err != nil {
			return err
		}
		if proof.Status == "exception" {
			return holdInvoiceForPaymentExceptionTx(tx, topUp.PaymentProvider, topUp.TradeNo, topUp.UserId, now, hex.EncodeToString(canonical[:]))
		}
		return nil
	}
	if err != nil {
		return err
	}
	if matchesExpected && existing.ReceiptKey == proof.ReceiptKey && existing.AmountMinor == proof.AmountMinor && existing.Currency == proof.Currency && existing.Provider == proof.Provider && existing.UserID == proof.UserID && existing.TenantID == proof.TenantID {
		return nil // Provider replay must not reset a previously detected anomaly.
	}
	if err := tx.Model(&existing).Updates(map[string]any{"status": "exception", "exception_receipt_key": proof.ReceiptKey}).Error; err != nil {
		return err
	}
	existingCanonical := sha256.Sum256([]byte("payment:receipt:v1:0:" + existing.ReceiptKey))
	return holdInvoiceForPaymentExceptionTx(tx, topUp.PaymentProvider, topUp.TradeNo, topUp.UserId, now, hex.EncodeToString(canonical[:]), hex.EncodeToString(existingCanonical[:]))
}
