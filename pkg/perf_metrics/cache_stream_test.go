package perfmetrics

import (
	"strconv"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Non-stream responses do not have a cache event. They still need to count as
// tracked requests so the status window can distinguish complete tracking from
// an observability gap, but must not affect the cache sample denominator.
func TestRecordRelaySampleExcludesNonStreamCacheUsage(t *testing.T) {
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

	RecordRelaySample(&nonStreamInfo, true, 0, &CacheUsage{PromptTokens: 100, CachedTokens: 100})
	RecordRelaySample(&streamInfo, true, 0, &CacheUsage{PromptTokens: 100, CachedTokens: 100})

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
	assert.Equal(t, int64(1), samples)
	assert.Equal(t, int64(1), hits)
	assert.Equal(t, int64(100), promptTokens)
	assert.Equal(t, int64(100), cachedTokens)
	require.NotZero(t, samples)
	assert.Equal(t, 100.0, float64(hits)/float64(samples)*100)
}

func TestRecordRelaySampleUsesClientStreamFlag(t *testing.T) {
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

	RecordRelaySample(info, true, 0, &CacheUsage{PromptTokens: 100, CachedTokens: 100})

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

	assert.Zero(t, samples)
	assert.Zero(t, hits)
	assert.Zero(t, promptTokens)
	assert.Zero(t, cachedTokens)
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
		CacheReadTokens:   49,
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

func TestRecordRelaySampleRequiresHalfContextForCacheHit(t *testing.T) {
	resetKKAIGroupSignalState(t)
	group := "cache-half-threshold-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	info := &relaycommon.RelayInfo{
		OriginModelName: "cache-test-model",
		UsingGroup:      group,
		StartTime:       time.Now().Add(-time.Second),
		IsStream:        true,
	}

	RecordRelaySample(info, true, 0, &CacheUsage{PromptTokens: 100, CachedTokens: 49})
	RecordRelaySample(info, true, 0, &CacheUsage{PromptTokens: 100, CachedTokens: 50})
	RecordRelaySample(info, true, 0, &CacheUsage{PromptTokens: 20_055, CachedTokens: 3_840})

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
