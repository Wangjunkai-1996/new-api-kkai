package perfmetrics

import (
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetKKAIGroupSignalState(t *testing.T) {
	t.Helper()
	originalRedisEnabled := common.RedisEnabled
	originalRedisClient := common.RDB
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
	}
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedisClient
		clearState()
	})
	common.RedisEnabled = false
	common.RDB = nil
	clearState()
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
		"req":     "8",
		"ok":      "7",
		"lat":     "4000",
		"ttft":    "1200",
		"ttft_n":  "6",
		"last_ts": "456",
	})
	assert.Equal(t, KKAIGroupBucket{
		Group:          "vip",
		BucketTs:       123,
		RequestCount:   8,
		SuccessCount:   7,
		TotalLatencyMs: 4000,
		TtftSumMs:      1200,
		TtftCount:      6,
		LastSampleAt:   456,
	}, bucket)

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
