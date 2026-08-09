package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrKKAIPolicyCooldownUnavailable = errors.New("KKAI conversation cooldown store unavailable")

type kkaiPolicyRedisClient interface {
	Eval(context.Context, string, []string, ...interface{}) *redis.Cmd
}

type redisKKAIPolicyCooldownStore struct {
	client kkaiPolicyRedisClient
	now    func() time.Time
}

func NewRedisKKAIPolicyCooldownStore(client *redis.Client) KKAIPolicyCooldownStore {
	return newRedisKKAIPolicyCooldownStore(client)
}

func newRedisKKAIPolicyCooldownStore(client kkaiPolicyRedisClient) *redisKKAIPolicyCooldownStore {
	return &redisKKAIPolicyCooldownStore{client: client, now: time.Now}
}

func (s *redisKKAIPolicyCooldownStore) Check(ctx context.Context, key string) (KKAIPolicyCooldownState, error) {
	return s.eval(ctx, key, "check", "", true)
}

func (s *redisKKAIPolicyCooldownStore) RecordCyber(ctx context.Context, key string, eventID string, stable bool) (KKAIPolicyCooldownState, error) {
	return s.eval(ctx, key, "cyber", eventID, stable)
}

func (s *redisKKAIPolicyCooldownStore) RecordKeyword(ctx context.Context, key string, eventID string, stable bool) (KKAIPolicyCooldownState, error) {
	return s.eval(ctx, key, "keyword", eventID, stable)
}

func (s *redisKKAIPolicyCooldownStore) eval(ctx context.Context, key string, operation string, eventID string, stable bool) (KKAIPolicyCooldownState, error) {
	if s == nil || s.client == nil {
		return KKAIPolicyCooldownState{}, ErrKKAIPolicyCooldownUnavailable
	}
	if key == "" {
		return KKAIPolicyCooldownState{}, fmt.Errorf("%w: empty scope key", ErrKKAIPolicyCooldownUnavailable)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	result, err := s.client.Eval(ctx, kkaiConversationPolicyLua, []string{key},
		operation,
		now.Unix(),
		eventID,
		boolToLua(stable),
		kkaiPolicyKeywordThreshold,
		int(kkaiPolicyKeywordWindow/time.Second),
	).Result()
	if err != nil {
		return KKAIPolicyCooldownState{}, err
	}
	return parseKKAIPolicyCooldownResult(result)
}

func parseKKAIPolicyCooldownResult(value any) (KKAIPolicyCooldownState, error) {
	values, ok := value.([]interface{})
	if !ok || len(values) < 5 {
		return KKAIPolicyCooldownState{}, fmt.Errorf("invalid cooldown result: %T", value)
	}
	blocked, err := redisResultInt64(values[0])
	if err != nil {
		return KKAIPolicyCooldownState{}, err
	}
	retryAfter, err := redisResultInt64(values[1])
	if err != nil {
		return KKAIPolicyCooldownState{}, err
	}
	strike, err := redisResultInt64(values[2])
	if err != nil {
		return KKAIPolicyCooldownState{}, err
	}
	blockedUntil, err := redisResultInt64(values[3])
	if err != nil {
		return KKAIPolicyCooldownState{}, err
	}
	keywordHits, err := redisResultInt64(values[4])
	if err != nil {
		return KKAIPolicyCooldownState{}, err
	}
	return KKAIPolicyCooldownState{
		Blocked:       blocked != 0,
		RetryAfter:    maxInt(int(retryAfter)),
		Strike:        maxInt(int(strike)),
		BlockedUntil:  blockedUntil,
		KeywordHits:   maxInt(int(keywordHits)),
	}, nil
}

func redisResultInt64(value any) (int64, error) {
	switch number := value.(type) {
	case int:
		return int64(number), nil
	case int64:
		return number, nil
	case uint64:
		if number > uint64(^uint64(0)>>1) {
			return 0, errors.New("redis integer overflow")
		}
		return int64(number), nil
	case string:
		return strconv.ParseInt(number, 10, 64)
	default:
		return 0, fmt.Errorf("invalid redis integer: %T", value)
	}
}

func boolToLua(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func maxInt(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

const kkaiConversationPolicyLua = `
local key = KEYS[1]
local operation = ARGV[1]
local now = tonumber(ARGV[2])
local event_id = ARGV[3] or ""
local stable = ARGV[4] == "1"
local keyword_threshold = tonumber(ARGV[5]) or 3
local keyword_window = tonumber(ARGV[6]) or 600

local blocked_until = tonumber(redis.call("HGET", key, "blocked_until") or "0")
local strikes = tonumber(redis.call("HGET", key, "strikes") or "0")
local last_violation = tonumber(redis.call("HGET", key, "last_violation") or "0")
local last_event_id = redis.call("HGET", key, "last_event_id") or ""

local function current_state(keyword_hits)
    if blocked_until > now then
        return {1, blocked_until - now, strikes, blocked_until, keyword_hits or 0}
    end
    return {0, 0, strikes, blocked_until, keyword_hits or 0}
end

if operation == "check" then
    local keyword_hits = tonumber(redis.call("HGET", key, "keyword_hits") or "0")
    return current_state(keyword_hits)
end

if event_id ~= "" and last_event_id == event_id then
    return current_state(0)
end

if blocked_until > now then
    return current_state(0)
end

if last_violation > 0 and now - last_violation >= 86400 then
    strikes = 0
end

local duration = 60
if stable then
    strikes = math.min(strikes + 1, 7)
    duration = math.min(60 * (2 ^ (strikes - 1)), 3600)
else
    strikes = 0
end

if operation == "cyber" then
    blocked_until = now + duration
    redis.call("HSET", key,
        "strikes", strikes,
        "blocked_until", blocked_until,
        "last_violation", now,
        "last_event_id", event_id,
        "keyword_hits", 0,
        "keyword_window_until", 0)
    redis.call("EXPIRE", key, 86400)
    return {1, duration, strikes, blocked_until, 0}
end

if operation == "keyword" then
    local keyword_hits = tonumber(redis.call("HGET", key, "keyword_hits") or "0")
    local keyword_window_until = tonumber(redis.call("HGET", key, "keyword_window_until") or "0")
    if keyword_window_until <= now then
        keyword_hits = 0
        keyword_window_until = now + keyword_window
    end
    keyword_hits = keyword_hits + 1
    if keyword_hits < keyword_threshold then
        redis.call("HSET", key,
            "keyword_hits", keyword_hits,
            "keyword_window_until", keyword_window_until,
            "last_event_id", event_id)
        redis.call("EXPIRE", key, 86400)
        return {0, 0, strikes, blocked_until, keyword_hits}
    end

    blocked_until = now + duration
    redis.call("HSET", key,
        "strikes", strikes,
        "blocked_until", blocked_until,
        "last_violation", now,
        "last_event_id", event_id,
        "keyword_hits", 0,
        "keyword_window_until", 0)
    redis.call("EXPIRE", key, 86400)
    return {1, duration, strikes, blocked_until, 0}
end

return current_state(0)
`
