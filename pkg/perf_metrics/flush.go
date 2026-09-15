package perfmetrics

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/model"
)

// FlushLocalBuckets persists this process's increments, including its active
// bucket, so draining a serving slot does not discard the current interval.
func FlushLocalBuckets(ctx context.Context) error {
	var flushErrors []error
	hotBuckets.Range(func(key, value any) bool {
		if err := ctx.Err(); err != nil {
			flushErrors = append(flushErrors, err)
			return false
		}
		k := key.(bucketKey)

		bucket := value.(*atomicBucket)
		drained := bucket.drain()
		if drained.requestCount == 0 {
			deleteOldEmptyBucket(k, key)
			return true
		}

		err := model.UpsertPerfMetric(ctx, &model.PerfMetric{
			ModelName:      k.model,
			Group:          k.group,
			BucketTs:       k.bucketTs,
			RequestCount:   drained.requestCount,
			SuccessCount:   drained.successCount,
			TotalLatencyMs: drained.totalLatencyMs,
			TtftSumMs:      drained.ttftSumMs,
			TtftCount:      drained.ttftCount,
			OutputTokens:   drained.outputTokens,
			GenerationMs:   drained.generationMs,
		})
		if err != nil {
			bucket.addCounters(drained)
			flushErrors = append(flushErrors, fmt.Errorf("flush perf metric bucket model=%s group=%s bucket=%d: %w", k.model, k.group, k.bucketTs, err))
			return true
		}

		deleteOldEmptyBucket(k, key)
		return true
	})
	return errors.Join(append(flushErrors, ctx.Err())...)
}

func deleteOldEmptyBucket(k bucketKey, rawKey any) {
	if k.bucketTs < bucketStart(time.Now().Add(-24*time.Hour).Unix()) {
		hotBuckets.Delete(rawKey)
	}
}

func cleanupExpiredMetrics(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix()
	if err := model.DeletePerfMetricsBefore(ctx, cutoff); err != nil {
		return fmt.Errorf("cleanup expired perf metrics: %w", err)
	}
	return nil
}

func redisCounters(values map[string]string) counters {
	return counters{
		requestCount:   parseRedisInt(values["req"]),
		successCount:   parseRedisInt(values["ok"]),
		totalLatencyMs: parseRedisInt(values["lat"]),
		ttftSumMs:      parseRedisInt(values["ttft"]),
		ttftCount:      parseRedisInt(values["ttft_n"]),
		outputTokens:   parseRedisInt(values["out"]),
		generationMs:   parseRedisInt(values["gen_ms"]),
	}
}

func parseRedisInt(value string) int64 {
	if value == "" {
		return 0
	}
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}
