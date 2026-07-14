package main

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestApplicationBackgroundJobsDeclareLeaderWriteBoundary(t *testing.T) {
	t.Setenv("CHANNEL_UPDATE_FREQUENCY", "")
	t.Setenv("BATCH_UPDATE_ENABLED", "false")
	t.Setenv(service.KKAIRiskStreamSecretEnvironmentVariable, "")
	oldFrequency := common.SyncFrequency
	common.SyncFrequency = 60
	t.Cleanup(func() { common.SyncFrequency = oldFrequency })

	registry, err := newApplicationBackgroundJobs("node-test-worker")
	require.NoError(t, err)
	descriptors := registry.Descriptors()
	require.NotEmpty(t, descriptors)
	for _, descriptor := range descriptors {
		if descriptor.Name == "runtime-cache-sync" {
			require.False(t, descriptor.WritesData)
			require.False(t, descriptor.RequiresLeaderLease)
			continue
		}
		require.True(t, descriptor.WritesData, descriptor.Name)
		require.True(t, descriptor.RequiresLeaderLease, descriptor.Name)
	}
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
