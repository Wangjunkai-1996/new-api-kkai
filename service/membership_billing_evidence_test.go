package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func enableMembershipEvidenceFixture(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.MembershipBillingRecovery{}, &model.MembershipBillingScanProgress{}))
	previousMode, previousCheck := MembershipBillingRecoveryMode(), membershipBillingRecoverySchemaCheck
	membershipBillingRecoveryMode.Store("apply")
	membershipBillingRecoverySchemaCheck = func(context.Context, *gorm.DB) error { return nil }
	membershipBillingRecoverySchema.db = nil
	t.Cleanup(func() {
		membershipBillingRecoveryMode.Store(previousMode)
		membershipBillingRecoverySchemaCheck = previousCheck
		membershipBillingRecoverySchema.db = nil
	})
}

func reserveMembershipEvidenceFixture(t *testing.T, user model.User, requestID string) *relaycommon.RelayInfo {
	t.Helper()
	info := &relaycommon.RelayInfo{UserId: user.Id, RequestId: requestID, ChannelMeta: &relaycommon.ChannelMeta{},
		UsingGroup: "gpt", IsPlayground: true, OriginModelName: "test-model",
		PriceData:         types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.2}},
		MembershipPricing: &types.MembershipPricing{MaximumBeforeGroup: 800},
	}
	require.Nil(t, preConsumeMembershipBilling(info))
	return info
}

func TestMembershipEvidenceFailedTerminalSurvivesAndRecoversOnce(t *testing.T) {
	for _, release := range []bool{false, true} {
		name := "usage_including_local_fee"
		if release {
			name = "confirmed_zero"
		}
		t.Run(name, func(t *testing.T) {
			c, user := setupMembershipServiceTest(t)
			enableMembershipEvidenceFixture(t)
			info := reserveMembershipEvidenceFixture(t, user, name)
			if !release {
				info.MembershipHasBillableUsage, info.MembershipViolationFee = true, 8
			}
			// Mutate only the in-memory quote; evidence must bind the persisted one.
			info.MembershipPricing.MaximumBeforeGroup = 12345
			injected := errors.New("injected settlement storage failure")
			failOnce := true
			const callback = "test:terminal_commit_failure"
			require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
				if failOnce && tx.Statement.Table == "kkai_membership_accounts" {
					failOnce = false
					tx.AddError(injected)
				}
			}))
			t.Cleanup(func() { require.NoError(t, model.DB.Callback().Update().Remove(callback)) })
			if release {
				info.Billing.Refund(c)
			} else {
				require.ErrorIs(t, info.Billing.Settle(20), injected)
			}
			require.False(t, failOnce, "the money transaction must actually fail")
			assert.False(t, info.Billing.NeedsRefund(), "a failed terminal belongs to recovery")
			stored, err := model.GetMembershipBillingRecord(info.RequestId)
			require.NoError(t, err)
			assert.Equal(t, "reserved", stored.State)
			var recovery model.MembershipBillingRecovery
			require.NoError(t, model.DB.First(&recovery, "request_id = ?", info.RequestId).Error)
			assert.Equal(t, model.MembershipRecoveryReady, recovery.State)
			var evidence model.MembershipRelayEvidenceInput
			require.NoError(t, common.UnmarshalJsonStr(recovery.EvidencePayload, &evidence))
			assert.Equal(t, model.MembershipBillingSnapshotHash(stored.Snapshot), evidence.SnapshotHash)
			actual := int64(20)
			if release {
				actual = 0
				assert.Equal(t, model.MembershipEvidenceZero, evidence.Kind)
			}
			require.NotNil(t, evidence.AssessedQuota)
			assert.Equal(t, actual, *evidence.AssessedQuota)
			completed, err := model.ResolveMembershipRelayEvidence(context.Background(), info.RequestId)
			require.NoError(t, err)
			require.True(t, completed.Applied)
			replayed, err := model.ResolveMembershipRelayEvidence(context.Background(), info.RequestId)
			require.NoError(t, err)
			assert.False(t, replayed.Applied)
			self, err := model.GetMembershipSelf(user.Id)
			require.NoError(t, err)
			assert.Equal(t, actual, self.Account.AmountUsed)
			assert.Zero(t, self.Account.AmountReserved)
			var updated model.User
			require.NoError(t, model.DB.First(&updated, user.Id).Error)
			expectedRequests := 1
			if release {
				expectedRequests = 0
			}
			assert.Equal(t, expectedRequests, updated.RequestCount)
		})
	}
}

