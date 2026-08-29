package service

import (
	"time"

	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
)

const (
	KKAIGroupHealthOperational = "operational"
	KKAIGroupHealthDegraded    = "degraded"
	KKAIGroupHealthOutage      = "outage"
	KKAIGroupHealthUnknown     = "unknown"

	KKAIGroupConfidenceExcellent   = "excellent"
	KKAIGroupConfidenceSmooth      = "smooth"
	KKAIGroupConfidenceStable      = "stable"
	KKAIGroupConfidenceUnstable    = "unstable"
	KKAIGroupConfidenceUnavailable = "unavailable"
	KKAIGroupConfidenceUnknown     = "unknown"

	KKAIGroupHealthConfidenceHigh   = "high"
	KKAIGroupHealthConfidenceMedium = "medium"
	KKAIGroupHealthConfidenceLow    = "low"

	KKAIGroupExperienceLightning = "lightning"
	KKAIGroupExperienceSmooth    = "smooth"
	KKAIGroupExperienceNormal    = "normal"
	KKAIGroupExperienceUnknown   = "unknown"

	KKAIGroupCacheStatusOK          = "ok"
	KKAIGroupCacheStatusEmpty       = "empty"
	KKAIGroupCacheStatusUnavailable = "unavailable"

	kkaiGroupStatusMessageExcellent   = "Group status message: excellent"
	kkaiGroupStatusMessageSmooth      = "Group status message: smooth"
	kkaiGroupStatusMessageStable      = "Group status message: stable"
	kkaiGroupStatusMessageUnstable    = "Group status message: unstable"
	kkaiGroupStatusMessageUnavailable = "Group status message: unavailable"
	kkaiGroupStatusMessageUnknown     = "Group status message: unknown"
	kkaiGroupStatusMessageLiveSuccess = "Group status message: live success"
	kkaiGroupStatusMessageLiveWaiting = "Group status message: live waiting"
	kkaiGroupStatusMessageLiveFailure = "Group status message: live failure"
	kkaiGroupStatusMessageStale       = "Group status message: stale"

	kkaiGroupHealthMinSamples        = int64(20)
	kkaiGroupHealthLiveMinSamples    = int64(8)
	kkaiGroupHealthMediumSamples     = int64(100)
	kkaiGroupHealthOutageSuccessRate = 80.0
	kkaiGroupHealthLiveOutageRate    = 50.0
	kkaiGroupHealthDegradedRate      = 95.0
	kkaiGroupConfidenceStableRate    = 95.0
	kkaiGroupConfidenceSmoothRate    = 99.0
	kkaiGroupConfidenceExcellentRate = 99.9
	kkaiGroupExperienceLightningMs   = int64(2000)
	kkaiGroupExperienceSmoothMs      = int64(5000)
	kkaiGroupRecentEventLimit        = perfmetrics.KKAIGroupRecentSignalLimit
)

var (
	kkaiGroupStatusNow              = time.Now
	loadKKAIPerfMetricBuckets       = model.GetKKAIPerfMetricBuckets
	queryKKAIGroupMinuteBuckets     = perfmetrics.QueryKKAIGroupMinuteBuckets
	queryKKAIGroupHistoricalBuckets = perfmetrics.QueryKKAIGroupHistoricalBuckets
	queryKKAIGroupRecentSignals     = perfmetrics.QueryKKAIGroupRecentSignals
)

type KKAIGroupStatusRequest struct {
	UsableGroups map[string]string
	AutoGroups   []string
	Hours        int
	Window       string
}

type KKAIGroupStatusResult struct {
	GeneratedAt    int64                  `json:"generated_at"`
	Window         string                 `json:"window"`
	WindowMinutes  int                    `json:"window_minutes"`
	WindowHours    int                    `json:"window_hours"`
	DataSource     string                 `json:"data_source"`
	RedisAvailable bool                   `json:"redis_available"`
	Groups         []KKAIGroupStatusEntry `json:"groups"`
}

type KKAIGroupStatusEntry struct {
	Group            string                 `json:"group"`
	Desc             string                 `json:"desc"`
	DisplayName      string                 `json:"display_name"`
	Status           string                 `json:"status"`
	Confidence       string                 `json:"confidence"`
	Message          string                 `json:"message"`
	ConfidenceStatus string                 `json:"confidence_status"`
	ExperienceLabel  string                 `json:"experience_label"`
	DisplayMessage   string                 `json:"display_message"`
	RequestCount     int64                  `json:"request_count"`
	SuccessRate      float64                `json:"success_rate"`
	AvgLatencyMs     int64                  `json:"avg_latency_ms"`
	AvgTtftMs        int64                  `json:"avg_ttft_ms"`
	UpdatedAt        int64                  `json:"updated_at"`
	SampledAt        int64                  `json:"sampled_at"`
	Stale            bool                   `json:"stale"`
	DataSource       string                 `json:"data_source"`
	RecentEvents     []KKAIGroupRecentEvent `json:"recent_events"`
	CacheStats       *KKAIGroupCacheStats   `json:"cache_stats,omitempty"`
}

type KKAIGroupCacheStats struct {
	Status      string `json:"status"`
	SampleCount int64  `json:"sample_count"`
	// RequestHitRate counts stream requests whose cached input reaches 50% of
	// the normalized prompt context.
	RequestHitRate *float64 `json:"request_hit_rate"`
}

type KKAIGroupRecentEvent struct {
	Ts           int64  `json:"ts"`
	Status       string `json:"status"`
	TtftMs       int64  `json:"ttft_ms,omitempty"`
	LatencyMs    int64  `json:"latency_ms,omitempty"`
	eventID      string
	observedAtNs int64
}

type kkaiGroupStatusWindow struct {
	name        string
	minutes     int
	compatHours int
	live        bool
	staleAfter  time.Duration
}

type kkaiGroupMetrics struct {
	requestCount      int64
	successCount      int64
	totalLatencyMs    int64
	ttftSumMs         int64
	ttftCount         int64
	cacheSampleCount  int64
	cacheTrackedCount int64
	cacheHitCount     int64
	cachePromptTokens int64
	cacheReadTokens   int64
	sampledAt         int64
}

type kkaiGroupMetricKey struct {
	group    string
	bucketTs int64
}
