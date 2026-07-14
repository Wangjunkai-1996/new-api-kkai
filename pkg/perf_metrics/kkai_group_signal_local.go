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

func localKKAIGroupSignals(startTs int64, groups []string) []KKAIGroupSignalEvent {
	events := make([]KKAIGroupSignalEvent, 0)
	for _, group := range groups {
		raw, ok := kkaiGroupSignals.Load(group)
		if !ok {
			continue
		}
		buffer := raw.(*kkaiGroupSignalBuffer)
		buffer.mu.Lock()
		first := sort.Search(len(buffer.events), func(index int) bool { return buffer.events[index].Ts >= startTs })
		events = append(events, buffer.events[first:]...)
		buffer.mu.Unlock()
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Ts < events[j].Ts })
	if len(events) > 60*len(groups) {
		events = events[len(events)-60*len(groups):]
	}
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

	signalCutoff := now.Add(-kkaiGroupSignalMaxAge).Unix()
	kkaiGroupSignals.Range(func(_, rawValue any) bool {
		buffer := rawValue.(*kkaiGroupSignalBuffer)
		buffer.mu.Lock()
		first := sort.Search(len(buffer.events), func(index int) bool {
			return buffer.events[index].Ts >= signalCutoff
		})
		if first == len(buffer.events) {
			buffer.events = nil
		} else if first > 0 {
			buffer.events = append([]KKAIGroupSignalEvent(nil), buffer.events[first:]...)
		}
		buffer.mu.Unlock()
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
