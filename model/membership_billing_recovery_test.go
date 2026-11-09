package model

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMembershipRecoveryTest(t *testing.T) (User, *MembershipCatalog) {
	t.Helper()
	user, catalog := setupMembershipTest(t)
	require.NoError(t, DB.AutoMigrate(&MembershipBillingRecovery{}, &Log{}, &Channel{}))
	membershipRecoverySchema.Lock()
	previousChecker := membershipRecoverySchema.check
	membershipRecoverySchema.Unlock()
	RegisterMembershipBillingRecoverySchemaChecker(func(_ context.Context, db *gorm.DB) error {
		if !db.Migrator().HasTable(&MembershipBillingRecovery{}) {
			return ErrMembershipSchemaNotReady
		}
		return nil
	})
	oldLogDB := LOG_DB
	LOG_DB = DB
	t.Cleanup(func() {
		LOG_DB = oldLogDB
		RegisterMembershipBillingRecoverySchemaChecker(previousChecker)
	})
	return user, catalog
}

func membershipRecoveryEvidence(record *MembershipBillingRecord, quota int64) MembershipRelayEvidenceInput {
	now := common.GetTimestamp()
	return MembershipRelayEvidenceInput{Version: 1, Origin: MembershipEvidenceLocal, Kind: MembershipEvidenceFinalUsage,
		RequestID: record.RequestID, RequestHash: record.RequestHash, UserID: record.UserID, TokenID: record.TokenID,
		ChannelID: record.ReviewChannelID, Source: record.Source, AccountID: record.AccountID,
		SnapshotHash: MembershipBillingSnapshotHash(record.Snapshot), AssessedQuota: &quota,
		FinalizedAt: now, ObservedAt: now, BillableUsage: true, LocalFeesResolved: true}
}

