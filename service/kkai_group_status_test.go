package service

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withKKAIGroupStatusSources(
	t *testing.T,
	now time.Time,
	databaseBuckets []model.KKAIPerfMetricBucket,
	realtime perfmetrics.KKAIGroupBucketResult,
	historical perfmetrics.KKAIGroupBucketResult,
	signals perfmetrics.KKAIGroupSignalResult,
) {
	t.Helper()
	originalNow := kkaiGroupStatusNow
	originalLoad := loadKKAIPerfMetricBuckets
	originalQueryMinute := queryKKAIGroupMinuteBuckets
	originalQueryHistorical := queryKKAIGroupHistoricalBuckets
	originalQuerySignals := queryKKAIGroupRecentSignals
	kkaiGroupCacheSnapshots.Lock()
	clear(kkaiGroupCacheSnapshots.values)
	kkaiGroupCacheSnapshots.Unlock()
	t.Cleanup(func() {
		kkaiGroupStatusNow = originalNow
		loadKKAIPerfMetricBuckets = originalLoad
		queryKKAIGroupMinuteBuckets = originalQueryMinute
		queryKKAIGroupHistoricalBuckets = originalQueryHistorical
		queryKKAIGroupRecentSignals = originalQuerySignals
		kkaiGroupCacheSnapshots.Lock()
		clear(kkaiGroupCacheSnapshots.values)
		kkaiGroupCacheSnapshots.Unlock()
	})

	kkaiGroupStatusNow = func() time.Time { return now }
	loadKKAIPerfMetricBuckets = func(int64, int64, []string) ([]model.KKAIPerfMetricBucket, error) {
		return databaseBuckets, nil
	}
	queryKKAIGroupMinuteBuckets = func(int64, int64, []string) perfmetrics.KKAIGroupBucketResult {
		return realtime
	}
	queryKKAIGroupHistoricalBuckets = func(int64, int64, []string) perfmetrics.KKAIGroupBucketResult {
		return historical
	}
	queryKKAIGroupRecentSignals = func(_ []string, limit int) perfmetrics.KKAIGroupSignalResult {
		require.Equal(t, kkaiGroupRecentEventLimit, limit)
		return signals
	}
}

func TestKKAIGroupCacheSnapshotIsSharedByVisibleGroupsAndExpires(t *testing.T) {
	clock := time.Date(2026, time.August, 25, 10, 37, 42, 0, time.UTC)
	withKKAIGroupStatusSources(t, clock, nil,
		perfmetrics.KKAIGroupBucketResult{RedisAvailable: true},
		perfmetrics.KKAIGroupBucketResult{},
		perfmetrics.KKAIGroupSignalResult{RedisAvailable: true},
	)
	kkaiGroupStatusNow = func() time.Time { return clock }
	queries := 0
	queryKKAIGroupHistoricalBuckets = func(startTs, endTs int64, groups []string) perfmetrics.KKAIGroupBucketResult {
		queries++
		assert.Equal(t, clock.Add(-24*time.Hour).Unix(), startTs)
		assert.Equal(t, clock.Unix(), endTs)
		return perfmetrics.KKAIGroupBucketResult{RedisAvailable: true, Buckets: []perfmetrics.KKAIGroupBucket{
			{Group: groups[0], BucketTs: clock.Add(-time.Minute).Unix(), CacheSampleCount: 1, CacheHitCount: 1},
		}}
	}
	status := func(group string) KKAIGroupStatusEntry {
		result, err := GetKKAIGroupStatuses(KKAIGroupStatusRequest{Window: "now", UsableGroups: map[string]string{group: group}})
		require.NoError(t, err)
		require.Len(t, result.Groups, 1)
		return result.Groups[0]
	}
	require.NotNil(t, status("a").CacheStats)
	clock = clock.Add(15 * time.Second)
	require.NotNil(t, status("a").CacheStats)
	assert.Equal(t, 1, queries)
	require.NotNil(t, status("b").CacheStats)
	assert.Equal(t, 2, queries)
	clock = clock.Add(46 * time.Second)
	require.NotNil(t, status("a").CacheStats)
	assert.Equal(t, 3, queries)
}

