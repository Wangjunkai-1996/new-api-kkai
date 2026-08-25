package model

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
)

const userCacheSchemaVersion = 1

// UserBase struct remains the same as it represents the cached data structure
type UserBase struct {
	Id          int    `json:"id"`
	Group       string `json:"group"`
	Email       string `json:"email"`
	Quota       int64  `json:"quota"`
	Status      int    `json:"status"`
	Username    string `json:"username"`
	Setting     string `json:"setting"`
	CacheSchema int    `json:"-"`
}

func (user *UserBase) WriteContext(c *gin.Context) {
	common.SetContextKey(c, constant.ContextKeyUserGroup, user.Group)
	common.SetContextKey(c, constant.ContextKeyUserQuota, user.Quota)
	common.SetContextKey(c, constant.ContextKeyUserStatus, user.Status)
	common.SetContextKey(c, constant.ContextKeyUserEmail, user.Email)
	common.SetContextKey(c, constant.ContextKeyUserName, user.Username)
	common.SetContextKey(c, constant.ContextKeyUserSetting, user.GetSetting())
}

func (user *UserBase) GetSetting() dto.UserSetting {
	setting := dto.UserSetting{}
	if user.Setting != "" {
		err := common.Unmarshal([]byte(user.Setting), &setting)
		if err != nil {
			common.SysLog("failed to unmarshal setting: " + err.Error())
		}
	}
	return setting
}

// getUserCacheKey returns the key for user cache
func getUserCacheKey(userId int) string {
	return fmt.Sprintf("user:%d", userId)
}

func userCacheTTLSeconds() int {
	ttl := common.RedisKeyCacheSeconds()
	if ttl <= 0 {
		return 60
	}
	return ttl
}

// invalidateUserCache clears user cache
func invalidateUserCache(userId int) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisDelKey(getUserCacheKey(userId))
}

// InvalidateUserCache lets transactional callers invalidate a completed
// user's snapshot without exposing the cache key format.
func InvalidateUserCache(userId int) error {
	return invalidateUserCache(userId)
}

func populateUserCache(user User) error {
	if !common.RedisEnabled {
		return nil
	}
	return writeUserCache(user.ToBaseUser(), true)
}

// updateUserCache refreshes non-quota user cache fields.
// Quota is maintained by atomic quota delta paths and must not be overwritten
// by stale user snapshots from profile/settings updates.
func updateUserCache(user User) error {
	if !common.RedisEnabled {
		return nil
	}
	return writeUserCache(user.ToBaseUser(), false)
}

// writeUserCache creates a complete cache hash on a cold read. Refreshes of an
// existing hash never overwrite Quota because live reservations update it
// atomically ahead of the database snapshot.
func writeUserCache(user *UserBase, includeQuota bool) error {
	if user == nil || user.Id <= 0 || !common.RedisEnabled {
		return nil
	}
	user.CacheSchema = userCacheSchemaVersion
	includeQuotaArg := 0
	if includeQuota {
		includeQuotaArg = 1
	}
	const script = `
local include_quota = tonumber(ARGV[8]) == 1
local exists = redis.call('EXISTS', KEYS[1]) == 1
local schema = tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0')
if not include_quota and (not exists or schema ~= tonumber(ARGV[7])) then
  return 0
end
if include_quota and (not exists or schema ~= tonumber(ARGV[7])) then
  redis.call('DEL', KEYS[1])
  redis.call('HSET', KEYS[1],
    'Id', ARGV[1], 'Group', ARGV[2], 'Email', ARGV[3], 'Status', ARGV[4],
    'Username', ARGV[5], 'Setting', ARGV[6], 'CacheSchema', ARGV[7],
    'Quota', ARGV[9])
else
  redis.call('HSET', KEYS[1],
    'Id', ARGV[1], 'Group', ARGV[2], 'Email', ARGV[3], 'Status', ARGV[4],
    'Username', ARGV[5], 'Setting', ARGV[6], 'CacheSchema', ARGV[7])
  if include_quota and redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
    redis.call('HSET', KEYS[1], 'Quota', ARGV[9])
  end
end
redis.call('EXPIRE', KEYS[1], ARGV[10])
return 1`
	return common.RDB.Eval(context.Background(), script, []string{getUserCacheKey(user.Id)},
		user.Id, user.Group, user.Email, user.Status, user.Username, user.Setting,
		user.CacheSchema, includeQuotaArg, user.Quota, userCacheTTLSeconds(),
	).Err()
}