func TestMembershipRecoveryRetainsEvidenceAfterFundsRollback(t *testing.T) {
	user, _ := setupMembershipRecoveryTest(t)
	token := Token{UserId: user.Id, Key: "recovery-token", Status: common.TokenStatusEnabled, RemainQuota: 100, ExpiredTime: -1}
	require.NoError(t, DB.Create(&token).Error)
	input := membershipReservation(t, user.Id, "terminal-write-failure", 80, 80)
	input.SkipToken, input.TokenID, input.TokenKey = false, token.Id, token.Key
	record, err := ReserveMembershipBilling(input)
	require.NoError(t, err)
	evidence := membershipRecoveryEvidence(record, 30)
	require.NoError(t, RecordMembershipRelayEvidence(context.Background(), evidence))

	// The real funds transaction fails after the separately committed receipt.
	require.NoError(t, DB.Exec("CREATE TRIGGER recovery_fail_funds BEFORE UPDATE ON users BEGIN SELECT RAISE(ABORT, 'funds unavailable'); END").Error)
	_, err = ResolveMembershipRelayEvidence(context.Background(), record.RequestID)
	require.ErrorContains(t, err, "funds unavailable")
	var recovery MembershipBillingRecovery
	require.NoError(t, DB.First(&recovery, "request_id = ?", record.RequestID).Error)
	assert.Equal(t, MembershipRecoveryReady, recovery.State)
	stored, err := GetMembershipBillingRecord(record.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "reserved", stored.State)
	require.NoError(t, DB.Exec("DROP TRIGGER recovery_fail_funds").Error)

	settled, err := ResolveMembershipRelayEvidence(context.Background(), record.RequestID)
	require.NoError(t, err)
	assert.True(t, settled.Applied)
	assert.EqualValues(t, 30, settled.ChargedQuota)
	duplicate, err := ResolveMembershipRelayEvidence(context.Background(), record.RequestID)
	require.NoError(t, err)
	assert.False(t, duplicate.Applied)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.EqualValues(t, 470, user.Quota)
	assert.EqualValues(t, 30, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	require.NoError(t, DB.First(&token, token.Id).Error)
	assert.Equal(t, 70, token.RemainQuota)
	assert.Equal(t, 30, token.UsedQuota)
	require.NoError(t, DB.First(&recovery, "request_id = ?", record.RequestID).Error)
	assert.Equal(t, MembershipRecoveryCachePending, recovery.State)
	require.NoError(t, CompleteMembershipRelayRecoveryCache(context.Background(), record.RequestID, recovery.Revision))
	require.NoError(t, DB.First(&recovery, "request_id = ?", record.RequestID).Error)
	assert.Equal(t, MembershipRecoveryResolved, recovery.State)
	assert.Positive(t, recovery.ResolvedAt)
}

func TestMembershipRecoveryEvidenceConflictIsDurableAndPreservesOriginal(t *testing.T) {
	user, _ := setupMembershipRecoveryTest(t)
	record, err := ReserveMembershipBilling(membershipReservation(t, user.Id, "conflicting-terminal", 80, 80))
	require.NoError(t, err)
	evidence := membershipRecoveryEvidence(record, 25)
	require.NoError(t, RecordMembershipRelayEvidence(context.Background(), evidence))
	evidence.ObservedAt += 10
	require.NoError(t, RecordMembershipRelayEvidence(context.Background(), evidence))
	var first MembershipBillingRecovery
	require.NoError(t, DB.First(&first, "request_id = ?", record.RequestID).Error)
	assert.Equal(t, MembershipRecoveryReady, first.State)

	quota := int64(26)
	evidence.AssessedQuota = &quota
	require.ErrorIs(t, RecordMembershipRelayEvidence(context.Background(), evidence), ErrMembershipConflict)
	var conflicted MembershipBillingRecovery
	require.NoError(t, DB.First(&conflicted, "request_id = ?", record.RequestID).Error)
	assert.Equal(t, MembershipRecoveryConflict, conflicted.State)
	assert.Equal(t, first.EvidenceHash, conflicted.EvidenceHash)
	var preserved membershipRelayEvidencePayload
	require.NoError(t, common.UnmarshalJsonStr(conflicted.EvidencePayload, &preserved))
	require.NotNil(t, preserved.AssessedQuota)
	assert.EqualValues(t, 25, *preserved.AssessedQuota)
	assert.Len(t, preserved.Conflicts, 1)
	_, err = ResolveMembershipRelayEvidence(context.Background(), record.RequestID)
	assert.ErrorIs(t, err, ErrMembershipConflict)
	stored, err := GetMembershipBillingRecord(record.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "reserved", stored.State)
}

func TestMembershipRecoverySameReceiptCannotFundTwoRequests(t *testing.T) {
	user, _ := setupMembershipRecoveryTest(t)
	for _, id := range []string{"receipt-first", "receipt-reused"} {
		record, err := ReserveMembershipBilling(membershipReservation(t, user.Id, id, 40, 40))
		require.NoError(t, err)
		evidence := membershipRecoveryEvidence(record, 10)
		evidence.Origin, evidence.ReceiptID = MembershipEvidenceConsumeLog, "permanent-log-receipt"
		err = RecordMembershipRelayEvidence(context.Background(), evidence)
		if id == "receipt-first" {
			require.NoError(t, err)
			continue
		}
		require.ErrorIs(t, err, ErrMembershipConflict)
		var recovery MembershipBillingRecovery
		require.NoError(t, DB.First(&recovery, "request_id = ?", id).Error)
		assert.Equal(t, MembershipRecoveryConflict, recovery.State)
		assert.Equal(t, "receipt_reused", recovery.LastErrorCode)
		assert.Nil(t, recovery.ReceiptKey)
	}
	first, err := ResolveMembershipRelayEvidence(context.Background(), "receipt-first")
	require.NoError(t, err)
	assert.EqualValues(t, 10, first.ChargedQuota)
	_, err = ResolveMembershipRelayEvidence(context.Background(), "receipt-reused")
	assert.ErrorIs(t, err, ErrMembershipConflict)
}

func TestMembershipRecoveryEarlyEvidencePreservesSourceAndChargeCap(t *testing.T) {
	user, catalog := setupMembershipRecoveryTest(t)
	_, account := buyMembershipTest(t, user.Id, catalog, "gold", "recovery-membership")
	record, err := ReserveMembershipBilling(membershipReservation(t, user.Id, "early-overrun", 60, 70))
	require.NoError(t, err)
	require.NoError(t, RecordMembershipRelayEvidence(context.Background(), membershipRecoveryEvidence(record, 90)))
	// An independently saved receipt remains usable while the log database is down.
	LOG_DB = nil
	settled, err := CompensateExpiredMembershipRelay(context.Background(), record.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "settled", settled.State)
	assert.EqualValues(t, 60, settled.ChargedQuota)
	assert.EqualValues(t, 30, settled.ExcessQuota)
	require.NoError(t, DB.First(account, account.ID).Error)
	assert.EqualValues(t, 60, account.AmountUsed)
	assert.Zero(t, account.AmountReserved)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.EqualValues(t, 500, user.Quota)
}

func TestMembershipRecoveryZeroUsageAndConfirmedZeroHaveDifferentStatistics(t *testing.T) {
	for _, kind := range []string{MembershipEvidenceFinalUsage, MembershipEvidenceZero} {
		t.Run(kind, func(t *testing.T) {
			user, _ := setupMembershipRecoveryTest(t)
			record, err := ReserveMembershipBilling(membershipReservation(t, user.Id, "zero-terminal", 20, 20))
			require.NoError(t, err)
			evidence := membershipRecoveryEvidence(record, 0)
			evidence.Kind = kind
			if kind == MembershipEvidenceZero {
				evidence.BillableUsage, evidence.ZeroCostBasis = false, "local_no_dispatch_no_fee"
			}
			evidence.LocalFeesResolved = false
			require.ErrorIs(t, RecordMembershipRelayEvidence(context.Background(), evidence), ErrMembershipInvalidInput)
			evidence.LocalFeesResolved = true
			require.NoError(t, RecordMembershipRelayEvidence(context.Background(), evidence))
			settled, err := ResolveMembershipRelayEvidence(context.Background(), record.RequestID)
			require.NoError(t, err)
			require.NoError(t, DB.First(&user, user.Id).Error)
			assert.EqualValues(t, 500, user.Quota)
			if kind == MembershipEvidenceFinalUsage {
				assert.Equal(t, "settled", settled.State)
				assert.Equal(t, 1, user.RequestCount)
			} else {
				assert.Equal(t, "released", settled.State)
				assert.Zero(t, user.RequestCount)
			}
		})
	}
}

func TestMembershipRecoveryCompensationAgeLogFailureAndLateEvidence(t *testing.T) {
	user, _ := setupMembershipRecoveryTest(t)
	record, err := ReserveMembershipBilling(membershipReservation(t, user.Id, "aged-unknown", 50, 50))
	require.NoError(t, err)
	require.NoError(t, MarkMembershipRelayRecoveryPending(context.Background(), record.RequestID, 0, "transport_stopped"))
	require.NoError(t, DB.Model(record).Update("created_at", common.GetTimestamp()-119*60).Error)
	_, err = CompensateExpiredMembershipRelay(context.Background(), record.RequestID)
	require.ErrorIs(t, err, ErrMembershipConflict)
	require.NoError(t, DB.Model(record).Update("created_at", common.GetTimestamp()-121*60).Error)
	LOG_DB = nil
	_, err = CompensateExpiredMembershipRelay(context.Background(), record.RequestID)
	require.ErrorContains(t, err, "log database unavailable")
	LOG_DB = DB
	log := Log{RequestId: record.RequestID, UserId: user.Id, Type: LogTypeConsume, Quota: 10}
	require.NoError(t, LOG_DB.Create(&log).Error)
	_, err = CompensateExpiredMembershipRelay(context.Background(), record.RequestID)
	require.ErrorIs(t, err, ErrMembershipConflict)
	require.NoError(t, LOG_DB.Delete(&log).Error)
	released, err := CompensateExpiredMembershipRelay(context.Background(), record.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "released", released.State)
	assert.Contains(t, released.ReviewResolution, "no_evidence_compensation")
	require.NoError(t, RecordMembershipRelayEvidence(context.Background(), membershipRecoveryEvidence(record, 15)))
	late, err := ResolveMembershipRelayEvidence(context.Background(), record.RequestID)
	require.NoError(t, err)
	assert.False(t, late.Applied)
	assert.Equal(t, "released", late.State)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.EqualValues(t, 500, user.Quota)
	assert.Zero(t, user.UsedQuota)
	var recovery MembershipBillingRecovery
	require.NoError(t, DB.First(&recovery, "request_id = ?", record.RequestID).Error)
	assert.Equal(t, "late_terminal_after_completion", recovery.LastErrorCode)
}

func TestMembershipRecoveryCompensationWaitsForRunningTransport(t *testing.T) {
	user, _ := setupMembershipRecoveryTest(t)
	record, err := ReserveMembershipBilling(membershipReservation(t, user.Id, "running-over-two-hours", 50, 50))
	require.NoError(t, err)
	require.NoError(t, MarkMembershipBillingDispatched(record.RequestID, 3, common.GetTimestamp()+600))
	require.NoError(t, DB.Model(record).Update("created_at", common.GetTimestamp()-3*3600).Error)
	_, err = CompensateExpiredMembershipRelay(context.Background(), record.RequestID)
	require.ErrorIs(t, err, ErrMembershipConflict)
	stillReserved, err := GetMembershipBillingRecord(record.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "reserved", stillReserved.State)
	require.NoError(t, DB.Model(record).Update("review_after", common.GetTimestamp()-1).Error)
	released, err := CompensateExpiredMembershipRelay(context.Background(), record.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "released", released.State)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.EqualValues(t, 500, user.Quota)
}

func TestMembershipRecoveryRejectsForeignIdentityAmountsAndAsyncScope(t *testing.T) {
	user, _ := setupMembershipRecoveryTest(t)
	record, err := ReserveMembershipBilling(membershipReservation(t, user.Id, "protected-authorization", 20, 20))
	require.NoError(t, err)
	for _, amount := range []int64{-1, math.MaxInt32 + 1} {
		input := membershipRecoveryEvidence(record, amount)
		assert.ErrorIs(t, RecordMembershipRelayEvidence(context.Background(), input), ErrMembershipInvalidInput)
	}
	for _, mutate := range []func(*MembershipRelayEvidenceInput){
		func(in *MembershipRelayEvidenceInput) { in.UserID++ },
		func(in *MembershipRelayEvidenceInput) { in.RequestHash = "different-request" },
		func(in *MembershipRelayEvidenceInput) { in.SnapshotHash = "changed-price" },
		func(in *MembershipRelayEvidenceInput) { in.Source = MembershipSource },
	} {
		input := membershipRecoveryEvidence(record, 10)
		mutate(&input)
		assert.ErrorIs(t, RecordMembershipRelayEvidence(context.Background(), input), ErrMembershipConflict)
	}
	require.NoError(t, DB.Model(record).Update("billing_scope", MembershipBillingScopeTask).Error)
	input := membershipRecoveryEvidence(record, 0)
	input.Kind, input.BillableUsage, input.ZeroCostBasis = MembershipEvidenceZero, false, "local_no_dispatch_no_fee"
	assert.ErrorIs(t, RecordMembershipRelayEvidence(context.Background(), input), ErrMembershipConflict)
	_, err = ResolveMembershipRelayEvidence(context.Background(), record.RequestID)
	assert.ErrorIs(t, err, ErrMembershipConflict)
	stored, err := GetMembershipBillingRecord(record.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "reserved", stored.State)
}

func TestMembershipRecoveryFenceLossRollsBackEvidenceAndFunds(t *testing.T) {
	user, _ := setupMembershipRecoveryTest(t)
	record, err := ReserveMembershipBilling(membershipReservation(t, user.Id, "lost-recovery-leader", 30, 30))
	require.NoError(t, err)
	require.NoError(t, RecordMembershipRelayEvidence(context.Background(), membershipRecoveryEvidence(record, 10)))
	lost := errors.New("leader fence lost")
	calls := 0
	ctx := WithMembershipRecoveryGuard(context.Background(), func(_ *gorm.DB) error {
		calls++
		if calls == 2 {
			return lost
		}
		return nil
	})
	_, err = ResolveMembershipRelayEvidence(ctx, record.RequestID)
	require.ErrorIs(t, err, lost)
	stored, err := GetMembershipBillingRecord(record.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "reserved", stored.State)
	var recovery MembershipBillingRecovery
	require.NoError(t, DB.First(&recovery, "request_id = ?", record.RequestID).Error)
	assert.Equal(t, MembershipRecoveryReady, recovery.State)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.EqualValues(t, 470, user.Quota)
	assert.Zero(t, user.UsedQuota)
}

func TestMembershipRecoveryCorruptEvidenceCannotBecomeCompensation(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		updates map[string]any
	}{
		{"unknown-kind", map[string]any{"evidence_kind": "unrecognized"}},
		{"empty-kind", map[string]any{"evidence_kind": ""}},
		{"unknown-state", map[string]any{"state": "unrecognized"}},
		{"unknown-with-payload", map[string]any{"evidence_payload": `{"assessed_quota":10}`}},
		{"ready-without-evidence", map[string]any{"state": MembershipRecoveryReady}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			user, _ := setupMembershipRecoveryTest(t)
			record, err := ReserveMembershipBilling(membershipReservation(t, user.Id, "corrupt-recovery", 20, 20))
			require.NoError(t, err)
			require.NoError(t, MarkMembershipRelayRecoveryPending(context.Background(), record.RequestID, 0, "transport_stopped"))
			require.NoError(t, DB.Model(record).Update("created_at", common.GetTimestamp()-121*60).Error)
			require.NoError(t, DB.Model(&MembershipBillingRecovery{}).Where("request_id = ?", record.RequestID).Updates(fixture.updates).Error)
			_, err = CompensateExpiredMembershipRelay(context.Background(), record.RequestID)
			assert.ErrorIs(t, err, ErrMembershipConflict)
			stored, err := GetMembershipBillingRecord(record.RequestID)
			require.NoError(t, err)
			assert.Equal(t, "reserved", stored.State)
			require.NoError(t, DB.First(&user, user.Id).Error)
			assert.EqualValues(t, 480, user.Quota)
		})
	}
}