func TestKKAIGroupCacheSnapshotDoesNotReuseRedisFailures(t *testing.T) {
	now := time.Date(2026, time.August, 25, 10, 37, 42, 0, time.UTC)
	withKKAIGroupStatusSources(t, now, nil,
		perfmetrics.KKAIGroupBucketResult{RedisAvailable: true},
		perfmetrics.KKAIGroupBucketResult{},
		perfmetrics.KKAIGroupSignalResult{RedisAvailable: true},
	)
	queries := 0
	queryKKAIGroupHistoricalBuckets = func(int64, int64, []string) perfmetrics.KKAIGroupBucketResult {
		queries++
		return perfmetrics.KKAIGroupBucketResult{
			RedisAvailable: queries > 1,
			Buckets: []perfmetrics.KKAIGroupBucket{
				{Group: "a", BucketTs: now.Add(-time.Minute).Unix(), CacheSampleCount: 1, CacheHitCount: 1},
			},
		}
	}
	status := func() KKAIGroupStatusEntry {
		result, err := GetKKAIGroupStatuses(KKAIGroupStatusRequest{Window: "now", UsableGroups: map[string]string{"a": "A"}})
		require.NoError(t, err)
		require.Len(t, result.Groups, 1)
		return result.Groups[0]
	}
	assert.Nil(t, status().CacheStats)
	require.NotNil(t, status().CacheStats)
	assert.Equal(t, 2, queries)
	queryKKAIGroupMinuteBuckets = func(int64, int64, []string) perfmetrics.KKAIGroupBucketResult {
		return perfmetrics.KKAIGroupBucketResult{RedisAvailable: false}
	}
	assert.Nil(t, status().CacheStats)
	assert.Equal(t, 2, queries)
}

func TestKKAIGroupCacheSnapshotCoalescesConcurrentReads(t *testing.T) {
	now := time.Date(2026, time.August, 25, 10, 37, 42, 0, time.UTC)
	withKKAIGroupStatusSources(t, now, nil,
		perfmetrics.KKAIGroupBucketResult{},
		perfmetrics.KKAIGroupBucketResult{},
		perfmetrics.KKAIGroupSignalResult{},
	)
	var queries atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	queryKKAIGroupHistoricalBuckets = func(int64, int64, []string) perfmetrics.KKAIGroupBucketResult {
		if queries.Add(1) == 1 {
			close(entered)
		}
		<-release
		return perfmetrics.KKAIGroupBucketResult{RedisAvailable: true}
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			queryKKAIGroupCacheBuckets(now, []string{"a"})
		}()
	}
	<-entered
	close(release)
	wg.Wait()
	assert.Equal(t, int32(1), queries.Load())
}

func TestKKAIGroupStatusUsesMergedRealtimeBucketsAndActualSampleTime(t *testing.T) {
	now := time.Unix(1_784_020_200, 0)
	withKKAIGroupStatusSources(
		t,
		now,
		nil,
		perfmetrics.KKAIGroupBucketResult{
			Source:                 perfmetrics.KKAIGroupDataSourceRedis,
			RedisAvailable:         true,
			CacheTrackingStartedAt: now.Add(-10 * time.Minute).Unix(),
			Buckets: []perfmetrics.KKAIGroupBucket{
				{Group: "default", BucketTs: now.Add(-2 * time.Minute).Unix(), RequestCount: 12, SuccessCount: 12, TotalLatencyMs: 12_000, TtftSumMs: 6_000, TtftCount: 12, LastSampleAt: now.Add(-70 * time.Second).Unix()},
				{Group: "default", BucketTs: now.Add(-time.Minute).Unix(), RequestCount: 8, SuccessCount: 8, TotalLatencyMs: 4_000, TtftSumMs: 2_000, TtftCount: 8, LastSampleAt: now.Add(-20 * time.Second).Unix()},
			},
		},
		perfmetrics.KKAIGroupBucketResult{},
		perfmetrics.KKAIGroupSignalResult{
			Source:         perfmetrics.KKAIGroupDataSourceRedis,
			RedisAvailable: true,
			Events: []perfmetrics.KKAIGroupSignalEvent{
				{Group: "default", Ts: now.Add(-20 * time.Second).Unix(), Success: true, LatencyMs: 500, TtftMs: 250},
			},
		},
	)

	result, err := GetKKAIGroupStatuses(KKAIGroupStatusRequest{
		UsableGroups: map[string]string{"default": "Default"},
		Window:       "now",
	})
	require.NoError(t, err)
	require.Len(t, result.Groups, 1)
	entry := result.Groups[0]
	assert.Equal(t, int64(20), entry.RequestCount)
	assert.Equal(t, 100.0, entry.SuccessRate)
	assert.Equal(t, now.Add(-20*time.Second).Unix(), entry.UpdatedAt)
	assert.Equal(t, now.Add(-20*time.Second).Unix(), entry.SampledAt)
	assert.False(t, entry.Stale)
	assert.Equal(t, KKAIGroupConfidenceExcellent, entry.ConfidenceStatus)
	assert.Equal(t, perfmetrics.KKAIGroupDataSourceRedis, entry.DataSource)
	assert.True(t, result.RedisAvailable)
	require.Len(t, entry.RecentEvents, 1)
}

