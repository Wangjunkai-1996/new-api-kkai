package service

import (
	"context"
	"errors"
	"testing"
	"time"

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
	assert.Equal(t, "apply", MembershipBillingRecoveryMode())
	descriptors := registry.Descriptors()
	require.Len(t, descriptors, 1)
	assert.Equal(t, "membership-relay-reconcile", descriptors[0].Name)
	assert.True(t, descriptors[0].WritesData)
	assert.True(t, descriptors[0].RequiresLeaderLease)
}

func setupMembershipRecoveryWorkerTest(t *testing.T) model.User {
	t.Helper()
	_, user := setupMembershipServiceTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.MembershipBillingRecovery{}, &model.MembershipBillingScanProgress{}, &model.KKAIJobLease{}, &model.Log{}))
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
		record.CreatedAt = time.Now().Unix() - 73*3600
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
