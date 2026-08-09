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
	hourly perfmetrics.KKAIGroupBucketResult,
	signals perfmetrics.KKAIGroupSignalResult,
) {
	t.Helper()
	originalNow := kkaiGroupStatusNow
	originalLoad := loadKKAIPerfMetricBuckets
	originalQueryMinute := queryKKAIGroupMinuteBuckets
	originalQueryHour := queryKKAIGroupHourBuckets
	originalQuerySignals := queryKKAIGroupRecentSignals
	t.Cleanup(func() {
		kkaiGroupStatusNow = originalNow
		loadKKAIPerfMetricBuckets = originalLoad
		queryKKAIGroupMinuteBuckets = originalQueryMinute
		queryKKAIGroupHourBuckets = originalQueryHour
		queryKKAIGroupRecentSignals = originalQuerySignals
	})

	kkaiGroupStatusNow = func() time.Time { return now }
	loadKKAIPerfMetricBuckets = func(int64, int64, []string) ([]model.KKAIPerfMetricBucket, error) {
		return databaseBuckets, nil
	}
	queryKKAIGroupMinuteBuckets = func(int64, int64, []string) perfmetrics.KKAIGroupBucketResult {
		return realtime
	}
	queryKKAIGroupHourBuckets = func(int64, int64, []string) perfmetrics.KKAIGroupBucketResult {
		return hourly
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
			Source:         perfmetrics.KKAIGroupDataSourceRedis,
			RedisAvailable: true,
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

func TestKKAIGroupStatusMarksOldSuccessfulDataStale(t *testing.T) {
	now := time.Unix(1_784_020_200, 0)
	withKKAIGroupStatusSources(
		t,
		now,
		[]model.KKAIPerfMetricBucket{
			{Group: "default", BucketTs: now.Add(-30 * time.Minute).Unix(), RequestCount: 200, SuccessCount: 200, TotalLatencyMs: 200_000, TtftSumMs: 100_000, TtftCount: 200},
		},
		perfmetrics.KKAIGroupBucketResult{},
		perfmetrics.KKAIGroupBucketResult{Source: perfmetrics.KKAIGroupDataSourceNone, RedisAvailable: true},
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
		{Group: "default", BucketTs: hour.Unix(), RequestCount: 40, SuccessCount: 40, TotalLatencyMs: 20_000, LastSampleAt: hour.Add(45 * time.Minute).Unix()},
		{Group: "vip", BucketTs: hour.Unix(), RequestCount: 30, SuccessCount: 30, TotalLatencyMs: 15_000, LastSampleAt: hour.Add(50 * time.Minute).Unix()},
	}

	metrics := mergeKKAIDatabaseAndLiveBuckets(databaseBuckets, liveBuckets)

	assert.Equal(t, int64(100), metrics["default"].requestCount)
	assert.Equal(t, int64(99), metrics["default"].successCount)
	assert.Equal(t, hour.Add(45*time.Minute).Unix(), metrics["default"].sampledAt)
	assert.Equal(t, int64(30), metrics["vip"].requestCount)
	assert.Equal(t, int64(30), metrics["vip"].successCount)
	assert.Equal(t, hour.Add(50*time.Minute).Unix(), metrics["vip"].sampledAt)
}
