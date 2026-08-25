package model

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type cacheQuotaResult int

const (
	cacheQuotaInsufficient cacheQuotaResult = iota
	cacheQuotaOK
	cacheQuotaMiss
)

var (
	errQuotaCacheAheadOfDatabase  = errors.New("quota cache balance is ahead of database")
	errBatchQuotaCacheUnavailable = errors.New("quota cache is unavailable while batch updates are enabled")
)

const userQuotaReserveScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[3])
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
local quota = tonumber(redis.call('HGET', KEYS[1], 'Quota'))
if quota == nil or quota < tonumber(ARGV[1]) then
  return 0
end
redis.call('HINCRBY', KEYS[1], 'Quota', -tonumber(ARGV[1]))
return 1`

const userQuotaDeltaScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[3])
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
redis.call('HINCRBY', KEYS[1], 'Quota', tonumber(ARGV[1]))
return 1`

const tokenQuotaReserveScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or redis.call('HEXISTS', KEYS[1], 'RemainQuota') == 0
  or redis.call('HEXISTS', KEYS[1], 'UsedQuota') == 0 then
  return -1
end
local remain = tonumber(redis.call('HGET', KEYS[1], 'RemainQuota'))
if remain == nil or remain < tonumber(ARGV[1]) then
  return 0
end
redis.call('HINCRBY', KEYS[1], 'RemainQuota', -tonumber(ARGV[1]))
redis.call('HINCRBY', KEYS[1], 'UsedQuota', tonumber(ARGV[1]))
redis.call('HSET', KEYS[1], 'AccessedTime', ARGV[3])
return 1`

const tokenQuotaDeltaScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or redis.call('HEXISTS', KEYS[1], 'RemainQuota') == 0
  or redis.call('HEXISTS', KEYS[1], 'UsedQuota') == 0 then
  return -1
end
redis.call('HINCRBY', KEYS[1], 'RemainQuota', tonumber(ARGV[1]))
redis.call('HINCRBY', KEYS[1], 'UsedQuota', -tonumber(ARGV[1]))
redis.call('HSET', KEYS[1], 'AccessedTime', ARGV[3])
return 1`

func quotaResultFromLua(result int, err error) (cacheQuotaResult, error) {
	if err != nil {
		return cacheQuotaMiss, err
	}
	switch result {
	case 1:
		return cacheQuotaOK, nil
	case 0:
		return cacheQuotaInsufficient, nil
	default:
		return cacheQuotaMiss, nil
	}
}

func cacheTryReserveUserQuota(userID int, amount int64) (cacheQuotaResult, error) {
	result, err := common.RDB.Eval(context.Background(), userQuotaReserveScript,
		[]string{getUserCacheKey(userID)}, amount, userID, userCacheSchemaVersion).Int()
	return quotaResultFromLua(result, err)
}

func cacheApplyUserQuotaDelta(userID int, delta int64) (cacheQuotaResult, error) {
	result, err := common.RDB.Eval(context.Background(), userQuotaDeltaScript,
		[]string{getUserCacheKey(userID)}, delta, userID, userCacheSchemaVersion).Int()
	return quotaResultFromLua(result, err)
}

func cacheTryReserveTokenQuota(id int, key string, amount int64) (cacheQuotaResult, error) {
	result, err := common.RDB.Eval(context.Background(), tokenQuotaReserveScript,
		[]string{getTokenCacheKey(key)}, amount, id, common.GetTimestamp()).Int()
	return quotaResultFromLua(result, err)
}

func cacheApplyTokenQuotaDelta(id int, key string, delta int64) (cacheQuotaResult, error) {
	result, err := common.RDB.Eval(context.Background(), tokenQuotaDeltaScript,
		[]string{getTokenCacheKey(key)}, delta, id, common.GetTimestamp()).Int()
	return quotaResultFromLua(result, err)
}

// persistUserQuotaReservation 把已在缓存侧预扣成功的额度落库。同步模式
// 再次执行余额条件更新，防止 Redis 故障期间的数据库扣减被恢复后的旧缓存覆盖。
func persistUserQuotaReservation(id int, quota int) error {
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeUserQuota, id, -int64(quota))
		return nil
	}
	result := DB.Model(&User{}).
		Where("id = ? AND quota >= ?", id, quota).
		Update("quota", gorm.Expr("quota - ?", quota))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	return quotaReservationMissError(&User{}, id)
}

func persistTokenQuotaReservation(id int, quota int) error {
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeTokenQuota, id, -int64(quota))
		return nil
	}
	result := DB.Model(&Token{}).
		Where("id = ? AND remain_quota >= ?", id, quota).
		Updates(
			map[string]interface{}{
				"remain_quota":  gorm.Expr("remain_quota - ?", quota),
				"used_quota":    gorm.Expr("used_quota + ?", quota),
				"accessed_time": common.GetTimestamp(),
			},
		)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	return quotaReservationMissError(&Token{}, id)
}

