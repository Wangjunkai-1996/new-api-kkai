package model

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	KKAIOutboxTopicTaskBillingAudit     = "task.billing.audit.v1"
	TaskBillingAuditOperationSettlement = "settlement"
	TaskBillingAuditOperationRefund     = "refund"
	taskBillingAuditRequestIDPrefix     = "tb:"
	taskBillingAuditEventKeyPrefix      = "task-billing-audit:"
)

type TaskBillingAuditRequest struct {
	Reason      string
	QuotaClamps []*common.QuotaClamp
}

type TaskBillingAuditPayload struct {
	TaskID          int64                `json:"task_id"`
	BillingRevision int64                `json:"billing_revision"`
	Operation       string               `json:"operation"`
	LogType         int                  `json:"log_type"`
	Quota           int                  `json:"quota"`
	Reason          string               `json:"reason,omitempty"`
	PreviousQuota   int                  `json:"previous_quota"`
	CurrentQuota    int                  `json:"current_quota"`
	QuotaClamps     []*common.QuotaClamp `json:"quota_clamps,omitempty"`
}

func enqueueTaskBillingAudit(tx *gorm.DB, task *Task, payload TaskBillingAuditPayload) error {
	if tx == nil || task == nil || task.ID <= 0 || task.PrivateData.BillingRevision <= 0 || payload.Quota <= 0 {
		return ErrTaskBillingInvalidRequest
	}
	payload.TaskID = task.ID
	payload.BillingRevision = task.PrivateData.BillingRevision
	encoded, err := common.Marshal(payload)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	event := KKAIOutboxEvent{
		EventKey:    taskBillingAuditEventKey(task.ID, payload.BillingRevision),
		Topic:       KKAIOutboxTopicTaskBillingAudit,
		AggregateID: task.TaskID,
		Payload:     string(encoded),
		Status:      KKAIOutboxStatusPending,
		AvailableAt: now,
		CreatedAt:   now,
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&event).Error
}

func CompleteTaskBillingAuditLog(ctx context.Context, taskID int64, billingRevision int64, log *Log) (bool, error) {
	if taskID <= 0 || billingRevision <= 0 || log == nil || log.Quota <= 0 ||
		(log.Type != LogTypeConsume && log.Type != LogTypeRefund) {
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
		log.UserId = task.UserId
		log.ChannelId = task.ChannelId
		log.TokenId = task.PrivateData.TokenId
		log.Group = task.Group
		log.RequestId = taskBillingAuditRequestID(task.ID, billingRevision)
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
		if err := logDB.Model(&Log{}).Where("request_id = ?", log.RequestId).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return nil
		}
		if err := logDB.Create(log).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	return created, err
}

func taskBillingAuditEventKey(taskID int64, billingRevision int64) string {
	return taskBillingAuditEventKeyPrefix + strconv.FormatInt(taskID, 10) + ":" + strconv.FormatInt(billingRevision, 10)
}

func taskBillingAuditRequestID(taskID int64, billingRevision int64) string {
	return taskBillingAuditRequestIDPrefix + strconv.FormatInt(taskID, 10) + ":" + strconv.FormatInt(billingRevision, 10)
}