func TestKKAIGroupStatusCacheStatsUse24HoursAndOnlyShowPositiveHits(t *testing.T) {
	now := time.Date(2026, time.August, 25, 10, 37, 42, 0, time.UTC)
	for _, window := range []string{"now", "15m", "1h", "6h", "24h"} {
		t.Run(window, func(t *testing.T) {
			withKKAIGroupStatusSources(t, now, nil,
				perfmetrics.KKAIGroupBucketResult{RedisAvailable: true, Buckets: []perfmetrics.KKAIGroupBucket{
					{Group: "default", BucketTs: now.Add(-time.Minute).Unix(), RequestCount: 2, SuccessCount: 2, CacheSampleCount: 2, CacheHitCount: 2},
				}},
				perfmetrics.KKAIGroupBucketResult{},
				perfmetrics.KKAIGroupSignalResult{RedisAvailable: true},
			)
			var cacheWindowRequested bool
			queryKKAIGroupHistoricalBuckets = func(startTs, endTs int64, groups []string) perfmetrics.KKAIGroupBucketResult {
				assert.Equal(t, now.Unix(), endTs)
				assert.ElementsMatch(t, []string{"default", "vip", "no-cache", "unused"}, groups)
				if startTs != now.Add(-24*time.Hour).Unix() {
					assert.Equal(t, now.Add(-6*time.Hour).Unix(), startTs)
					return perfmetrics.KKAIGroupBucketResult{RedisAvailable: true}
				}
				cacheWindowRequested = true
				return perfmetrics.KKAIGroupBucketResult{
					RedisAvailable: true,
					// A newly started v4 tracker can display its observed samples.
					CacheTrackingStartedAt: now.Add(-time.Hour).Unix(),
					Buckets: []perfmetrics.KKAIGroupBucket{
						{Group: "default", BucketTs: now.Add(-24*time.Hour - 42*time.Second).Unix(), RequestCount: 100, CacheSampleCount: 100, CacheHitCount: 100},
						{Group: "default", BucketTs: now.Add(-time.Hour).Unix(), RequestCount: 200, CacheTrackedCount: 10, CacheSampleCount: 10, CacheHitCount: 9, CachePromptTokens: 1000, CacheReadTokens: 320},
						{Group: "default", BucketTs: now.Add(-30 * time.Minute).Unix(), RequestCount: 10, CacheSampleCount: 10, CacheHitCount: 1, CachePromptTokens: 10000, CacheReadTokens: 1000},
						{Group: "default", BucketTs: now.Add(time.Minute).Unix(), CacheSampleCount: 100, CacheHitCount: 100},
						{Group: "vip", BucketTs: now.Add(-time.Hour).Unix(), CacheSampleCount: 4, CacheHitCount: 3},
						{Group: "no-cache", BucketTs: now.Add(-time.Hour).Unix(), CacheSampleCount: 4},
					},
				}
			}
			result, err := GetKKAIGroupStatuses(KKAIGroupStatusRequest{
				UsableGroups: map[string]string{"default": "Default", "vip": "VIP", "no-cache": "No cache", "unused": "Unused"},
				Window:       window,
			})
			require.NoError(t, err)
			require.True(t, cacheWindowRequested)
			entries := make(map[string]KKAIGroupStatusEntry, len(result.Groups))
			for _, entry := range result.Groups {
				entries[entry.Group] = entry
			}
			require.NotNil(t, entries["default"].CacheStats)
			assert.Equal(t, int64(20), entries["default"].CacheStats.SampleCount)
			require.NotNil(t, entries["default"].CacheStats.RequestHitRate)
			assert.Equal(t, 50.0, *entries["default"].CacheStats.RequestHitRate)
			require.NotNil(t, entries["vip"].CacheStats)
			require.NotNil(t, entries["vip"].CacheStats.RequestHitRate)
			assert.Equal(t, 75.0, *entries["vip"].CacheStats.RequestHitRate)
			assert.Nil(t, entries["no-cache"].CacheStats)
			assert.Nil(t, entries["unused"].CacheStats)
		})
	}
}