// GetUserCache gets complete user cache from hash
func GetUserCache(userId int) (userCache *UserBase, err error) {
	// Try getting from Redis first
	userCache, err = cacheGetUserBase(userId)
	if err == nil {
		return userCache, nil
	}

	// If Redis fails, get from DB
	user, err := GetUserById(userId, false)
	if err != nil {
		return nil, err // Return nil and error if DB lookup fails
	}
	userCache = user.ToBaseUser()
	if common.RedisEnabled {
		if cacheErr := populateUserCache(*user); cacheErr != nil {
			common.SysLog("failed to synchronously populate user cache: " + cacheErr.Error())
		}
	}
	return userCache, nil
}

func cacheGetUserBase(userId int) (*UserBase, error) {
	if !common.RedisEnabled {
		return nil, fmt.Errorf("redis is not enabled")
	}
	var userCache UserBase
	// Try getting from Redis first
	err := common.RedisHGetObj(getUserCacheKey(userId), &userCache)
	if err != nil {
		return nil, err
	}
	if userCache.Id != userId || userCache.CacheSchema != userCacheSchemaVersion {
		return nil, fmt.Errorf("user cache schema is stale")
	}
	return &userCache, nil
}

// Add atomic quota operations using hash fields.
// 通过守卫式 Lua 脚本执行：哈希不存在时直接跳过（下次读取会从数据库水合），
// 不会像裸 HINCRBY 那样创建只含 Quota 字段的残缺哈希。
func cacheIncrUserQuota(userId int, delta int64) error {
	if !common.RedisEnabled {
		return nil
	}
	_, err := cacheApplyUserQuotaDelta(userId, delta)
	return err
}

func cacheDecrUserQuota(userId int, delta int64) error {
	return cacheIncrUserQuota(userId, -delta)
}

// syncCreditUserQuotaCache 在授信事务（充值/兑换等）提交后同步把增量补进缓存
// 余额。预扣以缓存值为准（存在期间），授信不能绕过它，否则新到账的额度在
// 缓存过期前不可用；缓存未命中无需处理，下次读取会从已提交的数据库余额水合。
func syncCreditUserQuotaCache(userId int, quota int, operation string) {
	if quota <= 0 {
		return
	}
	if err := cacheIncrUserQuota(userId, int64(quota)); err != nil {
		common.SysLog(fmt.Sprintf("failed to sync %s credit to user quota cache: %s", operation, err.Error()))
	}
}

// Helper functions to get individual fields if needed
func getUserGroupCache(userId int) (string, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return "", err
	}
	return cache.Group, nil
}

func getUserQuotaCache(userId int) (int64, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return 0, err
	}
	return cache.Quota, nil
}

func getUserNameCache(userId int) (string, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return "", err
	}
	return cache.Username, nil
}

func getUserSettingCache(userId int) (dto.UserSetting, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return dto.UserSetting{}, err
	}
	return cache.GetSetting(), nil
}

// New functions for individual field updates
func updateUserStatusCache(userId int, status bool) error {
	statusInt := common.UserStatusEnabled
	if !status {
		statusInt = common.UserStatusDisabled
	}
	return updateUserCacheField(userId, "Status", statusInt)
}

func updateUserQuotaCache(userId int, quota int64) error {
	return updateUserCacheField(userId, "Quota", quota)
}

func updateUserGroupCache(userId int, group string) error {
	return updateUserCacheField(userId, "Group", group)
}

func UpdateUserGroupCache(userId int, group string) error {
	return updateUserGroupCache(userId, group)
}

func updateUserEmailCache(userId int, email string) error {
	return updateUserCacheField(userId, "Email", email)
}

func updateUserNameCache(userId int, username string) error {
	return updateUserCacheField(userId, "Username", username)
}

func updateUserSettingCache(userId int, setting string) error {
	return updateUserCacheField(userId, "Setting", setting)
}

// updateUserCacheField updates only a complete cache hash. It never creates a
// partial hash that could be mistaken for an authoritative quota snapshot.
func updateUserCacheField(userId int, field string, value interface{}) error {
	if !common.RedisEnabled {
		return nil
	}
	const script = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[1])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[4]) then
  return 0
end
redis.call('HSET', KEYS[1], ARGV[2], ARGV[3])
redis.call('EXPIRE', KEYS[1], ARGV[5])
return 1`
	return common.RDB.Eval(context.Background(), script, []string{getUserCacheKey(userId)},
		userId, field, value, userCacheSchemaVersion, userCacheTTLSeconds(),
	).Err()
}

// GetUserLanguage returns the user's language preference from cache
// Uses the existing GetUserCache mechanism for efficiency
func GetUserLanguage(userId int) string {
	userCache, err := GetUserCache(userId)
	if err != nil {
		return ""
	}
	return userCache.GetSetting().Language
}
