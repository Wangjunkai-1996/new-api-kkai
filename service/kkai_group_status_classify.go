package service

import (
	"math"
	"time"

	"github.com/QuantumNous/new-api/setting"
)

func buildKKAIGroupStatusEntry(
	group string,
	desc string,
	metrics kkaiGroupMetrics,
	now time.Time,
	window kkaiGroupStatusWindow,
	dataSource string,
	cacheRedisAvailable bool,
	cacheWindowCovered bool,
	recentEvents []KKAIGroupRecentEvent,
) KKAIGroupStatusEntry {
	successRate := roundKKAIPercent(metrics.successRate())
	avgLatency := kkaiAverage(metrics.totalLatencyMs, metrics.requestCount)
	avgTtft := kkaiAverage(metrics.ttftSumMs, metrics.ttftCount)
	stale := metrics.sampledAt > 0 && now.Sub(time.Unix(metrics.sampledAt, 0)) > window.staleAfter
	confidenceStatus, message := classifyKKAIGroupConfidence(metrics, successRate, window, stale)
	entry := KKAIGroupStatusEntry{
		Group: group, Desc: desc,
		DisplayName: setting.GetGroupDisplayNameWithFallback(group, desc),
		Status:      kkaiLegacyGroupHealthStatus(confidenceStatus),
		Confidence:  kkaiGroupHealthConfidence(metrics.requestCount), Message: message,
		ConfidenceStatus: confidenceStatus, ExperienceLabel: classifyKKAIGroupExperience(metrics, avgTtft, window, stale),
		DisplayMessage: message, RequestCount: metrics.requestCount, SuccessRate: successRate,
		AvgLatencyMs: avgLatency, AvgTtftMs: avgTtft, UpdatedAt: metrics.sampledAt, SampledAt: metrics.sampledAt,
		Stale: stale, DataSource: dataSource, RecentEvents: recentEvents,
	}
	if group == "default" || group == "codex-plus" || group == "plus" {
		entry.CacheStats = buildKKAIGroupCacheStats(metrics, cacheRedisAvailable, cacheWindowCovered)
	}
	return entry
}

func buildKKAIGroupCacheStats(metrics kkaiGroupMetrics, redisAvailable bool, windowCovered bool) *KKAIGroupCacheStats {
	stats := &KKAIGroupCacheStats{
		Status:      KKAIGroupCacheStatusUnavailable,
		SampleCount: metrics.cacheSampleCount,
	}
	if !redisAvailable || !windowCovered || metrics.cacheTrackedCount != metrics.requestCount {
		return stats
	}
	if metrics.cacheSampleCount == 0 {
		stats.Status = KKAIGroupCacheStatusEmpty
		return stats
	}
	hitRate := roundKKAIPercent(float64(metrics.cacheHitCount) / float64(metrics.cacheSampleCount) * 100)
	stats.Status = KKAIGroupCacheStatusOK
	stats.RequestHitRate = &hitRate
	return stats
}

func classifyKKAIGroupConfidence(metrics kkaiGroupMetrics, successRate float64, window kkaiGroupStatusWindow, stale bool) (string, string) {
	if stale {
		return KKAIGroupConfidenceUnknown, kkaiGroupStatusMessageStale
	}
	if window.live {
		return classifyKKAILiveGroupConfidence(metrics, successRate)
	}
	if metrics.requestCount < minKKAISamplesForWindow(window) {
		return KKAIGroupConfidenceUnknown, kkaiGroupStatusMessageUnknown
	}
	if successRate < kkaiGroupHealthOutageSuccessRate {
		return KKAIGroupConfidenceUnavailable, kkaiGroupStatusMessageUnavailable
	}
	if successRate < kkaiGroupHealthDegradedRate {
		return KKAIGroupConfidenceUnstable, kkaiGroupStatusMessageUnstable
	}
	if successRate >= kkaiGroupConfidenceExcellentRate {
		return KKAIGroupConfidenceExcellent, kkaiGroupStatusMessageExcellent
	}
	if successRate >= kkaiGroupConfidenceSmoothRate {
		return KKAIGroupConfidenceSmooth, kkaiGroupStatusMessageSmooth
	}
	if successRate >= kkaiGroupConfidenceStableRate {
		return KKAIGroupConfidenceStable, kkaiGroupStatusMessageStable
	}
	return KKAIGroupConfidenceUnstable, kkaiGroupStatusMessageUnstable
}

