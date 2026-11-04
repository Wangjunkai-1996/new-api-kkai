package service

import (
	"testing"
	"time"

	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKKAIGroupStatusCountsDuplicateAutoCandidatesOnce(t *testing.T) {
	now := time.Unix(1_784_020_200, 0)
	withKKAIGroupStatusSources(t, now, nil,
		perfmetrics.KKAIGroupBucketResult{RedisAvailable: true, Buckets: []perfmetrics.KKAIGroupBucket{
			{Group: "default", RequestCount: 2, SuccessCount: 2, LastSampleAt: now.Unix()},
			{Group: "vip", RequestCount: 2, LastSampleAt: now.Unix()},
			{Group: "auto", RequestCount: 1, SuccessCount: 1, LastSampleAt: now.Unix()},
			{Group: "hidden", RequestCount: 10, LastSampleAt: now.Unix()},
		}},
		perfmetrics.KKAIGroupBucketResult{RedisAvailable: true, Buckets: []perfmetrics.KKAIGroupBucket{
			{Group: "default", BucketTs: now.Add(-time.Hour).Unix(), CacheSampleCount: 2, CacheHitCount: 2},
			{Group: "vip", BucketTs: now.Add(-time.Hour).Unix(), CacheSampleCount: 2},
			{Group: "auto", BucketTs: now.Add(-time.Hour).Unix(), CacheSampleCount: 1, CacheHitCount: 1},
		}},
		perfmetrics.KKAIGroupSignalResult{RedisAvailable: true, Events: []perfmetrics.KKAIGroupSignalEvent{
			{Group: "default", Ts: now.Add(-3 * time.Second).Unix(), Success: true},
			{Group: "vip", Ts: now.Add(-2 * time.Second).Unix()},
			{Group: "auto", Ts: now.Add(-time.Second).Unix(), Success: true},
			{Group: "hidden", Ts: now.Unix()},
		}},
	)

	result, err := GetKKAIGroupStatuses(KKAIGroupStatusRequest{
		UsableGroups: map[string]string{"default": "Default", "vip": "VIP", "auto": "Auto"},
		AutoGroups:   []string{"vip", "vip", "default", "auto", "hidden"},
		Window:       "now",
	})
	require.NoError(t, err)
	require.Len(t, result.Groups, 3)
	entry := result.Groups[0]
	require.Equal(t, "auto", entry.Group)
	assert.Equal(t, int64(5), entry.RequestCount)
	assert.Equal(t, 60.0, entry.SuccessRate)
	require.NotNil(t, entry.CacheStats)
	assert.Equal(t, int64(5), entry.CacheStats.SampleCount)
	require.NotNil(t, entry.CacheStats.RequestHitRate)
	assert.Equal(t, 60.0, *entry.CacheStats.RequestHitRate)
	assert.Equal(t, []KKAIGroupRecentEvent{
		{Ts: now.Add(-3 * time.Second).Unix(), Status: "success"},
		{Ts: now.Add(-2 * time.Second).Unix(), Status: "failure"},
		{Ts: now.Add(-time.Second).Unix(), Status: "success"},
	}, entry.RecentEvents)
}
