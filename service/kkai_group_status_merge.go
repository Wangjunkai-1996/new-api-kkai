package service

import (
	"sort"

	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
)

func mergeKKAIPerfBuckets(metrics map[string]kkaiGroupMetrics, buckets []perfmetrics.KKAIGroupBucket) {
	for _, bucket := range buckets {
		current := metrics[bucket.Group]
		current.requestCount += bucket.RequestCount
		current.successCount += bucket.SuccessCount
		current.totalLatencyMs += bucket.TotalLatencyMs
		current.ttftSumMs += bucket.TtftSumMs
		current.ttftCount += bucket.TtftCount
		current.cacheSampleCount += bucket.CacheSampleCount
		current.cacheTrackedCount += bucket.CacheTrackedCount
		current.cachePromptTokens += bucket.CachePromptTokens
		current.cacheReadTokens += bucket.CacheReadTokens
		current.sampledAt = max(current.sampledAt, bucket.LastSampleAt)
		metrics[bucket.Group] = current
	}
}

func mergeKKAIDatabaseAndLiveBuckets(databaseBuckets []model.KKAIPerfMetricBucket, liveBuckets []perfmetrics.KKAIGroupBucket) map[string]kkaiGroupMetrics {
	byBucket := make(map[kkaiGroupMetricKey]kkaiGroupMetrics, len(databaseBuckets)+len(liveBuckets))
	for _, bucket := range databaseBuckets {
		key := kkaiGroupMetricKey{group: bucket.Group, bucketTs: bucket.BucketTs - bucket.BucketTs%3600}
		current := byBucket[key]
		current.requestCount += bucket.RequestCount
		current.successCount += bucket.SuccessCount
		current.totalLatencyMs += bucket.TotalLatencyMs
		current.ttftSumMs += bucket.TtftSumMs
		current.ttftCount += bucket.TtftCount
		current.sampledAt = max(current.sampledAt, bucket.BucketTs)
		byBucket[key] = current
	}
	liveByHour := make(map[kkaiGroupMetricKey]kkaiGroupMetrics, len(liveBuckets))
	for _, bucket := range liveBuckets {
		key := kkaiGroupMetricKey{group: bucket.Group, bucketTs: bucket.BucketTs - bucket.BucketTs%3600}
		liveByHour[key] = liveByHour[key].add(kkaiGroupMetrics{
			requestCount:      bucket.RequestCount,
			successCount:      bucket.SuccessCount,
			totalLatencyMs:    bucket.TotalLatencyMs,
			ttftSumMs:         bucket.TtftSumMs,
			ttftCount:         bucket.TtftCount,
			cacheSampleCount:  bucket.CacheSampleCount,
			cacheTrackedCount: bucket.CacheTrackedCount,
			cachePromptTokens: bucket.CachePromptTokens,
			cacheReadTokens:   bucket.CacheReadTokens,
			sampledAt:         bucket.LastSampleAt,
		})
	}
	for key, live := range liveByHour {
		persisted, exists := byBucket[key]
		if !exists || live.requestCount >= persisted.requestCount {
			byBucket[key] = live
			continue
		}
		persisted.cacheSampleCount = live.cacheSampleCount
		persisted.cacheTrackedCount = live.cacheTrackedCount
		persisted.cachePromptTokens = live.cachePromptTokens
		persisted.cacheReadTokens = live.cacheReadTokens
		persisted.sampledAt = max(persisted.sampledAt, live.sampledAt)
		byBucket[key] = persisted
	}
	metrics := make(map[string]kkaiGroupMetrics)
	for key, bucket := range byBucket {
		metrics[key.group] = metrics[key.group].add(bucket)
	}
	return metrics
}

func combinedKKAIGroupDataSource(hasDatabase bool, liveSource string) string {
	if !hasDatabase {
		return liveSource
	}
	if liveSource == "" || liveSource == perfmetrics.KKAIGroupDataSourceNone {
		return "database"
	}
	return "database+" + liveSource
}

func applyKKAIAutoGroupMetrics(metrics map[string]kkaiGroupMetrics, usableGroups map[string]string, autoGroups []string) {
	if _, ok := usableGroups["auto"]; !ok {
		return
	}
	auto := metrics["auto"]
	for _, group := range autoGroups {
		if group == "auto" {
			continue
		}
		if _, ok := usableGroups[group]; ok {
			auto = auto.add(metrics[group])
		}
	}
	metrics["auto"] = auto
}

func kkaiGroupRecentEventsByGroup(events []perfmetrics.KKAIGroupSignalEvent, limit int) map[string][]KKAIGroupRecentEvent {
	result := make(map[string][]KKAIGroupRecentEvent)
	for _, event := range events {
		status := "failure"
		if event.Success {
			status = "success"
		}
		result[event.Group] = append(result[event.Group], KKAIGroupRecentEvent{
			Ts: event.Ts, Status: status, TtftMs: event.TtftMs, LatencyMs: event.LatencyMs,
			eventID: event.EventID, observedAtNs: event.ObservedAtNs,
		})
	}
	for group, groupEvents := range result {
		sort.SliceStable(groupEvents, func(i, j int) bool { return kkaiGroupRecentEventLess(groupEvents[i], groupEvents[j]) })
		if len(groupEvents) > limit {
			groupEvents = groupEvents[len(groupEvents)-limit:]
		}
		result[group] = groupEvents
	}
	return result
}

func applyKKAIAutoGroupEvents(events map[string][]KKAIGroupRecentEvent, usableGroups map[string]string, autoGroups []string, limit int) {
	if _, ok := usableGroups["auto"]; !ok {
		return
	}
	auto := append([]KKAIGroupRecentEvent(nil), events["auto"]...)
	for _, group := range autoGroups {
		if group == "auto" {
			continue
		}
		if _, ok := usableGroups[group]; ok {
			auto = append(auto, events[group]...)
		}
	}
	sort.SliceStable(auto, func(i, j int) bool { return kkaiGroupRecentEventLess(auto[i], auto[j]) })
	if len(auto) > limit {
		auto = auto[len(auto)-limit:]
	}
	events["auto"] = auto
}

func kkaiGroupRecentEventLess(left KKAIGroupRecentEvent, right KKAIGroupRecentEvent) bool {
	leftOrder := left.observedAtNs
	if leftOrder <= 0 {
		leftOrder = left.Ts * 1_000_000_000
	}
	rightOrder := right.observedAtNs
	if rightOrder <= 0 {
		rightOrder = right.Ts * 1_000_000_000
	}
	if leftOrder != rightOrder {
		return leftOrder < rightOrder
	}
	return left.eventID < right.eventID
}

func (metrics kkaiGroupMetrics) successRate() float64 {
	if metrics.requestCount <= 0 {
		return 0
	}
	return float64(metrics.successCount) / float64(metrics.requestCount) * 100
}

func (metrics kkaiGroupMetrics) add(other kkaiGroupMetrics) kkaiGroupMetrics {
	metrics.requestCount += other.requestCount
	metrics.successCount += other.successCount
	metrics.totalLatencyMs += other.totalLatencyMs
	metrics.ttftSumMs += other.ttftSumMs
	metrics.ttftCount += other.ttftCount
	metrics.cacheSampleCount += other.cacheSampleCount
	metrics.cacheTrackedCount += other.cacheTrackedCount
	metrics.cachePromptTokens += other.cachePromptTokens
	metrics.cacheReadTokens += other.cacheReadTokens
	metrics.sampledAt = max(metrics.sampledAt, other.sampledAt)
	return metrics
}