func quotaReservationMissError(record interface{}, id int) error {
	var count int64
	if err := DB.Model(record).Where("id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return errQuotaCacheAheadOfDatabase
}

func reserveUserQuotaDB(id int, quota int) (bool, error) {
	result := DB.Model(&User{}).
		Where("id = ? AND quota >= ?", id, quota).
		Update("quota", gorm.Expr("quota - ?", quota))
	return result.RowsAffected == 1, result.Error
}

func reserveTokenQuotaDB(id int, quota int) (bool, error) {
	result := DB.Model(&Token{}).
		Where("id = ? AND remain_quota >= ?", id, quota).
		Updates(map[string]interface{}{
			"remain_quota":  gorm.Expr("remain_quota - ?", quota),
			"used_quota":    gorm.Expr("used_quota + ?", quota),
			"accessed_time": common.GetTimestamp(),
		})
	return result.RowsAffected == 1, result.Error
}

// TryReserveUserQuota atomically checks and deducts a user's wallet quota.
// 缓存命中时以缓存余额为第一道门槛；同步模式落库时再次检查数据库余额，
// batch 模式则在缓存不可用时关闭预扣，避免未刷盘余额被重复授权。
func TryReserveUserQuota(id int, quota int) (bool, error) {
	if quota < 0 {
		return false, errors.New("quota 不能为负数！")
	}
	if quota == 0 {
		return true, nil
	}
	if !common.RedisEnabled {
		return reserveUserQuotaDB(id, quota)
	}

	result, err := cacheTryReserveUserQuota(id, int64(quota))
	if err == nil && result == cacheQuotaMiss {
		if _, hydrateErr := GetUserCache(id); hydrateErr != nil {
			err = hydrateErr
		} else {
			result, err = cacheTryReserveUserQuota(id, int64(quota))
		}
	}
	if err != nil || result == cacheQuotaMiss {
		if common.BatchUpdateEnabled {
			if err != nil {
				return false, fmt.Errorf("%w: %v", errBatchQuotaCacheUnavailable, err)
			}
			return false, errBatchQuotaCacheUnavailable
		}
		if err != nil {
			common.SysLog("user quota cache reserve unavailable, falling back to database: " + err.Error())
		}
		return reserveUserQuotaDB(id, quota)
	}
	if result == cacheQuotaInsufficient {
		return false, nil
	}
	if err = persistUserQuotaReservation(id, quota); err != nil {
		if errors.Is(err, errQuotaCacheAheadOfDatabase) {
			common.SysError(fmt.Sprintf("rejected user quota reservation from stale cache: user_id=%d quota=%d", id, quota))
			if invalidateErr := invalidateUserCache(id); invalidateErr != nil {
				common.SysError("failed to invalidate stale user quota cache: " + invalidateErr.Error())
			}
			return false, nil
		}
		compensated, compensateErr := cacheApplyUserQuotaDelta(id, int64(quota))
		if compensateErr != nil || compensated != cacheQuotaOK {
			common.SysError(fmt.Sprintf("failed to compensate reserved user quota: result=%d error=%v", compensated, compensateErr))
		}
		return false, err
	}
	return true, nil
}

// TryReserveTokenQuota atomically checks and deducts a token quota. Unlimited
// tokens skip the balance check but still update remain/used accounting.
func TryReserveTokenQuota(id int, key string, quota int, unlimited bool) (bool, error) {
	if quota < 0 {
		return false, errors.New("quota 不能为负数！")
	}
	if quota == 0 {
		return true, nil
	}
	if unlimited {
		return true, DecreaseTokenQuota(id, key, quota)
	}
	if !common.RedisEnabled {
		return reserveTokenQuotaDB(id, quota)
	}

	result, err := cacheTryReserveTokenQuota(id, key, int64(quota))
	if err == nil && result == cacheQuotaMiss {
		if _, hydrateErr := GetTokenByKey(key, true); hydrateErr != nil {
			err = hydrateErr
		} else {
			result, err = cacheTryReserveTokenQuota(id, key, int64(quota))
		}
	}
	if err != nil || result == cacheQuotaMiss {
		if common.BatchUpdateEnabled {
			if err != nil {
				return false, fmt.Errorf("%w: %v", errBatchQuotaCacheUnavailable, err)
			}
			return false, errBatchQuotaCacheUnavailable
		}
		if err != nil {
			common.SysLog("token quota cache reserve unavailable, falling back to database: " + err.Error())
		}
		return reserveTokenQuotaDB(id, quota)
	}
	if result == cacheQuotaInsufficient {
		return false, nil
	}
	if err = persistTokenQuotaReservation(id, quota); err != nil {
		if errors.Is(err, errQuotaCacheAheadOfDatabase) {
			common.SysError(fmt.Sprintf("rejected token quota reservation from stale cache: token_id=%d quota=%d", id, quota))
			if invalidateErr := invalidateTokenCacheForMutation(key); invalidateErr != nil {
				common.SysError("failed to invalidate stale token quota cache: " + invalidateErr.Error())
			}
			return false, nil
		}
		compensated, compensateErr := cacheApplyTokenQuotaDelta(id, key, int64(quota))
		if compensateErr != nil || compensated != cacheQuotaOK {
			common.SysError(fmt.Sprintf("failed to compensate reserved token quota: result=%d error=%v", compensated, compensateErr))
		}
		return false, err
	}
	return true, nil
}
