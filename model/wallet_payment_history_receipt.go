package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type walletConfirmedPayment struct {
	TopUpID          int
	AmountMinor      *int64
	Currency         string
	CurrencyExponent int
}

func (payment walletConfirmedPayment) hasValidCurrency() bool {
	if len(payment.Currency) != 3 {
		return false
	}
	for i := 0; i < len(payment.Currency); i++ {
		if payment.Currency[i] < 'A' || payment.Currency[i] > 'Z' {
			return false
		}
	}
	return true
}

// walletConfirmedPaymentQuery supplies the same cash evidence to lifetime
// summaries, order history and details. A nil amount means evidence is missing.
func walletConfirmedPaymentQuery(q *gorm.DB, now int64) *gorm.DB {
	q = WalletTopUpOnlyQuery(q).Where("top_ups.status = ?", common.TopUpStatusSuccess).
		Where("COALESCE(top_ups.payment_provider, '') NOT IN ?", []string{PaymentProviderBalance, PaymentProviderRedemption})
	q = q.Joins(`LEFT JOIN wallet_payment_receipts AS paid ON paid.id = (
 SELECT MIN(r.id) FROM wallet_payment_receipts AS r
 WHERE r.top_up_id = top_ups.id AND r.processing_status = 'credited' AND r.verification_status = 'verified'
) AND paid.provider = top_ups.payment_provider AND paid.provider_account_ref = top_ups.provider_account_ref
 AND paid.provider_environment = top_ups.provider_environment AND paid.currency = top_ups.currency
 AND paid.currency_exponent = top_ups.currency_exponent AND paid.pricing_verified = ?
 AND paid.paid_at > 0 AND paid.paid_at <= ? AND paid.evidence_ref <> ''
 AND paid.amount_minor >= 0 AND paid.currency_exponent BETWEEN 0 AND 6
 AND (paid.amount_minor > 0 OR paid.settlement_kind = 'no_payment_required')`, true, now)
	if q.Session(&gorm.Session{NewDB: true}).Migrator().HasTable(&InvoicePaymentReceipt{}) {
		// Only legacy Epay orders accept independent invoice reconciliation proof.
		// New lifecycle evidence cannot be replaced by a different proof source.
		q = q.Joins(`LEFT JOIN invoice_payment_receipts AS legacy ON COALESCE(top_ups.wallet_lifecycle_version, 0) = 0
 AND legacy.top_up_id = top_ups.id AND legacy.user_id = top_ups.user_id AND legacy.tenant_id = top_ups.tenant_id
 AND legacy.provider = top_ups.payment_provider AND legacy.provider = ? AND legacy.currency = ?
 AND legacy.status = 'verified' AND legacy.receipt_key <> '' AND legacy.amount_minor > 0
 AND legacy.paid_at > 0 AND legacy.paid_at <= ?`, PaymentProviderEpay, ResellerCurrencyCNY, now)
		return q.Select(`top_ups.id AS top_up_id, COALESCE(paid.amount_minor, legacy.amount_minor) AS amount_minor,
 COALESCE(paid.currency, CASE WHEN legacy.id IS NOT NULL THEN 'CNY' END) AS currency,
 COALESCE(paid.currency_exponent, CASE WHEN legacy.id IS NOT NULL THEN 2 END) AS currency_exponent`)
	}
	return q.Select("top_ups.id AS top_up_id, paid.amount_minor, paid.currency, paid.currency_exponent")
}

// GetWalletCreditedReceipts projects the page's confirmed actual amounts.
// Returned IDs are order IDs; source receipts remain private to their ledgers.
func GetWalletCreditedReceipts(ids []int) (map[int]WalletPaymentReceipt, error) {
	result := make(map[int]WalletPaymentReceipt, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	if len(ids) > 100 {
		return nil, ErrResellerFinanceInvalidInput
	}
	var payments []walletConfirmedPayment
	err := walletConfirmedPaymentQuery(DB.Model(&TopUp{}).Where("top_ups.id IN ?", ids), common.GetTimestamp()).Scan(&payments).Error
	if err != nil {
		return nil, err
	}
	for _, payment := range payments {
		if payment.AmountMinor == nil {
			continue
		}
		if !payment.hasValidCurrency() {
			return nil, ErrWalletPaymentEvidence
		}
		result[payment.TopUpID] = WalletPaymentReceipt{AmountMinor: *payment.AmountMinor, Currency: payment.Currency, CurrencyExponent: payment.CurrencyExponent}
	}
	return result, nil
}
