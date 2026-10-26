package service

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipTaskExpressionBudgetUsesSubmissionEstimateWithoutModelLimits(t *testing.T) {
	snapshot := &billingexpr.BillingSnapshot{TaskUsageBilling: true, ExprString: `tier("video",u("seconds")*u("image_count")*0.1)`,
		QuotaPerUnit: 500000, EstimatedQuotaBeforeGroup: 50000,
		UsageFacts: map[string]any{"seconds": 1.0, "image_count": 1.0}}
	info := &relaycommon.RelayInfo{TieredBillingSnapshot: snapshot, OriginModelName: "test-video"}
	require.NoError(t, PrepareTaskMembershipExpressionBudget(info, nil))
	require.NotNil(t, info.MembershipPricing)
	assert.Equal(t, 50000.0, info.MembershipPricing.MaximumBeforeGroup)
	assert.Equal(t, 500000.0, info.MembershipPricing.QuotaPerUnit)
}

func TestMembershipTaskExpressionBudgetRejectsInvalidEstimate(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fact     any
		estimate float64
	}{
		{"NaN fact", math.NaN(), 50000},
		{"infinite fact", math.Inf(1), 50000},
		{"negative fact", -1.0, 50000},
		{"NaN estimate", 1.0, math.NaN()},
		{"infinite estimate", 1.0, math.Inf(1)},
		{"negative estimate", 1.0, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{TieredBillingSnapshot: &billingexpr.BillingSnapshot{
				TaskUsageBilling: true, EstimatedQuotaBeforeGroup: tc.estimate,
				UsageFacts: map[string]any{"seconds": tc.fact},
			}}
			require.Error(t, PrepareTaskMembershipExpressionBudget(info, nil))
			assert.Nil(t, info.MembershipPricing)
		})
	}
}

func TestMembershipTaskCompletionPersistsActualForCrashRecovery(t *testing.T) {
	truncate(t)
	seedUser(t, 7201, 100000)
	task := makeTask(7201, 0, 19000, 0, model.MembershipSource, 0)
	task.PrivateData.BillingState = model.TaskBillingStateAccepted
	task.PrivateData.MembershipBillingRequestID = "task:" + task.TaskID
	task.PrivateData.MembershipPricing = &types.MembershipPricing{Version: 1, QuotaPerUnit: 500000}
	task.PrivateData.TargetQuota = common.GetPointer(19000)
	task.PrivateData.BillingContext = &model.TaskBillingContext{ModelPrice: 0.02, GroupRatio: 0.19, PerCallBilling: true, OtherRatios: map[string]float64{"seconds": 10}}
	require.NoError(t, model.DB.Create(task).Error)
	before := task.Snapshot()
	task.Status = model.TaskStatusSuccess
	result := &relaycommon.TaskInfo{UsageFacts: map[string]any{"seconds": 4.0}}
	previousQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 1000000
	t.Cleanup(func() { common.QuotaPerUnit = previousQuotaPerUnit })
	require.NoError(t, prepareTaskMembershipCompletion(task, result))
	won, err := task.UpdateWithStatusPreservingBilling(before)
	require.NoError(t, err)
	require.True(t, won)
	var recovered model.Task
	require.NoError(t, model.DB.First(&recovered, task.ID).Error)
	require.NotNil(t, recovered.PrivateData.MembershipCompletionQuota)
	require.NotNil(t, recovered.PrivateData.TargetQuota)
	assert.Equal(t, 7600, *recovered.PrivateData.MembershipCompletionQuota)
	assert.Equal(t, 7600, *recovered.PrivateData.TargetQuota, "recovery must settle final usage rather than the submission estimate")
	assert.Equal(t, 19000, recovered.Quota, "status persistence must not mutate the reserved funds")
}
