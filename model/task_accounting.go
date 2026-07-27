package model

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

var ErrTaskAccountingNotReady = errors.New("task accounting is not ready")

type TaskAccountingMutation struct {
	Task          *Task
	StatsRecorded bool
	Skipped       bool
}

// RecordTaskAccountingStatistics atomically records the one request-level
// usage entry owned by a durable Task. Billing settlement remains independent.
func RecordTaskAccountingStatistics(ctx context.Context, taskID int64) (*TaskAccountingMutation, error) {
	if taskID <= 0 {
		return nil, ErrTaskBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}

	mutation := &TaskAccountingMutation{}
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskBillingRow(tx, taskID)
		if err != nil {
			return err
		}
		mutation.Task = task
		switch task.PrivateData.AccountingState {
		case "", TaskAccountingStateCompleted, TaskAccountingStateStatsRecorded:
			return nil
		case TaskAccountingStatePending:
		default:
			return fmt.Errorf("%w: unknown state %q", ErrTaskBillingStateConflict, task.PrivateData.AccountingState)
		}
		if task.PrivateData.TargetQuota != nil {
			return ErrTaskAccountingNotReady
		}
		if !taskRequiresAccounting(task) {
			if !taskAccountingCanBeSkipped(task) {
				return ErrTaskAccountingNotReady
			}
			task.PrivateData.AccountingState = TaskAccountingStateCompleted
			if err := tx.Save(task).Error; err != nil {
				return err
			}
			mutation.Skipped = true
			return nil
		}

		accountingQuota := task.PrivateData.AccountingQuota
		if accountingQuota == 0 && task.Quota > 0 {
			accountingQuota = task.Quota
		}
		userUpdate := tx.Model(&User{}).Where("id = ?", task.UserId).Updates(map[string]any{
			"used_quota":    gorm.Expr("used_quota + ?", accountingQuota),
			"request_count": gorm.Expr("request_count + ?", 1),
		})
		if userUpdate.Error != nil {
			return userUpdate.Error
		}
		if userUpdate.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if task.ChannelId > 0 {
			channelUpdate := tx.Model(&Channel{}).Where("id = ?", task.ChannelId).
				Update("used_quota", gorm.Expr("used_quota + ?", accountingQuota))
			if channelUpdate.Error != nil {
				return channelUpdate.Error
			}
		}

		task.PrivateData.AccountingQuota = accountingQuota
		task.PrivateData.AccountingState = TaskAccountingStateStatsRecorded
		if err := tx.Save(task).Error; err != nil {
			return err
		}
		mutation.StatsRecorded = true
		return nil
	})
	return mutation, err
}

// CompleteTaskAccountingLog serializes log creation through the Task row. When
// LOG_DB is separate, a retry detects the deterministic request ID before
// completing the main-database state transition.
func CompleteTaskAccountingLog(ctx context.Context, taskID int64, log *Log) (bool, error) {
	if taskID <= 0 || log == nil {
		return false, ErrTaskBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}

	created := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskBillingRow(tx, taskID)
		if err != nil {
			return err
		}
		if task.PrivateData.AccountingState == TaskAccountingStateCompleted {
			return nil
		}
		if task.PrivateData.AccountingState != TaskAccountingStateStatsRecorded {
			return ErrTaskAccountingNotReady
		}

		if common.LogConsumeEnabled {
			log.UserId = task.UserId
			log.Type = LogTypeConsume
			log.Quota = task.PrivateData.AccountingQuota
			log.ChannelId = task.ChannelId
			log.TokenId = task.PrivateData.TokenId
			log.Group = task.Group
			log.RequestId = task.TaskID
			if log.CreatedAt == 0 {
				log.CreatedAt = common.GetTimestamp()
			}

			logDB := LOG_DB
			if logDB == nil {
				return errors.New("log database is not configured")
			}
			if LOG_DB == DB {
				logDB = tx
			} else {
				logDB = LOG_DB.WithContext(ctx)
			}
			var existing int64
			if err := logDB.Model(&Log{}).
				Where("request_id = ? AND user_id = ? AND type = ?", task.TaskID, task.UserId, LogTypeConsume).
				Count(&existing).Error; err != nil {
				return err
			}
			if existing == 0 {
				if err := logDB.Create(log).Error; err != nil {
					return err
				}
				created = true
			}
		}

		task.PrivateData.AccountingState = TaskAccountingStateCompleted
		return tx.Save(task).Error
	})
	return created, err
}

func taskRequiresAccounting(task *Task) bool {
	return task.PrivateData.AccountingRequired ||
		task.PrivateData.UpstreamTaskID != "" ||
		task.PrivateData.BillingState == TaskBillingStateAccepted ||
		task.PrivateData.BillingState == TaskBillingStateAmbiguous
}

func taskAccountingCanBeSkipped(task *Task) bool {
	if task.PrivateData.UpstreamTaskID != "" {
		return false
	}
	if task.PrivateData.BillingState == TaskBillingStateRefunded {
		return true
	}
	return task.Status == TaskStatusFailure &&
		(task.PrivateData.BillingState == TaskBillingStatePending ||
			task.PrivateData.BillingState == TaskBillingStateReserved ||
			task.PrivateData.BillingState == TaskBillingStateCompleted)
}

func recordTaskAccountingQuotaIncrease(tx *gorm.DB, task *Task, delta int) error {
	if delta <= 0 {
		return nil
	}
	if err := tx.Model(&User{}).Where("id = ?", task.UserId).
		Update("used_quota", gorm.Expr("used_quota + ?", delta)).Error; err != nil {
		return err
	}
	if task.ChannelId <= 0 {
		return nil
	}
	return tx.Model(&Channel{}).Where("id = ?", task.ChannelId).
		Update("used_quota", gorm.Expr("used_quota + ?", delta)).Error
}
