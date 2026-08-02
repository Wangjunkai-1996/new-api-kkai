package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"

	"github.com/stretchr/testify/require"
)

func TestImageStudioRuntimeRegistersOnlyItsIndependentJobs(t *testing.T) {
	registry := NewBackgroundJobRegistry()
	require.NoError(t, RegisterImageStudioBackgroundJobs(registry, "test-worker"))
	require.Equal(t, []BackgroundJobDescriptor{
		{
			Name: "image-studio-catalog-orphan-reconcile", Interval: time.Hour, RunOnStart: true,
			WritesData: true, RequiresLeaderLease: true,
		},
		{
			Name: "image-studio-idempotency-cleanup", Interval: time.Hour, RunOnStart: true,
			WritesData: true, RequiresLeaderLease: true,
		},
		{
			Name: "image-studio-outbox", Interval: 2 * time.Second, RunOnStart: true,
			WritesData: true, RequiresLeaderLease: true,
		},
		{
			Name: "image-studio-submission-reconcile", Interval: time.Minute, RunOnStart: true,
			WritesData: true, RequiresLeaderLease: true,
		},
	}, registry.Descriptors())
}

func TestImageStudioDisabledJobsDoNotInitializeStorageOrMediaTools(t *testing.T) {
	rawSetting := config.GlobalConfig.Get("image_studio")
	original, err := config.ConfigToMap(rawSetting)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.UpdateConfigFromMap(rawSetting, original))
	})
	require.NoError(t, config.UpdateConfigFromMap(rawSetting, map[string]string{
		"access_mode":    image_studio_setting.AccessModeOff,
		"worker_enabled": "false",
	}))

	previousDB := model.DB
	model.DB = nil
	t.Cleanup(func() { model.DB = previousDB })
	t.Setenv(videoFFmpegPathEnvironment, "/definitely/missing/ffmpeg")

	registry := NewBackgroundJobRegistry()
	require.NoError(t, RegisterImageStudioBackgroundJobs(registry, "test-worker"))
	for _, name := range []string{
		"image-studio-outbox",
		"image-studio-submission-reconcile",
		"image-studio-idempotency-cleanup",
	} {
		require.NoError(t, registry.jobs[name].Run(context.Background()))
	}
}
