package model

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

var ErrShopWorkerFenceLost = errors.New("SHOP_WORKER_OWNERSHIP_LOST")

type ShopWorkerFence struct {
	EventID      int64
	LockedBy     string
	LockTimeout  time.Duration
	LeaderName   string
	LeaderHolder string
	LeaderFence  int64
}
type shopWorkerFenceContextKey struct{}

// WithShopWorkerFence makes every shop mutation validate ownership in the same
// transaction as its write. Callers retain cancellation and request deadlines.
func WithShopWorkerFence(db *gorm.DB, fence ShopWorkerFence) *gorm.DB {
	if db == nil {
		return nil
	}
	return db.WithContext(context.WithValue(db.Statement.Context, shopWorkerFenceContextKey{}, fence))
}

// RequireShopWorkerFenceTx keeps the leader and claimed event locked until the
// business transaction commits. Database time, rather than worker time, is used.
func RequireShopWorkerFenceTx(tx *gorm.DB, fence ShopWorkerFence) error {
	if tx == nil || fence.EventID < 0 || fence.LeaderName == "" || fence.LeaderHolder == "" || fence.LeaderFence <= 0 || (fence.EventID > 0 && (fence.LockedBy == "" || fence.LockTimeout <= 0)) {
		return ErrShopWorkerFenceLost
	}
	// SQLite needs a real write statement before responsibility reads. PostgreSQL
	// and MySQL acquire the same lease row lock with this statement.
	if err := tx.Model(&KKAIJobLease{}).Where("lease_name = ?", fence.LeaderName).UpdateColumn("updated_at", gorm.Expr("updated_at")).Error; err != nil {
		return err
	}
	var leader KKAIJobLease
	if err := lockForUpdate(tx).Where("lease_name = ?", fence.LeaderName).First(&leader).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrShopWorkerFenceLost
		}
		return err
	}
	var event KKAIOutboxEvent
	if fence.EventID > 0 {
		if err := lockForUpdate(tx).First(&event, fence.EventID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrShopWorkerFenceLost
			}
			return err
		}
	}
	var query string
	switch tx.Dialector.Name() {
	case "postgres":
		query = "SELECT FLOOR(EXTRACT(EPOCH FROM clock_timestamp()))::bigint"
	case "mysql":
		query = "SELECT UNIX_TIMESTAMP()"
	case "sqlite":
		query = "SELECT CAST(strftime('%s','now') AS INTEGER)"
	default:
		return ErrShopWorkerFenceLost
	}
	var now int64
	if err := tx.Raw(query).Scan(&now).Error; err != nil {
		return err
	}
	seconds := int64(fence.LockTimeout / time.Second)
	if fence.LockTimeout%time.Second != 0 {
		seconds++
	}
	if leader.Holder != fence.LeaderHolder || leader.Fence != fence.LeaderFence || leader.LeaseUntil <= now || (fence.EventID > 0 && (event.Status != KKAIOutboxStatusPending || event.LockedBy != fence.LockedBy || event.LockedAt <= now-seconds)) {
		return ErrShopWorkerFenceLost
	}
	return nil
}
