package perfmetrics

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetKKAIGroupSignalState(t *testing.T) {
	t.Helper()
	originalRedisEnabled := common.RedisEnabled
	originalRedisClient := common.RDB
	metricsSetting, ok := config.GlobalConfig.Get("perf_metrics_setting").(*perf_metrics_setting.PerfMetricsSetting)
	require.True(t, ok)
	originalMetricsSetting := *metricsSetting
	metricsSetting.Enabled = true
	clearState := func() {
		kkaiGroupBuckets.Range(func(key any, _ any) bool {
			kkaiGroupBuckets.Delete(key)
			return true
		})
		kkaiGroupSignals.Range(func(key any, _ any) bool {
			kkaiGroupSignals.Delete(key)
			return true
		})
		kkaiGroupLastCleanupAt.Store(0)
		kkaiGroupCacheGapEpoch.Store(0)
		kkaiGroupCacheGapSeq.Store(0)
	}
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedisClient
		*metricsSetting = originalMetricsSetting
		clearState()
	})
	common.RedisEnabled = false
	common.RDB = nil
	clearState()
}

func TestKKAIGroupCacheTrackingOldRecoveryCannotClearNewGap(t *testing.T) {
	resetKKAIGroupSignalState(t)

	markKKAIGroupCacheGap()
	oldGap := kkaiGroupCacheGapEpoch.Load()
	completeKKAIRedisGroupSignalWrite(oldGap, nil)
	assert.Zero(t, kkaiGroupCacheGapEpoch.Load())

	markKKAIGroupCacheGap()
	newGap := kkaiGroupCacheGapEpoch.Load()
	require.NotEqual(t, oldGap, newGap)
	completeKKAIRedisGroupSignalWrite(oldGap, nil)

	assert.Equal(t, newGap, kkaiGroupCacheGapEpoch.Load())
}

