package perfmetrics

import (
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordRelayResultIncludesNonStreamCacheUsage(t *testing.T) {
	resetKKAIGroupSignalState(t)
	group := "cache-stream-boundary-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	baseInfo := &relaycommon.RelayInfo{
		OriginModelName: "cache-test-model",
		UsingGroup:      group,
		StartTime:       time.Now().Add(-time.Second),
	}

	nonStreamInfo := *baseInfo
	nonStreamInfo.IsStream = false
	streamInfo := *baseInfo
	streamInfo.IsStream = true

	nonStreamInfo.PerformanceCacheUsageKnown = true
	nonStreamInfo.PerformanceCachePromptTokens = 100

	nonStreamInfo.PerformanceCacheReadTokens = 100
	RecordRelayResult(context.Background(), &nonStreamInfo, nil)
	streamInfo.PerformanceCacheUsageKnown = true
	streamInfo.PerformanceCachePromptTokens = 100
	streamInfo.PerformanceCacheReadTokens = 100
	RecordRelayResult(context.Background(), &streamInfo, nil)

	result := QueryKKAIGroupMinuteBuckets(
		time.Now().Add(-2*time.Minute).Unix(), time.Now().Add(time.Second).Unix(), []string{group},
	)
	var tracked, samples, hits, promptTokens, cachedTokens int64
	for _, bucket := range result.Buckets {
		tracked += bucket.CacheTrackedCount
		samples += bucket.CacheSampleCount
		hits += bucket.CacheHitCount
		promptTokens += bucket.CachePromptTokens
		cachedTokens += bucket.CacheReadTokens
	}

	assert.Equal(t, int64(2), tracked)
	assert.Equal(t, int64(2), samples)
	assert.Equal(t, int64(2), hits)
	assert.Equal(t, int64(200), promptTokens)
	assert.Equal(t, int64(200), cachedTokens)
	require.NotZero(t, samples)
	assert.Equal(t, 100.0, float64(hits)/float64(samples)*100)
}

func TestRecordRelayResultCacheUsageDoesNotDependOnClientStreamFlag(t *testing.T) {
	resetKKAIGroupSignalState(t)
	group := "cache-client-stream-flag-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	clientStream := false
	info := &relaycommon.RelayInfo{
		OriginModelName: "cache-test-model",
		UsingGroup:      group,
		StartTime:       time.Now().Add(-time.Second),
		IsStream:        true,
		ClientIsStream:  &clientStream,
	}

	info.PerformanceCacheUsageKnown = true
	info.PerformanceCachePromptTokens = 100

	info.PerformanceCacheReadTokens = 100
	RecordRelayResult(context.Background(), info, nil)

	result := QueryKKAIGroupMinuteBuckets(
		time.Now().Add(-2*time.Minute).Unix(), time.Now().Add(time.Second).Unix(), []string{group},
	)
	var samples, hits, promptTokens, cachedTokens int64
	for _, bucket := range result.Buckets {
		samples += bucket.CacheSampleCount
		hits += bucket.CacheHitCount
		promptTokens += bucket.CachePromptTokens
		cachedTokens += bucket.CacheReadTokens
	}

	assert.Equal(t, int64(1), samples)
	assert.Equal(t, int64(1), hits)
	assert.Equal(t, int64(100), promptTokens)
	assert.Equal(t, int64(100), cachedTokens)
}

func TestRecordNormalizesLowCacheHitMarker(t *testing.T) {
	resetKKAIGroupSignalState(t)
	group := "cache-hit-marker-" + strconv.FormatInt(time.Now().UnixNano(), 36)

	Record(Sample{
		Model:             "cache-test-model",
		Group:             group,
		Success:           true,
		CacheTrackedCount: 1,
		CacheSampleCount:  1,
		CacheHitCount:     1,
		CachePromptTokens: 100,
		CacheReadTokens:   30,
	})

	result := QueryKKAIGroupMinuteBuckets(
		time.Now().Add(-2*time.Minute).Unix(), time.Now().Add(time.Second).Unix(), []string{group},
	)
	var samples, hits int64
	for _, bucket := range result.Buckets {
		samples += bucket.CacheSampleCount
		hits += bucket.CacheHitCount
	}

	assert.Equal(t, int64(1), samples)
	assert.Zero(t, hits)
}

func TestRecordRelayResultRequiresMoreThanThirtyPercentContextForCacheHit(t *testing.T) {
	resetKKAIGroupSignalState(t)
	group := "cache-half-threshold-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	info := &relaycommon.RelayInfo{
		OriginModelName: "cache-test-model",
		UsingGroup:      group,
		StartTime:       time.Now().Add(-time.Second),
		IsStream:        true,
	}

	info.PerformanceCacheUsageKnown = true
	info.PerformanceCachePromptTokens = 100

	info.PerformanceCacheReadTokens = 30
	RecordRelayResult(context.Background(), info, nil)
	info.PerformanceCacheUsageKnown = true
	info.PerformanceCachePromptTokens = 100
	info.PerformanceCacheReadTokens = 31
	RecordRelayResult(context.Background(), info, nil)
	info.PerformanceCacheUsageKnown = true
	info.PerformanceCachePromptTokens = 20_055
	info.PerformanceCacheReadTokens = 3_840
	RecordRelayResult(context.Background(), info, nil)

	result := QueryKKAIGroupMinuteBuckets(
		time.Now().Add(-2*time.Minute).Unix(), time.Now().Add(time.Second).Unix(), []string{group},
	)
	var samples, hits int64
	for _, bucket := range result.Buckets {
		samples += bucket.CacheSampleCount
		hits += bucket.CacheHitCount
	}

	assert.Equal(t, int64(3), samples)
	assert.Equal(t, int64(1), hits)
	assert.InDelta(t, 33.333333333333336, float64(hits)/float64(samples)*100, 0.000001)
}

func TestCacheUsageCountsAsHitThresholdAndOverflowBoundary(t *testing.T) {
	for _, test := range []struct {
		name           string
		prompt, cached int64
		want           bool
	}{
		{name: "exactly thirty percent", prompt: 100, cached: 30},
		{name: "above thirty percent", prompt: 100, cached: 31, want: true},
		{name: "fractional threshold", prompt: 3, cached: 1, want: true},
		{name: "int64 threshold below", prompt: math.MaxInt64, cached: 2_767_011_611_056_432_742},
		{name: "int64 threshold above", prompt: math.MaxInt64, cached: 2_767_011_611_056_432_743, want: true},
		{name: "cached tokens exceed input", prompt: 100, cached: 101},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, cacheUsageCountsAsHit(&CacheUsage{PromptTokens: test.prompt, CachedTokens: test.cached}))
		})
	}
}
