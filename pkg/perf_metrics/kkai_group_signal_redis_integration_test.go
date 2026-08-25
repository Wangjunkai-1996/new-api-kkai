package perfmetrics

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedis86KKAIGroupRecentSignalLifecycle(t *testing.T) {
	address := os.Getenv("KKAI_TEST_REDIS_ADDRESS")
	if address == "" {
		if os.Getenv("KKAI_TEST_REDIS_REQUIRED") == "true" {
			t.Fatal("KKAI_TEST_REDIS_ADDRESS is required for the mandatory Redis integration check")
		}
		t.Skip("Redis 8.6 integration environment is not configured")
	}

	resetKKAIGroupSignalState(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: address, DB: 14})
	common.RedisEnabled = true
	common.RDB = client
	group := "group-status-integration-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	streamKey := kkaiGroupRedisStreamKey(group)
	base := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	t.Cleanup(func() {
		bucketTs := base.Unix()
		_ = client.Del(
			context.Background(),
			streamKey,
			kkaiGroupRedisBucketKey(group, "minute", bucketTs-bucketTs%kkaiGroupMinuteSeconds),
			kkaiGroupRedisBucketKey(group, "historical-5m", bucketTs-bucketTs%kkaiGroupHistoricalSeconds),
		).Err()
		_ = client.Close()
	})
	require.NoError(t, client.Ping(ctx).Err())

	for index := 1; index <= 70; index++ {
		event := integrationKKAIGroupSignalEvent(group, base, index)
		require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{Stream: streamKey, Values: map[string]any{
			"ts": event.Ts, "success": 1, "latency": event.LatencyMs, "ttft": event.TtftMs,
			"event_id": event.EventID, "observed_at_ns": event.ObservedAtNs,
		}}).Err())
	}
	require.NoError(t, client.Expire(ctx, streamKey, 30*time.Minute).Err())

	localEvents := make([]KKAIGroupSignalEvent, 0, KKAIGroupRecentSignalLimit)
	for index := 12; index <= 71; index++ {
		localEvents = append(localEvents, integrationKKAIGroupSignalEvent(group, base, index))
	}
	kkaiGroupSignals.Store(group, &kkaiGroupSignalBuffer{events: localEvents})

	result := QueryKKAIGroupRecentSignals([]string{group, "unused"}, KKAIGroupRecentSignalLimit)
	assert.Equal(t, KKAIGroupDataSourceRedisLocal, result.Source)
	assert.True(t, result.RedisAvailable)
	require.Len(t, result.Events, KKAIGroupRecentSignalLimit)
	assert.Equal(t, "event-12", result.Events[0].EventID)
	assert.Equal(t, "event-71", result.Events[KKAIGroupRecentSignalLimit-1].EventID)
	ttl, err := client.TTL(ctx, streamKey).Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, time.Duration(0))

	require.NoError(t, maintainKKAIGroupSignalStreams(ctx))
	ttl, err = client.TTL(ctx, streamKey).Result()
	require.NoError(t, err)
	assert.Equal(t, time.Duration(-1), ttl)
	length, err := client.XLen(ctx, streamKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(KKAIGroupRecentSignalLimit), length)

	rollbackEvent := integrationKKAIGroupSignalEvent(group, base, 72)
	require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{Stream: streamKey, Values: map[string]any{
		"ts": rollbackEvent.Ts, "success": 1, "latency": rollbackEvent.LatencyMs, "ttft": rollbackEvent.TtftMs,
	}}).Err())
	require.NoError(t, client.Expire(ctx, streamKey, 30*time.Minute).Err())
	require.NoError(t, maintainKKAIGroupSignalStreams(ctx))
	ttl, err = client.TTL(ctx, streamKey).Result()
	require.NoError(t, err)
	assert.Equal(t, time.Duration(-1), ttl)
	length, err = client.XLen(ctx, streamKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(KKAIGroupRecentSignalLimit), length)

	require.NoError(t, client.Expire(ctx, streamKey, 5*time.Second).Err())
	newEvent := integrationKKAIGroupSignalEvent(group, base, 73)
	pipe := client.Pipeline()
	appendKKAIRedisGroupSignal(pipe, Sample{
		Group: group, Success: true, LatencyMs: newEvent.LatencyMs, TtftMs: newEvent.TtftMs,
		CacheTrackedCount: 1, CacheSampleCount: 1, CacheHitCount: 1,
		CachePromptTokens: 1000, CacheReadTokens: 900,
	}, time.Unix(0, newEvent.ObservedAtNs), newEvent)
	_, err = pipe.Exec(ctx)
	require.NoError(t, err)
	for _, bucket := range []struct {
		prefix     string
		resolution int64
	}{
		{prefix: "minute", resolution: kkaiGroupMinuteSeconds},
		{prefix: "historical-5m", resolution: kkaiGroupHistoricalSeconds},
	} {
		bucketTs := newEvent.Ts - newEvent.Ts%bucket.resolution
		values, getErr := client.HGetAll(ctx, kkaiGroupRedisBucketKey(group, bucket.prefix, bucketTs)).Result()
		require.NoError(t, getErr)
		assert.Equal(t, "1", values[kkaiGroupCacheTrackedField])
		assert.Equal(t, "1", values[kkaiGroupCacheSampleField])
		assert.Equal(t, "1", values[kkaiGroupCacheHitField])
		assert.Equal(t, "1000", values[kkaiGroupCachePromptField])
		assert.Equal(t, "900", values[kkaiGroupCacheReadField])
		assert.Empty(t, values["cache_tracked"])
		assert.Empty(t, values["cache_n"])
	}
	marker, markerErr := client.Get(ctx, kkaiGroupCacheTrackingMarkerKey).Int64()
	require.NoError(t, markerErr)
	assert.Positive(t, marker)
	ttl, err = client.TTL(ctx, streamKey).Result()
	require.NoError(t, err)
	assert.Equal(t, time.Duration(-1), ttl)
	length, err = client.XLen(ctx, streamKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(KKAIGroupRecentSignalLimit), length)
}

func integrationKKAIGroupSignalEvent(group string, base time.Time, index int) KKAIGroupSignalEvent {
	return KKAIGroupSignalEvent{
		Group: group, Ts: base.Unix(), Success: true, LatencyMs: int64(index), TtftMs: int64(index),
		EventID: "event-" + strconv.Itoa(index), ObservedAtNs: base.Add(time.Duration(index) * time.Nanosecond).UnixNano(),
	}
}
