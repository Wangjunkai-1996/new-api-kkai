package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateMembershipEpaySnapshotRejectsUnsafeURLs(t *testing.T) {
	t.Setenv("CRYPTO_SECRET", "")
	t.Setenv("WALLET_PAYMENT_SECRET", strings.Repeat("a", 32))
	t.Setenv("WALLET_PAYMENT_PREVIOUS_SECRETS", "")
	order := &model.MembershipOrder{
		UserID: 123, TradeNo: "MEMBER-snapshot", Provider: model.PaymentProviderEpay,
		PaymentMethod: "wxpay", Currency: "CNY",
	}
	tests := []struct {
		name    string
		address string
	}{
		{name: "HTTP", address: "http://merchant.example.test"},
		{name: "embedded credentials", address: "https://user:password@merchant.example.test"},
		{name: "query", address: "https://merchant.example.test?merchant=other"},
		{name: "fragment", address: "https://merchant.example.test#checkout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sealed, err := CreateMembershipEpaySnapshot(order, tt.address, "merchant-original", "original-merchant-api-key")
			require.ErrorIs(t, err, ErrMembershipPaymentUnavailable)
			assert.Empty(t, sealed)
			assert.NotContains(t, err.Error(), "original-merchant-api-key")
		})
	}
}

func TestCreateMembershipEpaySnapshotBindsOrderAndMerchant(t *testing.T) {
	t.Setenv("CRYPTO_SECRET", "")
	t.Setenv("WALLET_PAYMENT_SECRET", strings.Repeat("a", 32))
	t.Setenv("WALLET_PAYMENT_PREVIOUS_SECRETS", "")
	order := model.MembershipOrder{
		UserID: 123, TradeNo: "MEMBER-snapshot", Provider: model.PaymentProviderEpay,
		PaymentMethod: "wxpay", Currency: "CNY",
	}
	want := MembershipEpaySnapshot{
		Version: 1, APIURL: "https://merchant.example.test/original",
		MerchantID: "merchant-original", APIKey: "original-merchant-api-key", PaymentMethod: "wxpay",
	}
	sealed, err := CreateMembershipEpaySnapshot(&order, want.APIURL, want.MerchantID, want.APIKey)
	require.NoError(t, err)
	assert.NotContains(t, sealed, want.APIKey)
	var restored MembershipEpaySnapshot
	require.NoError(t, OpenWalletPayment(sealed, membershipEpayScope(&order, want.MerchantID), &restored))
	assert.Equal(t, want, restored)

	otherUser, otherOrder := order, order
	otherUser.UserID++
	otherOrder.TradeNo = "MEMBER-other"
	tests := []struct {
		name       string
		order      model.MembershipOrder
		merchantID string
	}{
		{name: "other user", order: otherUser, merchantID: want.MerchantID},
		{name: "other order", order: otherOrder, merchantID: want.MerchantID},
		{name: "other merchant", order: order, merchantID: "merchant-other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mismatched MembershipEpaySnapshot
			err := OpenWalletPayment(sealed, membershipEpayScope(&tt.order, tt.merchantID), &mismatched)
			require.ErrorIs(t, err, ErrWalletSecretUnavailable)
			assert.Equal(t, MembershipEpaySnapshot{}, mismatched)
		})
	}
}

func TestCreateMembershipEpaySnapshotSurvivesEncryptionKeyRotation(t *testing.T) {
	oldSecret := strings.Repeat("a", 32)
	t.Setenv("CRYPTO_SECRET", "")
	t.Setenv("WALLET_PAYMENT_SECRET", oldSecret)
	t.Setenv("WALLET_PAYMENT_PREVIOUS_SECRETS", "")
	order := &model.MembershipOrder{
		UserID: 123, TradeNo: "MEMBER-snapshot", Provider: model.PaymentProviderEpay,
		PaymentMethod: "wxpay", Currency: "CNY",
	}
	want := MembershipEpaySnapshot{
		Version: 1, APIURL: "https://merchant.example.test/original",
		MerchantID: "merchant-original", APIKey: "original-merchant-api-key", PaymentMethod: "wxpay",
	}
	sealed, err := CreateMembershipEpaySnapshot(order, want.APIURL, want.MerchantID, want.APIKey)
	require.NoError(t, err)
	assert.NotContains(t, sealed, want.APIKey)

	t.Setenv("WALLET_PAYMENT_SECRET", strings.Repeat("b", 32))
	var unavailable MembershipEpaySnapshot
	require.ErrorIs(t, OpenWalletPayment(sealed, membershipEpayScope(order, want.MerchantID), &unavailable), ErrWalletSecretUnavailable)
	assert.Equal(t, MembershipEpaySnapshot{}, unavailable)

	previous, err := common.Marshal([]string{oldSecret})
	require.NoError(t, err)
	t.Setenv("WALLET_PAYMENT_PREVIOUS_SECRETS", string(previous))
	var restored MembershipEpaySnapshot
	require.NoError(t, OpenWalletPayment(sealed, membershipEpayScope(order, want.MerchantID), &restored))
	assert.Equal(t, want, restored)
}
