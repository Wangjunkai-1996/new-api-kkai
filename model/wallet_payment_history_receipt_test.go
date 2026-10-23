package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalletHistoryPaidAmountExcludesSecondPaymentUnderReview(t *testing.T) {
	db := setupWalletPaymentDB(t)
	orderID, otherID := 100, 101
	for _, receipt := range []WalletPaymentReceipt{
		{ReceiptKey: "settled", TopUpID: &orderID, AmountMinor: 200, Currency: "USD", CurrencyExponent: 2, VerificationStatus: "verified", ProcessingStatus: "credited"},
		{ReceiptKey: "second-payment", TopUpID: &orderID, AmountMinor: 300, Currency: "USD", CurrencyExponent: 2, VerificationStatus: "verified", ProcessingStatus: "review"},
		{ReceiptKey: "other-order", TopUpID: &otherID, AmountMinor: 900, Currency: "USD", CurrencyExponent: 2, VerificationStatus: "verified", ProcessingStatus: "credited"},
	} {
		require.NoError(t, db.Create(&receipt).Error)
	}
	result, err := GetWalletCreditedReceipts([]int{orderID})
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.EqualValues(t, 200, result[orderID].AmountMinor, "the original receipt remains the displayed paid amount; a second payment needs separate review")
	assert.NotContains(t, result, otherID)
}
