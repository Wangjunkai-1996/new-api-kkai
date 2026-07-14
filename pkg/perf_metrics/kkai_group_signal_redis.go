package perfmetrics

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

func appendKKAIRedisGroupSignal(pipe redis.Pipeliner, sample Sample, observedAt time.Time) {
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
	pipe.XAdd(ctx, &redis.XAddArgs{Stream: streamKey, MaxLen: kkaiGroupStreamMaxLen, Approx: true, Values: map[string]any{
		"ts": observedAt.Unix(), "success": boolRedisValue(sample.Success), "latency": sample.LatencyMs, "ttft": sample.TtftMs,
	}})
	pipe.Expire(ctx, streamKey, kkaiGroupMinuteTTL)
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
		cmd      *redis.StringStringMapCmd
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

func QueryKKAIGroupSignals(minutes int, groups []string) KKAIGroupSignalResult {
	if minutes <= 0 || minutes > 30 {
		minutes = 15
	}
	startTs := time.Now().Add(-time.Duration(minutes) * time.Minute).Unix()
	local := localKKAIGroupSignals(startTs, groups)
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
	minID := fmt.Sprintf("%d-0", startTs*1000)
	for _, group := range groups {
		commands = append(commands, command{group, pipe.XRevRangeN(ctx, kkaiGroupRedisStreamKey(group), "+", minID, 60)})
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return KKAIGroupSignalResult{Source: kkaiGroupSource(false, len(local) > 0), RedisAvailable: false, Events: local}
	}
	events := make([]KKAIGroupSignalEvent, 0)
	groupsWithRedis := make(map[string]struct{})
	for _, item := range commands {
		messages, err := item.cmd.Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			continue
		}
		if len(messages) > 0 {
			groupsWithRedis[item.group] = struct{}{}
		}
		for _, message := range messages {
			event, ok := kkaiGroupSignalFromRedis(item.group, message.Values)
			if ok && event.Ts >= startTs {
				events = append(events, event)
			}
		}
	}
	localPresent := false
	for _, event := range local {
		if _, exists := groupsWithRedis[event.Group]; !exists {
			localPresent = true
			events = append(events, event)
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Ts < events[j].Ts })
	return KKAIGroupSignalResult{Source: kkaiGroupSource(len(groupsWithRedis) > 0, localPresent), RedisAvailable: true, Events: events}
}
