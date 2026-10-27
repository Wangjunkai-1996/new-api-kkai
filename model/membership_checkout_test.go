package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipOrderKeepsRemoteCheckoutPending(t *testing.T) {
	for _, provider := range []string{PaymentProviderStripe, PaymentProviderCreem, PaymentProviderWaffoPancake} {
		for _, checkoutState := range []string{"creating", "ready"} {
			t.Run(provider+"/"+checkoutState, func(t *testing.T) {
				user, catalog := setupMembershipTest(t)
				first, err := CreateMembershipOrder(MembershipOrderInput{
					UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: provider,
					PaymentMethod: provider, TradeNo: "remote-checkout-" + provider, ExpiresAt: common.GetTimestamp() + 600,
				})
				require.NoError(t, err)
				require.NoError(t, DB.Model(first).Updates(map[string]any{
					"checkout_state": checkoutState, "checkout_started_at": common.GetTimestamp(), "checkout_data": `{"payment_url":"https://payments.example/checkout"}`,
				}).Error)

				_, err = CreateMembershipOrder(MembershipOrderInput{
					UserID: user.Id, TierID: "diamond", ReleaseID: catalog.ReleaseID, Provider: provider,
					PaymentMethod: provider, TradeNo: "remote-checkout-replacement-" + provider, ExpiresAt: common.GetTimestamp() + 600,
				})
				assert.ErrorIs(t, err, ErrMembershipConflict)
				var stored MembershipOrder
				require.NoError(t, DB.First(&stored, first.ID).Error)
				assert.Equal(t, MembershipOrderPending, stored.Status)
				assert.Equal(t, checkoutState, stored.CheckoutState)
			})
		}
	}
}

func TestMembershipEpayStaleCheckoutClaimCanRecoverWithCompareAndSwap(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	order, err := CreateMembershipOrder(MembershipOrderInput{
		UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: PaymentProviderEpay,
		PaymentMethod: "alipay", TradeNo: "epay-stale-claim", ExpiresAt: common.GetTimestamp() + 600,
	})
	require.NoError(t, err)
	startedAt := time.Now().Unix() - int64(membershipEpayCheckoutRecoveryAfter/time.Second) - 1
	require.NoError(t, DB.Model(order).Updates(map[string]any{
		"checkout_state": "creating", "checkout_started_at": startedAt, "checkout_data": "",
	}).Error)

	claimed, create, err := ClaimMembershipCheckout(order.TradeNo, user.Id)
	require.NoError(t, err)
	assert.True(t, create)
	assert.Equal(t, "creating", claimed.CheckoutState)
	assert.Greater(t, claimed.CheckoutStartedAt, startedAt)
	require.NoError(t, SaveMembershipCheckoutClaim(order.TradeNo, claimed.CheckoutStartedAt, `{"submit_url":"https://pay.example/submit"}`))
	assert.ErrorIs(t, SaveMembershipCheckoutClaim(order.TradeNo, startedAt, `{"submit_url":"https://pay.example/stale"}`), ErrMembershipConflict)
}

func TestMembershipRemoteCreatingCheckoutRemainsForReview(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	order, err := CreateMembershipOrder(MembershipOrderInput{
		UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: PaymentProviderStripe,
		PaymentMethod: PaymentProviderStripe, TradeNo: "stripe-stale-claim", ExpiresAt: common.GetTimestamp() + 600,
	})
	require.NoError(t, err)
	startedAt := time.Now().Unix() - int64(membershipRemoteCheckoutReviewAfter/time.Second) - 1
	require.NoError(t, DB.Model(order).Updates(map[string]any{
		"checkout_state": "creating", "checkout_started_at": startedAt,
	}).Error)

	_, create, err := ClaimMembershipCheckout(order.TradeNo, user.Id)
	assert.ErrorIs(t, err, ErrMembershipConflict)
	assert.False(t, create)
	var stored MembershipOrder
	require.NoError(t, DB.First(&stored, order.ID).Error)
	assert.Equal(t, MembershipOrderPending, stored.Status)
	assert.Equal(t, "creating", stored.CheckoutState)
}
