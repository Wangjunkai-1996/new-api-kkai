package model

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// User auth cache fencing uses three Redis keys per user: the cached user
// hash, a short-lived pending fence published before a restrictive database
// transaction, and a monotonic committed version floor published after
// commit. Cache writes below either floor are rejected, readers below the
// effective floor fall back to the database, and the pending fence outlives
// every user-hash TTL so a rolled-back transaction heals without allowing a
// stale snapshot to re-authorize the user.

var ErrUserAuthCachePending = errors.New("user authentication state update is pending")

var ErrUserAuthVersionConflict = errors.New("user authentication version update conflicted")

func getUserAuthFenceKey(userId int) string {
	return fmt.Sprintf("auth:user:fence:%d", userId)
}

func getUserAuthVersionKey(userId int) string {
	return fmt.Sprintf("auth:user:version:%d", userId)
}

// A pending fence only covers the interval between publishing the next
// version and the surrounding database transaction reaching a decision. Its
// TTL must outlive every user hash that could have been populated before the
// fence, while still allowing an automatically rolled-back transaction to
// recover without an operator repairing Redis.
func userAuthFenceTTLSeconds() int {
	cacheTTL := userCacheTTLSeconds()
	extra := cacheTTL
	if extra < 60 {
		extra = 60
	}
	return cacheTTL + extra
}

type userCacheMetadata struct {
	Id             int
	TenantID       int64
	Group          string
	Email          string
	Status         int
	Role           int
	Username       string
	Setting        string
	AuthVersion    int64
	CacheSchema    int
	CacheExpiresAt int64
}

func userCacheAuthMetadata(user *UserBase) userCacheMetadata {
	return userCacheMetadata{
		Id: user.Id, TenantID: user.TenantID, Group: user.Group, Email: user.Email,
		Status: user.Status, Role: user.Role, Username: user.Username, Setting: user.Setting,
		AuthVersion: user.AuthVersion, CacheSchema: user.CacheSchema, CacheExpiresAt: user.CacheExpiresAt,
	}
}

// Redis failure before observing a cache retains the existing database-only
// fallback. Once a repair has observed Redis, its version floor is mandatory.
func readUserCacheFromDatabase(userId int, requireFloor bool) (*UserBase, error) {
	var user User
	if err := DB.Select("id", "tenant_id", "group", "email", "quota", "status", "role", "username", "setting", "auth_version").
		Where("id = ?", userId).First(&user).Error; err != nil {
		return nil, err
	}
	if user.AuthVersion <= 0 || user.TenantID < 0 {
		return nil, errAuthCacheIntegrity
	}
	floor, floorErr := getUserAuthVersionFloor(userId)
	if requireFloor && floorErr != nil {
		return nil, floorErr
	}
	if floorErr == nil && floor > user.AuthVersion {
		return nil, ErrUserAuthCachePending
	}
	return user.ToBaseUser(), nil
}

