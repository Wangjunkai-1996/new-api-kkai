package perfmetrics

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	KKAIGroupDataSourceRedis      = "redis"
	KKAIGroupDataSourceLocal      = "local"
	KKAIGroupDataSourceRedisLocal = "redis+local"
	KKAIGroupDataSourceNone       = "none"

	kkaiGroupMinuteSeconds = int64(60)
	kkaiGroupHourSeconds   = int64(3600)
	kkaiGroupStreamMaxLen  = int64(5000)
	kkaiGroupLocalEventMax = 5000
	kkaiGroupSignalMaxAge  = 30 * time.Minute
	kkaiGroupMinuteTTL     = 30 * time.Minute
	kkaiGroupHourTTL       = 26 * time.Hour
)

type KKAIGroupBucket struct {
	Group          string `json:"group"`
	BucketTs       int64  `json:"bucket_ts"`
	RequestCount   int64  `json:"request_count"`
	SuccessCount   int64  `json:"success_count"`
	TotalLatencyMs int64  `json:"total_latency_ms"`
	TtftSumMs      int64  `json:"ttft_sum_ms"`
	TtftCount      int64  `json:"ttft_count"`
	LastSampleAt   int64  `json:"last_sample_at"`
}

type KKAIGroupBucketResult struct {
	Source         string            `json:"source"`
	RedisAvailable bool              `json:"redis_available"`
	Buckets        []KKAIGroupBucket `json:"buckets"`
}

type KKAIGroupSignalEvent struct {
	Group     string `json:"group"`
	Ts        int64  `json:"ts"`
	Success   bool   `json:"success"`
	LatencyMs int64  `json:"latency_ms"`
	TtftMs    int64  `json:"ttft_ms"`
}

type KKAIGroupSignalResult struct {
	Source         string                 `json:"source"`
	RedisAvailable bool                   `json:"redis_available"`
	Events         []KKAIGroupSignalEvent `json:"events"`
}

type kkaiGroupBucketKey struct {
	group      string
	bucketTs   int64
	resolution int64
}

type kkaiAtomicGroupBucket struct {
	requestCount   atomic.Int64
	successCount   atomic.Int64
	totalLatencyMs atomic.Int64
	ttftSumMs      atomic.Int64
	ttftCount      atomic.Int64
	lastSampleAt   atomic.Int64
}

type kkaiGroupSignalBuffer struct {
	mu     sync.Mutex
	events []KKAIGroupSignalEvent
}

var (
	kkaiGroupBuckets       sync.Map
	kkaiGroupSignals       sync.Map
	kkaiGroupLastCleanupAt atomic.Int64
)

func recordKKAILocalGroupSignal(sample Sample, observedAt time.Time) {
	for _, resolution := range []int64{kkaiGroupMinuteSeconds, kkaiGroupHourSeconds} {
		bucketTs := observedAt.Unix() - observedAt.Unix()%resolution
		key := kkaiGroupBucketKey{group: sample.Group, bucketTs: bucketTs, resolution: resolution}
		actual, _ := kkaiGroupBuckets.LoadOrStore(key, &kkaiAtomicGroupBucket{})
		actual.(*kkaiAtomicGroupBucket).add(sample, observedAt.Unix())
	}

	actual, _ := kkaiGroupSignals.LoadOrStore(sample.Group, &kkaiGroupSignalBuffer{})
	buffer := actual.(*kkaiGroupSignalBuffer)
	buffer.mu.Lock()
	event := KKAIGroupSignalEvent{Group: sample.Group, Ts: observedAt.Unix(), Success: sample.Success, LatencyMs: sample.LatencyMs, TtftMs: sample.TtftMs}
	buffer.events = append(buffer.events, event)
	if len(buffer.events) > 1 && buffer.events[len(buffer.events)-2].Ts > event.Ts {
		sort.Slice(buffer.events, func(i, j int) bool { return buffer.events[i].Ts < buffer.events[j].Ts })
	}
	cutoff := observedAt.Add(-kkaiGroupSignalMaxAge).Unix()
	first := sort.Search(len(buffer.events), func(index int) bool { return buffer.events[index].Ts >= cutoff })
	if first > 0 {
		buffer.events = append([]KKAIGroupSignalEvent(nil), buffer.events[first:]...)
	}
	if len(buffer.events) > kkaiGroupLocalEventMax {
		buffer.events = append([]KKAIGroupSignalEvent(nil), buffer.events[len(buffer.events)-kkaiGroupLocalEventMax:]...)
	}
	buffer.mu.Unlock()

	lastCleanup := kkaiGroupLastCleanupAt.Load()
	if observedAt.Unix()-lastCleanup >= 600 && kkaiGroupLastCleanupAt.CompareAndSwap(lastCleanup, observedAt.Unix()) {
		cleanupKKAILocalGroupBuckets(observedAt)
	}
}
