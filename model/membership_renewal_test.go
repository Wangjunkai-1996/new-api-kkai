package model

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipRenewalCarriesTailQuotaAndOriginalReservationsOnce(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	_, original := buyMembershipTest(t, user.Id, catalog, "gold", "original")
	require.NoError(t, DB.Model(original).Updates(map[string]any{
		"amount_used": 97, "expires_at": common.GetTimestamp() + 9*24*60*60,
	}).Error)
	reserved, err := ReserveMembershipBilling(membershipReservation(t, user.Id, "in-flight", 2, 2))
	require.NoError(t, err)
	assert.EqualValues(t, 1, reserved.RemainingQuota)

	order, renewed := buyMembershipTest(t, user.Id, catalog, "gold", "renewal")
	var terms MembershipOrderTerms
	require.NoError(t, common.UnmarshalJsonStr(order.Terms, &terms))
	assert.Equal(t, "renew", terms.Action)
	assert.Equal(t, original.ID, renewed.ID)
	assert.Equal(t, original.Revision+1, renewed.Revision)
	assert.EqualValues(t, 200, renewed.AmountTotal)
	assert.EqualValues(t, 97, renewed.AmountUsed)
	assert.EqualValues(t, 2, renewed.AmountReserved)
	assert.EqualValues(t, 101, renewed.Available())
	assert.Equal(t, order.PaidAt+MembershipDurationSeconds, renewed.ExpiresAt)

	_, err = CompleteMembershipPayment(MembershipPaymentInput{
		TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: order.ReceiptID,
		AmountMinor: order.AmountMinor, Currency: order.Currency, OneTime: true, Paid: true,
	})
	require.NoError(t, err)
	var replayed MembershipAccount
	require.NoError(t, DB.First(&replayed, renewed.ID).Error)
	assert.Equal(t, *renewed, replayed)

	settled, err := SettleMembershipBilling("in-flight", 1)
	require.NoError(t, err)
	assert.Equal(t, reserved.Snapshot, settled.Snapshot)
	assert.Equal(t, reserved.AccountRevision, settled.AccountRevision)
	require.NoError(t, DB.First(&replayed, renewed.ID).Error)
	assert.EqualValues(t, 98, replayed.AmountUsed)
	assert.Zero(t, replayed.AmountReserved)
	assert.EqualValues(t, 102, replayed.Available())
}

func TestMembershipRenewalRechecksAccountAtPayment(t *testing.T) {
	for _, scenario := range []string{"exhausted", "expired", "revision changed", "account replaced", "cancelled", "quota overflow"} {
		t.Run(scenario, func(t *testing.T) {
			user, catalog := setupMembershipTest(t)
			_, original := buyMembershipTest(t, user.Id, catalog, "gold", "original")
			order, err := CreateMembershipOrder(MembershipOrderInput{
				UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: PaymentProviderEpay,
				TradeNo: "renewal", ExpiresAt: common.GetTimestamp() + 600,
				ExpectedAccountID: original.ID, ExpectedRevision: original.Revision,
			})
			require.NoError(t, err)
			switch scenario {
			case "exhausted":
				_, err = ReserveMembershipBilling(membershipReservation(t, user.Id, "consume-rest", 100, 100))
				require.NoError(t, err)
				_, err = SettleMembershipBilling("consume-rest", 100)
				require.NoError(t, err)
			case "expired":
				_, err = ReserveMembershipBilling(membershipReservation(t, user.Id, "old-in-flight", 20, 20))
				require.NoError(t, err)
				require.NoError(t, DB.Model(original).Update("expires_at", common.GetTimestamp()-1).Error)
			case "revision changed":
				require.NoError(t, DB.Model(original).Update("revision", original.Revision+1).Error)
			case "account replaced":
				replacement := *original
				replacement.ID = 0
				require.NoError(t, DB.Create(&replacement).Error)
				require.NoError(t, DB.Model(&MembershipUserState{}).Where("user_id = ?", user.Id).Update("current_account_id", replacement.ID).Error)
			case "cancelled":
				require.NoError(t, DB.Model(original).Update("status", MembershipCancelled).Error)
			case "quota overflow":
				require.NoError(t, DB.Model(original).Update("amount_total", math.MaxInt64-50).Error)
			}
			before, err := GetMembershipSelf(user.Id)
			require.NoError(t, err)
			paid, err := CompleteMembershipPayment(MembershipPaymentInput{
				TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: "renewal-receipt",
				AmountMinor: order.AmountMinor, Currency: order.Currency, OneTime: true, Paid: true,
			})
			require.NoError(t, err)
			after, err := GetMembershipSelf(user.Id)
			require.NoError(t, err)
			require.NotNil(t, after.Account)
			if scenario != "exhausted" && scenario != "expired" {
				assert.Equal(t, MembershipOrderException, paid.Status)
				assert.Equal(t, before.Account, after.Account)
				return
			}
			assert.Equal(t, MembershipOrderSuccess, paid.Status)
			assert.Equal(t, MembershipActive, after.Account.Status)
			assert.EqualValues(t, 100, after.Account.Available())
			assert.Equal(t, paid.PaidAt+MembershipDurationSeconds, after.Account.ExpiresAt)
			if scenario == "exhausted" {
				assert.Equal(t, original.ID, after.Account.ID)
				assert.Equal(t, original.Revision+1, after.Account.Revision)
				assert.EqualValues(t, 200, after.Account.AmountTotal)
				assert.EqualValues(t, 100, after.Account.AmountUsed)
				return
			}
			assert.NotEqual(t, original.ID, after.Account.ID)
			assert.EqualValues(t, 100, after.Account.AmountTotal)
			_, err = ReleaseMembershipBilling("old-in-flight")
			require.NoError(t, err)
			require.NoError(t, DB.First(original, original.ID).Error)
			assert.Equal(t, MembershipExpired, original.Status)
			assert.Zero(t, original.AmountReserved)
			var current MembershipAccount
			require.NoError(t, DB.First(&current, after.Account.ID).Error)
			assert.Equal(t, *after.Account, current, "expired quota refunds must stay in the original account")
		})
	}
}
