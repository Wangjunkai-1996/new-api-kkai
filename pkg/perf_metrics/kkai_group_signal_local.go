package perfmetrics

import (
	"sort"
	"time"
)

func localKKAIGroupBuckets(startTs int64, endTs int64, groups []string, resolution int64) []KKAIGroupBucket {
	allowed := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		allowed[group] = struct{}{}
	}
	buckets := make([]KKAIGroupBucket, 0)
	kkaiGroupBuckets.Range(func(rawKey any, rawValue any) bool {
		key := rawKey.(kkaiGroupBucketKey)
		if key.resolution != resolution || key.bucketTs < startTs-resolution || key.bucketTs > endTs {
			return true
		}
		if _, ok := allowed[key.group]; !ok {
			return true
		}
		bucket := rawValue.(*kkaiAtomicGroupBucket).snapshot(key.group, key.bucketTs)
		if bucket.RequestCount > 0 {
			buckets = append(buckets, bucket)
		}
		return true
	})
	return buckets
}

func localKKAIGroupRecentSignals(groups []string, limit int) []KKAIGroupSignalEvent {
	events := make([]KKAIGroupSignalEvent, 0, limit*len(groups))
	for _, group := range groups {
		raw, ok := kkaiGroupSignals.Load(group)
		if !ok {
			continue
		}
		buffer := raw.(*kkaiGroupSignalBuffer)
		buffer.mu.Lock()
		first := max(0, len(buffer.events)-limit)
		events = append(events, buffer.events[first:]...)
		buffer.mu.Unlock()
	}
	sort.SliceStable(events, func(i, j int) bool { return kkaiGroupSignalLess(events[i], events[j]) })
	return events
}

func cleanupKKAILocalGroupBuckets(now time.Time) {
	minuteCutoff := now.Add(-kkaiGroupMinuteTTL).Unix()
	hourCutoff := now.Add(-kkaiGroupHourTTL).Unix()
	kkaiGroupBuckets.Range(func(rawKey any, _ any) bool {
		key := rawKey.(kkaiGroupBucketKey)
		if (key.resolution == kkaiGroupMinuteSeconds && key.bucketTs < minuteCutoff) ||
			(key.resolution == kkaiGroupHourSeconds && key.bucketTs < hourCutoff) {
			kkaiGroupBuckets.Delete(rawKey)
		}
		return true
	})
}

func (bucket *kkaiAtomicGroupBucket) add(sample Sample, sampledAt int64) {
	bucket.requestCount.Add(1)
	if sample.Success {
		bucket.successCount.Add(1)
	}
	if sample.LatencyMs > 0 {
		bucket.totalLatencyMs.Add(sample.LatencyMs)
	}
	if sample.HasTtft && sample.TtftMs >= 0 {
		bucket.ttftSumMs.Add(sample.TtftMs)
		bucket.ttftCount.Add(1)
	}
	for {
		current := bucket.lastSampleAt.Load()
		if sampledAt <= current || bucket.lastSampleAt.CompareAndSwap(current, sampledAt) {
			break
		}
	}
}

func (bucket *kkaiAtomicGroupBucket) snapshot(group string, bucketTs int64) KKAIGroupBucket {
	return KKAIGroupBucket{
		Group: group, BucketTs: bucketTs,
		RequestCount: bucket.requestCount.Load(), SuccessCount: bucket.successCount.Load(),
		TotalLatencyMs: bucket.totalLatencyMs.Load(), TtftSumMs: bucket.ttftSumMs.Load(),
		TtftCount: bucket.ttftCount.Load(), LastSampleAt: bucket.lastSampleAt.Load(),
	}
}