func TestMembershipEvidenceUnknownNeverBecomesZero(t *testing.T) {
	c, user := setupMembershipServiceTest(t)
	enableMembershipEvidenceFixture(t)
	info := reserveMembershipEvidenceFixture(t, user, "unknown-recovery")
	info.BillingOutcomeUnknown = true
	require.Error(t, SettleBilling(c, info, 0))
	info.Billing.Refund(c)
	var recovery model.MembershipBillingRecovery
	require.NoError(t, model.DB.First(&recovery, "request_id = ?", info.RequestId).Error)
	assert.Equal(t, model.MembershipRecoveryPending, recovery.State)
	assert.Equal(t, model.MembershipEvidenceUnknown, recovery.EvidenceKind)
	_, err := model.ResolveMembershipRelayEvidence(context.Background(), info.RequestId)
	require.Error(t, err)
	self, err := model.GetMembershipSelf(user.Id)
	require.NoError(t, err)
	assert.Positive(t, self.Account.AmountReserved)
	assert.Zero(t, self.Account.AmountUsed)
}

func TestMembershipEvidenceRefundCannotEraseLocalFee(t *testing.T) {
	c, user := setupMembershipServiceTest(t)
	enableMembershipEvidenceFixture(t)
	info := reserveMembershipEvidenceFixture(t, user, "local-fee-recovery")
	info.MembershipViolationFee = 8
	info.Billing.Refund(c)
	stored, err := model.GetMembershipBillingRecord(info.RequestId)
	require.NoError(t, err)
	assert.Equal(t, "reserved", stored.State)
	var recovery model.MembershipBillingRecovery
	require.NoError(t, model.DB.First(&recovery, "request_id = ?", info.RequestId).Error)
	assert.Equal(t, model.MembershipEvidenceUnknown, recovery.EvidenceKind)
	assert.Equal(t, model.MembershipRecoveryPending, recovery.State)
}

type membershipEvidenceSQLCounter struct {
	logger.Interface
	statements int
}

func (counter *membershipEvidenceSQLCounter) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	counter.statements++
	counter.Interface.Trace(ctx, begin, fc, err)
}

func TestMembershipEvidenceSuccessAddsNoSynchronousSQL(t *testing.T) {
	_, user := setupMembershipServiceTest(t)
	enableMembershipEvidenceFixture(t)
	counter := &membershipEvidenceSQLCounter{Interface: model.DB.Logger}
	model.DB.Logger = counter
	var baseline int
	for _, mode := range []string{"off", "apply"} {
		membershipBillingRecoveryMode.Store(mode)
		checks := 0
		membershipBillingRecoverySchemaCheck = func(context.Context, *gorm.DB) error {
			checks++
			return errors.New("successful requests must not check recovery schema")
		}
		membershipBillingRecoverySchema.db = nil
		counter.statements = 0
		info := reserveMembershipEvidenceFixture(t, user, "success-"+mode)
		require.NoError(t, model.MarkMembershipBillingDispatched(info.RequestId, 3, common.GetTimestamp()+300))
		info.ChannelId, info.MembershipHasBillableUsage = 3, true
		require.NoError(t, info.Billing.Settle(10))
		if mode == "off" {
			baseline = counter.statements
		} else {
			assert.Equal(t, baseline, counter.statements, "reserve, dispatch and settle retain the same SQL count")
		}
		assert.Zero(t, checks)
	}
	var recoveries int64
	require.NoError(t, model.DB.Model(&model.MembershipBillingRecovery{}).Count(&recoveries).Error)
	assert.Zero(t, recoveries)
}
