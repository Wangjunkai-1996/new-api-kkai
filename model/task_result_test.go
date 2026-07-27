package model

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAssetHostedTaskPublicResultHidesUpstreamPayload(t *testing.T) {
	task := &Task{
		PrivateData: TaskPrivateData{
			ResultURL:         "https://upstream.example/private.mp4",
			ArchiveSource:     "data:video/mp4;base64,cHJpdmF0ZQ==",
			AssetHostedResult: true,
		},
		Data: json.RawMessage(`{"upstream_url":"https://upstream.example/private.mp4"}`),
	}

	assert.True(t, task.IsAssetHostedResult())
	assert.Empty(t, task.PublicResultURL())
	assert.Nil(t, task.PublicData())
	assert.Equal(t, "https://upstream.example/private.mp4", task.GetResultURL())
	assert.JSONEq(t, `{"upstream_url":"https://upstream.example/private.mp4"}`, string(task.Data))

	task.FailReason = "provider error: HTTPS://upstream.example/legacy-private.mp4"
	assert.Equal(t, "video generation failed", task.PublicFailReason())
	task.FailReason = "provider rejected the request"
	assert.Equal(t, "video generation failed", task.PublicFailReason())
}

func TestClearAssetHostedTaskResultSourcePreservesAuditData(t *testing.T) {
	truncateTables(t)
	task := &Task{
		TaskID: "task_archive_complete",
		Status: TaskStatusSuccess,
		Quota:  321,
		PrivateData: TaskPrivateData{
			ResultURL:         "https://gateway.example/v1/videos/task_archive_complete/content",
			ArchiveSource:     "data:video/mp4;base64,dmlkZW8=",
			AssetHostedResult: true,
			BillingState:      TaskBillingStateCompleted,
			TokenQuota:        321,
		},
		Data: json.RawMessage(`{"provider":"raw audit payload"}`),
	}
	insertTask(t, task)

	cleared := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var clearErr error
		cleared, clearErr = ClearAssetHostedTaskResultSource(context.Background(), tx, task.ID, task.PrivateData.ArchiveSource)
		return clearErr
	})
	require.NoError(t, err)
	require.True(t, cleared)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.Empty(t, reloaded.PrivateData.ResultURL)
	assert.Empty(t, reloaded.PrivateData.ArchiveSource)
	assert.True(t, reloaded.PrivateData.AssetHostedResult)
	assert.Equal(t, TaskBillingStateCompleted, reloaded.PrivateData.BillingState)
	assert.Equal(t, 321, reloaded.PrivateData.TokenQuota)
	assert.Equal(t, 321, reloaded.Quota)
	assert.JSONEq(t, `{"provider":"raw audit payload"}`, string(reloaded.Data))
}

func TestClearAssetHostedTaskResultSourceRejectsStaleArchive(t *testing.T) {
	truncateTables(t)
	task := &Task{
		TaskID: "task_archive_stale",
		PrivateData: TaskPrivateData{
			ResultURL:         "https://upstream.example/new.mp4",
			ArchiveSource:     "https://upstream.example/new.mp4",
			AssetHostedResult: true,
		},
	}
	insertTask(t, task)

	cleared := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var clearErr error
		cleared, clearErr = ClearAssetHostedTaskResultSource(context.Background(), tx, task.ID, "https://upstream.example/old.mp4")
		return clearErr
	})
	require.NoError(t, err)
	assert.False(t, cleared)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, "https://upstream.example/new.mp4", reloaded.PrivateData.ResultURL)
	assert.Equal(t, "https://upstream.example/new.mp4", reloaded.PrivateData.ArchiveSource)
}