func TestKKAIGroupCacheTrackingIgnoresPreviousGenerationMarkers(t *testing.T) {
	resetKKAIGroupSignalState(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	common.RedisEnabled = true
	common.RDB = client

	for _, key := range []string{
		"kkai:group-status:cache-v1:started_at",
		"kkai:group-status:cache-v2:started_at",
	} {
		require.NoError(t, client.Set(
			context.Background(), key, time.Now().Add(-time.Hour).Unix(), 0,
		).Err())
	}

	result := QueryKKAIGroupMinuteBuckets(
		time.Now().Add(-time.Minute).Unix(), time.Now().Unix(), []string{"default"},
	)

	assert.True(t, result.RedisAvailable)
	assert.Zero(t, result.CacheTrackingStartedAt)
}

func TestKKAIGroupCacheTrackingResetsAfterCollectionIsReenabled(t *testing.T) {
	resetKKAIGroupSignalState(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	common.RedisEnabled = true
	common.RDB = client
	metricsSetting := config.GlobalConfig.Get("perf_metrics_setting").(*perf_metrics_setting.PerfMetricsSetting)
	info := &relaycommon.RelayInfo{
		OriginModelName: "cache-test-model",
		UsingGroup:      "collection-transition",
		StartTime:       time.Now().Add(-time.Second),
		IsStream:        true,
	}

	initialMarker := time.Now().Add(-time.Hour).Unix()
	require.NoError(t, client.Set(context.Background(), kkaiGroupCacheTrackingMarkerKey, initialMarker, 0).Err())

	metricsSetting.Enabled = false
	RecordRelaySample(info, true, 0, &CacheUsage{PromptTokens: 100, CachedTokens: 90})
	assert.NotZero(t, kkaiGroupCacheGapEpoch.Load())
	result := QueryKKAIGroupMinuteBuckets(time.Now().Add(-time.Minute).Unix(), time.Now().Unix(), []string{info.UsingGroup})
	assert.True(t, result.RedisAvailable)
	assert.Zero(t, result.CacheTrackingStartedAt)

	metricsSetting.Enabled = true
	RecordRelaySample(info, true, 0, &CacheUsage{PromptTokens: 100, CachedTokens: 90})
	assert.Zero(t, kkaiGroupCacheGapEpoch.Load())
	recoveredMarker, err := client.Get(context.Background(), kkaiGroupCacheTrackingMarkerKey).Int64()
	require.NoError(t, err)
	assert.Greater(t, recoveredMarker, initialMarker)
}

func TestKKAIGroupCacheTrackingResetsAfterRedisIsReenabled(t *testing.T) {
	resetKKAIGroupSignalState(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	common.RedisEnabled = true
	common.RDB = client
	info := &relaycommon.RelayInfo{
		OriginModelName: "cache-test-model",
		UsingGroup:      "redis-transition",
		StartTime:       time.Now().Add(-time.Second),
		IsStream:        true,
	}

	initialMarker := time.Now().Add(-time.Hour).Unix()
	require.NoError(t, client.Set(context.Background(), kkaiGroupCacheTrackingMarkerKey, initialMarker, 0).Err())

	common.RedisEnabled = false
	RecordRelaySample(info, true, 0, &CacheUsage{PromptTokens: 100, CachedTokens: 90})
	assert.NotZero(t, kkaiGroupCacheGapEpoch.Load())
	result := QueryKKAIGroupMinuteBuckets(time.Now().Add(-time.Minute).Unix(), time.Now().Unix(), []string{info.UsingGroup})
	assert.False(t, result.RedisAvailable)
	assert.Zero(t, result.CacheTrackingStartedAt)

	common.RedisEnabled = true
	RecordRelaySample(info, true, 0, &CacheUsage{PromptTokens: 100, CachedTokens: 90})
	assert.Zero(t, kkaiGroupCacheGapEpoch.Load())
	recoveredMarker, err := client.Get(context.Background(), kkaiGroupCacheTrackingMarkerKey).Int64()
	require.NoError(t, err)
	assert.Greater(t, recoveredMarker, initialMarker)
}

func TestKKAIGroupLocalSignalsAggregateAndKeepActualTimestamp(t *testing.T) {
	resetKKAIGroupSignalState(t)
	now := time.Now().Add(-time.Minute).Truncate(time.Minute).Add(45 * time.Second)
	later := now.Add(-5 * time.Second)
	earlier := now.Add(-20 * time.Second)

	recordKKAILocalGroupSignal(Sample{
		Group:     "default",
		LatencyMs: 700,
		TtftMs:    300,
		HasTtft:   true,
		Success:   true,
	}, later)
	recordKKAILocalGroupSignal(Sample{
		Group:     "default",
		LatencyMs: 1300,
		TtftMs:    500,
		HasTtft:   true,
		Success:   false,
	}, earlier)

	result := QueryKKAIGroupMinuteBuckets(now.Add(-time.Minute).Unix(), now.Unix(), []string{"default"})
	require.Len(t, result.Buckets, 1)
	bucket := result.Buckets[0]
	assert.Equal(t, KKAIGroupDataSourceLocal, result.Source)
	assert.False(t, result.RedisAvailable)
	assert.Equal(t, int64(2), bucket.RequestCount)
	assert.Equal(t, int64(1), bucket.SuccessCount)
	assert.Equal(t, int64(2000), bucket.TotalLatencyMs)
	assert.Equal(t, int64(800), bucket.TtftSumMs)
	assert.Equal(t, int64(2), bucket.TtftCount)
	assert.Equal(t, later.Unix(), bucket.LastSampleAt)

	signals := QueryKKAIGroupRecentSignals([]string{"default"}, KKAIGroupRecentSignalLimit)
	require.Len(t, signals.Events, 2)
	assert.Equal(t, earlier.Unix(), signals.Events[0].Ts)
	assert.Equal(t, later.Unix(), signals.Events[1].Ts)
}

func TestKKAIGroupLocalCacheUsageCountsRequestHits(t *testing.T) {
	resetKKAIGroupSignalState(t)
	group := "cache-weighted-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	info := &relaycommon.RelayInfo{
		OriginModelName: "cache-test-model",
		UsingGroup:      group,
		StartTime:       time.Now().Add(-time.Second),
		IsStream:        true,
	}

	RecordRelaySample(info, true, 0, &CacheUsage{PromptTokens: 100, CachedTokens: 100})
	RecordRelaySample(info, true, 0, &CacheUsage{PromptTokens: 900, CachedTokens: 0})

	buckets := QueryKKAIGroupMinuteBuckets(time.Now().Add(-2*time.Minute).Unix(), time.Now().Add(time.Second).Unix(), []string{group})
	var tracked, samples, hits, promptTokens, cachedTokens int64
	for _, bucket := range buckets.Buckets {
		tracked += bucket.CacheTrackedCount
		samples += bucket.CacheSampleCount
		hits += bucket.CacheHitCount
		promptTokens += bucket.CachePromptTokens
		cachedTokens += bucket.CacheReadTokens
	}
	assert.Equal(t, int64(2), tracked)
	assert.Equal(t, int64(2), samples)
	assert.Equal(t, int64(1), hits)
	assert.Equal(t, int64(1000), promptTokens)
	assert.Equal(t, int64(100), cachedTokens)
	assert.InDelta(t, 50.0, float64(hits)/float64(samples)*100, 0.001)
}

func TestKKAIGroupLocalCacheUsageDistinguishesZeroHitFromIneligibleSamples(t *testing.T) {
	resetKKAIGroupSignalState(t)
	group := "cache-eligibility-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	info := &relaycommon.RelayInfo{
		OriginModelName: "cache-test-model",
		UsingGroup:      group,
		StartTime:       time.Now().Add(-time.Second),
		IsStream:        true,
	}

	RecordRelaySample(info, true, 0, &CacheUsage{PromptTokens: 100, CachedTokens: 0})
	RecordRelaySample(info, true, 0, nil)
	RecordRelaySample(info, true, 0, &CacheUsage{PromptTokens: 0, CachedTokens: 0})
	RecordRelaySample(info, true, 0, &CacheUsage{PromptTokens: 100, CachedTokens: -1})
	RecordRelaySample(info, true, 0, &CacheUsage{PromptTokens: 100, CachedTokens: 101})
	RecordRelaySample(info, false, 0, &CacheUsage{PromptTokens: 100, CachedTokens: 80})

	buckets := QueryKKAIGroupMinuteBuckets(time.Now().Add(-2*time.Minute).Unix(), time.Now().Add(time.Second).Unix(), []string{group})
	var requests, successes, tracked, samples, hits, promptTokens, cachedTokens int64
	for _, bucket := range buckets.Buckets {
		requests += bucket.RequestCount
		successes += bucket.SuccessCount
		tracked += bucket.CacheTrackedCount
		samples += bucket.CacheSampleCount
		hits += bucket.CacheHitCount
		promptTokens += bucket.CachePromptTokens
		cachedTokens += bucket.CacheReadTokens
	}
	assert.Equal(t, int64(6), requests)
	assert.Equal(t, int64(5), successes)
	assert.Equal(t, int64(6), tracked)
	assert.Equal(t, int64(1), samples)
	assert.Zero(t, hits)
	assert.Equal(t, int64(100), promptTokens)
	assert.Zero(t, cachedTokens)
}

func TestKKAIGroupHistoricalBucketsUseFiveMinuteBoundaries(t *testing.T) {
	resetKKAIGroupSignalState(t)
	base := time.Unix(1_800_000_000, 0).Truncate(5 * time.Minute)
	group := "historical-boundary"

	recordKKAILocalGroupSignal(Sample{Group: group, Success: true, CacheTrackedCount: 1}, base.Add(299*time.Second))
	recordKKAILocalGroupSignal(Sample{Group: group, Success: true, CacheTrackedCount: 1}, base.Add(5*time.Minute))

	result := QueryKKAIGroupHistoricalBuckets(base.Unix(), base.Add(10*time.Minute).Unix(), []string{group})
	require.Len(t, result.Buckets, 2)
	assert.Equal(t, base.Unix(), result.Buckets[0].BucketTs)
	assert.Equal(t, base.Add(5*time.Minute).Unix(), result.Buckets[1].BucketTs)
	assert.Equal(t, int64(1), result.Buckets[0].CacheTrackedCount)
	assert.Equal(t, int64(1), result.Buckets[1].CacheTrackedCount)
}

func TestKKAIGroupLocalBucketsDoNotIncludeTheBucketBeforeStart(t *testing.T) {
	resetKKAIGroupSignalState(t)
	base := time.Unix(1_800_000_000, 0).Truncate(time.Minute)
	group := "local-start-boundary"

	recordKKAILocalGroupSignal(Sample{Group: group, Success: true}, base.Add(-time.Second))
	recordKKAILocalGroupSignal(Sample{Group: group, Success: true}, base)

	result := QueryKKAIGroupMinuteBuckets(base.Unix(), base.Add(time.Minute).Unix(), []string{group})
	require.Len(t, result.Buckets, 1)
	assert.Equal(t, base.Unix(), result.Buckets[0].BucketTs)
}

func TestKKAIGroupCacheTrackingMarkerRecoversAfterRedisGaps(t *testing.T) {
	resetKKAIGroupSignalState(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	common.RedisEnabled = true
	common.RDB = client
	group := "cache-marker"
	first := time.Unix(1_800_000_000, 0)
	sample := Sample{Model: "cache-model", Group: group, Success: true, CacheTrackedCount: 1}

	recordRedis(
		bucketKey{model: sample.Model, group: group, bucketTs: bucketStart(first.Unix())},
		sample,
		first,
		KKAIGroupSignalEvent{Group: group, Ts: first.Unix(), EventID: "first", ObservedAtNs: first.UnixNano()},
	)
	marker, err := client.Get(context.Background(), kkaiGroupCacheTrackingMarkerKey).Int64()
	require.NoError(t, err)
	assert.Equal(t, first.Unix(), marker)
	minuteKey := kkaiGroupRedisBucketKey(group, "minute", first.Unix()-first.Unix()%kkaiGroupMinuteSeconds)
	minuteTTL, err := client.TTL(context.Background(), minuteKey).Result()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, minuteTTL, 70*time.Minute)

	second := first.Add(time.Minute)
	recordRedis(
		bucketKey{model: sample.Model, group: group, bucketTs: bucketStart(second.Unix())},
		sample,
		second,
		KKAIGroupSignalEvent{Group: group, Ts: second.Unix(), EventID: "second", ObservedAtNs: second.UnixNano()},
	)
	marker, err = client.Get(context.Background(), kkaiGroupCacheTrackingMarkerKey).Int64()
	require.NoError(t, err)
	assert.Equal(t, first.Unix(), marker)
	result := QueryKKAIGroupMinuteBuckets(first.Unix(), second.Unix(), []string{group})
	assert.True(t, result.RedisAvailable)
	assert.Equal(t, first.Unix(), result.CacheTrackingStartedAt)

	server.Close()
	result = QueryKKAIGroupMinuteBuckets(first.Unix(), second.Unix(), []string{group})
	assert.False(t, result.RedisAvailable)
	assert.Zero(t, result.CacheTrackingStartedAt)
	assert.Zero(t, kkaiGroupCacheGapEpoch.Load())
	require.NoError(t, server.Restart())
	require.NoError(t, client.Ping(context.Background()).Err())

	afterReadFailure := second.Add(time.Minute)
	recordRedis(
		bucketKey{model: sample.Model, group: group, bucketTs: bucketStart(afterReadFailure.Unix())},
		sample,
		afterReadFailure,
		KKAIGroupSignalEvent{Group: group, Ts: afterReadFailure.Unix(), EventID: "after-read-failure", ObservedAtNs: afterReadFailure.UnixNano()},
	)
	assert.Zero(t, kkaiGroupCacheGapEpoch.Load())
	marker, err = client.Get(context.Background(), kkaiGroupCacheTrackingMarkerKey).Int64()
	require.NoError(t, err)
	assert.Equal(t, first.Unix(), marker)
	result = QueryKKAIGroupMinuteBuckets(first.Unix(), afterReadFailure.Unix(), []string{group})
	assert.Equal(t, first.Unix(), result.CacheTrackingStartedAt)

	server.Close()
	failedWrite := afterReadFailure.Add(time.Minute)
	recordRedis(
		bucketKey{model: sample.Model, group: group, bucketTs: bucketStart(failedWrite.Unix())},
		sample,
		failedWrite,
		KKAIGroupSignalEvent{Group: group, Ts: failedWrite.Unix(), EventID: "failed", ObservedAtNs: failedWrite.UnixNano()},
	)
	assert.NotZero(t, kkaiGroupCacheGapEpoch.Load())
	require.NoError(t, server.Restart())
	require.NoError(t, client.Ping(context.Background()).Err())

	writeRecovered := failedWrite.Add(time.Minute)
	recordRedis(
		bucketKey{model: sample.Model, group: group, bucketTs: bucketStart(writeRecovered.Unix())},
		sample,
		writeRecovered,
		KKAIGroupSignalEvent{Group: group, Ts: writeRecovered.Unix(), EventID: "write-recovered", ObservedAtNs: writeRecovered.UnixNano()},
	)
	assert.Zero(t, kkaiGroupCacheGapEpoch.Load())
	marker, err = client.Get(context.Background(), kkaiGroupCacheTrackingMarkerKey).Int64()
	require.NoError(t, err)
	assert.Equal(t, writeRecovered.Unix(), marker)
}

func TestQueryKKAIGroupRecentSignalsKeepsLatestLimitPerGroupRegardlessOfAge(t *testing.T) {
	resetKKAIGroupSignalState(t)
	base := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	for index := 0; index < 70; index++ {
		recordKKAILocalGroupSignal(Sample{Group: "default", Success: index%2 == 0}, base.Add(time.Duration(index)*time.Second))
	}
	for index := 0; index < 3; index++ {
		recordKKAILocalGroupSignal(Sample{Group: "vip", Success: true}, base.Add(time.Duration(100+index)*time.Second))
	}

	result := QueryKKAIGroupRecentSignals([]string{"default", "vip"}, KKAIGroupRecentSignalLimit)
	assert.Equal(t, KKAIGroupDataSourceLocal, result.Source)
	assert.False(t, result.RedisAvailable)

	byGroup := make(map[string][]KKAIGroupSignalEvent)
	for _, event := range result.Events {
		byGroup[event.Group] = append(byGroup[event.Group], event)
	}
	require.Len(t, byGroup["default"], KKAIGroupRecentSignalLimit)
	assert.Equal(t, base.Add(10*time.Second).Unix(), byGroup["default"][0].Ts)
	assert.Equal(t, base.Add(69*time.Second).Unix(), byGroup["default"][KKAIGroupRecentSignalLimit-1].Ts)
	require.Len(t, byGroup["vip"], 3)
	assert.Equal(t, base.Add(100*time.Second).Unix(), byGroup["vip"][0].Ts)
	assert.Less(t, byGroup["vip"][2].Ts, time.Now().Add(-time.Hour).Unix())
}

func TestQueryKKAIGroupRecentSignalsReturnsEmptyForUnusedGroup(t *testing.T) {
	resetKKAIGroupSignalState(t)

	result := QueryKKAIGroupRecentSignals([]string{"unused"}, KKAIGroupRecentSignalLimit)

	assert.Equal(t, KKAIGroupDataSourceNone, result.Source)
	assert.False(t, result.RedisAvailable)
	assert.Empty(t, result.Events)
}

func TestKKAIGroupRedisPayloadParsing(t *testing.T) {
	bucket := kkaiGroupBucketFromRedis("vip", 123, map[string]string{
		"req":                      "8",
		"ok":                       "7",
		"lat":                      "4000",
		"ttft":                     "1200",
		"ttft_n":                   "6",
		kkaiGroupCacheTrackedField: "8",
		kkaiGroupCacheSampleField:  "5",
		kkaiGroupCacheHitField:     "4",
		kkaiGroupCachePromptField:  "1000",
		kkaiGroupCacheReadField:    "900",
		"last_ts":                  "456",
	})
	assert.Equal(t, KKAIGroupBucket{
		Group:             "vip",
		BucketTs:          123,
		RequestCount:      8,
		SuccessCount:      7,
		TotalLatencyMs:    4000,
		TtftSumMs:         1200,
		TtftCount:         6,
		CacheTrackedCount: 8,
		CacheSampleCount:  5,
		CacheHitCount:     4,
		CachePromptTokens: 1000,
		CacheReadTokens:   900,
		LastSampleAt:      456,
	}, bucket)
	legacyBucket := kkaiGroupBucketFromRedis("legacy", 120, map[string]string{
		"req": "3", "ok": "3", "last_ts": "450",
	})
	assert.Equal(t, int64(3), legacyBucket.RequestCount)
	assert.Zero(t, legacyBucket.CacheTrackedCount)
	assert.Zero(t, legacyBucket.CacheSampleCount)
	assert.Zero(t, legacyBucket.CacheHitCount)
	assert.Zero(t, legacyBucket.CachePromptTokens)
	assert.Zero(t, legacyBucket.CacheReadTokens)

	wrongVersionBucket := kkaiGroupBucketFromRedis("wrong-version", 120, map[string]string{
		"req": "2", "ok": "2", "cache_tracked": "2", "cache_n": "2",
		"cache_prompt": "200", "cache_read": "200", "last_ts": "450",
	})
	assert.Equal(t, int64(2), wrongVersionBucket.RequestCount)
	assert.Zero(t, wrongVersionBucket.CacheTrackedCount)
	assert.Zero(t, wrongVersionBucket.CacheSampleCount)
	assert.Zero(t, wrongVersionBucket.CacheHitCount)
	assert.Zero(t, wrongVersionBucket.CachePromptTokens)
	assert.Zero(t, wrongVersionBucket.CacheReadTokens)

	event, ok := kkaiGroupSignalFromRedis("vip", map[string]interface{}{
		"ts":             "789",
		"success":        "1",
		"latency":        "600",
		"ttft":           "200",
		"event_id":       "event-789",
		"observed_at_ns": "789123456789",
	})
	require.True(t, ok)
	assert.Equal(t, KKAIGroupSignalEvent{
		Group: "vip", Ts: 789, Success: true, LatencyMs: 600, TtftMs: 200,
		EventID: "event-789", ObservedAtNs: 789123456789,
	}, event)

	legacy, ok := kkaiGroupSignalFromRedis("vip", map[string]interface{}{"ts": "789"})
	require.True(t, ok)
	assert.Equal(t, int64(789_000_000_000), legacy.ObservedAtNs)
}

func TestMergeKKAIGroupRecentSignalsPreservesRedisOutageHistory(t *testing.T) {
	base := time.Unix(1_784_020_200, 0)
	redisEvents := []KKAIGroupSignalEvent{
		{Group: "default", Ts: base.Add(-time.Minute).Unix(), EventID: "before-outage", ObservedAtNs: base.Add(-time.Minute).UnixNano()},
		{Group: "default", Ts: base.Unix(), EventID: "event-61", ObservedAtNs: base.Add(61 * time.Nanosecond).UnixNano()},
	}
	localEvents := make([]KKAIGroupSignalEvent, 0, KKAIGroupRecentSignalLimit)
	for index := 2; index <= 61; index++ {
		localEvents = append(localEvents, KKAIGroupSignalEvent{
			Group: "default", Ts: base.Unix(), EventID: "event-" + strconv.Itoa(index),
			ObservedAtNs: base.Add(time.Duration(index) * time.Nanosecond).UnixNano(),
		})
	}

	events, redisPresent, localPresent := mergeKKAIGroupRecentSignals(
		[]string{"default"}, redisEvents, localEvents, KKAIGroupRecentSignalLimit,
	)

	assert.True(t, redisPresent)
	assert.True(t, localPresent)
	require.Len(t, events, KKAIGroupRecentSignalLimit)
	assert.Equal(t, "event-2", events[0].EventID)
	assert.Equal(t, "event-61", events[KKAIGroupRecentSignalLimit-1].EventID)
	seen := make(map[string]struct{}, len(events))
	for _, event := range events {
		_, duplicate := seen[event.EventID]
		assert.False(t, duplicate)
		seen[event.EventID] = struct{}{}
	}
}

func TestCleanupKKAIGroupBucketsRetainsSignalHistory(t *testing.T) {
	resetKKAIGroupSignalState(t)
	now := time.Now().Truncate(time.Second)
	recordKKAILocalGroupSignal(Sample{Group: "retired", Success: true}, now.Add(-time.Hour))

	cleanupKKAILocalGroupBuckets(now)

	result := localKKAIGroupRecentSignals([]string{"retired"}, KKAIGroupRecentSignalLimit)
	require.Len(t, result, 1)
	assert.Equal(t, now.Add(-time.Hour).Unix(), result[0].Ts)
	raw, ok := kkaiGroupSignals.Load("retired")
	require.True(t, ok)
	buffer := raw.(*kkaiGroupSignalBuffer)
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	require.Len(t, buffer.events, 1)
}
