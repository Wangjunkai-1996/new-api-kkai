package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMembershipRecoveryJobModeRequiresKnownValueAndLeader(t *testing.T) {
	previous := MembershipBillingRecoveryMode()
	t.Cleanup(func() { membershipBillingRecoveryMode.Store(previous) })
	t.Setenv(MembershipBillingRecoveryModeEnvironment, "unsupported")
	registry := NewBackgroundJobRegistry()
	require.ErrorContains(t, RegisterMembershipBillingRecoveryJob(registry, "worker"), "invalid KKAI_MEMBERSHIP_BILLING_RECOVERY_MODE")
	assert.Empty(t, registry.Descriptors())
	t.Setenv(MembershipBillingRecoveryModeEnvironment, "apply")
	require.NoError(t, RegisterMembershipBillingRecoveryJob(registry, "worker"))
	assert.Equal(t, "paused", MembershipBillingRecoveryMode(), "durable control must be observed before activation")
	descriptors := registry.Descriptors()
	require.Len(t, descriptors, 2)
	assert.Equal(t, "membership-recovery-control", descriptors[0].Name)
	assert.False(t, descriptors[0].WritesData)
	assert.Equal(t, time.Second, descriptors[0].Interval)
	assert.Equal(t, "membership-relay-reconcile", descriptors[1].Name)
	assert.True(t, descriptors[1].WritesData)
	assert.True(t, descriptors[1].RequiresLeaderLease)
}

func setupMembershipRecoveryWorkerTest(t *testing.T) model.User {
	t.Helper()
	_, user := setupMembershipServiceTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.MembershipBillingRecovery{}, &model.MembershipBillingScanProgress{}, &model.KKAIJobLease{}, &model.Log{}, &model.Option{}))
	require.NoError(t, model.DB.Create(&model.Option{Key: model.MembershipBillingRecoveryControlOption, Value: "off:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}).Error)
	initializeMembershipRecoveryControl("recovery-worker", "paused")
	require.NoError(t, model.DB.Create(&model.KKAIJobLease{LeaseName: "newapi-background-writer", Holder: "recovery-worker", Fence: 1, LeaseUntil: time.Now().Unix() + 3600}).Error)
	previousLogDB, previousCheck, previousMode := model.LOG_DB, membershipBillingRecoverySchemaCheck, MembershipBillingRecoveryMode()
	model.LOG_DB = model.DB
	membershipBillingRecoverySchemaCheck = func(context.Context, *gorm.DB) error { return nil }
	membershipBillingRecoverySchema.Lock()
	membershipBillingRecoverySchema.db = nil
	membershipBillingRecoverySchema.Unlock()
	model.RegisterMembershipBillingRecoverySchemaChecker(CheckMembershipBillingRecoveryRuntimeSchema)
	t.Cleanup(func() {
		model.LOG_DB, membershipBillingRecoverySchemaCheck = previousLogDB, previousCheck
		membershipBillingRecoveryMode.Store(previousMode)
		membershipBillingRecoverySchema.Lock()
		membershipBillingRecoverySchema.db = nil
		membershipBillingRecoverySchema.Unlock()
		model.RegisterMembershipBillingRecoverySchemaChecker(CheckMembershipBillingRecoveryRuntimeSchema)
	})
	return user
}

func reserveMembershipRecoveryWorkerTest(t *testing.T, user model.User, id string, old, ready bool) *model.MembershipBillingRecord {
	t.Helper()
	info := &relaycommon.RelayInfo{UserId: user.Id, RequestId: id, UsingGroup: "gpt", IsPlayground: true,
		OriginModelName: "test-model", PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.2}}}
	input, err := PrepareMembershipReservation(info, 100)
	require.NoError(t, err)
	record, err := model.ReserveMembershipBilling(input)
	require.NoError(t, err)
	if old {
		record.CreatedAt = time.Now().Unix() - 3*3600
		require.NoError(t, model.DB.Model(record).Update("created_at", record.CreatedAt).Error)
	}
	if ready {
		actual := int64(1)
		require.NoError(t, model.RecordMembershipRelayEvidence(context.Background(), model.MembershipRelayEvidenceInput{
			Version: 1, Origin: model.MembershipEvidenceLocal, Kind: model.MembershipEvidenceFinalUsage,
			RequestID: record.RequestID, RequestHash: record.RequestHash, UserID: record.UserID, TokenID: record.TokenID,
			Source: record.Source, AccountID: record.AccountID, SnapshotHash: model.MembershipBillingSnapshotHash(record.Snapshot),
			FinalizedAt: time.Now().Unix(), ObservedAt: time.Now().Unix(), AssessedQuota: &actual, BillableUsage: true, LocalFeesResolved: true,
		}))
	}
	return record
}

