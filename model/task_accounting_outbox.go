package model

import (
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const KKAIOutboxTopicTaskAccounting = "task.accounting.v1"

type TaskAccountingPayload struct {
	TaskID int64 `json:"task_id"`
}

func enqueueTaskAccounting(tx *gorm.DB, task *Task) error {
	if tx == nil || task == nil || task.ID <= 0 {
		return ErrTaskBillingInvalidRequest
	}
	payload, err := common.Marshal(TaskAccountingPayload{TaskID: task.ID})
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	event := KKAIOutboxEvent{
		EventKey:    "task-accounting:" + strconv.FormatInt(task.ID, 10),
		Topic:       KKAIOutboxTopicTaskAccounting,
		AggregateID: task.TaskID,
		Payload:     string(payload),
		Status:      KKAIOutboxStatusPending,
		AvailableAt: now,
		CreatedAt:   now,
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&event).Error
}
