package model

import (
	"errors"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

var ErrUserQuotaOverflow = errors.New("user quota exceeds BIGINT range")

func (user *User) TransferAffQuotaToQuota(quota int) error {
	if float64(quota) < common.QuotaPerUnit {
		return fmt.Errorf("转移额度最小为%s！", logger.LogQuota(int(common.QuotaPerUnit)))
	}
	quotaDelta := int64(quota)
	updated := userQuotaCreditQuery(DB, user.Id, quotaDelta).
		Where("aff_quota >= ?", quota).
		Updates(map[string]interface{}{
			"aff_quota": gorm.Expr("aff_quota - ?", quota),
			"quota":     gorm.Expr("quota + ?", quotaDelta),
		})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected == 1 {
		updateUserQuotaCacheByDelta(user.Id, quotaDelta)
		return nil
	}

	var current User
	if err := DB.Select("id", "quota", "aff_quota").Where("id = ?", user.Id).First(&current).Error; err != nil {
		return err
	}
	if current.AffQuota < quota {
		return errors.New("邀请额度不足！")
	}
	return ErrUserQuotaOverflow
}

// GetUserQuota gets quota from Redis first, falls back to DB if needed.
func GetUserQuota(id int, fromDB bool) (quota int64, err error) {
	defer func() {
		if shouldUpdateRedis(fromDB, err) {
			gopool.Go(func() {
				if err := updateUserQuotaCache(id, quota); err != nil {
					common.SysLog("failed to update user quota cache: " + err.Error())
				}
			})
		}
	}()
	if !fromDB && common.RedisEnabled {
		quota, err = getUserQuotaCache(id)
		if err == nil {
			return quota, nil
		}
	}
	fromDB = true
	if err = DB.Model(&User{}).Where("id = ?", id).Select("quota").Find(&quota).Error; err != nil {
		return 0, err
	}
	return quota, nil
}

func GetUserUsedQuota(id int) (quota int64, err error) {
	err = DB.Model(&User{}).Where("id = ?", id).Select("used_quota").Find(&quota).Error
	return quota, err
}

func IncreaseUserQuota(id int, quota int64, db bool) error {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if quota == 0 {
		return nil
	}
	if !db && common.BatchUpdateEnabled {
		updateUserQuotaCacheByDelta(id, quota)
		addNewRecord(BatchUpdateTypeUserQuota, id, quota)
		return nil
	}
	if err := increaseUserQuota(id, quota); err != nil {
		return err
	}
	updateUserQuotaCacheByDelta(id, quota)
	return nil
}

func userQuotaCreditQuery(db *gorm.DB, id int, quota int64) *gorm.DB {
	return db.Model(&User{}).
		Where("id = ? AND quota <= ?", id, int64(math.MaxInt64)-quota)
}

func increaseUserQuota(id int, quota int64) error {
	return increaseUserQuotaWithDB(DB, id, quota)
}

func increaseUserQuotaWithDB(db *gorm.DB, id int, quota int64) error {
	updated := userQuotaCreditQuery(db, id, quota).
		Update("quota", gorm.Expr("quota + ?", quota))
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected == 1 {
		return nil
	}
	var count int64
	if err := db.Model(&User{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return ErrUserQuotaOverflow
}

func DecreaseUserQuota(id int, quota int64, db bool) error {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if quota == 0 {
		return nil
	}
	if !db && common.BatchUpdateEnabled {
		updateUserQuotaCacheByDelta(id, -quota)
		addNewRecord(BatchUpdateTypeUserQuota, id, -quota)
		return nil
	}
	if err := decreaseUserQuota(id, quota); err != nil {
		return err
	}
	updateUserQuotaCacheByDelta(id, -quota)
	return nil
}

func updateUserQuotaCacheByDelta(id int, delta int64) {
	if !common.RedisEnabled {
		return
	}
	gopool.Go(func() {
		if err := cacheIncrUserQuota(id, delta); err != nil {
			common.SysLog("failed to update user quota cache: " + err.Error())
		}
	})
}

func decreaseUserQuota(id int, quota int64) error {
	return DB.Model(&User{}).Where("id = ?", id).
		Update("quota", gorm.Expr("quota - ?", quota)).Error
}

func DeltaUpdateUserQuota(id int, delta int64) error {
	if delta == 0 {
		return nil
	}
	if delta > 0 {
		return IncreaseUserQuota(id, delta, false)
	}
	return DecreaseUserQuota(id, -delta, false)
}

func UpdateUserUsedQuotaAndRequestCount(id int, quota int64) {
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeUsedQuota, id, quota)
		addNewRecord(BatchUpdateTypeRequestCount, id, 1)
		return
	}
	updateUserUsedQuotaAndRequestCount(id, quota, 1)
}

func updateUserUsedQuotaAndRequestCount(id int, quota int64, count int64) {
	err := DB.Model(&User{}).Where("id = ?", id).Updates(map[string]interface{}{
		"used_quota":    gorm.Expr("used_quota + ?", quota),
		"request_count": gorm.Expr("request_count + ?", count),
	}).Error
	if err != nil {
		common.SysLog("failed to update user used quota and request count: " + err.Error())
	}
}

func updateUserQuotaUsedQuotaAndRequestCount(id int, quota int64, usedQuota int64, requestCount int64) {
	if quota == 0 && usedQuota == 0 && requestCount == 0 {
		return
	}
	query := DB.Model(&User{}).Where("id = ?", id)
	if quota > 0 {
		query = userQuotaCreditQuery(DB, id, quota)
	}
	updated := query.Updates(map[string]interface{}{
		"quota":         gorm.Expr("quota + ?", quota),
		"used_quota":    gorm.Expr("used_quota + ?", usedQuota),
		"request_count": gorm.Expr("request_count + ?", requestCount),
	})
	if updated.Error != nil {
		common.SysLog("failed to batch update user quota, used quota and request count: " + updated.Error.Error())
		return
	}
	if updated.RowsAffected == 1 || quota <= 0 {
		return
	}
	common.SysLog(fmt.Sprintf("rejected overflowing batch user quota credit: user_id=%d delta_quota=%d", id, quota))
	if err := invalidateUserCache(id); err != nil {
		common.SysLog("failed to invalidate user cache after rejected quota credit: " + err.Error())
	}
	if usedQuota != 0 || requestCount != 0 {
		updateUserUsedQuotaAndRequestCount(id, usedQuota, requestCount)
	}
}

func updateUserUsedQuota(id int, quota int64) {
	err := DB.Model(&User{}).Where("id = ?", id).
		Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error
	if err != nil {
		common.SysLog("failed to update user used quota: " + err.Error())
	}
}

func updateUserRequestCount(id int, count int64) {
	err := DB.Model(&User{}).Where("id = ?", id).
		Update("request_count", gorm.Expr("request_count + ?", count)).Error
	if err != nil {
		common.SysLog("failed to update user request count: " + err.Error())
	}
}
