package service

import (
	"strconv"
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
	t.Cleanup(func() {
		kkaiGroupStatusNow = originalNow
		loadKKAIPerfMetricBuckets = originalLoad
		queryKKAIGroupMinuteBuckets = originalQueryMinute
		queryKKAIGroupHistoricalBuckets = originalQueryHistorical
		queryKKAIGroupRecentSignals = originalQuerySignals
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

func TestKKAIGroupStatusCacheStatsAreTokenWeightedAndLimitedToCacheGroups(t *testing.T) {
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
				{Group: "default", BucketTs: now.Add(-2 * time.Minute).Unix(), RequestCount: 1, SuccessCount: 1, CacheTrackedCount: 1, CacheSampleCount: 1, CachePromptTokens: 100, CacheReadTokens: 100, LastSampleAt: now.Add(-90 * time.Second).Unix()},
				{Group: "default", BucketTs: now.Add(-time.Minute).Unix(), RequestCount: 9, SuccessCount: 9, CacheTrackedCount: 9, CacheSampleCount: 9, CachePromptTokens: 900, CacheReadTokens: 0, LastSampleAt: now.Add(-30 * time.Second).Unix()},
				{Group: "codex-plus", BucketTs: now.Add(-time.Minute).Unix(), RequestCount: 2, SuccessCount: 2, CacheTrackedCount: 2, CacheSampleCount: 2, CachePromptTokens: 200, CacheReadTokens: 186, LastSampleAt: now.Add(-20 * time.Second).Unix()},
				{Group: "plus", BucketTs: now.Add(-time.Minute).Unix(), RequestCount: 2, SuccessCount: 2, CacheTrackedCount: 2, CacheSampleCount: 2, CachePromptTokens: 200, CacheReadTokens: 100, LastSampleAt: now.Add(-20 * time.Second).Unix()},
				{Group: "vip", BucketTs: now.Add(-time.Minute).Unix(), RequestCount: 2, SuccessCount: 2, CacheTrackedCount: 2, CacheSampleCount: 2, CachePromptTokens: 200, CacheReadTokens: 200, LastSampleAt: now.Add(-20 * time.Second).Unix()},
			},
		},
		perfmetrics.KKAIGroupBucketResult{},
		perfmetrics.KKAIGroupSignalResult{RedisAvailable: true},
	)

	result, err := GetKKAIGroupStatuses(KKAIGroupStatusRequest{
		UsableGroups: map[string]string{
			"default": "Default", "codex-plus": "Plus", "plus": "Legacy Plus", "vip": "VIP",
		},
		Window: "now",
	})
	require.NoError(t, err)

	entries := make(map[string]KKAIGroupStatusEntry, len(result.Groups))
	for _, entry := range result.Groups {
		entries[entry.Group] = entry
	}

	require.NotNil(t, entries["default"].CacheStats)
	assert.Equal(t, KKAIGroupCacheStatusOK, entries["default"].CacheStats.Status)
	assert.Equal(t, int64(10), entries["default"].CacheStats.SampleCount)
	require.NotNil(t, entries["default"].CacheStats.HitRate)
	assert.Equal(t, 10.0, *entries["default"].CacheStats.HitRate)

	for group, expectedRate := range map[string]float64{"codex-plus": 93, "plus": 50} {
		require.NotNil(t, entries[group].CacheStats)
		assert.Equal(t, KKAIGroupCacheStatusOK, entries[group].CacheStats.Status)
		require.NotNil(t, entries[group].CacheStats.HitRate)
		assert.Equal(t, expectedRate, *entries[group].CacheStats.HitRate)
	}
	assert.Nil(t, entries["vip"].CacheStats)
}