func TestBuildKKAIGroupCacheStatsHidesMissingOrUnavailableSamples(t *testing.T) {
	tests := []struct {
		name           string
		metrics        kkaiGroupMetrics
		redisAvailable bool
	}{
		{name: "zero hits", metrics: kkaiGroupMetrics{cacheSampleCount: 3}, redisAvailable: true},
		{name: "no samples", redisAvailable: true},
		{name: "legacy buckets", metrics: kkaiGroupMetrics{requestCount: 10}, redisAvailable: true},
		{name: "redis outage", metrics: kkaiGroupMetrics{cacheSampleCount: 2, cacheHitCount: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Nil(t, buildKKAIGroupCacheStats(test.metrics, test.redisAvailable))
		})
	}
}

func TestKKAIGroupStatusOneHourWindowUsesMinuteBucketsAcrossHourBoundary(t *testing.T) {
	now := time.Date(2026, time.August, 25, 10, 37, 42, 0, time.UTC)
	withKKAIGroupStatusSources(
		t,
		now,
		[]model.KKAIPerfMetricBucket{{Group: "default", BucketTs: now.Add(-time.Hour).Unix(), RequestCount: 1_000}},
		perfmetrics.KKAIGroupBucketResult{},
		perfmetrics.KKAIGroupBucketResult{
			Buckets: []perfmetrics.KKAIGroupBucket{{Group: "default", BucketTs: now.Add(-time.Hour).Unix(), RequestCount: 2_000}},
		},
		perfmetrics.KKAIGroupSignalResult{RedisAvailable: true},
	)
	queryKKAIGroupMinuteBuckets = func(startTs int64, endTs int64, groups []string) perfmetrics.KKAIGroupBucketResult {
		assert.Equal(t, now.Add(-time.Hour).Unix(), startTs)
		assert.Equal(t, now.Unix(), endTs)
		assert.Equal(t, []string{"default"}, groups)
		return perfmetrics.KKAIGroupBucketResult{
			Source:                 perfmetrics.KKAIGroupDataSourceRedis,
			RedisAvailable:         true,
			CacheTrackingStartedAt: now.Add(-2 * time.Hour).Unix(),
			Buckets: []perfmetrics.KKAIGroupBucket{
				{Group: "default", BucketTs: now.Add(-52 * time.Minute).Unix(), RequestCount: 5, SuccessCount: 5, CacheTrackedCount: 5, LastSampleAt: now.Add(-50 * time.Minute).Unix()},
				{Group: "default", BucketTs: now.Add(-7 * time.Minute).Unix(), RequestCount: 7, SuccessCount: 7, CacheTrackedCount: 7, LastSampleAt: now.Add(-6 * time.Minute).Unix()},
			},
		}
	}

	result, err := GetKKAIGroupStatuses(KKAIGroupStatusRequest{
		UsableGroups: map[string]string{"default": "Default"},
		Window:       "1h",
	})
	require.NoError(t, err)
	require.Len(t, result.Groups, 1)
	assert.Equal(t, int64(12), result.Groups[0].RequestCount)
	assert.Equal(t, perfmetrics.KKAIGroupDataSourceRedis, result.DataSource)
}