func classifyKKAILiveGroupConfidence(metrics kkaiGroupMetrics, successRate float64) (string, string) {
	if metrics.requestCount == 0 {
		return KKAIGroupConfidenceUnknown, kkaiGroupStatusMessageLiveWaiting
	}
	if metrics.requestCount < kkaiGroupHealthLiveMinSamples {
		if successRate == 100 {
			return KKAIGroupConfidenceStable, kkaiGroupStatusMessageLiveSuccess
		}
		if successRate < kkaiGroupHealthLiveOutageRate {
			return KKAIGroupConfidenceUnavailable, kkaiGroupStatusMessageLiveFailure
		}
		return KKAIGroupConfidenceUnstable, kkaiGroupStatusMessageUnstable
	}
	if successRate < kkaiGroupHealthLiveOutageRate {
		return KKAIGroupConfidenceUnavailable, kkaiGroupStatusMessageLiveFailure
	}
	if successRate < kkaiGroupHealthOutageSuccessRate {
		return KKAIGroupConfidenceUnstable, kkaiGroupStatusMessageUnstable
	}
	if successRate >= kkaiGroupConfidenceExcellentRate {
		return KKAIGroupConfidenceExcellent, kkaiGroupStatusMessageExcellent
	}
	if successRate >= kkaiGroupConfidenceSmoothRate {
		return KKAIGroupConfidenceSmooth, kkaiGroupStatusMessageSmooth
	}
	return KKAIGroupConfidenceStable, kkaiGroupStatusMessageLiveSuccess
}

func classifyKKAIGroupExperience(metrics kkaiGroupMetrics, avgTtft int64, window kkaiGroupStatusWindow, stale bool) string {
	if stale || metrics.ttftCount < minKKAISamplesForWindow(window) || avgTtft <= 0 {
		return KKAIGroupExperienceUnknown
	}
	if avgTtft < kkaiGroupExperienceLightningMs {
		return KKAIGroupExperienceLightning
	}
	if avgTtft <= kkaiGroupExperienceSmoothMs {
		return KKAIGroupExperienceSmooth
	}
	return KKAIGroupExperienceNormal
}

func kkaiLegacyGroupHealthStatus(confidenceStatus string) string {
	switch confidenceStatus {
	case KKAIGroupConfidenceExcellent, KKAIGroupConfidenceSmooth, KKAIGroupConfidenceStable:
		return KKAIGroupHealthOperational
	case KKAIGroupConfidenceUnstable:
		return KKAIGroupHealthDegraded
	case KKAIGroupConfidenceUnavailable:
		return KKAIGroupHealthOutage
	default:
		return KKAIGroupHealthUnknown
	}
}

func kkaiGroupHealthConfidence(requestCount int64) string {
	if requestCount < kkaiGroupHealthMinSamples {
		return KKAIGroupHealthConfidenceLow
	}
	if requestCount >= kkaiGroupHealthMediumSamples {
		return KKAIGroupHealthConfidenceHigh
	}
	return KKAIGroupHealthConfidenceMedium
}

func minKKAISamplesForWindow(window kkaiGroupStatusWindow) int64 {
	if window.live || window.minutes <= 15 {
		return kkaiGroupHealthLiveMinSamples
	}
	return kkaiGroupHealthMinSamples
}

func kkaiAverage(sum int64, count int64) int64 {
	if count <= 0 {
		return 0
	}
	return sum / count
}

func roundKKAIPercent(value float64) float64 {
	return math.Round(value*100) / 100
}
