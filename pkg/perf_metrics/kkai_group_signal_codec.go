package perfmetrics

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

func kkaiGroupBucketFromRedis(group string, bucketTs int64, values map[string]string) KKAIGroupBucket {
	return KKAIGroupBucket{Group: group, BucketTs: bucketTs, RequestCount: parseRedisInt(values["req"]),
		SuccessCount: parseRedisInt(values["ok"]), TotalLatencyMs: parseRedisInt(values["lat"]),
		TtftSumMs: parseRedisInt(values["ttft"]), TtftCount: parseRedisInt(values["ttft_n"]),
		CacheTrackedCount: parseRedisInt(values[kkaiGroupCacheTrackedField]),
		CacheSampleCount:  parseRedisInt(values[kkaiGroupCacheSampleField]),
		CacheHitCount:     parseRedisInt(values[kkaiGroupCacheHitField]),
		CachePromptTokens: parseRedisInt(values[kkaiGroupCachePromptField]),
		CacheReadTokens:   parseRedisInt(values[kkaiGroupCacheReadField]), LastSampleAt: parseRedisInt(values["last_ts"])}
}

func kkaiGroupSignalFromRedis(group string, values map[string]interface{}) (KKAIGroupSignalEvent, bool) {
	ts, ok := redisValueInt64(values["ts"])
	if !ok || ts <= 0 {
		return KKAIGroupSignalEvent{}, false
	}
	success, _ := redisValueInt64(values["success"])
	latency, _ := redisValueInt64(values["latency"])
	ttft, _ := redisValueInt64(values["ttft"])
	observedAtNs, _ := redisValueInt64(values["observed_at_ns"])
	if observedAtNs <= 0 {
		observedAtNs = ts * 1_000_000_000
	}
	return KKAIGroupSignalEvent{
		Group: group, Ts: ts, Success: success == 1, LatencyMs: latency, TtftMs: ttft,
		EventID: redisValueString(values["event_id"]), ObservedAtNs: observedAtNs,
	}, true
}

func redisValueString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return ""
	}
}

func redisValueInt64(value interface{}) (int64, bool) {
	switch typed := value.(type) {
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		return parsed, err == nil
	case []byte:
		parsed, err := strconv.ParseInt(string(typed), 10, 64)
		return parsed, err == nil
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	default:
		return 0, false
	}
}

func kkaiGroupRedisBucketKey(group string, prefix string, bucketTs int64) string {
	return fmt.Sprintf("kkai:group-status:%s:%s:%d", kkaiGroupDigest(group), prefix, bucketTs)
}

func kkaiGroupRedisStreamKey(group string) string {
	return "kkai:group-status:" + kkaiGroupDigest(group) + ":signals"
}

func kkaiGroupDigest(group string) string {
	digest := sha256.Sum256([]byte(group))
	return hex.EncodeToString(digest[:12])
}

func kkaiGroupSource(redisPresent bool, localPresent bool) string {
	if redisPresent && localPresent {
		return KKAIGroupDataSourceRedisLocal
	}
	if redisPresent {
		return KKAIGroupDataSourceRedis
	}
	if localPresent {
		return KKAIGroupDataSourceLocal
	}
	return KKAIGroupDataSourceNone
}

func boolRedisValue(value bool) int {
	if value {
		return 1
	}
	return 0
}
