package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipTaskExpressionBudgetUsesVerifiedLimitsAndRejectsEstimates(t *testing.T) {
	setupMembershipServiceTest(t)
	catalog, err := model.GetMembershipCatalog()
	require.NoError(t, err)
	catalog.ModelLimits = map[string]model.MembershipModelLimit{"test-video": {TaskUsageLimits: map[string]float64{"seconds": 5, "image_count": 2}, Evidence: "provider enforces aggregate request dimensions"}}
	_, err = model.PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
	require.NoError(t, err)
	snapshot := &billingexpr.BillingSnapshot{TaskUsageBilling: true, ExprString: `tier("video",u("seconds")*u("image_count")*0.1)`, QuotaPerUnit: 500000,
		UsageFacts: map[string]any{"seconds": 1.0, "image_count": 1.0}}
	info := &relaycommon.RelayInfo{TieredBillingSnapshot: snapshot, OriginModelName: "test-video"}
	schema := map[string]jsplugin.UsageFieldSchema{"seconds": {Type: "number", Unit: "second"}, "image_count": {Type: "number", Unit: "count"}, "tokens": {Type: "number", Unit: "token"}}
	require.NoError(t, PrepareTaskMembershipExpressionBudget(info, schema))
	require.NotNil(t, info.MembershipPricing)
	assert.Equal(t, 500000.0, info.MembershipPricing.MaximumBeforeGroup)
	snapshot.ExprString = `tier("token",u("tokens")*0.1)`
	snapshot.UsageFacts["tokens"] = 100.0
	require.ErrorContains(t, PrepareTaskMembershipExpressionBudget(info, schema), "no verified maximum")
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
