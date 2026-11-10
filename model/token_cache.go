package model

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// Quota and activity counters have their own atomic writers. TenantID and
// AutoGroups are explicit because their JSON tags intentionally hide them.
func (token Token) cacheMAC() (string, error) {
	key := getTokenCacheKey(token.Key)
	token.Key = ""
	token.RemainQuota, token.UsedQuota, token.AccessedTime = 0, 0, 0
	if token.AllowIps != nil && *token.AllowIps == "" {
		token.AllowIps = nil
	}
	return authCacheMAC(key, struct {
		Token          Token
		TenantID       int64
		AutoGroups     string
		CacheExpiresAt int64
	}{token, token.TenantID, token.AutoGroups, token.CacheExpiresAt})
}

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
	return invalidateTokenCacheForMutationContext(context.Background(), key)
}

func invalidateTokenCacheForMutationContext(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !common.RedisEnabled || key == "" {
		return nil
	}
	const script = `
redis.call('SET', KEYS[2], 1, 'EX', ARGV[1])
redis.call('DEL', KEYS[1], KEYS[3])
return 1`
	return common.RDB.Eval(ctx, script, []string{
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

// cacheInitToken is a cold fill only. Repairing a live hash requires an
// observation taken BEFORE the database read, via repairTokenCache.
func cacheInitToken(token Token, generation string) (int, error) {
	return repairTokenCache(token, generation, nil)
}

// repairTokenCache publishes trusted metadata without overwriting live quota.
// A nil observation permits cold initialization only; it never repairs a hash
// read after the database snapshot. Return 0 when fenced, 1 for cold fill, 2
// for an existing hash. A metadata conflict requires a fresh database read.
func repairTokenCache(token Token, generation string, observed map[string]string) (int, error) {
	if !common.RedisEnabled || generation == "" {
		return 0, nil
	}
	if token.Id <= 0 || token.UserId <= 0 || token.TenantID < 0 || token.Key == "" || token.DeletedAt.Valid {
		return 0, errAuthCacheIntegrity
	}
	allowIps := ""
	if token.AllowIps != nil {
		allowIps = *token.AllowIps
	}
	repair := observed != nil
	if observed == nil {
		observed = map[string]string{}
	}
	snapshot, err := common.Marshal(observed)
	if err != nil {
		return 0, err
	}
	token.CacheExpiresAt = time.Now().Add(time.Duration(tokenCacheTTLSeconds()) * time.Second).UnixMilli()
	mac, err := token.cacheMAC()
	if err != nil {
		return 0, err
	}
	const script = authCacheCASLua + `
if redis.call('EXISTS', KEYS[2]) == 1 or redis.call('GET', KEYS[3]) ~= ARGV[16] then
  return 0
end
local exists = redis.call('EXISTS', KEYS[1]) == 1
if exists and ARGV[22] == 'false' then
  return 2
end
if not authCacheMatches(ARGV[21], {RemainQuota=true, UsedQuota=true, AccessedTime=true}) then
  return -1
end
if exists then
  if redis.call('HGET', KEYS[1], 'Id') ~= ARGV[1]
    or redis.call('HGET', KEYS[1], 'UserId') ~= ARGV[2]
    or redis.call('HGET', KEYS[1], 'TenantID') ~= ARGV[18] then
    return -2
  end
  for _, field in ipairs({'RemainQuota', 'UsedQuota', 'AccessedTime'}) do
    if redis.call('HEXISTS', KEYS[1], field) == 0 then
      return -2
    end
    local checked = redis.pcall('HINCRBY', KEYS[1], field, '0')
    if type(checked) == 'table' and checked.err then
      return -2
    end
  end
end
redis.call('HDEL', KEYS[1], 'DeletedAt', 'Key')
redis.call('HSET', KEYS[1],
  'Id', ARGV[1], 'UserId', ARGV[2], 'TenantID', ARGV[18], 'Status', ARGV[3], 'Name', ARGV[4],
  'CreatedTime', ARGV[5], 'ExpiredTime', ARGV[7],
  'UnlimitedQuota', ARGV[8], 'ModelLimitsEnabled', ARGV[9], 'ModelLimits', ARGV[10],
  'AllowIps', ARGV[11], 'Group', ARGV[12], 'CrossGroupRetry', ARGV[13],
  'AllowUserQuotaQuery', ARGV[23], 'AutoGroups', ARGV[17], 'CacheExpiresAt', ARGV[19], 'CacheMAC', ARGV[20])
if not exists then
  redis.call('HSET', KEYS[1], 'RemainQuota', ARGV[14], 'UsedQuota', ARGV[15], 'AccessedTime', ARGV[6])
end
redis.call('PEXPIREAT', KEYS[1], ARGV[19])
if exists then
  return 2
end
return 1`
	result, err := common.RDB.Eval(context.Background(), script, []string{
		getTokenCacheKey(token.Key), getTokenCacheFenceKey(token.Key), getTokenCacheGenerationKey(token.Key),
	},
		token.Id, token.UserId, token.Status, token.Name,
		token.CreatedTime, token.AccessedTime, token.ExpiredTime,
		strconv.FormatBool(token.UnlimitedQuota), strconv.FormatBool(token.ModelLimitsEnabled),
		token.ModelLimits, allowIps, token.Group, strconv.FormatBool(token.CrossGroupRetry),
		token.RemainQuota, token.UsedQuota,
		generation, token.AutoGroups, token.TenantID,
		token.CacheExpiresAt, mac, string(snapshot), strconv.FormatBool(repair), strconv.FormatBool(token.AllowUserQuotaQuery),
	).Int()
	if err != nil {
		return 0, err
	}
	switch result {
	case -1:
		return 0, errAuthCacheSnapshotChanged
	case -2:
		return 0, errAuthCacheUnsafeCounters
	default:
		return result, nil
	}
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
	token := Token{AutoGroups: missingAutoGroups, TenantID: -1}
	if err := common.RedisHGetObj(getTokenCacheKey(key), &token, "RemainQuota", "UsedQuota", "AccessedTime"); err != nil {
		return nil, err
	}
	if token.Id <= 0 || token.UserId <= 0 || token.AutoGroups == missingAutoGroups || token.TenantID < 0 {
		return nil, fmt.Errorf("token cache is incomplete")
	}
	token.Key = key
	mac, err := token.cacheMAC()
	if err != nil || !validAuthCacheMAC(mac, token.CacheMAC, token.CacheExpiresAt) {
		return nil, errAuthCacheIntegrity
	}
	return &token, nil
}
