package model

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const KKAIOutboxTopicTaskBillingCacheReconcile = "task.billing.cache.reconcile.v1"

type TaskBillingCacheReconcilePayload struct {
	TaskID int64 `json:"task_id"`
}

func persistTaskBillingMutation(tx *gorm.DB, task *Task) error {
	if tx == nil || task == nil || task.ID <= 0 {
		return ErrTaskBillingInvalidRequest
	}
	task.PrivateData.BillingRevision++
	if err := tx.Save(task).Error; err != nil {
		return err
	}
	payload, err := common.Marshal(TaskBillingCacheReconcilePayload{TaskID: task.ID})
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	event := KKAIOutboxEvent{
		EventKey:    "task-billing-cache:" + strconv.FormatInt(task.ID, 10) + ":" + strconv.FormatInt(task.PrivateData.BillingRevision, 10),
		Topic:       KKAIOutboxTopicTaskBillingCacheReconcile,
		AggregateID: task.TaskID,
		Payload:     string(payload),
		Status:      KKAIOutboxStatusPending,
		AvailableAt: now,
		CreatedAt:   now,
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&event).Error
}

func reconcileTaskBillingCacheAfterCommit(ctx context.Context, taskID int64) {
	if err := ReconcileTaskBillingQuotaCache(context.WithoutCancel(ctx), taskID); err != nil {
		common.SysLog(fmt.Sprintf("failed to reconcile durable task billing cache for task %d: %s", taskID, err.Error()))
	}
}

// ReconcileTaskBillingQuotaCache refreshes quota from current database values.
// Token authorization is hydrated only by authentication's guarded cold read.
func ReconcileTaskBillingQuotaCache(ctx context.Context, taskID int64) error {
	if taskID <= 0 {
		return ErrTaskBillingInvalidRequest
	}
	if !common.RedisEnabled {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var task Task
	if err := DB.WithContext(ctx).First(&task, taskID).Error; err != nil {
		return err
	}
	if task.PrivateData.BillingSource == TaskBillingSourceWallet {
		var quota int64
		if err := DB.WithContext(ctx).Model(&User{}).Where("id = ?", task.UserId).Select("quota").Scan(&quota).Error; err != nil {
			return err
		}
		if err := updateUserQuotaCache(task.UserId, quota); err != nil {
			return err
		}
	}
	if task.PrivateData.TokenId <= 0 {
		return nil
	}

	var token Token
	err := DB.WithContext(ctx).Unscoped().Select("id", commonKeyCol).
		Where("id = ?", task.PrivateData.TokenId).First(&token).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	// Bind the quota snapshot to the generation before reading it. An older
	// reconciliation must not restore a token limit edited while it was paused.
	generation, err := beginTokenCacheRead(token.Key)
	if err != nil {
		return err
	}
	if err := DB.WithContext(ctx).Unscoped().First(&token, token.Id).Error; err != nil {
		return err
	}
	if token.DeletedAt.Valid {
		return cacheDeleteToken(token.Key)
	}
	return cacheSetToken(token, generation)
}
