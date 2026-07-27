package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	assert.Equal(t, publicFailureReason, other["reason"])
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, rawProviderReason, adminInfo["provider_failure_reason"])
}
