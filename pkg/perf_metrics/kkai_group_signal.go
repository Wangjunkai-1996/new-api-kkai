package perfmetrics

import (
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

const (
	KKAIGroupDataSourceRedis      = "redis"
	KKAIGroupDataSourceLocal      = "local"
	KKAIGroupDataSourceRedisLocal = "redis+local"
	KKAIGroupDataSourceNone       = "none"
	KKAIGroupRecentSignalLimit    = 60

	kkaiGroupMinuteSeconds     = int64(60)
	kkaiGroupHistoricalSeconds = int64(300)
	kkaiGroupStreamMaxLen      = int64(KKAIGroupRecentSignalLimit)
	kkaiGroupLocalEventMax     = KKAIGroupRecentSignalLimit
	kkaiGroupMinuteTTL         = 70 * time.Minute
	kkaiGroupHistoricalTTL     = 26 * time.Hour

	kkaiGroupCacheTrackingMarkerKey = "kkai:group-status:cache-v2:started_at"
	kkaiGroupCacheTrackedField      = "cache_v2_tracked"
	kkaiGroupCacheSampleField       = "cache_v2_n"
	kkaiGroupCacheHitField          = "cache_v2_hit"
	kkaiGroupCachePromptField       = "cache_v2_prompt"
	kkaiGroupCacheReadField         = "cache_v2_read"
)

type KKAIGroupBucket struct {
	Group             string `json:"group"`
	BucketTs          int64  `json:"bucket_ts"`
	RequestCount      int64  `json:"request_count"`
	SuccessCount      int64  `json:"success_count"`
	TotalLatencyMs    int64  `json:"total_latency_ms"`
	TtftSumMs         int64  `json:"ttft_sum_ms"`
	TtftCount         int64  `json:"ttft_count"`
	CacheTrackedCount int64  `json:"cache_tracked_count"`
	CacheSampleCount  int64  `json:"cache_sample_count"`
	CacheHitCount     int64  `json:"cache_hit_count"`
	CachePromptTokens int64  `json:"cache_prompt_tokens"`
	CacheReadTokens   int64  `json:"cache_read_tokens"`
	LastSampleAt      int64  `json:"last_sample_at"`
}

type KKAIGroupBucketResult struct {
	Source                 string            `json:"source"`
	RedisAvailable         bool              `json:"redis_available"`
	CacheTrackingStartedAt int64             `json:"cache_tracking_started_at"`
	Buckets                []KKAIGroupBucket `json:"buckets"`
}

type KKAIGroupSignalEvent struct {
	Group        string `json:"group"`
	Ts           int64  `json:"ts"`
	Success      bool   `json:"success"`
	LatencyMs    int64  `json:"latency_ms"`
	TtftMs       int64  `json:"ttft_ms"`
	EventID      string `json:"-"`
	ObservedAtNs int64  `json:"-"`
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
	requestCount      atomic.Int64
	successCount      atomic.Int64
	totalLatencyMs    atomic.Int64
	ttftSumMs         atomic.Int64
	ttftCount         atomic.Int64
	cacheTrackedCount atomic.Int64
	cacheSampleCount  atomic.Int64
	cacheHitCount     atomic.Int64
	cachePromptTokens atomic.Int64
	cacheReadTokens   atomic.Int64
	lastSampleAt      atomic.Int64
}

type kkaiGroupSignalBuffer struct {
	mu     sync.Mutex
	events []KKAIGroupSignalEvent
}

var (
	kkaiGroupBuckets       sync.Map
	kkaiGroupSignals       sync.Map
	kkaiGroupLastCleanupAt atomic.Int64
	kkaiGroupSignalSeq     atomic.Uint64
	kkaiGroupSignalNodeID  = uuid.NewString()
	kkaiGroupCacheGapEpoch atomic.Uint64
	kkaiGroupCacheGapSeq   atomic.Uint64
)

func markKKAIGroupCacheGap() {
	epoch := kkaiGroupCacheGapSeq.Add(1)
	if epoch == 0 {
		epoch = kkaiGroupCacheGapSeq.Add(1)
	}
	kkaiGroupCacheGapEpoch.Store(epoch)
}

func recordKKAILocalGroupSignal(sample Sample, observedAt time.Time) KKAIGroupSignalEvent {
	for _, resolution := range []int64{kkaiGroupMinuteSeconds, kkaiGroupHistoricalSeconds} {
		bucketTs := observedAt.Unix() - observedAt.Unix()%resolution
		key := kkaiGroupBucketKey{group: sample.Group, bucketTs: bucketTs, resolution: resolution}
		actual, _ := kkaiGroupBuckets.LoadOrStore(key, &kkaiAtomicGroupBucket{})
		actual.(*kkaiAtomicGroupBucket).add(sample, observedAt.Unix())
	}

	actual, _ := kkaiGroupSignals.LoadOrStore(sample.Group, &kkaiGroupSignalBuffer{})
	buffer := actual.(*kkaiGroupSignalBuffer)
	buffer.mu.Lock()
	event := KKAIGroupSignalEvent{
		Group: sample.Group, Ts: observedAt.Unix(), Success: sample.Success,
		LatencyMs: sample.LatencyMs, TtftMs: sample.TtftMs,
		EventID:      kkaiGroupSignalNodeID + "-" + strconv.FormatUint(kkaiGroupSignalSeq.Add(1), 36),
		ObservedAtNs: observedAt.UnixNano(),
	}
	buffer.events = append(buffer.events, event)
	if len(buffer.events) > 1 && kkaiGroupSignalLess(event, buffer.events[len(buffer.events)-2]) {
		sort.SliceStable(buffer.events, func(i, j int) bool { return kkaiGroupSignalLess(buffer.events[i], buffer.events[j]) })
	}
	if len(buffer.events) > kkaiGroupLocalEventMax {
		buffer.events = append([]KKAIGroupSignalEvent(nil), buffer.events[len(buffer.events)-kkaiGroupLocalEventMax:]...)
	}
	buffer.mu.Unlock()

	lastCleanup := kkaiGroupLastCleanupAt.Load()
	if observedAt.Unix()-lastCleanup >= 600 && kkaiGroupLastCleanupAt.CompareAndSwap(lastCleanup, observedAt.Unix()) {
		cleanupKKAILocalGroupBuckets(observedAt)
	}
	return event
}

func kkaiGroupSignalLess(left KKAIGroupSignalEvent, right KKAIGroupSignalEvent) bool {
	if left.ObservedAtNs != right.ObservedAtNs {
		return left.ObservedAtNs < right.ObservedAtNs
	}
	if left.Ts != right.Ts {
		return left.Ts < right.Ts
	}
	return left.EventID < right.EventID
}
