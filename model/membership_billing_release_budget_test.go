package model

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipStandardReleaseRollingBudget(t *testing.T) {
	user, _ := setupMembershipRecoveryTest(t)
	const now = int64(2_000_000_000)
	const cutoff = now - 24*60*60
	seed := func(id string, resolution int64, amount int64, unit float64, reason string) {
		t.Helper()
		row := MembershipBillingRecord{RequestID: id, Scope: MembershipBillingScopeRelay, State: "released", Source: MembershipSource,
			UserID: user.Id, CreatedAt: resolution - 60*60, ReviewResolvedAt: resolution, ReviewReason: reason,
			ReservedQuota: amount, Snapshot: fmt.Sprintf(`{"quota_per_unit":%v}`, unit)}
		require.NoError(t, DB.Create(&row).Error)
	}
	// Four $6 V1 records and one $6 V2 from the same member. One V1 was
	// created yesterday before the rolling window but released inside it.
	for i := range 4 {
		seed(fmt.Sprintf("pilot-%d", i), cutoff+30*60, 6, 1, membershipPilotCompensationReason)
	}
	seed("standard", now-2, 12, 2, membershipStandardCompensationReason)
	seed("ordinary-two-hour", now-2, 600, 1, "no_evidence_compensation")
	seed("expired", cutoff, 6, 1, membershipPilotCompensationReason)

	for _, test := range []struct {
		name   string
		second int64
		quota  int64
		unit   float64
		fits   bool
	}{
		{"at member limit", now, 5, 1, true},
		{"above member limit by one microdollar", now, 5_000_001, 1_000_000, false},
		{"expired records restore capacity", now + 30*60, 6, 1, true},
		{"single request cap", now + 30*60, 6_000_001, 1_000_000, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fits, err := membershipStandardReleaseFitsTx(DB, user.Id, test.quota, test.unit, test.second)
			require.NoError(t, err)
			assert.Equal(t, test.fits, fits)
		})
	}
	seed("invalid-unit", now, 1, 0, membershipStandardCompensationReason)
	fits, err := membershipStandardReleaseFitsTx(DB, user.Id, 1, 1_000_000, now)
	require.NoError(t, err)
	assert.False(t, fits)
}

func TestMembershipStandardReleaseDefersWhenHistoryExceedsReadBudget(t *testing.T) {
	user, _ := setupMembershipRecoveryTest(t)
	const now = int64(2_000_000_000)
	var rows []MembershipBillingRecord
	for i := range membershipEarlyReleaseHistoryLimit + 1 {
		rows = append(rows, MembershipBillingRecord{RequestID: fmt.Sprintf("small-%d", i), Scope: MembershipBillingScopeRelay,
			State: "released", Source: MembershipSource, UserID: user.Id, CreatedAt: now - 60*60,
			ReviewResolvedAt: now - 60, ReviewReason: membershipPilotCompensationReason, ReservedQuota: 1, Snapshot: `{"quota_per_unit":1000000}`})
	}
	require.NoError(t, DB.CreateInBatches(rows, 20).Error)
	fits, err := membershipStandardReleaseFitsTx(DB, user.Id, 1, 1_000_000, now)
	require.NoError(t, err)
	assert.False(t, fits)
}
