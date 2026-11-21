package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskRecoveryOutboxBindsFirstReceiptAndIgnoresReplay(t *testing.T) {
	truncate(t)
	seedUser(t, 627, 900)
	task := makeTask(627, 7, 100, 0, BillingSourceWallet, 0)
	task.PrivateData.BillingState = model.TaskBillingStateDispatching
	task.PrivateData.CredentialDigest = model.CredentialDigest("winning-key")
	require.NoError(t, model.DB.Create(task).Error)
	ctx := t.Context()
	require.NoError(t, EnqueueTaskBillingRecovery(ctx, model.DB, task))
	var event model.KKAIOutboxEvent
	require.NoError(t, model.DB.Where("topic = ?", KKAIOutboxTopicTaskBillingRecovery).First(&event).Error)
	original := event.Payload
	valid := TaskBillingRecoveryAcceptanceReceipt{UpstreamTaskID: "shared-id", ChannelID: 7, CredentialDigest: task.PrivateData.CredentialDigest, Status: model.TaskStatusSuccess, TargetQuota: 100}
	for _, tc := range []struct {
		channel int
		digest  string
	}{{8, valid.CredentialDigest}, {7, model.CredentialDigest("stale-key")}, {7, ""}} {
		bad := valid
		bad.ChannelID, bad.CredentialDigest = tc.channel, tc.digest
		bad.Status, bad.TargetQuota = model.TaskStatusFailure, 0
		require.ErrorIs(t, StoreTaskBillingRecoveryAcceptanceReceipt(ctx, task.ID, bad), model.ErrTaskBillingStateConflict)
		body, err := common.Marshal(TaskBillingRecoveryPayload{TaskID: task.ID, Acceptance: &bad})
		require.NoError(t, err)
		err = (TaskBillingRecoveryHandler{}).Handle(ctx, model.KKAIOutboxEvent{Payload: string(body)})
		require.ErrorIs(t, err, model.ErrTaskBillingStateConflict)
		require.ErrorAs(t, err, &permanentKKAIOutboxError{})
	}
	require.NoError(t, model.DB.First(&event, event.ID).Error)
	assert.Equal(t, original, event.Payload)
	assert.EqualValues(t, 900, getUserQuota(t, 627))
	require.NoError(t, StoreTaskBillingRecoveryAcceptanceReceipt(ctx, task.ID, valid))
	require.NoError(t, model.DB.First(&event, event.ID).Error)
	acceptedPayload := event.Payload
	require.NoError(t, (TaskBillingRecoveryHandler{}).Handle(ctx, event))
	require.NoError(t, model.DB.Model(&event).Updates(map[string]any{"status": model.KKAIOutboxStatusDelivered, "delivered_at": 99}).Error)
	for _, replayDigest := range []string{"", valid.CredentialDigest} {
		replay := valid
		replay.Status, replay.TargetQuota, replay.CredentialDigest = model.TaskStatusFailure, 0, replayDigest
		require.NoError(t, StoreTaskBillingRecoveryAcceptanceReceipt(ctx, task.ID, replay))
	}
	bad := valid
	bad.ChannelID = 8
	require.ErrorIs(t, StoreTaskBillingRecoveryAcceptanceReceipt(ctx, task.ID, bad), model.ErrTaskBillingStateConflict)
	require.NoError(t, model.DB.First(&event, event.ID).Error)
	assert.Equal(t, acceptedPayload, event.Payload)
	assert.Equal(t, model.KKAIOutboxStatusDelivered, event.Status)
	assert.EqualValues(t, 99, event.DeliveredAt)
	require.NoError(t, (TaskBillingRecoveryHandler{}).Handle(ctx, event))
	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, stored.Status)
	assert.Equal(t, 100, stored.Quota)
	assert.EqualValues(t, 900, getUserQuota(t, 627))
}

func TestTaskRecoveryOutboxPreservesConfirmedLegacyReceipt(t *testing.T) {
	truncate(t)
	task := makeTask(628, 7, 100, 0, BillingSourceWallet, 0)
	task.PrivateData.BillingState = model.TaskBillingStateDispatching
	task.PrivateData.Key = "original"
	require.NoError(t, model.DB.Create(task).Error)
	ctx := t.Context()
	require.NoError(t, EnqueueTaskBillingRecovery(ctx, model.DB, task))
	receipt := TaskBillingRecoveryAcceptanceReceipt{ChannelID: 7, UpstreamTaskID: "shared-id", Status: model.TaskStatusSuccess, TargetQuota: 100}
	require.NoError(t, StoreTaskBillingRecoveryAcceptanceReceipt(ctx, task.ID, receipt))
	var event model.KKAIOutboxEvent
	require.NoError(t, model.DB.Where("topic = ?", KKAIOutboxTopicTaskBillingRecovery).First(&event).Error)
	original := event.Payload
	receipt.CredentialDigest, receipt.Status, receipt.TargetQuota = model.CredentialDigest("original"), model.TaskStatusFailure, 0
	require.NoError(t, StoreTaskBillingRecoveryAcceptanceReceipt(ctx, task.ID, receipt))
	require.NoError(t, model.DB.First(&event, event.ID).Error)
	assert.Equal(t, original, event.Payload)
}
