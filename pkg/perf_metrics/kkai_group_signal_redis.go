package perfmetrics

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/redis/go-redis/v9"
)

func appendKKAIRedisGroupSignal(pipe redis.Pipeliner, sample Sample, observedAt time.Time, event KKAIGroupSignalEvent) {
	if pipe == nil {
		return
	}
	ctx := context.Background()
	for _, bucket := range []struct {
		resolution int64
		ttl        time.Duration
		prefix     string
	}{
		{kkaiGroupMinuteSeconds, kkaiGroupMinuteTTL, "minute"},
		{kkaiGroupHourSeconds, kkaiGroupHourTTL, "hour"},
	} {
		bucketTs := observedAt.Unix() - observedAt.Unix()%bucket.resolution
		key := kkaiGroupRedisBucketKey(sample.Group, bucket.prefix, bucketTs)
		pipe.HIncrBy(ctx, key, "req", 1)
		if sample.Success {
			pipe.HIncrBy(ctx, key, "ok", 1)
		}
		if sample.LatencyMs > 0 {
			pipe.HIncrBy(ctx, key, "lat", sample.LatencyMs)
		}
		if sample.HasTtft && sample.TtftMs >= 0 {
			pipe.HIncrBy(ctx, key, "ttft", sample.TtftMs)
			pipe.HIncrBy(ctx, key, "ttft_n", 1)
		}
		pipe.HSet(ctx, key, "last_ts", observedAt.Unix())
		pipe.Expire(ctx, key, bucket.ttl)
	}
	streamKey := kkaiGroupRedisStreamKey(sample.Group)
	pipe.XAdd(ctx, &redis.XAddArgs{Stream: streamKey, MaxLen: kkaiGroupStreamMaxLen, Values: map[string]any{
		"ts": observedAt.Unix(), "success": boolRedisValue(sample.Success), "latency": sample.LatencyMs, "ttft": sample.TtftMs,
		"event_id": event.EventID, "observed_at_ns": event.ObservedAtNs,
	}})
	pipe.Persist(ctx, streamKey)
}

func maintainKKAIGroupSignalStreams(ctx context.Context) error {
	if !common.RedisEnabled || common.RDB == nil {
		return nil
	}
	var cursor uint64
	for {
		keys, nextCursor, err := common.RDB.Scan(ctx, cursor, "kkai:group-status:*:signals", 100).Result()
		if err != nil {
			return fmt.Errorf("scan streams: %w", err)
		}
		if len(keys) > 0 {
			pipe := common.RDB.Pipeline()
			for _, key := range keys {
				pipe.XTrimMaxLen(ctx, key, kkaiGroupStreamMaxLen)
				pipe.Persist(ctx, key)
			}
			if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
				return fmt.Errorf("normalize streams: %w", err)
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			return nil
		}
	}
}

func QueryKKAIGroupMinuteBuckets(startTs int64, endTs int64, groups []string) KKAIGroupBucketResult {
	return queryKKAIGroupBuckets(startTs, endTs, groups, kkaiGroupMinuteSeconds, "minute")
}

func QueryKKAIGroupHourBuckets(startTs int64, endTs int64, groups []string) KKAIGroupBucketResult {
	return queryKKAIGroupBuckets(startTs, endTs, groups, kkaiGroupHourSeconds, "hour")
}