// Observe Redis before reading the database: old writers can modify metadata
// without changing either AuthVersion or an existing MAC. After publication,
// re-read the full metadata so a committed change whose cache writer is delayed
// cannot leave a newly signed stale snapshot behind.
func refreshUserCacheFromDatabase(userId int, includeQuota bool) (*UserBase, error) {
	if !common.RedisEnabled || common.RDB == nil {
		return readUserCacheFromDatabase(userId, false)
	}
	key := getUserCacheKey(userId)
	for attempt := range 3 {
		observed, err := common.RDB.HGetAll(context.Background(), key).Result()
		if err != nil {
			if !includeQuota || attempt > 0 {
				return nil, err
			}
			return readUserCacheFromDatabase(userId, false)
		}
		user, err := readUserCacheFromDatabase(userId, true)
		if err != nil {
			return nil, err
		}
		if err = writeUserCache(user, includeQuota, observed); err != nil {
			if errors.Is(err, errAuthCacheSnapshotChanged) {
				continue
			}
			if errors.Is(err, ErrUserAuthCachePending) || errors.Is(err, errAuthCacheUnsafeCounters) || errors.Is(err, errAuthCacheIntegrity) {
				return nil, err
			}
			// A transport error can arrive after Redis executed the script.
			// Revoke that possible publication and reject this request: its
			// post-read revocation guard could not be confirmed.
			if discardErr := discardAuthCacheSignature(key, user.CacheMAC); discardErr != nil {
				common.SysLog("failed to discard unconfirmed user cache signature: " + discardErr.Error())
			}
			return nil, err
		}

		verified, verifyErr := readUserCacheFromDatabase(userId, true)
		if verifyErr == nil {
			verified.CacheExpiresAt = user.CacheExpiresAt
			if userCacheAuthMetadata(verified) == userCacheAuthMetadata(user) {
				return user, nil
			}
		}
		if err := discardAuthCacheSignature(key, user.CacheMAC); err != nil {
			return nil, fmt.Errorf("discard unconfirmed user cache signature: %w", err)
		}
		if verifyErr != nil {
			return nil, verifyErr
		}
	}
	// Stop publishing a busy row. This request gets a fresh authoritative read
	// and the current version floor, without renewing an invalid cache lease.
	return readUserCacheFromDatabase(userId, true)
}

func writeUserCache(user *UserBase, includeQuota bool, observed map[string]string) error {
	if user == nil || user.Id <= 0 || user.AuthVersion <= 0 || user.TenantID < 0 {
		return errAuthCacheIntegrity
	}
	user.CacheSchema = userCacheSchemaVersion
	user.CacheExpiresAt = time.Now().Add(time.Duration(userCacheTTLSeconds()) * time.Second).UnixMilli()
	var err error
	user.CacheMAC, err = authCacheMAC(getUserCacheKey(user.Id), userCacheAuthMetadata(user))
	if err != nil {
		return err
	}
	observedJSON, err := common.Marshal(observed)
	if err != nil {
		return err
	}
	includeQuotaArg := "0"
	if includeQuota {
		includeQuotaArg = "1"
	}
	const script = authCacheCASLua + `
if not authCacheMatches(ARGV[15], {Quota = true}) then
  return {-1, ''}
end
local incoming = tonumber(ARGV[1])
local pending = tonumber(redis.call('GET', KEYS[2]) or '0')
local committed = tonumber(redis.call('GET', KEYS[3]) or '0')
local current = tonumber(redis.call('HGET', KEYS[1], 'AuthVersion') or '0') or 0
if pending == nil or committed == nil then
  return {-3, ''}
end
if pending > incoming or committed > incoming or current > incoming then
  return {0, ''}
end
local exists = redis.call('EXISTS', KEYS[1]) == 1
if exists then
  if redis.call('HGET', KEYS[1], 'Id') ~= ARGV[2]
    or redis.call('HGET', KEYS[1], 'TenantID') ~= ARGV[3]
    or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
    return {-2, ''}
  end
  -- Redis validates its own canonical signed-int64 format without converting
  -- the live balance through Lua's floating-point number representation.
  local checked = redis.pcall('HINCRBY', KEYS[1], 'Quota', '0')
  if type(checked) == 'table' and checked.err then
    return {-2, ''}
  end
end
if committed < incoming then
  redis.call('SET', KEYS[3], ARGV[1])
end
if pending > 0 and pending <= incoming then
  redis.call('DEL', KEYS[2])
end
if not exists and ARGV[11] == '0' then
  return {1, ''}
end
redis.call('HSET', KEYS[1],
  'Id', ARGV[2], 'TenantID', ARGV[3], 'Group', ARGV[4], 'Email', ARGV[5],
  'Status', ARGV[6], 'Role', ARGV[7], 'Username', ARGV[8],
  'Setting', ARGV[9], 'AuthVersion', ARGV[1], 'CacheSchema', ARGV[10],
  'CacheExpiresAt', ARGV[13], 'CacheMAC', ARGV[14])
if not exists then
  redis.call('HSET', KEYS[1], 'Quota', ARGV[12])
  redis.call('PEXPIREAT', KEYS[1], ARGV[13])
end
-- Existing hashes keep their lease. A failed database recheck must not
-- extend the lifetime of metadata whose signature is about to be discarded.
return {1, redis.call('HGET', KEYS[1], 'Quota')}`
	result, err := common.RDB.Eval(context.Background(), script,
		[]string{getUserCacheKey(user.Id), getUserAuthFenceKey(user.Id), getUserAuthVersionKey(user.Id)},
		user.AuthVersion, user.Id, user.TenantID, user.Group, user.Email, user.Status, user.Role,
		user.Username, user.Setting, user.CacheSchema, includeQuotaArg, user.Quota,
		user.CacheExpiresAt, user.CacheMAC, string(observedJSON),
	).Slice()
	if err != nil {
		return err
	}
	if len(result) != 2 {
		return fmt.Errorf("invalid user cache refresh result")
	}
	switch result[0] {
	case int64(0):
		return ErrUserAuthCachePending
	case int64(-1):
		return errAuthCacheSnapshotChanged
	case int64(-2):
		return errAuthCacheUnsafeCounters
	case int64(-3):
		return errAuthCacheIntegrity
	case int64(1):
		if quota, ok := result[1].(string); ok && quota != "" {
			user.Quota, err = strconv.ParseInt(quota, 10, 64)
			return err
		}
		return nil
	default:
		return fmt.Errorf("invalid user cache refresh result")
	}
}

