package model

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
)

func getTokenCacheKey(key string) string {
	return fmt.Sprintf("token:%s", common.GenerateHMAC(key))
}

func getTokenCacheFenceKey(key string) string {
	return fmt.Sprintf("token:fence:%s", common.GenerateHMAC(key))
}

func getTokenCacheGenerationKey(key string) string {
	return fmt.Sprintf("token:generation:%s", common.GenerateHMAC(key))
}

func tokenCacheTTLSeconds() int {
	ttl := common.RedisKeyCacheSeconds()
	if ttl <= 0 {
		return 60
	}
	return ttl
}

// The fence covers the metadata database write. Read generations additionally
// reject snapshots held beyond this window, without retaining permanent keys.
// While the fence exists readers serve the database without caching.
const tokenCacheFenceSeconds = 10

var errTokenCacheSnapshotStale = errors.New("token cache snapshot changed; retry reconciliation")

// beginTokenCacheRead must precede the database read. Expiration or invalidation
// revokes the generation, so even a delayed reader cannot republish old state.
func beginTokenCacheRead(key string) (string, error) {
	if !common.RedisEnabled {
		return "", nil
	}
	const script = `
if redis.call('EXISTS', KEYS[2]) == 1 then
  return ''
end
local generation = redis.call('GET', KEYS[1])
if generation then
  return generation
end
redis.call('SET', KEYS[1], ARGV[1], 'EX', ARGV[2])
return ARGV[1]`
	return common.RDB.Eval(context.Background(), script, []string{
		getTokenCacheGenerationKey(key), getTokenCacheFenceKey(key),
	}, common.GetUUID(), tokenCacheFenceSeconds).Text()
}

// invalidateTokenCacheForMutation is called before a token metadata mutation
// writes to the database: it raises the fence and drops the cached hash so no
// reader can act on (or re-publish) the pre-mutation state.
func invalidateTokenCacheForMutation(key string) error {
	if !common.RedisEnabled || key == "" {
		return nil
	}
	const script = `
redis.call('SET', KEYS[2], 1, 'EX', ARGV[1])
redis.call('DEL', KEYS[1], KEYS[3])
return 1`
	return common.RDB.Eval(context.Background(), script, []string{
		getTokenCacheKey(key), getTokenCacheFenceKey(key), getTokenCacheGenerationKey(key),
	}, tokenCacheFenceSeconds).Err()
}

