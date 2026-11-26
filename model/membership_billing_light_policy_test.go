package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMembershipStandardLightHoldFundingAndSettlement(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	buyMembershipTest(t, user.Id, catalog, "gold", "standard-funding")
	t.Setenv("KKAI_MEMBERSHIP_PILOT_HOLD_USER_IDS", "*")
	token := Token{UserId: user.Id, Key: "standard-key", Status: common.TokenStatusEnabled, RemainQuota: 5, ExpiredTime: -1}
	require.NoError(t, DB.Create(&token).Error)
	input := membershipReservation(t, user.Id, "standard", 4, 5)
	input.PilotTextHTTP, input.TokenID, input.TokenKey, input.SkipToken = true, token.Id, token.Key, false
	for _, tc := range []struct{ quote, hold int64 }{{10, 3}, {4, 2}, {19, 3}, {40, 20}} {
		input.RequestID = fmt.Sprintf("standard-%d", tc.quote)
		input.SubscriptionQuota = tc.quote
		input.SubscriptionSnapshot = fmt.Sprintf(`{"version":1,"quota_per_unit":10,"price":{"QuotaToPreConsume":%d}}`, tc.quote)
		if tc.quote == 10 {
			_, err := ReserveMembershipBilling(input)
			require.ErrorIs(t, err, ErrTaskBillingInsufficientToken)
			require.NoError(t, DB.Model(&token).Update("remain_quota", 100).Error)
		}
		r, err := ReserveMembershipBilling(input)
		require.NoError(t, err)
		require.Equal(t, tc.hold, r.ReservedQuota)
		assert.Equal(t, tc.hold, r.TokenReserved)
		var price types.MembershipPricing
		require.NoError(t, common.UnmarshalJsonStr(r.Snapshot, &price))
		assert.Zero(t, price.FullReservedQuota)
		assert.Equal(t, tc.quote, price.StandardFullReservedQuota)
		// Replaying through an outer transaction restores the committed quote.
		require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
			replay, err := ReserveMembershipBillingTx(tx, input)
			if err == nil {
				assert.False(t, replay.Applied)
				assert.Equal(t, tc.hold, replay.ReservedQuota)
			}
			return err
		}))
		final, err := SettleMembershipBilling(input.RequestID, tc.quote+1)
		require.NoError(t, err)
		assert.Equal(t, tc.hold, final.ChargedQuota)
		assert.Equal(t, tc.quote+1, final.ActualQuota)
	}
	var legacyCounters int64
	require.NoError(t, DB.Model(&Option{}).Where(&Option{Key: membershipPilotHold}).Count(&legacyCounters).Error)
	assert.Zero(t, legacyCounters)
	input.RequestID = "existing-outer"
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		record, err := ReserveMembershipBillingTx(tx, input)
		if err == nil {
			assert.Equal(t, input.SubscriptionQuota, record.ReservedQuota)
		}
		return err
	}))
}

func TestMembershipStandardLightHoldExposureFallback(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	buyMembershipTest(t, user.Id, catalog, "gold", "standard-exposure")
	t.Setenv("KKAI_MEMBERSHIP_PILOT_HOLD_USER_IDS", "*")
	input := membershipReservation(t, user.Id, "user-limit", 30, 35)
	input.PilotTextHTTP = true
	input.SubscriptionSnapshot = `{"version":1,"quota_per_unit":10,"price":{"QuotaToPreConsume":30}}`
	// The previous pilot's outstanding discounted holds also occupy capacity.
	var records []MembershipBillingRecord
	for i := range 50 {
		records = append(records, MembershipBillingRecord{
			UserID: user.Id, RequestID: fmt.Sprintf("previous-hold-%d", i), Scope: MembershipBillingScopeRelay,
			Source: MembershipSource, State: "reserved", ReservedQuota: 20,
			Snapshot: `{"quota_per_unit":10,"full_reserved_quota":40}`,
		})
	}
	require.NoError(t, DB.Create(&records).Error)
	full, err := ReserveMembershipBilling(input)
	require.NoError(t, err)
	assert.EqualValues(t, 30, full.ReservedQuota)
	_, err = ReleaseMembershipBilling(full.RequestID)
	require.NoError(t, err)
	// An older backend can finish a record without implementing the new policy.
	require.NoError(t, DB.Model(&records[0]).Update("state", "released").Error)
	input.RequestID = "available-again"
	reduced, err := ReserveMembershipBilling(input)
	require.NoError(t, err)
	assert.EqualValues(t, 10, reduced.ReservedQuota)

	// Incomplete risk metadata must fail closed, without failing the request.
	require.NoError(t, DB.Model(&records[0]).Updates(map[string]any{
		"state": "reserved", "snapshot": `{"standard_full_reserved_quota":30,"standard_hold_risk_micros":"broken"}`,
	}).Error)
	_, err = ReleaseMembershipBilling(reduced.RequestID)
	require.NoError(t, err)
	input.RequestID = "unknown-risk"
	guarded, err := ReserveMembershipBilling(input)
	require.NoError(t, err)
	assert.EqualValues(t, 30, guarded.ReservedQuota)
}
