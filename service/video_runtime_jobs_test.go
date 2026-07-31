package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"

	"github.com/stretchr/testify/require"
)

type videoStudioWorkerJobsRecorder struct {
	expireCalls  int
	cleanupCalls int
	outboxCalls  int
}

func (recorder *videoStudioWorkerJobsRecorder) ExpireUploads(context.Context) error {
	recorder.expireCalls++
	return nil
}

func (recorder *videoStudioWorkerJobsRecorder) CleanupReferences(context.Context) error {
	recorder.cleanupCalls++
	return nil
}

func (recorder *videoStudioWorkerJobsRecorder) ProcessOutbox(context.Context) error {
	recorder.outboxCalls++
	return nil
}

func TestVideoStudioRuntimeRegistersIdempotencyCleanupWhenAPIIsEnabled(t *testing.T) {
	rawSetting := config.GlobalConfig.Get("video_studio")
	original, err := config.ConfigToMap(rawSetting)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.UpdateConfigFromMap(rawSetting, original))
	})
	require.NoError(t, config.UpdateConfigFromMap(rawSetting, map[string]string{
		"access_mode":             video_studio_setting.AccessModeAll,
		"archive_enqueue_enabled": "false",
		"worker_enabled":          "false",
		"backfill_enabled":        "false",
	}))

	registry := NewBackgroundJobRegistry()
	require.NoError(t, RegisterVideoStudioBackgroundJobs(registry, "test-worker"))
	descriptors := registry.Descriptors()
	require.Equal(t, []BackgroundJobDescriptor{
		{
			Name: "video-studio-backfill", Interval: time.Minute, RunOnStart: true,
			WritesData: true, RequiresLeaderLease: true,
		},
		{
			Name: "video-studio-idempotency-cleanup", Interval: time.Hour, RunOnStart: true,
			WritesData: true, RequiresLeaderLease: true,
		},
		{
			Name: "video-studio-outbox", Interval: 2 * time.Second, RunOnStart: true,
			WritesData: true, RequiresLeaderLease: true,
		},
		{
			Name: "video-studio-reconcile", Interval: 5 * time.Second, RunOnStart: true,
			WritesData: true, RequiresLeaderLease: true,
		},
		{
			Name: "video-studio-reference-cleanup", Interval: time.Hour, RunOnStart: true,
			WritesData: true, RequiresLeaderLease: true,
		},
		{
			Name: "video-studio-upload-expiry", Interval: time.Minute, RunOnStart: true,
			WritesData: true, RequiresLeaderLease: true,
		},
	}, descriptors)
	require.NoError(t, registry.jobs["video-studio-outbox"].Run(context.Background()))
}

func TestVideoStudioWorkerJobsFollowRuntimeToggleWithoutReregistration(t *testing.T) {
	rawSetting := config.GlobalConfig.Get("video_studio")
	original, err := config.ConfigToMap(rawSetting)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.UpdateConfigFromMap(rawSetting, original))
	})
	require.NoError(t, config.UpdateConfigFromMap(rawSetting, map[string]string{
		"worker_enabled": "false",
	}))
	require.ErrorIs(t, ValidateVideoAssetProcessingAvailable(), ErrVideoAssetProcessingUnavailable)

	registry := NewBackgroundJobRegistry()
	recorder := &videoStudioWorkerJobsRecorder{}
	require.NoError(t, registerVideoStudioWorkerJobs(registry, recorder))
	for _, name := range []string{
		"video-studio-upload-expiry",
		"video-studio-reference-cleanup",
		"video-studio-outbox",
	} {
		require.NoError(t, registry.jobs[name].Run(context.Background()))
	}
	require.Zero(t, recorder.expireCalls)
	require.Zero(t, recorder.cleanupCalls)
	require.Zero(t, recorder.outboxCalls)

	require.NoError(t, config.UpdateConfigFromMap(rawSetting, map[string]string{
		"worker_enabled": "true",
	}))
	require.NoError(t, ValidateVideoAssetProcessingAvailable())
	for _, name := range []string{
		"video-studio-upload-expiry",
		"video-studio-reference-cleanup",
		"video-studio-outbox",
	} {
		require.NoError(t, registry.jobs[name].Run(context.Background()))
	}
	require.Equal(t, 1, recorder.expireCalls)
	require.Equal(t, 1, recorder.cleanupCalls)
	require.Equal(t, 1, recorder.outboxCalls)
}

