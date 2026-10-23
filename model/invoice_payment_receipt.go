package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type TopUpPaymentReceipt struct {
	ProviderReceiptID string
	AmountMinor       int64
	Currency          string
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
