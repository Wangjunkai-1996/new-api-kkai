package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipTaskSettlementLogsDoNotAttributeWholeTaskSavingsToRefundsOrDeltas(t *testing.T) {
	for _, tc := range []struct {
		name      string
		operation string
		logType   int
	}{
		{name: "failed task refund", operation: model.TaskBillingAuditOperationRefund, logType: model.LogTypeRefund},
		{name: "unused reservation refund", operation: model.TaskBillingAuditOperationSettlement, logType: model.LogTypeRefund},
		{name: "additional consumption", operation: model.TaskBillingAuditOperationSettlement, logType: model.LogTypeConsume},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &model.Task{
				TaskID: "member-task", Quota: 180,
				PrivateData: model.TaskPrivateData{
					BillingSource: model.MembershipSource, MembershipBillingRequestID: "member-task-charge", MembershipAssessedQuota: 180,
					MembershipPricing: &types.MembershipPricing{
						AccountID: 12, TierID: "gold", TierName: "Gold", TierStyle: "gold", BaseRatio: 0.3, Discount: 0.6, WalletRatio: 0.288,
						Price: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.18}},
					},
				},
			}
			other, _ := taskBillingAuditLogDetails(task, model.TaskBillingAuditPayload{
				Operation: tc.operation, LogType: tc.logType, Quota: 20, PreviousQuota: 160, CurrentQuota: 180,
			})
			snapshot := other.Snapshot()
			assert.Equal(t, model.MembershipSource, snapshot["billing_source"])
			assert.NotContains(t, snapshot, "membership_estimated_saved_quota")
			assert.NotContains(t, snapshot, "membership_estimated_wallet_quota")
			if tc.logType == model.LogTypeRefund {
				assert.NotContains(t, snapshot, "membership_savings")
				assert.NotContains(t, snapshot, "membership_discount")
			} else {
				assert.Equal(t, "Gold", snapshot["membership_tier_name"])
				assert.Equal(t, 0.6, snapshot["membership_discount"])
				assert.InDelta(t, 0.375, snapshot["membership_savings"], 1e-9)
			}
		})
	}
}

func TestTaskBillingAuditLogDetailsKeepsManagedProviderReasonAdminOnly(t *testing.T) {
	const rawProviderReason = "failed at https://provider.example/video.mp4?token=query-secret key=echoed-provider-key"
	const publicFailureReason = "video generation failed"
	task := &model.Task{
		TaskID:     "task_managed_failure_audit",
		FailReason: publicFailureReason,
		PrivateData: model.TaskPrivateData{
			AssetHostedResult: true,
		},
	}
	payload := model.TaskBillingAuditPayload{
		Operation: model.TaskBillingAuditOperationRefund,
		Reason:    rawProviderReason,
	}

	other, content := taskBillingAuditLogDetails(task, payload)

	assert.Empty(t, content)
	snapshot := other.Snapshot()
	assert.Equal(t, publicFailureReason, snapshot["reason"])
	adminInfo, ok := snapshot["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, rawProviderReason, adminInfo["provider_failure_reason"])
}