func invalidateTokensCache(tokens []Token) error {
	var firstErr error
	for _, token := range tokens {
		if err := invalidateTokenCacheForMutation(token.Key); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// A second invalidation after commit closes the window when a database write
// outlives the pre-write fence. Do not hide an already committed mutation if
// Redis fails while revoking snapshots from that window.
func invalidateTokenCacheAfterMutation(key string, mutationErr error) error {
	if mutationErr != nil {
		return mutationErr
	}
	if err := invalidateTokenCacheForMutation(key); err != nil {
		return fmt.Errorf("token database mutation committed but cache invalidation failed: %w", err)
	}
	return nil
}

func cacheDeleteToken(key string) error {
	return invalidateTokenCacheForMutation(key)
}

// cacheSetToken reconciles committed quota only. It must never publish token
// authorization fields or recreate a revoked/expired hash from a stale snapshot.
// Invalidating read generations also prevents a concurrent cold reader from
// publishing quota read before the settlement committed.
func cacheSetToken(token Token, generation string) error {
	if !common.RedisEnabled {
		return nil
	}
	if generation == "" {
		return errTokenCacheSnapshotStale
	}
	const script = `
if redis.call('EXISTS', KEYS[2]) == 1 or redis.call('GET', KEYS[3]) ~= ARGV[6] then
  return -1
end
redis.call('DEL', KEYS[3])
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[1])
  or redis.call('HEXISTS', KEYS[1], 'RemainQuota') == 0
  or redis.call('HEXISTS', KEYS[1], 'UsedQuota') == 0 then
  return 0
end
redis.call('HSET', KEYS[1], 'RemainQuota', ARGV[2], 'UsedQuota', ARGV[3], 'AccessedTime', ARGV[4])
redis.call('EXPIRE', KEYS[1], ARGV[5])
return 1`
	result, err := common.RDB.Eval(context.Background(), script, []string{
		getTokenCacheKey(token.Key), getTokenCacheFenceKey(token.Key), getTokenCacheGenerationKey(token.Key),
	}, token.Id, token.RemainQuota, token.UsedQuota, token.AccessedTime, tokenCacheTTLSeconds(), generation).Int()
	if err != nil {
		return err
	}
	if result == -1 {
		return errTokenCacheSnapshotStale
	}
	return nil
}

// cacheInitToken publishes a database snapshot only when no mutation fence is
// active and the hash is cold. An existing hash only gets its TTL refreshed
// and missing AutoGroups metadata backfilled during the cache format upgrade:
// its RemainQuota may already be ahead of this snapshot because atomic
// pre-consume decrements Redis first, so a snapshot must never overwrite any
// field of a live hash.
// 返回值：0=被 fence 拦截，1=完成初始化，2=哈希已存在，保留额度和已有元数据。
func cacheInitToken(token Token, generation string) (int, error) {
	if !common.RedisEnabled || generation == "" {
		return 0, nil
	}
	allowIps := ""
	if token.AllowIps != nil {
		allowIps = *token.AllowIps
	}
	const script = `
if redis.call('EXISTS', KEYS[2]) == 1 or redis.call('GET', KEYS[3]) ~= ARGV[17] then
  return 0
end
if redis.call('EXISTS', KEYS[1]) == 1 then
  if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') == tonumber(ARGV[1]) then
    redis.call('HSETNX', KEYS[1], 'AutoGroups', ARGV[18])
  end
  redis.call('EXPIRE', KEYS[1], ARGV[16])
  return 2
end
redis.call('HSET', KEYS[1],
  'Id', ARGV[1], 'UserId', ARGV[2], 'Status', ARGV[3], 'Name', ARGV[4],
  'CreatedTime', ARGV[5], 'AccessedTime', ARGV[6], 'ExpiredTime', ARGV[7],
  'UnlimitedQuota', ARGV[8], 'ModelLimitsEnabled', ARGV[9], 'ModelLimits', ARGV[10],
  'AllowIps', ARGV[11], 'Group', ARGV[12], 'CrossGroupRetry', ARGV[13],
  'RemainQuota', ARGV[14], 'UsedQuota', ARGV[15], 'AutoGroups', ARGV[18])
redis.call('EXPIRE', KEYS[1], ARGV[16])
return 1`

	return common.RDB.Eval(context.Background(), script, []string{
		getTokenCacheKey(token.Key), getTokenCacheFenceKey(token.Key), getTokenCacheGenerationKey(token.Key),
	},
		token.Id, token.UserId, token.Status, token.Name,
		token.CreatedTime, token.AccessedTime, token.ExpiredTime,
		strconv.FormatBool(token.UnlimitedQuota), strconv.FormatBool(token.ModelLimitsEnabled),
		token.ModelLimits, allowIps, token.Group, strconv.FormatBool(token.CrossGroupRetry),
		token.RemainQuota, token.UsedQuota,
		tokenCacheTTLSeconds(), generation, token.AutoGroups,
	).Int()
}

// cacheGetTokenByKey 从缓存读取 token；不完整的哈希（如仅有配额字段）会被拒绝。
func cacheGetTokenByKey(key string) (*Token, error) {
	if !common.RedisEnabled {
		return nil, fmt.Errorf("redis is not enabled")
	}
	// AutoGroups may legitimately be empty. The decoder leaves absent fields
	// untouched, so this non-JSON sentinel identifies hashes from older builds
	// without another Redis read or discarding their in-flight quota deltas.
	const missingAutoGroups = "\x00"
	token := Token{AutoGroups: missingAutoGroups}
	if err := common.RedisHGetObj(getTokenCacheKey(key), &token); err != nil {
		return nil, err
	}
	if token.Id <= 0 || token.AutoGroups == missingAutoGroups {
		return nil, fmt.Errorf("token cache is incomplete")
	}
	token.Key = key
	return &token, nil
}