func TestMembershipRecoveryCacheInvalidationDistinguishesCancellationLookupFailureAndDeletion(t *testing.T) {
	t.Run("cancelled", func(t *testing.T) {
		user, _ := setupMembershipRecoveryTest(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := MembershipBillingAfterCommitContext(ctx, &MembershipBillingRecord{UserID: user.Id, Applied: true})
		assert.ErrorIs(t, err, context.Canceled)
	})
	t.Run("token lookup fails", func(t *testing.T) {
		user, _ := setupMembershipRecoveryTest(t)
		token := Token{UserId: user.Id, Key: "cache-lookup-failure", Status: common.TokenStatusEnabled, ExpiredTime: -1}
		require.NoError(t, DB.Create(&token).Error)
		lookupFailure := errors.New("token database unavailable")
		const callback = "test:membership_recovery_token_lookup"
		require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Table == "tokens" {
				tx.AddError(lookupFailure)
			}
		}))
		t.Cleanup(func() { require.NoError(t, DB.Callback().Query().Remove(callback)) })
		err := MembershipBillingAfterCommitContext(context.Background(), &MembershipBillingRecord{UserID: user.Id, TokenID: token.Id, Applied: true})
		assert.ErrorIs(t, err, lookupFailure)
	})
	t.Run("token deleted", func(t *testing.T) {
		user, _ := setupMembershipRecoveryTest(t)
		token := Token{UserId: user.Id, Key: "cache-deleted-token", Status: common.TokenStatusEnabled, ExpiredTime: -1}
		require.NoError(t, DB.Create(&token).Error)
		require.NoError(t, DB.Delete(&token).Error)
		err := MembershipBillingAfterCommitContext(context.Background(), &MembershipBillingRecord{UserID: user.Id, TokenID: token.Id, Applied: true})
		require.NoError(t, err)
		require.NoError(t, DB.First(&user, user.Id).Error)
		assert.EqualValues(t, 500, user.Quota)
	})
}