func getUserAuthVersionFloor(userId int) (int64, error) {
	if !common.RedisEnabled || common.RDB == nil {
		return 0, nil
	}
	values, err := common.RDB.MGet(context.Background(), getUserAuthFenceKey(userId), getUserAuthVersionKey(userId)).Result()
	if err != nil {
		return 0, err
	}
	var floor int64
	for _, value := range values {
		if value == nil {
			continue
		}
		parsed, err := strconv.ParseInt(fmt.Sprint(value), 10, 64)
		if err != nil {
			return 0, err
		}
		if parsed > floor {
			floor = parsed
		}
	}
	return floor, nil
}

// SetUserAuthVersionFence publishes a fail-closed version before a restrictive
// database update. Pending fences expire only after every pre-existing user
// hash must have expired; a committed update is promoted separately to a
// permanent monotonic version floor.
func SetUserAuthVersionFence(userId int, authVersion int64) error {
	if !common.RedisEnabled {
		return nil
	}
	if userId <= 0 || authVersion <= 0 {
		return fmt.Errorf("invalid user auth fence")
	}
	const script = `
local current = tonumber(redis.call('GET', KEYS[1]) or '0')
local incoming = tonumber(ARGV[1])
if current < incoming then
  redis.call('SET', KEYS[1], ARGV[1], 'EX', ARGV[2])
elseif current == incoming then
  redis.call('EXPIRE', KEYS[1], ARGV[2])
elseif redis.call('TTL', KEYS[1]) < 0 then
  redis.call('EXPIRE', KEYS[1], ARGV[2])
end
return 1`
	return common.RDB.Eval(context.Background(), script, []string{getUserAuthFenceKey(userId)}, authVersion, userAuthFenceTTLSeconds()).Err()
}

// publishCommittedUserAuthVersion records the durable lower bound used to
// reject an arbitrarily delayed cache fill after a committed security change.
// It also removes this transaction's now-obsolete pending fence.
func publishCommittedUserAuthVersion(userId int, authVersion int64) error {
	if !common.RedisEnabled {
		return nil
	}
	if userId <= 0 || authVersion <= 0 {
		return fmt.Errorf("invalid committed user auth version")
	}
	const script = `
local incoming = tonumber(ARGV[1])
local committed = tonumber(redis.call('GET', KEYS[1]) or '0')
local pending = tonumber(redis.call('GET', KEYS[2]) or '0')
if committed < incoming then
  redis.call('SET', KEYS[1], ARGV[1])
end
if pending > 0 and pending <= incoming then
  redis.call('DEL', KEYS[2])
end
return 1`
	return common.RDB.Eval(context.Background(), script,
		[]string{getUserAuthVersionKey(userId), getUserAuthFenceKey(userId)}, authVersion,
	).Err()
}

