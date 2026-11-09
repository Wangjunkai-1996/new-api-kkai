package model

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMembershipRecoveryScanTest(t *testing.T) {
	t.Helper()
	setupMembershipTest(t)
	require.NoError(t, DB.AutoMigrate(&MembershipBillingRecovery{}, &MembershipBillingScanProgress{}, &KKAIJobLease{}))
	membershipRecoverySchema.Lock()
	previous := membershipRecoverySchema.check
	membershipRecoverySchema.Unlock()
	RegisterMembershipBillingRecoverySchemaChecker(func(context.Context, *gorm.DB) error { return nil })
	t.Cleanup(func() { RegisterMembershipBillingRecoverySchemaChecker(previous) })
}

func TestMembershipRecoveryScanAdvancesPastFiveHundredConflicts(t *testing.T) {
	setupMembershipRecoveryScanTest(t)
	now := time.Now().Unix()
	rows := make([]MembershipBillingRecord, 501)
	for i := range rows {
		rows[i] = MembershipBillingRecord{RequestID: fmt.Sprintf("blocked-%04d", i), Scope: MembershipBillingScopeRelay, State: "reserved", CreatedAt: now - 73*3600, ReviewStartedAt: now - 3600}
	}
	require.NoError(t, DB.CreateInBatches(rows, 50).Error)
	for round := 0; round < 5; round++ {
		page, progress, err := MembershipBillingScanPage(context.Background(), "aged", now, 100)
		require.NoError(t, err)
		require.Len(t, page, 100)
		assert.Equal(t, rows[round*100].RequestID, page[0].RequestID)
		// Conflict handling is deliberately not money movement. Persisting the
		// last handled position is sufficient to let the next round progress.
		require.NoError(t, AdvanceMembershipBillingScan(context.Background(), progress, &page[len(page)-1], now))
	}
	page, _, err := MembershipBillingScanPage(context.Background(), "aged", now, 100)
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, rows[500].RequestID, page[0].RequestID)
}

func TestMembershipRecoveryScanResumesInsidePageAndWrapsFixedUpperBound(t *testing.T) {
	setupMembershipRecoveryScanTest(t)
	now := time.Now().Unix()
	rows := []MembershipBillingRecord{
		{RequestID: "a", Scope: MembershipBillingScopeRelay, State: "reserved", CreatedAt: now - 80*3600},
		{RequestID: "b", Scope: MembershipBillingScopeRelay, State: "reserved", CreatedAt: now - 80*3600},
		{RequestID: "c", Scope: MembershipBillingScopeRelay, State: "reserved", CreatedAt: now - 79*3600},
	}
	require.NoError(t, DB.Create(&rows).Error)
	page, firstProgress, err := MembershipBillingScanPage(context.Background(), "aged", now, 100)
	require.NoError(t, err)
	require.Len(t, page, 3)
	require.NoError(t, AdvanceMembershipBillingScan(context.Background(), firstProgress, &page[0], now))
	assert.ErrorIs(t, AdvanceMembershipBillingScan(context.Background(), firstProgress, &page[1], now), ErrMembershipConflict, "stale cursor cannot overwrite committed progress")
	late := MembershipBillingRecord{RequestID: "later", Scope: MembershipBillingScopeRelay, State: "reserved", CreatedAt: now - 74*3600}
	require.NoError(t, DB.Create(&late).Error)
	page, progress, err := MembershipBillingScanPage(context.Background(), "aged", now, 100)
	require.NoError(t, err)
	require.Len(t, page, 2)
	assert.Equal(t, "b", page[0].RequestID, "a fetched but unhandled row must not be skipped")
	assert.Equal(t, "c", page[1].RequestID, "newer arrivals are outside this sweep")
	require.NoError(t, AdvanceMembershipBillingScan(context.Background(), progress, &page[1], now))
	page, progress, err = MembershipBillingScanPage(context.Background(), "aged", now, 100)
	require.NoError(t, err)
	assert.Empty(t, page)
	require.NoError(t, AdvanceMembershipBillingScan(context.Background(), progress, nil, now))
	page, _, err = MembershipBillingScanPage(context.Background(), "aged", now, 100)
	require.NoError(t, err)
	require.Len(t, page, 4)
	assert.Equal(t, "later", page[3].RequestID)
}

func TestMembershipRecoveryScanFindsDueOrphansBeforeSeventyTwoHours(t *testing.T) {
	setupMembershipRecoveryScanTest(t)
	now := time.Now().Unix()
	rows := []MembershipBillingRecord{
		{RequestID: "ended", Scope: MembershipBillingScopeRelay, State: "reserved", CreatedAt: now - 120, DispatchedAt: now - 100, ReviewAfter: now - 1},
		{RequestID: "still-running", Scope: MembershipBillingScopeRelay, State: "reserved", CreatedAt: now - 120, DispatchedAt: now - 100, ReviewAfter: now + 600},
		{RequestID: "async", Scope: MembershipBillingScopeTask, State: "reserved", CreatedAt: now - 120, DispatchedAt: now - 100, ReviewAfter: now - 1},
	}
	require.NoError(t, DB.Create(&rows).Error)
	page, _, err := MembershipBillingScanPage(context.Background(), "dispatched_orphan", now, 100)
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, "ended", page[0].RequestID)
	aged, _, err := MembershipBillingScanPage(context.Background(), "aged", now, 100)
	require.NoError(t, err)
	assert.Empty(t, aged)
}

func TestMembershipRecoveryScanCannotWriteWithLostLeaderFence(t *testing.T) {
	setupMembershipRecoveryScanTest(t)
	now := time.Now().Unix()
	require.NoError(t, DB.Create(&KKAIJobLease{LeaseName: "recovery", Holder: "new-worker", Fence: 2, LeaseUntil: now + 60}).Error)
	ctx := WithMembershipRecoveryGuard(context.Background(), func(tx *gorm.DB) error {
		return RequireMembershipRecoveryLeaderTx(tx, MembershipRecoveryLeaderFence{Name: "recovery", Holder: "old-worker", Fence: 1})
	})
	_, _, err := MembershipBillingScanPage(ctx, "aged", now, 100)
	assert.ErrorIs(t, err, ErrMembershipRecoveryFenceLost)
	var count int64
	require.NoError(t, DB.Model(&MembershipBillingScanProgress{}).Count(&count).Error)
	assert.Zero(t, count)
}