func TestKKAIGroupStatusMarksOldSuccessfulDataStale(t *testing.T) {
	now := time.Unix(1_784_020_200, 0)
	withKKAIGroupStatusSources(
		t,
		now,
		nil,
		perfmetrics.KKAIGroupBucketResult{
			Source:         perfmetrics.KKAIGroupDataSourceRedis,
			RedisAvailable: true,
			Buckets: []perfmetrics.KKAIGroupBucket{
				{Group: "default", BucketTs: now.Add(-30 * time.Minute).Unix(), RequestCount: 200, SuccessCount: 200, TotalLatencyMs: 200_000, TtftSumMs: 100_000, TtftCount: 200, LastSampleAt: now.Add(-30 * time.Minute).Unix()},
			},
		},
		perfmetrics.KKAIGroupBucketResult{},
		perfmetrics.KKAIGroupSignalResult{
			Source:         perfmetrics.KKAIGroupDataSourceRedis,
			RedisAvailable: true,
			Events: []perfmetrics.KKAIGroupSignalEvent{
				{Group: "default", Ts: now.Add(-2 * time.Hour).Unix(), Success: true},
			},
		},
	)

	result, err := GetKKAIGroupStatuses(KKAIGroupStatusRequest{
		UsableGroups: map[string]string{"default": "Default"},
		Window:       "1h",
	})
	require.NoError(t, err)
	require.Len(t, result.Groups, 1)
	entry := result.Groups[0]
	assert.True(t, entry.Stale)
	assert.Equal(t, KKAIGroupHealthUnknown, entry.Status)
	assert.Equal(t, KKAIGroupConfidenceUnknown, entry.ConfidenceStatus)
	assert.Equal(t, kkaiGroupStatusMessageStale, entry.DisplayMessage)
	require.Len(t, entry.RecentEvents, 1)
	assert.Equal(t, now.Add(-2*time.Hour).Unix(), entry.RecentEvents[0].Ts)
}

func TestKKAIGroupStatusHistoricalSignalsDoNotCreateCurrentHealth(t *testing.T) {
	now := time.Unix(1_784_020_200, 0)
	withKKAIGroupStatusSources(
		t,
		now,
		nil,
		perfmetrics.KKAIGroupBucketResult{Source: perfmetrics.KKAIGroupDataSourceNone, RedisAvailable: true},
		perfmetrics.KKAIGroupBucketResult{},
		perfmetrics.KKAIGroupSignalResult{
			Source:         perfmetrics.KKAIGroupDataSourceRedis,
			RedisAvailable: true,
			Events: []perfmetrics.KKAIGroupSignalEvent{
				{Group: "default", Ts: now.Add(-2 * time.Hour).Unix(), Success: true},
			},
		},
	)

	result, err := GetKKAIGroupStatuses(KKAIGroupStatusRequest{
		UsableGroups: map[string]string{"default": "Default"},
		Window:       "now",
	})
	require.NoError(t, err)
	require.Len(t, result.Groups, 1)
	entry := result.Groups[0]
	assert.Equal(t, KKAIGroupHealthUnknown, entry.Status)
	assert.Equal(t, KKAIGroupConfidenceUnknown, entry.ConfidenceStatus)
	assert.Equal(t, int64(0), entry.SampledAt)
	require.Len(t, entry.RecentEvents, 1)
	assert.Equal(t, now.Add(-2*time.Hour).Unix(), entry.RecentEvents[0].Ts)
}

func TestKKAIGroupStatusUnusedGroupReturnsEmptyRecentEvents(t *testing.T) {
	now := time.Unix(1_784_020_200, 0)
	withKKAIGroupStatusSources(
		t,
		now,
		nil,
		perfmetrics.KKAIGroupBucketResult{Source: perfmetrics.KKAIGroupDataSourceNone, RedisAvailable: true},
		perfmetrics.KKAIGroupBucketResult{},
		perfmetrics.KKAIGroupSignalResult{Source: perfmetrics.KKAIGroupDataSourceNone, RedisAvailable: true},
	)

	result, err := GetKKAIGroupStatuses(KKAIGroupStatusRequest{
		UsableGroups: map[string]string{"unused": "Unused"},
		Window:       "now",
	})
	require.NoError(t, err)
	require.Len(t, result.Groups, 1)
	assert.NotNil(t, result.Groups[0].RecentEvents)
	assert.Empty(t, result.Groups[0].RecentEvents)
}