// IncrementUserAuthVersionWithTx locks the user, publishes the next deny
// fence, then persists the version in the caller's transaction. Unscoped is
// intentional so the same fail-closed path also covers hard deletion of an
// already soft-deleted user.
func IncrementUserAuthVersionWithTx(tx *gorm.DB, userId int) (int64, error) {
	if tx == nil || userId <= 0 {
		return 0, fmt.Errorf("invalid user auth version update")
	}
	for range 3 {
		var user User
		if err := lockForUpdate(tx.Unscoped()).Select("id", "auth_version").Where("id = ?", userId).First(&user).Error; err != nil {
			return 0, err
		}
		current := user.AuthVersion
		if current < 1 {
			current = 1
		}
		next := current + 1
		if err := SetUserAuthVersionFence(userId, next); err != nil {
			return 0, err
		}
		result := tx.Unscoped().Model(&User{}).
			Where("id = ? AND auth_version = ?", userId, user.AuthVersion).
			Update("auth_version", next)
		if result.Error != nil {
			return 0, result.Error
		}
		if result.RowsAffected == 1 {
			return next, nil
		}
	}
	return 0, ErrUserAuthVersionConflict
}

// BumpUserAuthVersion is the transaction-owning variant used by password,
// role, status and security-factor changes outside another transaction.
func BumpUserAuthVersion(userId int) (int64, error) {
	var next int64
	if err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		next, err = IncrementUserAuthVersionWithTx(tx, userId)
		return err
	}); err != nil {
		return 0, err
	}
	if err := PublishUserAuthCache(userId); err != nil {
		return next, err
	}
	return next, nil
}

// PublishUserAuthCache refreshes the current database state after a successful
// auth-sensitive transaction without touching the cached quota field.
func PublishUserAuthCache(userId int) error {
	_, err := refreshUserCacheFromDatabase(userId, false)
	return err
}

// InitializeUserAuthVersions must run after AutoMigrate when upgrading an
// existing database. It is idempotent and portable across all supported DBs.
func InitializeUserAuthVersions() error {
	return DB.Model(&User{}).Where("auth_version IS NULL OR auth_version < ?", 1).Update("auth_version", 1).Error
}

func updateUserCacheFieldAtVersion(userId int, field string, value interface{}, authVersion int64) error {
	if !common.RedisEnabled {
		return nil
	}
	if field != "Quota" {
		_, err := refreshUserCacheFromDatabase(userId, false)
		return err
	}
	if userId <= 0 || authVersion <= 0 {
		return fmt.Errorf("invalid user auth version")
	}
	const script = `
local incoming = tonumber(ARGV[1])
local pending = tonumber(redis.call('GET', KEYS[2]) or '0')
local committed = tonumber(redis.call('GET', KEYS[3]) or '0')
local current = tonumber(redis.call('HGET', KEYS[1], 'AuthVersion') or '0')
if pending > incoming or committed > incoming or current > incoming then
  return 0
end
if committed < incoming then
  redis.call('SET', KEYS[3], ARGV[1])
end
if pending > 0 and pending <= incoming then
  redis.call('DEL', KEYS[2])
end
if redis.call('EXISTS', KEYS[1]) == 0 then
  return 1
end
if current ~= incoming then
  return 1
end
redis.call('HSET', KEYS[1], ARGV[2], ARGV[3], 'CacheSchema', ARGV[4])
return 1`
	result, err := common.RDB.Eval(context.Background(), script,
		[]string{getUserCacheKey(userId), getUserAuthFenceKey(userId), getUserAuthVersionKey(userId)},
		authVersion, field, value, userCacheSchemaVersion,
	).Int()
	if err != nil {
		return err
	}
	if result == 0 {
		return ErrUserAuthCachePending
	}
	return nil
}
