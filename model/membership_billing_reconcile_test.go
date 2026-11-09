package model

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipRelayReconcileClosesOnlyAgedProvenOrUnverifiableCharges(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	_, account := buyMembershipTest(t, user.Id, catalog, "gold", "auto-review")
	require.NoError(t, DB.AutoMigrate(&Log{}, &Channel{}))
	previousLogDB := LOG_DB
	LOG_DB = DB
	t.Cleanup(func() { LOG_DB = previousLogDB })
	channel := Channel{Name: "reconcile-channel", Key: "test", Type: 1, Status: 1}
	require.NoError(t, DB.Create(&channel).Error)

	aged := time.Now().Add(-3 * time.Hour).Unix()
	for _, fixture := range []struct {
		id      string
		quota   int64
		old     bool
		receipt string
	}{
		{"unverifiable", 20, true, ""},
		{"confirmed", 30, true, "valid"},
		{"conflicting", 20, true, "wrong-source"},
		{"not-due", 10, false, ""},
		{"orphan", 10, true, ""},
		{"zero-charge", 10, true, "zero"},
	} {
		input := membershipReservation(t, user.Id, fixture.id, fixture.quota, fixture.quota)
		record, err := ReserveMembershipBilling(input)
		require.NoError(t, err)
		if fixture.id == "orphan" {
			require.NoError(t, MarkMembershipBillingDispatched(record.RequestID, channel.Id, common.GetTimestamp()+600))
			require.NoError(t, DB.Model(record).Update("review_after", common.GetTimestamp()-1).Error)
		} else {
			require.NoError(t, MarkMembershipBillingUncertain(record.RequestID, channel.Id, "upstream_outcome_unknown"))
		}
		if fixture.old {
			require.NoError(t, DB.Model(record).Update("created_at", aged).Error)
		}
		if fixture.id == "unverifiable" {
			require.NoError(t, DB.Create(&Log{RequestId: fixture.id, UserId: user.Id,
				ChannelId: channel.Id, Type: LogTypeError, Quota: 0}).Error)
		}
		if fixture.receipt != "" {
			source := MembershipSource
			if fixture.receipt == "wrong-source" {
				source = MembershipWalletSource
			}
			quota := 12
			if fixture.receipt == "zero" {
				quota = 0
			}
			other := fmt.Sprintf(`{"billing_source":%q,"admin_info":{"membership_billing":{"request_id":%q,"assessed":%d,"charged":%d}}}`, source, fixture.id, quota, quota)
			require.NoError(t, DB.Create(&Log{RequestId: fixture.id, UserId: user.Id, TokenId: 0,
				ChannelId: channel.Id, Type: LogTypeConsume, Quota: quota, Other: other}).Error)
		}
	}

	resolved, err := ReconcileDueMembershipRelayBilling(context.Background(), 50)
	require.ErrorContains(t, err, "conflicting consume receipt")
	assert.Equal(t, 4, resolved)
	for _, check := range []struct {
		id     string
		state  string
		actual int64
	}{
		{"unverifiable", "released", 0},
		{"confirmed", "settled", 12},
		{"conflicting", "reserved", 0},
		{"not-due", "reserved", 0},
		{"orphan", "released", 0},
		{"zero-charge", "released", 0},
	} {
		record, err := GetMembershipBillingRecord(check.id)
		require.NoError(t, err)
		assert.Equal(t, check.state, record.State, check.id)
		assert.Equal(t, check.actual, record.ActualQuota, check.id)
		if check.state != "reserved" {
			assert.Contains(t, record.ReviewResolution, `"actor_id":0`)
		}
	}
	require.NoError(t, DB.First(account, account.ID).Error)
	assert.EqualValues(t, 30, account.AmountReserved)
	assert.EqualValues(t, 12, account.AmountUsed)
	resolved, err = ReconcileDueMembershipRelayBilling(context.Background(), 50)
	require.ErrorContains(t, err, "conflicting consume receipt")
	assert.Zero(t, resolved)
	require.NoError(t, DB.First(account, account.ID).Error)
	assert.EqualValues(t, 30, account.AmountReserved)
	assert.EqualValues(t, 12, account.AmountUsed)

	// A manual resolver still requires an authenticated actor.
	_, err = ResolveMembershipBillingReview(MembershipBillingResolutionInput{
		RequestID: "conflicting", ActorID: 0, OperationID: "manual", Action: "release", Reason: "test",
	})
	assert.ErrorIs(t, err, ErrMembershipInvalidInput)
	_, err = SettleMembershipRelayBilling("unverifiable", 12, channel.Id, true)
	assert.ErrorIs(t, err, ErrMembershipConflict)
}