func TestMembershipRecoveryWorkerModesKeepEarlyEvidenceAndAgedFallbackSeparate(t *testing.T) {
	for _, mode := range []string{"off", "observe", "apply", "paused"} {
		t.Run(mode, func(t *testing.T) {
			user := setupMembershipRecoveryWorkerTest(t)
			membershipBillingRecoveryMode.Store(mode)
			require.NoError(t, model.DB.Model(&model.Option{}).Where(map[string]any{"key": model.MembershipBillingRecoveryControlOption}).Update("value", mode+":aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Error)
			reserveMembershipRecoveryWorkerTest(t, user, "young-ready", false, true)
			reserveMembershipRecoveryWorkerTest(t, user, "aged-ready", true, true)
			reserveMembershipRecoveryWorkerTest(t, user, "aged-unknown", true, false)
			worker := &membershipBillingRecoveryWorker{workerID: "recovery-worker"}
			require.NoError(t, worker.run(context.Background()))
			for _, id := range []string{"young-ready", "aged-ready", "aged-unknown"} {
				record, err := model.GetMembershipBillingRecord(id)
				require.NoError(t, err)
				want := "reserved"
				if mode != "paused" && (id != "young-ready" || mode == "apply") {
					want = "settled"
					if id == "aged-unknown" {
						want = "released"
					}
				}
				assert.Equal(t, want, record.State, id)
			}
			if mode != "apply" {
				var young model.MembershipBillingRecovery
				require.NoError(t, model.DB.First(&young, "request_id = ?", "young-ready").Error)
				assert.Equal(t, model.MembershipRecoveryReady, young.State)
			}
		})
	}
}

func TestMembershipRecoveryWorkerCompensatesOnlyEligiblePilotHold(t *testing.T) {
	for _, test := range []struct {
		name, mode, reason   string
		stoppedMinutes       int64
		disallow, logFailure bool
		consumeLog, overCap  bool
		fundsFailure         bool
		standard             bool
		wantEarlyRelease     bool
	}{
		{name: "eligible", mode: "apply", reason: model.MembershipPilotEarlyReviewReason, stoppedMinutes: 16, wantEarlyRelease: true},
		{name: "standard", mode: "apply", reason: model.MembershipPilotEarlyReviewReason, stoppedMinutes: 16, standard: true, wantEarlyRelease: true},
		{name: "observe", mode: "observe", reason: model.MembershipPilotEarlyReviewReason, stoppedMinutes: 16},
		{name: "too recent", mode: "apply", reason: model.MembershipPilotEarlyReviewReason, stoppedMinutes: 14},
		{name: "outside pilot", mode: "apply", reason: model.MembershipPilotEarlyReviewReason, stoppedMinutes: 16, disallow: true},
		{name: "historical reason", mode: "apply", reason: "upstream_outcome_unknown", stoppedMinutes: 16},
		{name: "consume evidence conflict", mode: "apply", reason: model.MembershipPilotEarlyReviewReason, stoppedMinutes: 16, consumeLog: true},
		{name: "failed log lookup", mode: "apply", reason: model.MembershipPilotEarlyReviewReason, stoppedMinutes: 16, logFailure: true},
		{name: "over cap", mode: "apply", reason: model.MembershipPilotEarlyReviewReason, stoppedMinutes: 16, overCap: true},
		{name: "standard over cap", mode: "apply", reason: model.MembershipPilotEarlyReviewReason, stoppedMinutes: 16, standard: true, overCap: true},
		{name: "failed refund", mode: "apply", reason: model.MembershipPilotEarlyReviewReason, stoppedMinutes: 16, fundsFailure: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			user := setupMembershipRecoveryWorkerTest(t)
			previousConsume := common.LogConsumeEnabled
			common.LogConsumeEnabled = true
			t.Cleanup(func() { common.LogConsumeEnabled = previousConsume })
			if test.standard {
				t.Setenv("KKAI_MEMBERSHIP_PILOT_EARLY_RELEASE_USER_IDS", "*")
			} else if !test.disallow {
				t.Setenv("KKAI_MEMBERSHIP_PILOT_EARLY_RELEASE_USER_IDS", strconv.Itoa(user.Id))
			} else {
				t.Setenv("KKAI_MEMBERSHIP_PILOT_EARLY_RELEASE_USER_IDS", "")
			}
			require.NoError(t, model.DB.Model(&model.Option{}).
				Where(map[string]any{"key": model.MembershipBillingRecoveryControlOption}).
				Update("value", test.mode+":aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Error)

			record := reserveMembershipRecoveryWorkerTest(t, user, "pilot-hold", false, false)
			require.Equal(t, model.MembershipSource, record.Source)
			var held model.MembershipAccount
			require.NoError(t, model.DB.First(&held, record.AccountID).Error)
			require.Equal(t, record.ReservedQuota, held.AmountReserved)
			ctx := context.Background()
			now := time.Now().Unix()
			require.NoError(t, model.MarkMembershipBillingDispatched(record.RequestID, 3, now+3600))
			require.NoError(t, model.MarkMembershipRelayRecoveryPending(ctx, record.RequestID, 3, test.reason))
			require.NoError(t, model.DB.Model(record).Updates(map[string]any{
				"created_at": now - 20*60, "dispatched_at": now - 19*60, "review_started_at": now - test.stoppedMinutes*60,
			}).Error)
			if test.name == "too recent" {
				require.NoError(t, model.DB.Model(&model.MembershipBillingRecovery{}).Where("request_id = ?", record.RequestID).Update("attempt_count", 4).Error)
			}
			if test.consumeLog {
				log := model.Log{RequestId: record.RequestID, UserId: user.Id, Type: model.LogTypeConsume, Quota: 1}
				require.NoError(t, model.LOG_DB.Create(&log).Error)
			}
			if test.logFailure {
				model.LOG_DB = nil
			}
			if test.overCap {
				require.NoError(t, model.DB.Model(record).Update("snapshot", `{"quota_per_unit":1}`).Error)
			}
			if test.fundsFailure {
				const callback = "test:pilot_refund_funds_failure"
				require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
					if tx.Statement.Table == (model.MembershipAccount{}).TableName() {
						tx.AddError(errors.New("pilot refund failed"))
					}
				}))
				t.Cleanup(func() { require.NoError(t, model.DB.Callback().Update().Remove(callback)) })
			}
			worker := &membershipBillingRecoveryWorker{workerID: "recovery-worker"}
			err := worker.run(ctx)
			switch {
			case test.consumeLog:
				require.ErrorIs(t, err, model.ErrMembershipConflict)
			case test.logFailure:
				require.ErrorContains(t, err, "log database unavailable")
			case test.fundsFailure:
				require.ErrorContains(t, err, "pilot refund failed")
			default:
				require.NoError(t, err)
			}
			current, err := model.GetMembershipBillingRecord(record.RequestID)
			require.NoError(t, err)
			if test.name == "too recent" {
				var recovery model.MembershipBillingRecovery
				require.NoError(t, model.DB.First(&recovery, "request_id = ?", record.RequestID).Error)
				assert.Equal(t, now+60, recovery.NextAttemptAt)
			}
			var account model.MembershipAccount
			require.NoError(t, model.DB.First(&account, record.AccountID).Error)
			if test.wantEarlyRelease {
				assert.Equal(t, "released", current.State)
				if test.standard {
					assert.Equal(t, "standard_early_no_evidence_compensation", current.ReviewReason)
					var pilotCounters int64
					require.NoError(t, model.DB.Model(&model.Option{}).Where(map[string]any{"key": "MembershipBillingPilotEarlyReleaseSpent"}).Count(&pilotCounters).Error)
					assert.Zero(t, pilotCounters)
				} else {
					assert.Equal(t, "pilot_early_no_evidence_compensation", current.ReviewReason)
				}
				assert.Zero(t, current.ChargedQuota)
				assert.Equal(t, held.AmountUsed, account.AmountUsed)
				assert.Zero(t, account.AmountReserved)
			} else {
				assert.Equal(t, "reserved", current.State)
				assert.Equal(t, held.AmountReserved, account.AmountReserved)
				var budgetRows int64
				require.NoError(t, model.DB.Model(&model.Option{}).Where(map[string]any{"key": "MembershipBillingPilotEarlyReleaseSpent"}).Count(&budgetRows).Error)
				assert.Zero(t, budgetRows)
			}
			if test.overCap {
				var recovery model.MembershipBillingRecovery
				require.NoError(t, model.DB.First(&recovery, "request_id = ?", record.RequestID).Error)
				assert.Greater(t, recovery.NextAttemptAt, now)
				require.NoError(t, model.DB.Model(record).Update("created_at", now-2*3600-1).Error)
				require.NoError(t, worker.run(ctx))
				current, err = model.GetMembershipBillingRecord(record.RequestID)
				require.NoError(t, err)
				assert.Equal(t, "released", current.State)
				assert.Equal(t, "no_evidence_compensation", current.ReviewReason)
			}
		})
	}
}