func TestKKAIGroupStatusFallsBackToLocalSignalsWhenRedisFails(t *testing.T) {
	now := time.Unix(1_784_020_200, 0)
	withKKAIGroupStatusSources(
		t,
		now,
		nil,
		perfmetrics.KKAIGroupBucketResult{
			Source:         perfmetrics.KKAIGroupDataSourceLocal,
			RedisAvailable: false,
			Buckets: []perfmetrics.KKAIGroupBucket{
				{Group: "vip", BucketTs: now.Add(-time.Minute).Unix(), RequestCount: 8, SuccessCount: 8, TotalLatencyMs: 4_000, TtftSumMs: 2_000, TtftCount: 8, LastSampleAt: now.Add(-10 * time.Second).Unix()},
			},
		},
		perfmetrics.KKAIGroupBucketResult{},
		perfmetrics.KKAIGroupSignalResult{Source: perfmetrics.KKAIGroupDataSourceLocal, RedisAvailable: false},
	)

	result, err := GetKKAIGroupStatuses(KKAIGroupStatusRequest{
		UsableGroups: map[string]string{"vip": "VIP"},
		Window:       "now",
	})
	require.NoError(t, err)
	require.Len(t, result.Groups, 1)
	assert.False(t, result.RedisAvailable)
	assert.Equal(t, perfmetrics.KKAIGroupDataSourceLocal, result.DataSource)
	assert.Equal(t, KKAIGroupHealthOperational, result.Groups[0].Status)
	assert.Equal(t, perfmetrics.KKAIGroupDataSourceLocal, result.Groups[0].DataSource)
}

func TestKKAIGroupStatusAggregatesConfiguredAutoGroups(t *testing.T) {
	now := time.Unix(1_784_020_200, 0)
	base := now.Add(-80 * time.Second)
	signalEvents := make([]perfmetrics.KKAIGroupSignalEvent, 0, 80)
	for index := 39; index >= 0; index-- {
		signalEvents = append(signalEvents,
			perfmetrics.KKAIGroupSignalEvent{
				Group: "default", Ts: base.Unix(), Success: true, LatencyMs: int64(index * 2),
				EventID: "default-" + strconv.Itoa(index), ObservedAtNs: base.Add(time.Duration(index*2) * time.Nanosecond).UnixNano(),
			},
			perfmetrics.KKAIGroupSignalEvent{
				Group: "vip", Ts: base.Unix(), Success: true, LatencyMs: int64(index*2 + 1),
				EventID: "vip-" + strconv.Itoa(index), ObservedAtNs: base.Add(time.Duration(index*2+1) * time.Nanosecond).UnixNano(),
			},
		)
	}
	withKKAIGroupStatusSources(
		t,
		now,
		nil,
		perfmetrics.KKAIGroupBucketResult{
			Source:         perfmetrics.KKAIGroupDataSourceRedis,
			RedisAvailable: true,
			Buckets: []perfmetrics.KKAIGroupBucket{
				{Group: "default", BucketTs: now.Unix(), RequestCount: 10, SuccessCount: 10, LastSampleAt: now.Add(-5 * time.Second).Unix()},
				{Group: "vip", BucketTs: now.Unix(), RequestCount: 6, SuccessCount: 5, LastSampleAt: now.Add(-3 * time.Second).Unix()},
			},
		},
		perfmetrics.KKAIGroupBucketResult{RedisAvailable: true, Buckets: []perfmetrics.KKAIGroupBucket{
			{Group: "default", BucketTs: now.Add(-time.Hour).Unix(), CacheSampleCount: 6, CacheHitCount: 6},
			{Group: "vip", BucketTs: now.Add(-time.Hour).Unix(), CacheSampleCount: 4, CacheHitCount: 2},
		}},
		perfmetrics.KKAIGroupSignalResult{Events: signalEvents},
	)

	result, err := GetKKAIGroupStatuses(KKAIGroupStatusRequest{
		UsableGroups: map[string]string{"default": "Default", "vip": "VIP", "auto": "Auto"},
		AutoGroups:   []string{"default", "vip"},
		Window:       "now",
	})
	require.NoError(t, err)

	entries := make(map[string]KKAIGroupStatusEntry, len(result.Groups))
	for _, entry := range result.Groups {
		entries[entry.Group] = entry
	}
	require.Contains(t, entries, "auto")
	assert.Nil(t, entries["auto"].Ratio)
	require.NotNil(t, entries["auto"].CacheStats)
	require.NotNil(t, entries["auto"].CacheStats.RequestHitRate)
	assert.Equal(t, 80.0, *entries["auto"].CacheStats.RequestHitRate)
	assert.Equal(t, int64(16), entries["auto"].RequestCount)
	assert.Equal(t, 93.75, entries["auto"].SuccessRate)
	assert.Equal(t, now.Add(-3*time.Second).Unix(), entries["auto"].SampledAt)
	require.Len(t, entries["default"].RecentEvents, 40)
	require.Len(t, entries["vip"].RecentEvents, 40)
	require.Len(t, entries["auto"].RecentEvents, kkaiGroupRecentEventLimit)
	assert.Equal(t, int64(20), entries["auto"].RecentEvents[0].LatencyMs)
	assert.Equal(t, int64(79), entries["auto"].RecentEvents[kkaiGroupRecentEventLimit-1].LatencyMs)
}