func queryKKAIGroupBuckets(startTs int64, endTs int64, groups []string, resolution int64, prefix string) KKAIGroupBucketResult {
	local := localKKAIGroupBuckets(startTs, endTs, groups, resolution)
	if !common.RedisEnabled || common.RDB == nil {
		return KKAIGroupBucketResult{Source: kkaiGroupSource(false, len(local) > 0), RedisAvailable: false, Buckets: local}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pipe := common.RDB.Pipeline()
	type command struct {
		group    string
		bucketTs int64
		cmd      *redis.MapStringStringCmd
	}
	commands := make([]command, 0)
	startBucket, endBucket := startTs-startTs%resolution, endTs-endTs%resolution
	for _, group := range groups {
		for bucketTs := startBucket; bucketTs <= endBucket; bucketTs += resolution {
			commands = append(commands, command{group, bucketTs, pipe.HGetAll(ctx, kkaiGroupRedisBucketKey(group, prefix, bucketTs))})
		}
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return KKAIGroupBucketResult{Source: kkaiGroupSource(false, len(local) > 0), RedisAvailable: false, Buckets: local}
	}
	byKey := make(map[kkaiGroupBucketKey]KKAIGroupBucket, len(commands)+len(local))
	redisPresent := false
	for _, item := range commands {
		values, err := item.cmd.Result()
		if err != nil || len(values) == 0 {
			continue
		}
		redisPresent = true
		byKey[kkaiGroupBucketKey{item.group, item.bucketTs, resolution}] = kkaiGroupBucketFromRedis(item.group, item.bucketTs, values)
	}
	localPresent := false
	for _, bucket := range local {
		key := kkaiGroupBucketKey{bucket.Group, bucket.BucketTs, resolution}
		if _, exists := byKey[key]; !exists {
			localPresent = true
			byKey[key] = bucket
		}
	}
	buckets := make([]KKAIGroupBucket, 0, len(byKey))
	for _, bucket := range byKey {
		buckets = append(buckets, bucket)
	}
	sort.Slice(buckets, func(i, j int) bool {
		if buckets[i].Group == buckets[j].Group {
			return buckets[i].BucketTs < buckets[j].BucketTs
		}
		return buckets[i].Group < buckets[j].Group
	})
	return KKAIGroupBucketResult{Source: kkaiGroupSource(redisPresent, localPresent), RedisAvailable: true, Buckets: buckets}
}

func QueryKKAIGroupRecentSignals(groups []string, limit int) KKAIGroupSignalResult {
	if limit <= 0 {
		limit = KKAIGroupRecentSignalLimit
	} else if limit > kkaiGroupLocalEventMax {
		limit = kkaiGroupLocalEventMax
	}
	local := localKKAIGroupRecentSignals(groups, limit)
	if !common.RedisEnabled || common.RDB == nil {
		return KKAIGroupSignalResult{Source: kkaiGroupSource(false, len(local) > 0), RedisAvailable: false, Events: local}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pipe := common.RDB.Pipeline()
	type command struct {
		group string
		cmd   *redis.XMessageSliceCmd
	}
	commands := make([]command, 0, len(groups))
	for _, group := range groups {
		streamKey := kkaiGroupRedisStreamKey(group)
		commands = append(commands, command{group, pipe.XRevRangeN(ctx, streamKey, "+", "-", int64(limit))})
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return KKAIGroupSignalResult{Source: kkaiGroupSource(false, len(local) > 0), RedisAvailable: false, Events: local}
	}
	redisEvents := make([]KKAIGroupSignalEvent, 0)
	for _, item := range commands {
		messages, err := item.cmd.Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			continue
		}
		for index := len(messages) - 1; index >= 0; index-- {
			message := messages[index]
			event, ok := kkaiGroupSignalFromRedis(item.group, message.Values)
			if ok {
				redisEvents = append(redisEvents, event)
			}
		}
	}
	events, redisPresent, localPresent := mergeKKAIGroupRecentSignals(groups, redisEvents, local, limit)
	return KKAIGroupSignalResult{Source: kkaiGroupSource(redisPresent, localPresent), RedisAvailable: true, Events: events}
}

type kkaiGroupSignalCandidate struct {
	event     KKAIGroupSignalEvent
	fromRedis bool
}

func mergeKKAIGroupRecentSignals(
	groups []string,
	redisEvents []KKAIGroupSignalEvent,
	localEvents []KKAIGroupSignalEvent,
	limit int,
) ([]KKAIGroupSignalEvent, bool, bool) {
	byGroup := make(map[string][]kkaiGroupSignalCandidate, len(groups))
	seenEventIDs := make(map[string]struct{}, len(redisEvents)+len(localEvents))
	for _, event := range redisEvents {
		if event.EventID != "" {
			if _, exists := seenEventIDs[event.EventID]; exists {
				continue
			}
			seenEventIDs[event.EventID] = struct{}{}
		}
		byGroup[event.Group] = append(byGroup[event.Group], kkaiGroupSignalCandidate{event: event, fromRedis: true})
	}
	for _, event := range localEvents {
		if event.EventID != "" {
			if _, exists := seenEventIDs[event.EventID]; exists {
				continue
			}
			seenEventIDs[event.EventID] = struct{}{}
		}
		byGroup[event.Group] = append(byGroup[event.Group], kkaiGroupSignalCandidate{event: event})
	}

	events := make([]KKAIGroupSignalEvent, 0, limit*len(groups))
	redisPresent := false
	localPresent := false
	emittedGroups := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		if _, emitted := emittedGroups[group]; emitted {
			continue
		}
		emittedGroups[group] = struct{}{}
		candidates := byGroup[group]
		sort.SliceStable(candidates, func(i, j int) bool {
			return kkaiGroupSignalLess(candidates[i].event, candidates[j].event)
		})
		if len(candidates) > limit {
			candidates = candidates[len(candidates)-limit:]
		}
		for _, candidate := range candidates {
			events = append(events, candidate.event)
			if candidate.fromRedis {
				redisPresent = true
			} else {
				localPresent = true
			}
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return kkaiGroupSignalLess(events[i], events[j]) })
	return events, redisPresent, localPresent
}
