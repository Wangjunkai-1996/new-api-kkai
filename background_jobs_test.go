package main

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestApplicationBackgroundJobsDeclareLeaderWriteBoundary(t *testing.T) {
	t.Setenv("CHANNEL_UPDATE_FREQUENCY", "")
	t.Setenv("BATCH_UPDATE_ENABLED", "false")
	t.Setenv(service.KKAIRiskStreamSecretEnvironmentVariable, "")
	db, err := gorm.Open(sqlite.Open("file:background-jobs-"+time.Now().Format("150405.000000000")+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	oldFrequency := common.SyncFrequency
	common.SyncFrequency = 60
	t.Cleanup(func() { common.SyncFrequency = oldFrequency })

	registry, err := newApplicationBackgroundJobs("node-test-worker")
	require.NoError(t, err)
	descriptors := registry.Descriptors()
	require.NotEmpty(t, descriptors)
	foundPerformanceMetricFlush := false
	for _, descriptor := range descriptors {
		if descriptor.Name == "performance-metric-flush" {
			foundPerformanceMetricFlush = true
			require.True(t, descriptor.RunOnStart)
		}
		if descriptor.Name == "runtime-cache-sync" {
			require.False(t, descriptor.WritesData)
			require.False(t, descriptor.RequiresLeaderLease)
			require.False(t, descriptor.FlushesProcessLocalState)
			continue
		}
		if descriptor.Name == "quota-dashboard-flush" || descriptor.Name == "performance-metric-flush" {
			require.True(t, descriptor.WritesData)
			require.False(t, descriptor.RequiresLeaderLease)
			require.True(t, descriptor.FlushesProcessLocalState)
			continue
		}
		require.True(t, descriptor.WritesData, descriptor.Name)
		require.True(t, descriptor.RequiresLeaderLease, descriptor.Name)
		require.False(t, descriptor.FlushesProcessLocalState, descriptor.Name)
	}
	require.True(t, foundPerformanceMetricFlush)
}

func TestServingBackgroundRuntimeOwnsProcessLocalFlushes(t *testing.T) {
	t.Setenv(common.NodeRoleEnvironmentVariable, string(common.NodeRoleServing))
	t.Setenv("DISABLE_BACKGROUND_TASKS", "true")
	require.NoError(t, common.InitNodeRoleFromEnvironment())
	t.Cleanup(func() {
		t.Setenv(common.NodeRoleEnvironmentVariable, string(common.NodeRoleLeader))
		t.Setenv("DISABLE_BACKGROUND_TASKS", "false")
		require.NoError(t, common.InitNodeRoleFromEnvironment())
	})

	runtime := currentBackgroundJobRuntime("node-test-worker")
	require.False(t, runtime.WriteJobsEnabled)
	require.True(t, runtime.LocalWriteJobsEnabled)
}

func TestServingBackgroundShutdownPersistsPerformanceSamples(t *testing.T) {
	t.Setenv(common.NodeRoleEnvironmentVariable, string(common.NodeRoleServing))
	t.Setenv("DISABLE_BACKGROUND_TASKS", "true")
	t.Setenv("BATCH_UPDATE_ENABLED", "false")
	t.Setenv("CHANNEL_UPDATE_FREQUENCY", "")
	t.Setenv(service.KKAIRiskStreamSecretEnvironmentVariable, "")
	previousRole := common.CurrentNodeRole()
	previousDisabled := common.WriteBackgroundTasksDisabled()
	require.NoError(t, common.InitNodeRoleFromEnvironment())
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}))
	previousDB, previousRedis := model.DB, common.RedisEnabled
	previousExport, previousFrequency := common.DataExportEnabled, common.SyncFrequency
	metricsSetting := config.GlobalConfig.Get("perf_metrics_setting").(*perf_metrics_setting.PerfMetricsSetting)
	previousMetrics := *metricsSetting
	model.DB, common.RedisEnabled = db, false
	common.DataExportEnabled, common.SyncFrequency = false, 60
	metricsSetting.Enabled = true
	t.Cleanup(func() {
		model.DB, common.RedisEnabled = previousDB, previousRedis
		common.DataExportEnabled, common.SyncFrequency = previousExport, previousFrequency
		*metricsSetting = previousMetrics
		t.Setenv(common.NodeRoleEnvironmentVariable, string(previousRole))
		if previousDisabled {
			t.Setenv("DISABLE_BACKGROUND_TASKS", "true")
		} else {
			t.Setenv("DISABLE_BACKGROUND_TASKS", "false")
		}
		require.NoError(t, common.InitNodeRoleFromEnvironment())
		require.NoError(t, sqlDB.Close())
	})
	registry, err := newApplicationBackgroundJobs("node-metric-shutdown")
	require.NoError(t, err)
	perfmetrics.Record(perfmetrics.Sample{Model: "serving-shutdown", Group: "default", Success: true, LatencyMs: 200})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, registry.Run(ctx, currentBackgroundJobRuntime("node-metric-shutdown")))
	var row model.PerfMetric
	require.NoError(t, db.Where("model_name = ?", "serving-shutdown").First(&row).Error)
	assert.EqualValues(t, 1, row.RequestCount)
	assert.EqualValues(t, 200, row.TotalLatencyMs)
}

func TestApplicationBackgroundJobsRejectLocalBatchQuotaBuffer(t *testing.T) {
	for _, value := range []string{"true", "TRUE", "1", "sometimes"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("BATCH_UPDATE_ENABLED", value)
			t.Setenv("CHANNEL_UPDATE_FREQUENCY", "")
			t.Setenv(service.KKAIRiskStreamSecretEnvironmentVariable, "")
			oldFrequency := common.SyncFrequency
			common.SyncFrequency = 60
			t.Cleanup(func() { common.SyncFrequency = oldFrequency })

			_, err := newApplicationBackgroundJobs("node-test-worker")
			require.ErrorContains(t, err, "BATCH_UPDATE_ENABLED")
		})
	}
}

func TestSyncRuntimeCachesReloadsOptionsChannelsAndPricing(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:runtime-cache-sync-"+time.Now().Format("150405.000000000")+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.Channel{}, &model.Ability{}))
	previousDB := model.DB
	previousMemoryCache := common.MemoryCacheEnabled
	previousOptions := common.OptionMap
	model.DB = db
	common.MemoryCacheEnabled = true
	common.OptionMap = make(map[string]string)
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCache
		common.OptionMap = previousOptions
	})

	baseURL := "https://guard.internal.example"
	channel := model.Channel{
		Type:    1,
		Key:     "test-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "before-sync",
		BaseURL: &baseURL,
		Models:  "gpt-cache-sync",
		Group:   "default",
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-cache-sync",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)
	require.NoError(t, db.Create(&model.Option{Key: "TopUpLink", Value: "https://before.example"}).Error)
	model.InitChannelCache()

	require.NoError(t, db.Model(&model.Option{}).Where("key = ?", "TopUpLink").Update("value", "https://after.example").Error)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("name", "after-sync").Error)
	_ = syncRuntimeCaches(context.Background())

	require.Equal(t, "https://after.example", common.OptionMap["TopUpLink"])
	reloaded, err := model.CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, "after-sync", reloaded.Name)
	foundPricing := false
	for _, item := range model.GetPricing() {
		if item.ModelName == "gpt-cache-sync" {
			foundPricing = true
			break
		}
	}
	require.True(t, foundPricing)
}