func TestVideoStudioReconcileFollowsRuntimeToggleWithoutReregistration(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	rawSetting := config.GlobalConfig.Get("video_studio")
	original, err := config.ConfigToMap(rawSetting)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.UpdateConfigFromMap(rawSetting, original))
	})
	require.NoError(t, config.UpdateConfigFromMap(rawSetting, map[string]string{
		"access_mode":             video_studio_setting.AccessModeOff,
		"archive_enqueue_enabled": "false",
		"worker_enabled":          "false",
		"backfill_enabled":        "false",
	}))

	archiveableTask := createVideoReconcileCandidate(
		t, db, 0, "runtime-toggle-task", "https://media.example/runtime-toggle.mp4",
	)
	registry := NewBackgroundJobRegistry()
	require.NoError(t, RegisterVideoStudioBackgroundJobs(registry, "test-worker"))
	job, registered := registry.jobs["video-studio-reconcile"]
	require.True(t, registered)

	require.NoError(t, job.Run(context.Background()))
	var links []model.KKAIVideoTaskAsset
	require.NoError(t, db.Where("role = ?", model.VideoTaskAssetRoleOutput).Find(&links).Error)
	require.Empty(t, links)

	require.NoError(t, config.UpdateConfigFromMap(rawSetting, map[string]string{
		"archive_enqueue_enabled": "true",
	}))
	require.NoError(t, job.Run(context.Background()))
	require.NoError(t, db.Where("role = ?", model.VideoTaskAssetRoleOutput).Find(&links).Error)
	require.Len(t, links, 1)
	require.Equal(t, archiveableTask.ID, links[0].TaskID)
}

func TestVideoStudioReconcileRuntimeJobRetainsCursorAcrossRuns(t *testing.T) {
	db := newVideoPipelineTestDB(t)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	rawSetting := config.GlobalConfig.Get("video_studio")
	original, err := config.ConfigToMap(rawSetting)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.UpdateConfigFromMap(rawSetting, original))
	})
	require.NoError(t, config.UpdateConfigFromMap(rawSetting, map[string]string{
		"access_mode":             video_studio_setting.AccessModeOff,
		"archive_enqueue_enabled": "true",
		"worker_enabled":          "false",
		"backfill_enabled":        "false",
	}))

	for index := 0; index < 101; index++ {
		createVideoReconcileCandidate(t, db, 0, fmt.Sprintf("runtime-reconcile-blocker-%03d", index), "")
	}
	archiveableTask := createVideoReconcileCandidate(
		t, db, 0, "runtime-reconcile-deep-task", "https://media.example/runtime-deep.mp4",
	)
	registry := NewBackgroundJobRegistry()
	require.NoError(t, RegisterVideoStudioBackgroundJobs(registry, "test-worker"))
	job, registered := registry.jobs["video-studio-reconcile"]
	require.True(t, registered)

	var links []model.KKAIVideoTaskAsset
	for round := 0; round < 3 && len(links) == 0; round++ {
		require.NoError(t, job.Run(context.Background()))
		require.NoError(t, db.Where("role = ?", model.VideoTaskAssetRoleOutput).Find(&links).Error)
	}
	require.Len(t, links, 1)
	require.Equal(t, archiveableTask.ID, links[0].TaskID)

	require.NoError(t, job.Run(context.Background()))
	var assets int64
	var events int64
	var successfulTasks int64
	require.NoError(t, db.Model(&model.KKAIVideoAsset{}).Count(&assets).Error)
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("topic = ?", VideoOutboxTopicArchive).Count(&events).Error)
	require.NoError(t, db.Model(&model.Task{}).Where("status = ?", model.TaskStatusSuccess).Count(&successfulTasks).Error)
	require.EqualValues(t, 1, assets)
	require.EqualValues(t, 1, events)
	require.EqualValues(t, 102, successfulTasks)
}
