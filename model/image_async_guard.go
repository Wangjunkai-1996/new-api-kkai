package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrImageExecutionFenceLost         = errors.New("image execution ownership lost")
	ErrImageGenerationCapacityExceeded = errors.New("image generation capacity exceeded")
)

type ImageExecutionFence struct {
	EventID      int64
	LockedBy     string
	LockTimeout  time.Duration
	LeaderName   string
	LeaderHolder string
	LeaderFence  int64
}

// LockImageStudioCapacityTx serializes admission through an existing v3 table.
// The caller must keep its checks and inserts in this same transaction.
func LockImageStudioCapacityTx(tx *gorm.DB) error {
	if tx == nil {
		return ErrImageBillingInvalidRequest
	}
	const name = "image-studio-admission"
	row := KKAIJobLease{LeaseName: name, Holder: "admission"}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return err
	}
	// UPDATE obtains a write lock on every dialect, including SQLite where
	// SELECT FOR UPDATE is unavailable. No lease expiry releases this DB lock.
	return tx.Model(&KKAIJobLease{}).Where("lease_name = ?", name).
		Update("updated_at", time.Now().Unix()).Error
}

// CheckImageGenerationCapacityTx must precede generation creation in the same
// transaction. Both legacy synchronous and asynchronous admissions use this gate.
func CheckImageGenerationCapacityTx(tx *gorm.DB, userID, maxTotal, maxPerUser int) error {
	if userID <= 0 || maxTotal <= 0 || maxPerUser <= 0 {
		return ErrImageBillingInvalidRequest
	}
	if err := LockImageStudioCapacityTx(tx); err != nil {
		return err
	}
	active := []string{ImageGenerationStatusSubmitting, ImageGenerationStatusRecovering}
	var total, owned int64
	if err := tx.Model(&KKAIImageGeneration{}).Where("status IN ?", active).Count(&total).Error; err != nil {
		return err
	}
	if total >= int64(maxTotal) {
		return ErrImageGenerationCapacityExceeded
	}
	if err := tx.Model(&KKAIImageGeneration{}).Where("status IN ? AND user_id = ?", active, userID).
		Count(&owned).Error; err != nil {
		return err
	}
	if owned >= int64(maxPerUser) {
		return ErrImageGenerationCapacityExceeded
	}
	return nil
}

// CheckImageExecutionCapacityTx shares execution slots with legacy requests.
// Queued async work consumes admission capacity but not an execution slot.
func CheckImageExecutionCapacityTx(tx *gorm.DB, maxConcurrent int, excludeGenerationID int64) error {
	if maxConcurrent <= 0 || excludeGenerationID < 0 {
		return ErrImageBillingInvalidRequest
	}
	if err := LockImageStudioCapacityTx(tx); err != nil {
		return err
	}
	legacy := tx.Model(&KKAIImageGeneration{}).Where("status IN ? AND id <> ?", []string{ImageGenerationStatusSubmitting, ImageGenerationStatusRecovering}, excludeGenerationID)
	var running, synchronous int64
	if tx.Migrator().HasTable(&KKAIImageExecution{}) {
		legacy = legacy.Where("id NOT IN (?)", tx.Model(&KKAIImageExecution{}).Select("generation_id"))
		if err := tx.Model(&KKAIImageExecution{}).Where("generation_id <> ? AND state IN ? AND recovery_deadline_at > ?", excludeGenerationID,
			[]string{ImageGenerationExecutionPreparing, ImageGenerationExecutionDispatching, ImageGenerationExecutionUnknown}, time.Now().Unix()).Count(&running).Error; err != nil {
			return err
		}
	}
	if err := legacy.Count(&synchronous).Error; err != nil {
		return err
	}
	if running+synchronous >= int64(maxConcurrent) {
		return ErrImageGenerationCapacityExceeded
	}
	return nil
}

// RequireImageWorkerFenceTx holds the leader and event rows until the caller's
// transaction commits; checking outside that transaction would permit stale writes.
func RequireImageWorkerFenceTx(tx *gorm.DB, fence ImageExecutionFence) error {
	if tx == nil || fence.EventID < 0 ||
		(fence.EventID > 0 && (fence.LockedBy == "" || fence.LockTimeout <= 0)) ||
		fence.LeaderName == "" || fence.LeaderHolder == "" || fence.LeaderFence <= 0 {
		return ErrImageExecutionFenceLost
	}
	var leader KKAIJobLease
	if err := lockForUpdate(tx).Where("lease_name = ?", fence.LeaderName).First(&leader).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrImageExecutionFenceLost
		}
		return err
	}
	var event KKAIOutboxEvent
	if fence.EventID > 0 {
		if err := lockForUpdate(tx).First(&event, fence.EventID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrImageExecutionFenceLost
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
		return ErrImageExecutionFenceLost
	}
	var now int64
	if err := tx.Raw(query).Scan(&now).Error; err != nil {
		return err
	}
	seconds := int64(fence.LockTimeout / time.Second)
	if fence.LockTimeout%time.Second != 0 {
		seconds++
	}
	if leader.Holder != fence.LeaderHolder || leader.Fence != fence.LeaderFence || leader.LeaseUntil <= now ||
		(fence.EventID > 0 && (event.Status != KKAIOutboxStatusPending || event.LockedBy != fence.LockedBy || event.LockedAt <= now-seconds)) {
		return ErrImageExecutionFenceLost
	}
	return nil
}

// ClaimImageGenerationExecutionTx adopts an already claimed outbox event without
// changing the persisted execution phase; callers must never re-send dispatching work.
func ClaimImageGenerationExecutionTx(tx *gorm.DB, generationID int64, fence ImageExecutionFence) (*KKAIImageExecution, error) {
	if err := RequireImageWorkerFenceTx(tx, fence); err != nil {
		return nil, err
	}
	var execution KKAIImageExecution
	if err := lockForUpdate(tx).First(&execution, "generation_id = ?", generationID).Error; err != nil {
		return nil, err
	}
	if execution.EventID != fence.EventID || execution.State == ImageGenerationExecutionDone {
		return nil, ErrImageExecutionFenceLost
	}
	execution.Fence = fence.LockedBy
	execution.UpdatedAt = time.Now().Unix()
	if err := tx.Save(&execution).Error; err != nil {
		return nil, err
	}
	return &execution, nil
}

func RequireImageExecutionFenceTx(tx *gorm.DB, generationID int64, fence ImageExecutionFence) (*KKAIImageExecution, error) {
	if err := RequireImageWorkerFenceTx(tx, fence); err != nil {
		return nil, err
	}
	var execution KKAIImageExecution
	if err := lockForUpdate(tx).First(&execution, "generation_id = ?", generationID).Error; err != nil {
		return nil, err
	}
	if execution.EventID != fence.EventID || execution.Fence != fence.LockedBy || execution.State == ImageGenerationExecutionDone {
		return nil, ErrImageExecutionFenceLost
	}
	return &execution, nil
}