func TestBuildKKAIGroupCacheStatsDistinguishesZeroHitEmptyAndUnavailable(t *testing.T) {
	tests := []struct {
		name           string
		metrics        kkaiGroupMetrics
		redisAvailable bool
		windowCovered  bool
		wantStatus     string
		wantRate       float64
		hasRate        bool
	}{
		{name: "zero hit is valid", metrics: kkaiGroupMetrics{requestCount: 3, cacheTrackedCount: 3, cacheSampleCount: 3, cachePromptTokens: 300}, redisAvailable: true, windowCovered: true, wantStatus: KKAIGroupCacheStatusOK, hasRate: true},
		{name: "no samples with redis", redisAvailable: true, windowCovered: true, wantStatus: KKAIGroupCacheStatusEmpty},
		{name: "no samples without redis", windowCovered: true, wantStatus: KKAIGroupCacheStatusUnavailable},
		{name: "window is not fully covered", metrics: kkaiGroupMetrics{requestCount: 2, cacheTrackedCount: 2, cacheSampleCount: 2, cachePromptTokens: 200, cacheReadTokens: 100}, redisAvailable: true, wantStatus: KKAIGroupCacheStatusUnavailable},
		{name: "fully tracked requests without eligible usage are empty", metrics: kkaiGroupMetrics{requestCount: 2, cacheTrackedCount: 2}, redisAvailable: true, windowCovered: true, wantStatus: KKAIGroupCacheStatusEmpty},
		{name: "old bucket without tracking is unavailable", metrics: kkaiGroupMetrics{requestCount: 10}, redisAvailable: true, windowCovered: true, wantStatus: KKAIGroupCacheStatusUnavailable},
		{name: "partial tracking is unavailable", metrics: kkaiGroupMetrics{requestCount: 10, cacheTrackedCount: 8, cacheSampleCount: 8, cachePromptTokens: 800, cacheReadTokens: 400}, redisAvailable: true, windowCovered: true, wantStatus: KKAIGroupCacheStatusUnavailable},
		{name: "redis outage overrides local samples", metrics: kkaiGroupMetrics{requestCount: 2, cacheTrackedCount: 2, cacheSampleCount: 2, cachePromptTokens: 200, cacheReadTokens: 100}, windowCovered: true, wantStatus: KKAIGroupCacheStatusUnavailable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stats := buildKKAIGroupCacheStats(test.metrics, test.redisAvailable, test.windowCovered)
			assert.Equal(t, test.wantStatus, stats.Status)
			assert.Equal(t, test.metrics.cacheSampleCount, stats.SampleCount)
			if !test.hasRate {
				assert.Nil(t, stats.HitRate)
				return
			}
			require.NotNil(t, stats.HitRate)
			assert.Equal(t, test.wantRate, *stats.HitRate)
		})
	}
}