func TestMergeKKAIDatabaseAndLiveBucketsKeepsMostCompleteHourlyAggregate(t *testing.T) {
	hour := time.Unix(1_784_016_000, 0)
	databaseBuckets := []model.KKAIPerfMetricBucket{
		{Group: "default", BucketTs: hour.Unix(), RequestCount: 100, SuccessCount: 99, TotalLatencyMs: 100_000},
		{Group: "vip", BucketTs: hour.Unix(), RequestCount: 20, SuccessCount: 18, TotalLatencyMs: 30_000},
	}
	liveBuckets := []perfmetrics.KKAIGroupBucket{
		{Group: "default", BucketTs: hour.Add(5 * time.Minute).Unix(), RequestCount: 15, SuccessCount: 15, TotalLatencyMs: 7_500, CacheTrackedCount: 15, CacheSampleCount: 15, CacheHitCount: 14, CachePromptTokens: 1_500, CacheReadTokens: 1_350, LastSampleAt: hour.Add(9 * time.Minute).Unix()},
		{Group: "default", BucketTs: hour.Add(35 * time.Minute).Unix(), RequestCount: 25, SuccessCount: 25, TotalLatencyMs: 12_500, CacheTrackedCount: 25, CacheSampleCount: 25, CacheHitCount: 23, CachePromptTokens: 2_500, CacheReadTokens: 2_250, LastSampleAt: hour.Add(45 * time.Minute).Unix()},
		{Group: "vip", BucketTs: hour.Unix(), RequestCount: 30, SuccessCount: 30, TotalLatencyMs: 15_000, LastSampleAt: hour.Add(50 * time.Minute).Unix()},
	}

	metrics := mergeKKAIDatabaseAndLiveBuckets(databaseBuckets, liveBuckets)

	assert.Equal(t, int64(100), metrics["default"].requestCount)
	assert.Equal(t, int64(99), metrics["default"].successCount)
	assert.Equal(t, hour.Add(45*time.Minute).Unix(), metrics["default"].sampledAt)
	assert.Equal(t, int64(40), metrics["default"].cacheTrackedCount)
	assert.Equal(t, int64(40), metrics["default"].cacheSampleCount)
	assert.Equal(t, int64(37), metrics["default"].cacheHitCount)
	assert.Equal(t, int64(4_000), metrics["default"].cachePromptTokens)
	assert.Equal(t, int64(3_600), metrics["default"].cacheReadTokens)
	assert.Equal(t, int64(30), metrics["vip"].requestCount)
	assert.Equal(t, int64(30), metrics["vip"].successCount)
	assert.Equal(t, hour.Add(50*time.Minute).Unix(), metrics["vip"].sampledAt)
}
