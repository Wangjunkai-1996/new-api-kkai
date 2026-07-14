package perfmetrics

import (
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

	signals := QueryKKAIGroupSignals(5, []string{"default"})
	require.Len(t, signals.Events, 2)
	assert.Equal(t, earlier.Unix(), signals.Events[0].Ts)
	assert.Equal(t, later.Unix(), signals.Events[1].Ts)
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
		"ts":      "789",
		"success": "1",
		"latency": "600",
		"ttft":    "200",
	})
	require.True(t, ok)
	assert.Equal(t, KKAIGroupSignalEvent{Group: "vip", Ts: 789, Success: true, LatencyMs: 600, TtftMs: 200}, event)
}

func TestCleanupKKAIGroupSignalsReleasesExpiredEvents(t *testing.T) {
	resetKKAIGroupSignalState(t)
	now := time.Now().Truncate(time.Second)
	recordKKAILocalGroupSignal(Sample{Group: "retired", Success: true}, now.Add(-time.Hour))

	cleanupKKAILocalGroupBuckets(now)

	result := localKKAIGroupSignals(now.Add(-2*time.Hour).Unix(), []string{"retired"})
	assert.Empty(t, result)
	raw, ok := kkaiGroupSignals.Load("retired")
	require.True(t, ok)
	buffer := raw.(*kkaiGroupSignalBuffer)
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	assert.Nil(t, buffer.events)
}