func TestKKAIGroupStatusCacheStatsRequireFullTrackingWindow(t *testing.T) {
	now := time.Date(2026, time.August, 25, 10, 37, 42, 0, time.UTC)
	windowStart := now.Add(-5 * time.Minute).Unix()
	windowStartBucket := windowStart - windowStart%60
	tests := []struct {
		name          string
		trackingStart int64
		bucket        perfmetrics.KKAIGroupBucket
		wantStatus    string
	}{
		{
			name:          "tracking starts inside selected window",
			trackingStart: now.Add(-2 * time.Minute).Unix(),
			bucket:        perfmetrics.KKAIGroupBucket{Group: "default", RequestCount: 1, SuccessCount: 1, CacheTrackedCount: 1, CacheSampleCount: 1, CachePromptTokens: 100, CacheReadTokens: 93},
			wantStatus:    KKAIGroupCacheStatusUnavailable,
		},
		{
			name:          "tracking starts at exact window boundary but after bucket boundary",
			trackingStart: windowStart,
			bucket:        perfmetrics.KKAIGroupBucket{Group: "default", RequestCount: 1, SuccessCount: 1, CacheTrackedCount: 1, CacheSampleCount: 1, CachePromptTokens: 100, CacheReadTokens: 93},
			wantStatus:    KKAIGroupCacheStatusUnavailable,
		},
		{
			name:          "tracking starts at selected bucket boundary",
			trackingStart: windowStartBucket,
			bucket:        perfmetrics.KKAIGroupBucket{Group: "default", RequestCount: 1, SuccessCount: 1, CacheTrackedCount: 1, CacheSampleCount: 1, CachePromptTokens: 100, CacheReadTokens: 93},
			wantStatus:    KKAIGroupCacheStatusOK,
		},
		{
			name:          "fully tracked window without eligible samples",
			trackingStart: now.Add(-10 * time.Minute).Unix(),
			bucket:        perfmetrics.KKAIGroupBucket{Group: "default", RequestCount: 1, SuccessCount: 1, CacheTrackedCount: 1},
			wantStatus:    KKAIGroupCacheStatusEmpty,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.bucket.BucketTs = now.Add(-time.Minute).Unix()
			test.bucket.LastSampleAt = now.Add(-30 * time.Second).Unix()
			withKKAIGroupStatusSources(
				t,
				now,
				nil,
				perfmetrics.KKAIGroupBucketResult{
					Source:                 perfmetrics.KKAIGroupDataSourceRedis,
					RedisAvailable:         true,
					CacheTrackingStartedAt: test.trackingStart,
					Buckets:                []perfmetrics.KKAIGroupBucket{test.bucket},
				},
				perfmetrics.KKAIGroupBucketResult{},
				perfmetrics.KKAIGroupSignalResult{RedisAvailable: true},
			)

			result, err := GetKKAIGroupStatuses(KKAIGroupStatusRequest{
				UsableGroups: map[string]string{"default": "Default"},
				Window:       "now",
			})
			require.NoError(t, err)
			require.Len(t, result.Groups, 1)
			require.NotNil(t, result.Groups[0].CacheStats)
			assert.Equal(t, test.wantStatus, result.Groups[0].CacheStats.Status)
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
		perfmetrics.KKAIGroupBucketResult{},
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
		{Group: "default", BucketTs: hour.Add(5 * time.Minute).Unix(), RequestCount: 15, SuccessCount: 15, TotalLatencyMs: 7_500, CacheTrackedCount: 15, CacheSampleCount: 15, CachePromptTokens: 1_500, CacheReadTokens: 1_350, LastSampleAt: hour.Add(9 * time.Minute).Unix()},
		{Group: "default", BucketTs: hour.Add(35 * time.Minute).Unix(), RequestCount: 25, SuccessCount: 25, TotalLatencyMs: 12_500, CacheTrackedCount: 25, CacheSampleCount: 25, CachePromptTokens: 2_500, CacheReadTokens: 2_250, LastSampleAt: hour.Add(45 * time.Minute).Unix()},
		{Group: "vip", BucketTs: hour.Unix(), RequestCount: 30, SuccessCount: 30, TotalLatencyMs: 15_000, LastSampleAt: hour.Add(50 * time.Minute).Unix()},
	}

	metrics := mergeKKAIDatabaseAndLiveBuckets(databaseBuckets, liveBuckets)

	assert.Equal(t, int64(100), metrics["default"].requestCount)
	assert.Equal(t, int64(99), metrics["default"].successCount)
	assert.Equal(t, hour.Add(45*time.Minute).Unix(), metrics["default"].sampledAt)
	assert.Equal(t, int64(40), metrics["default"].cacheTrackedCount)
	assert.Equal(t, int64(40), metrics["default"].cacheSampleCount)
	assert.Equal(t, int64(4_000), metrics["default"].cachePromptTokens)
	assert.Equal(t, int64(3_600), metrics["default"].cacheReadTokens)
	assert.Equal(t, int64(30), metrics["vip"].requestCount)
	assert.Equal(t, int64(30), metrics["vip"].successCount)
	assert.Equal(t, hour.Add(50*time.Minute).Unix(), metrics["vip"].sampledAt)
}
