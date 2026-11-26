package model

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMembershipTextPilotRequiresFullFundingAndCapsSettlement(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	_, account := buyMembershipTest(t, user.Id, catalog, "gold", "text-pilot")
	t.Setenv("KKAI_MEMBERSHIP_PILOT_HOLD_USER_IDS", strconv.Itoa(user.Id))
	token := Token{UserId: user.Id, Key: "limited-text-key", Status: common.TokenStatusEnabled, RemainQuota: 60, ExpiredTime: -1}
	require.NoError(t, DB.Create(&token).Error)
	input := membershipReservation(t, user.Id, "text-pilot", 80, 90)
	input.PilotTextHTTP, input.SkipToken, input.TokenID, input.TokenKey = true, false, token.Id, token.Key
	encoded, err := common.Marshal(types.MembershipPricing{
		Version: 1, GroupKey: input.GroupKey, QuotaPerUnit: 10, MaxOutputTokens: 4096,
		Price: types.PriceData{QuotaToPreConsume: 80},
	})
	require.NoError(t, err)
	input.SubscriptionSnapshot = string(encoded)

	// The reduced hold would fit, but the key must still authorize the full quote.
	_, err = ReserveMembershipBilling(input)
	require.ErrorIs(t, err, ErrTaskBillingInsufficientToken)
	assert.ErrorIs(t, DB.Where(&Option{Key: membershipPilotHold}).First(&Option{}).Error, gorm.ErrRecordNotFound)
	require.NoError(t, DB.Model(&token).Update("remain_quota", 100).Error)

	record, err := ReserveMembershipBilling(input)
	require.NoError(t, err)
	assert.Equal(t, MembershipSource, record.Source)
	assert.EqualValues(t, 60, record.ReservedQuota)
	assert.EqualValues(t, 40, record.RemainingQuota)
	var snapshot types.MembershipPricing
	require.NoError(t, common.UnmarshalJsonStr(record.Snapshot, &snapshot))
	assert.EqualValues(t, 80, snapshot.FullReservedQuota)
	assert.Equal(t, 80, snapshot.Price.QuotaToPreConsume)
	assert.Equal(t, 4096, snapshot.MaxOutputTokens)

	input.PilotTextHTTP = false
	replay, err := ReserveMembershipBilling(input)
	require.NoError(t, err)
	assert.False(t, replay.Applied)
	assert.Equal(t, record.ReservedQuota, replay.ReservedQuota)
	var budget Option
	require.NoError(t, DB.Where(&Option{Key: membershipPilotHold}).First(&budget).Error)
	assert.JSONEq(t, `{"`+strconv.Itoa(user.Id)+`":2000000}`, budget.Value)

	settled, err := SettleMembershipRelayBilling(input.RequestID, 90, 0, true)
	require.NoError(t, err)
	assert.EqualValues(t, 90, settled.ActualQuota)
	assert.EqualValues(t, 60, settled.ChargedQuota)
	assert.EqualValues(t, 30, settled.ExcessQuota)
	assert.EqualValues(t, 20, min(settled.ActualQuota, snapshot.FullReservedQuota)-settled.ChargedQuota)
	require.NoError(t, DB.First(account, account.ID).Error)
	assert.Zero(t, account.AmountReserved)
	assert.EqualValues(t, 60, account.AmountUsed)
	require.NoError(t, DB.First(&token, token.Id).Error)
	assert.Equal(t, 40, token.RemainQuota)
	assert.Equal(t, 60, token.UsedQuota)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.EqualValues(t, 500, user.Quota)

	// With only 40 member quota available, the full quote selects the wallet
	// even though half of the requested member quote would fit.
	input.RequestID, input.PilotTextHTTP, input.SkipToken = "text-pilot-wallet", true, true
	input.SubscriptionQuota = 50
	wallet, err := ReserveMembershipBilling(input)
	require.NoError(t, err)
	assert.Equal(t, MembershipWalletSource, wallet.Source)
	assert.EqualValues(t, 90, wallet.ReservedQuota)

	input.RequestID, input.SubscriptionQuota, input.SkipToken = "text-pilot-short", 15, false
	snapshot.FullReservedQuota = 0
	snapshot.Price.QuotaToPreConsume = 15
	encoded, err = common.Marshal(snapshot)
	require.NoError(t, err)
	input.SubscriptionSnapshot = string(encoded)
	short, err := ReserveMembershipBilling(input)
	require.NoError(t, err)
	assert.EqualValues(t, 8, short.ReservedQuota)
	settled, err = SettleMembershipRelayBilling(input.RequestID, 3, 0, true)
	require.NoError(t, err)
	assert.EqualValues(t, 3, settled.ChargedQuota)
	assert.Zero(t, settled.ExcessQuota)
	require.NoError(t, DB.First(account, account.ID).Error)
	assert.Zero(t, account.AmountReserved)
	assert.EqualValues(t, 63, account.AmountUsed)
	require.NoError(t, DB.First(&token, token.Id).Error)
	assert.Equal(t, 37, token.RemainQuota)
	require.NoError(t, DB.First(&budget).Error)
	assert.JSONEq(t, `{"`+strconv.Itoa(user.Id)+`":2000000}`, budget.Value)
}

func TestMembershipPilotSettlementRecyclesOnlyKnownUnusedRisk(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	buyMembershipTest(t, user.Id, catalog, "gold", "pilot-settle")
	t.Setenv("KKAI_MEMBERSHIP_PILOT_HOLD_USER_IDS", strconv.Itoa(user.Id))
	// With three quota per dollar, one quota consumes 333334 micro-dollars.
	// Round the incurred loss up independently before returning unused risk.
	input := membershipReservation(t, user.Id, "pilot-partial", 10, 10)
	input.PilotTextHTTP = true
	input.SubscriptionSnapshot = `{"version":1,"quota_per_unit":3,"price":{"QuotaToPreConsume":10}}`
	for _, scenario := range []struct {
		id         string
		actual     int64
		compensate bool
		wantMicros int64
	}{
		{"pilot-partial", 6, false, 333334},
		{"pilot-zero", 0, false, 333334},
		{"pilot-compensation", 0, true, 2000001},
	} {
		input.RequestID = scenario.id
		r, err := ReserveMembershipBilling(input)
		require.NoError(t, err)
		require.EqualValues(t, 5, r.ReservedQuota)
		for replay := range 2 {
			var final *MembershipBillingRecord
			if scenario.compensate {
				if replay == 0 {
					require.NoError(t, MarkMembershipBillingUncertain(input.RequestID, 0, "upstream_outcome_unknown"))
				}
				final, err = ResolveMembershipBillingReview(MembershipBillingResolutionInput{
					RequestID: input.RequestID, ActorID: 1, OperationID: "manual-waiver", Reason: "unknown cost", Action: "release",
				})
			} else if scenario.actual == 0 {
				final, err = ReleaseMembershipBilling(input.RequestID)
			} else {
				final, err = SettleMembershipBilling(input.RequestID, scenario.actual)
			}
			require.NoError(t, err, scenario.id)
			assert.Equal(t, replay == 0, final.Applied)
			var budget Option
			require.NoError(t, DB.Where(&Option{Key: membershipPilotHold}).First(&budget).Error)
			var totals map[int]int64
			require.NoError(t, common.UnmarshalJsonStr(budget.Value, &totals))
			assert.Equal(t, scenario.wantMicros, totals[user.Id])
		}
	}
}
