package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

var (
	ErrKKAIPolicyKeyCooldownUnavailable  = errors.New("KKAI key cooldown store unavailable")
	ErrKKAIPolicyKeyCooldownInvalidEvent = errors.New("KKAI key cooldown event is invalid")
)

type kkaiPolicyKeyCooldownRedisClient interface {
	Eval(context.Context, string, []string, ...interface{}) *redis.Cmd
}

type redisKKAIPolicyKeyCooldownStore struct {
	client kkaiPolicyKeyCooldownRedisClient
}

func NewRedisKKAIPolicyKeyCooldownStore(client *redis.Client) KKAIPolicyKeyCooldownStore {
	return newRedisKKAIPolicyKeyCooldownStore(client)
}

func newRedisKKAIPolicyKeyCooldownStore(client kkaiPolicyKeyCooldownRedisClient) *redisKKAIPolicyKeyCooldownStore {
	return &redisKKAIPolicyKeyCooldownStore{client: client}
}

func (s *redisKKAIPolicyKeyCooldownStore) Check(ctx context.Context, key string) (KKAIPolicyKeyCooldownState, error) {
	return s.eval(ctx, key, "check", "")
}

func (s *redisKKAIPolicyKeyCooldownStore) Record(ctx context.Context, key string, eventDigest string) (KKAIPolicyKeyCooldownState, error) {
	if eventDigest == "" {
		return KKAIPolicyKeyCooldownState{}, ErrKKAIPolicyKeyCooldownInvalidEvent
	}
	return s.eval(ctx, key, "record", eventDigest)
}

func (s *redisKKAIPolicyKeyCooldownStore) eval(ctx context.Context, key string, operation string, eventDigest string) (KKAIPolicyKeyCooldownState, error) {
	if s == nil || s.client == nil || key == "" {
		return KKAIPolicyKeyCooldownState{}, ErrKKAIPolicyKeyCooldownUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := s.client.Eval(ctx, kkaiPolicyKeyCooldownLua, []string{key}, operation, eventDigest).Result()
	if err != nil {
		return KKAIPolicyKeyCooldownState{}, err
	}
	return parseKKAIPolicyKeyCooldownResult(result)
}

func parseKKAIPolicyKeyCooldownResult(value any) (KKAIPolicyKeyCooldownState, error) {
	values, ok := value.([]interface{})
	if !ok || len(values) != 4 {
		return KKAIPolicyKeyCooldownState{}, fmt.Errorf("invalid key cooldown result: %T", value)
	}
	blocked, err := kkaiPolicyRedisInt64(values[0])
	if err != nil {
		return KKAIPolicyKeyCooldownState{}, err
	}
	retryAfter, err := kkaiPolicyRedisInt64(values[1])
	if err != nil {
		return KKAIPolicyKeyCooldownState{}, err
	}
	strike, err := kkaiPolicyRedisInt64(values[2])
	if err != nil {
		return KKAIPolicyKeyCooldownState{}, err
	}
	blockedUntil, err := kkaiPolicyRedisInt64(values[3])
	if err != nil {
		return KKAIPolicyKeyCooldownState{}, err
	}
	if blocked != 0 && blocked != 1 {
		return KKAIPolicyKeyCooldownState{}, errors.New("invalid key cooldown blocked flag")
	}
	if retryAfter < 0 || retryAfter > kkaiPolicyKeyCooldownMaxSeconds {
		return KKAIPolicyKeyCooldownState{}, errors.New("invalid key cooldown retry-after")
	}
	if strike < 0 || strike > kkaiPolicyKeyCooldownMaxStrike {
		return KKAIPolicyKeyCooldownState{}, errors.New("invalid key cooldown strike")
	}
	if blockedUntil < 0 {
		return KKAIPolicyKeyCooldownState{}, errors.New("invalid key cooldown blocked-until")
	}
	if blocked == 1 && (retryAfter < 1 || strike < 1 || blockedUntil == 0) {
		return KKAIPolicyKeyCooldownState{}, errors.New("inconsistent blocked key cooldown state")
	}
	if blocked == 0 && retryAfter != 0 {
		return KKAIPolicyKeyCooldownState{}, errors.New("inconsistent unblocked key cooldown state")
	}
	return KKAIPolicyKeyCooldownState{
		Blocked:      blocked != 0,
		RetryAfter:   kkaiPolicyNonNegativeInt(retryAfter),
		Strike:       kkaiPolicyNonNegativeInt(strike),
		BlockedUntil: blockedUntil,
	}, nil
}

func kkaiPolicyRedisInt64(value any) (int64, error) {
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
	case []byte:
		return strconv.ParseInt(string(number), 10, 64)
	default:
		return 0, fmt.Errorf("invalid redis integer: %T", value)
	}
}

func kkaiPolicyNonNegativeInt(value int64) int {
	if value <= 0 {
		return 0
	}
	maxInt := int64(^uint(0) >> 1)
	if value > maxInt {
		return int(maxInt)
	}
	return int(value)
}

const kkaiPolicyKeyCooldownLua = `
local key = KEYS[1]
local operation = ARGV[1]
local event_digest = ARGV[2] or ""
local redis_time = redis.call("TIME")
local now = tonumber(redis_time[1]) * 1000 + math.floor(tonumber(redis_time[2]) / 1000)

local blocked_until = tonumber(redis.call("HGET", key, "blocked_until") or "0")
local strikes = tonumber(redis.call("HGET", key, "strikes") or "0")
local function current_state()
    if blocked_until > now then
        return {1, math.floor((blocked_until - now + 999) / 1000), strikes, blocked_until}
    end
    return {0, 0, strikes, blocked_until}
end

if operation == "check" then
    return current_state()
end

if operation ~= "record" or event_digest == "" then
    return redis.error_reply("invalid key cooldown operation")
end

local event_field = "event:" .. event_digest
if redis.call("HEXISTS", key, event_field) == 1 then
    return current_state()
end
redis.call("HSET", key, event_field, 1)

strikes = 1
local proposed_blocked_until = now + 60000
if proposed_blocked_until > blocked_until then
    blocked_until = proposed_blocked_until
end

redis.call("HSET", key,
    "strikes", strikes,
	"blocked_until", blocked_until)
local ttl = redis.call("TTL", key)
if ttl < 86400 then
    redis.call("EXPIRE", key, 86400)
end
return current_state()
`
