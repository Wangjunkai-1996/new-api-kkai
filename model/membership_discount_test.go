package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipDiscountChangesApplyToExistingAndNewMembers(t *testing.T) {
	for _, tierID := range []string{"gold", "diamond", "king"} {
		t.Run(tierID, func(t *testing.T) {
			user, catalog := setupMembershipTest(t)
			order, account := buyMembershipTest(t, user.Id, catalog, tierID, "original")
			for _, coefficient := range []float64{0.6, 0.85, 1} {
				for i := range catalog.Tiers {
					catalog.Tiers[i].Discounts["gpt"] = coefficient
				}
				var err error
				catalog, err = PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
				require.NoError(t, err)
				newUser := createReserveTestUser(t, 500)
				buyMembershipTest(t, newUser.Id, catalog, tierID, "new-"+newUser.Username)
				for _, userID := range []int{user.Id, newUser.Id} {
					quote, err := GetMembershipQuote(userID, "gpt", 0.2, 0.2)
					require.NoError(t, err)
					assert.Equal(t, coefficient, quote.Discount)
					for _, tier := range catalog.Tiers {
						if tier.ID == tierID {
							assert.Equal(t, tier.Name, quote.TierName)
							assert.Equal(t, tier.Style, quote.TierStyle)
						}
					}
					assert.InDelta(t, 0.2*coefficient, quote.MembershipRatio, 1e-9)
					assert.Equal(t, catalog.ReleaseID, quote.ReleaseID)
				}
				self, err := GetMembershipSelf(user.Id)
				require.NoError(t, err)
				require.NotNil(t, self.Account)
				require.NotNil(t, self.Tier)
				assert.Equal(t, coefficient, self.Discounts["gpt"])
				assert.Equal(t, coefficient, self.Tier.Discounts["gpt"])
				assert.Equal(t, *account, *self.Account, "publishing discounts must not rewrite purchased quota or expiry")
				stored, err := GetMembershipOrder(order.TradeNo, user.Id)
				require.NoError(t, err)
				assert.Equal(t, order.Terms, stored.Terms)
				assert.Equal(t, order.AmountMinor, stored.AmountMinor)
			}
		})
	}
}

func TestMembershipDiscountPublicationRejectsUnreservedQuotesAndPreservesReservations(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	buyMembershipTest(t, user.Id, catalog, "gold", "original")
	authorized := membershipReservation(t, user.Id, "authorized", 10, 11)
	reserved, err := ReserveMembershipBilling(authorized)
	require.NoError(t, err)
	stale := membershipReservation(t, user.Id, "unreserved", 10, 11)
	for i := range catalog.Tiers {
		catalog.Tiers[i].Discounts["gpt"] = 0.6
	}
	catalog, err = PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
	require.NoError(t, err)
	_, err = ReserveMembershipBilling(stale)
	assert.ErrorIs(t, err, ErrMembershipQuoteChanged)
	stale.ExpectedReleaseID = 0
	_, err = ReserveMembershipBilling(stale)
	assert.ErrorIs(t, err, ErrMembershipQuoteChanged)
	var count int64
	require.NoError(t, DB.Model(&MembershipBillingRecord{}).Where("request_id = ?", stale.RequestID).Count(&count).Error)
	assert.Zero(t, count)
	replayed, err := ReserveMembershipBilling(authorized)
	require.NoError(t, err)
	assert.False(t, replayed.Applied)
	assert.Equal(t, reserved.Snapshot, replayed.Snapshot)
	settled, err := SettleMembershipBilling(authorized.RequestID, 8)
	require.NoError(t, err)
	assert.Equal(t, reserved.Snapshot, settled.Snapshot)
	fresh := membershipReservation(t, user.Id, stale.RequestID, 6, 11)
	assert.Equal(t, catalog.ReleaseID, fresh.ExpectedReleaseID)
	record, err := ReserveMembershipBilling(fresh)
	require.NoError(t, err)
	assert.Equal(t, MembershipSource, record.Source)
	assert.EqualValues(t, 6, record.ReservedQuota)
}

func TestMembershipLatePaymentGrantRequiresTierInCurrentCatalog(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	order, err := CreateMembershipOrder(MembershipOrderInput{UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: PaymentProviderEpay, TradeNo: "removed-tier", ExpiresAt: common.GetTimestamp() + 600})
	require.NoError(t, err)
	_, err = CloseMembershipOrder(order.TradeNo, user.Id)
	require.NoError(t, err)
	paid, err := CompleteMembershipPayment(MembershipPaymentInput{TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: "late-receipt", AmountMinor: order.AmountMinor, Currency: order.Currency, OneTime: true, Paid: true})
	require.NoError(t, err)
	require.Equal(t, MembershipOrderException, paid.Status)
	gold := catalog.Tiers[0]
	catalog.Tiers = catalog.Tiers[1:]
	catalog, err = PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
	require.NoError(t, err)
	_, err = ResolveMembershipOrder(order.TradeNo, 1, "grant-removed", "grant", "Verified late receipt")
	require.ErrorIs(t, err, ErrMembershipConflict)
	stored, err := GetMembershipOrder(order.TradeNo, user.Id)
	require.NoError(t, err)
	assert.Equal(t, MembershipOrderException, stored.Status)
	assert.Equal(t, paid.PaidAt, stored.PaidAt)
	assert.Zero(t, stored.ResolvedAt)
	self, err := GetMembershipSelf(user.Id)
	require.NoError(t, err)
	assert.Nil(t, self.Account)
	gold.Enabled = false
	gold.Discounts["gpt"] = 0.99
	catalog.Tiers = append(catalog.Tiers, gold)
	_, err = PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
	require.NoError(t, err)
	granted, err := ResolveMembershipOrder(order.TradeNo, 1, "grant-restored", "grant", "Restored tier for verified payment")
	require.NoError(t, err)
	assert.Equal(t, MembershipOrderSuccess, granted.Status)
	quote, err := GetMembershipQuote(user.Id, "gpt", 0.2, 0.2)
	require.NoError(t, err)
	assert.Equal(t, 0.99, quote.Discount, "stopped sales must not cancel an active member's latest discount")
}