func TestMembershipRecoveryWorkerRefusesPartialSchemaInsteadOfLegacyFallback(t *testing.T) {
	user := setupMembershipRecoveryWorkerTest(t)
	membershipBillingRecoveryMode.Store("off")
	reserveMembershipRecoveryWorkerTest(t, user, "must-remain-held", true, false)
	want := errors.New("canonical recovery receipt is invalid")
	membershipBillingRecoverySchemaCheck = func(context.Context, *gorm.DB) error { return want }
	membershipBillingRecoverySchema.Lock()
	membershipBillingRecoverySchema.db = nil
	membershipBillingRecoverySchema.Unlock()
	worker := &membershipBillingRecoveryWorker{workerID: "recovery-worker"}
	assert.ErrorIs(t, worker.run(context.Background()), want)
	record, err := model.GetMembershipBillingRecord("must-remain-held")
	require.NoError(t, err)
	assert.Equal(t, "reserved", record.State)
}

func TestMembershipRecoveryClosedConflictsAreNotBlockedByUnresolvedBacklog(t *testing.T) {
	user := setupMembershipRecoveryWorkerTest(t)
	require.NoError(t, model.DB.Model(&model.MembershipAccount{}).Where("user_id = ?", user.Id).Update("amount_total", 10000).Error)
	now := time.Now().Unix()
	var unresolved []model.MembershipBillingRecovery
	for i := 0; i < 12; i++ {
		record := reserveMembershipRecoveryWorkerTest(t, user, fmt.Sprintf("open-conflict-%02d", i), true, false)
		recovery := model.MembershipBillingRecovery{RequestID: record.RequestID, State: model.MembershipRecoveryConflict,
			EvidenceKind: model.MembershipEvidenceUnknown, Revision: 1, NextAttemptAt: now - 2}
		require.NoError(t, model.DB.Create(&recovery).Error)
		unresolved = append(unresolved, recovery)
	}
	for _, state := range []string{"settled", "released"} {
		record := reserveMembershipRecoveryWorkerTest(t, user, "closed-"+state, false, false)
		var err error
		if state == "settled" {
			_, err = model.SettleMembershipRelayBilling(record.RequestID, 1, 0, true)
		} else {
			_, err = model.ReleaseMembershipBilling(record.RequestID)
		}
		require.NoError(t, err)
		require.NoError(t, model.DB.Create(&model.MembershipBillingRecovery{RequestID: record.RequestID,
			State: model.MembershipRecoveryConflict, EvidenceKind: model.MembershipEvidenceUnknown,
			Revision: 1, NextAttemptAt: now - 1}).Error)
	}
	var before model.MembershipAccount
	require.NoError(t, model.DB.First(&before, "user_id = ?", user.Id).Error)
	worker := &membershipBillingRecoveryWorker{workerID: "recovery-worker"}
	require.NoError(t, worker.discover(context.Background(), "conflict", true, &membershipBillingRecoveryCounts{}))
	var untouched int64
	require.NoError(t, model.DB.Model(&model.MembershipBillingRecovery{}).Where("state = ? AND revision = 1", model.MembershipRecoveryConflict).Count(&untouched).Error)
	assert.EqualValues(t, 14, untouched, "observe must not alter conflict records")
	require.NoError(t, model.DB.Model(&model.Option{}).Where(map[string]any{"key": model.MembershipBillingRecoveryControlOption}).Update("value", "apply:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Error)
	for run := 0; run < 2; run++ {
		require.NoError(t, worker.run(context.Background()))
		for _, expected := range unresolved {
			var actual model.MembershipBillingRecovery
			require.NoError(t, model.DB.First(&actual, "request_id = ?", expected.RequestID).Error)
			assert.Equal(t, expected, actual, "unresolved conflicts must retain evidence and scheduling")
			record, err := model.GetMembershipBillingRecord(expected.RequestID)
			require.NoError(t, err)
			assert.Equal(t, "reserved", record.State, "aged conflicts must not become compensation")
		}
		for _, state := range []string{"settled", "released"} {
			var recovery model.MembershipBillingRecovery
			require.NoError(t, model.DB.First(&recovery, "request_id = ?", "closed-"+state).Error)
			assert.Equal(t, model.MembershipRecoveryResolved, recovery.State)
		}
		var after model.MembershipAccount
		require.NoError(t, model.DB.First(&after, before.ID).Error)
		assert.Equal(t, before.AmountReserved, after.AmountReserved)
		assert.Equal(t, before.AmountUsed, after.AmountUsed, "cleanup must not apply funds twice")
	}
}

func TestMembershipRecoveryConsumeLogRejectsConflictingProof(t *testing.T) {
	record := model.MembershipBillingRecord{
		RequestID: "request-1", RequestHash: "request-hash", UserID: 3, TokenID: 4,
		ReviewChannelID: 5, Source: model.MembershipSource, AccountID: 7,
		Snapshot: `{"frozen":true}`, ReservedQuota: 100,
	}
	log := model.Log{
		Id: 9, RequestId: record.RequestID, UserId: record.UserID, TokenId: record.TokenID,
		ChannelId: record.ReviewChannelID, Quota: 20, CreatedAt: 100,
		Other: `{"billing_source":"membership","admin_info":{"membership_billing":{"request_id":"request-1","assessed":20,"charged":20}}}`,
	}
	evidence, err := membershipRecoveryLogEvidence(record, []model.Log{log})
	require.NoError(t, err)
	assert.Equal(t, model.MembershipEvidenceFinalUsage, evidence.Kind)
	assert.Equal(t, "request-hash", evidence.RequestHash)
	assert.EqualValues(t, 20, *evidence.AssessedQuota)
	assert.Equal(t, "9", evidence.ReceiptID)
	log.ChannelId++
	_, err = membershipRecoveryLogEvidence(record, []model.Log{log})
	assert.ErrorIs(t, err, model.ErrMembershipConflict)
	_, err = membershipRecoveryLogEvidence(record, nil)
	assert.ErrorIs(t, err, model.ErrMembershipConflict)
}
