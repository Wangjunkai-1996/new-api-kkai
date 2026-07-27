package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

const (
	KKAIOutboxTopicTaskBillingRecovery = "task.billing.recover.v1"
	TaskBillingRecoveryDelay           = 10 * time.Minute
	taskBillingRecoveryPollInterval    = 5 * time.Minute
)

type TaskBillingRecoveryPayload struct {
	TaskID     int64                                 `json:"task_id"`
	Acceptance *TaskBillingRecoveryAcceptanceReceipt `json:"acceptance,omitempty"`
}

type TaskBillingRecoveryAcceptanceReceipt struct {
	UpstreamTaskID string             `json:"upstream_task_id"`
	RawResponse    json.RawMessage    `json:"raw_response"`
	ChannelID      int                `json:"channel_id"`
	Status         model.TaskStatus   `json:"status"`
	Progress       string             `json:"progress"`
	FailReason     string             `json:"fail_reason,omitempty"`
	FinishTime     int64              `json:"finish_time,omitempty"`
	OtherRatios    map[string]float64 `json:"other_ratios,omitempty"`
	TargetQuota    int                `json:"target_quota"`
}

type TaskBillingRecoveryHandler struct{}

func EnqueueTaskBillingRecovery(ctx context.Context, tx *gorm.DB, task *model.Task) error {
	if tx == nil || task == nil || task.ID <= 0 {
		return ErrKKAIOutboxInvalidConfiguration
	}
	recoveryAt := time.Unix(task.PrivateData.RecoveryAt, 0)
	if task.PrivateData.RecoveryAt <= 0 {
		recoveryAt = time.Now().Add(TaskBillingRecoveryDelay)
	}
	return EnqueueKKAIOutboxEvent(
		ctx,
		tx,
		"task-billing-recovery:"+strconv.FormatInt(task.ID, 10),
		KKAIOutboxTopicTaskBillingRecovery,
		task.TaskID,
		recoveryAt,
		TaskBillingRecoveryPayload{TaskID: task.ID},
	)
}

func StoreTaskBillingRecoveryAcceptanceReceipt(ctx context.Context, taskID int64, receipt TaskBillingRecoveryAcceptanceReceipt) error {
	if taskID <= 0 || receipt.UpstreamTaskID == "" || receipt.TargetQuota < 0 ||
		(receipt.Status != model.TaskStatusSubmitted && receipt.Status != model.TaskStatusUnknown) {
		return model.ErrTaskBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}

	eventKey := "task-billing-recovery:" + strconv.FormatInt(taskID, 10)
	return model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var event model.KKAIOutboxEvent
		if err := tx.Where("event_key = ? AND topic = ?", eventKey, KKAIOutboxTopicTaskBillingRecovery).First(&event).Error; err != nil {
			return err
		}
		if event.LockedAt != 0 || event.LockedBy != "" {
			return ErrKKAIOutboxLockLost
		}

		payload := TaskBillingRecoveryPayload{}
		if err := common.UnmarshalJsonStr(event.Payload, &payload); err != nil {
			return err
		}
		if payload.TaskID != taskID {
			return model.ErrTaskBillingStateConflict
		}
		if payload.Acceptance != nil && payload.Acceptance.UpstreamTaskID != receipt.UpstreamTaskID {
			return model.ErrTaskBillingStateConflict
		}
		receipt.RawResponse = append(json.RawMessage(nil), receipt.RawResponse...)
		payload.Acceptance = &receipt
		encoded, err := common.Marshal(payload)
		if err != nil {
			return err
		}

		now := time.Now().Unix()
		update := tx.Model(&model.KKAIOutboxEvent{}).
			Where("id = ? AND payload = ? AND locked_at = ? AND locked_by = ?", event.ID, event.Payload, 0, "").
			Updates(map[string]any{
				"payload":      string(encoded),
				"status":       model.KKAIOutboxStatusPending,
				"attempts":     0,
				"available_at": now,
				"locked_at":    0,
				"locked_by":    "",
				"last_error":   "",
				"delivered_at": 0,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected == 0 {
			return ErrKKAIOutboxLockLost
		}
		return nil
	})
}

func (TaskBillingRecoveryHandler) Handle(ctx context.Context, event model.KKAIOutboxEvent) error {
	payload := TaskBillingRecoveryPayload{}
	if err := common.UnmarshalJsonStr(event.Payload, &payload); err != nil {
		return PermanentKKAIOutboxError(fmt.Errorf("invalid task billing recovery payload: %w", err))
	}
	if payload.TaskID <= 0 {
		return PermanentKKAIOutboxError(errors.New("invalid task billing recovery payload: task_id must be positive"))
	}
	var task model.Task
	if payload.Acceptance != nil {
		receipt := payload.Acceptance
		recoveredTask, recovered, err := model.RecoverTaskSubmissionAcceptance(ctx, payload.TaskID, model.TaskSubmissionAcceptance{
			UpstreamTaskID: receipt.UpstreamTaskID,
			TaskData:       receipt.RawResponse,
			ChannelID:      receipt.ChannelID,
			Status:         receipt.Status,
			Progress:       receipt.Progress,
			FailReason:     receipt.FailReason,
			FinishTime:     receipt.FinishTime,
			OtherRatios:    receipt.OtherRatios,
			TargetQuota:    receipt.TargetQuota,
		})
		if err != nil {
			return err
		}
		if recoveredTask != nil {
			task = *recoveredTask
		}
		if !recovered {
			return PermanentKKAIOutboxError(errors.New("accepted task receipt conflicts with current task state"))
		}
	} else if err := model.DB.WithContext(ctx).First(&task, payload.TaskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	if task.PrivateData.TargetQuota != nil &&
		(task.PrivateData.BillingState == model.TaskBillingStateAccepted || task.PrivateData.BillingState == model.TaskBillingStateAmbiguous) &&
		task.Status != model.TaskStatusFailure {
		RecalculateTaskQuota(ctx, &task, *task.PrivateData.TargetQuota, "submission settlement recovery")
		if task.PrivateData.TargetQuota != nil {
			return DeferKKAIOutboxUntil(time.Now().Add(taskBillingRecoveryPollInterval), errors.New("task submission settlement is still pending"))
		}
	}

	switch task.PrivateData.BillingState {
	case "", model.TaskBillingStateCompleted:
		return nil
	case model.TaskBillingStatePending:
		if err := failUnsubmittedTask(ctx, &task, "task submission stopped before billing reservation"); err != nil {
			return err
		}
		return model.CompleteTaskBillingRecovery(ctx, task.ID)
	case model.TaskBillingStateReserved:
		if err := refundDurableTaskQuota(ctx, &task, "task submission stopped before upstream acceptance"); err != nil {
			return err
		}
		return failUnsubmittedTask(ctx, &task, "task submission stopped before upstream acceptance")
	case model.TaskBillingStateDispatching:
		if task.Status == model.TaskStatusFailure {
			return refundDurableTaskQuota(ctx, &task, task.FailReason)
		}
		return finishAmbiguousTask(ctx, &task)
	case model.TaskBillingStateAmbiguous:
		if task.PrivateData.UpstreamTaskID == "" {
			return model.CompleteTaskBillingRecovery(ctx, task.ID)
		}
		return DeferKKAIOutboxUntil(time.Now().Add(taskBillingRecoveryPollInterval), errors.New("accepted task remains pollable"))
	case model.TaskBillingStateAccepted:
		switch task.Status {
		case model.TaskStatusFailure:
			return refundDurableTaskQuota(ctx, &task, task.FailReason)
		case model.TaskStatusSuccess:
			return model.CompleteTaskBillingRecovery(ctx, task.ID)
		default:
			return DeferKKAIOutboxUntil(time.Now().Add(taskBillingRecoveryPollInterval), errors.New("accepted task is not terminal"))
		}
	case model.TaskBillingStateRefunded:
		if task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure {
			return nil
		}
		return failUnsubmittedTask(ctx, &task, "task billing was refunded before submission completed")
	default:
		return PermanentKKAIOutboxError(fmt.Errorf("unknown task billing state %q", task.PrivateData.BillingState))
	}
}

func failUnsubmittedTask(ctx context.Context, task *model.Task, reason string) error {
	if task == nil || task.ID <= 0 {
		return model.ErrTaskBillingInvalidRequest
	}
	failedTask, won, err := model.FailTaskBeforeSubmission(ctx, task.ID, reason)
	if failedTask != nil {
		*task = *failedTask
	}
	if err != nil {
		return err
	}
	if !won {
		return DeferKKAIOutboxUntil(time.Now().Add(taskBillingRecoveryPollInterval), errors.New("task changed during billing recovery"))
	}
	return nil
}

func finishAmbiguousTask(ctx context.Context, task *model.Task) error {
	if task == nil || task.ID <= 0 {
		return model.ErrTaskBillingInvalidRequest
	}
	markedTask, marked, err := model.MarkTaskSubmissionAmbiguous(ctx, task.ID, model.TaskSubmissionAmbiguity{
		Reason: "task submission outcome is unknown",
	})
	if markedTask != nil {
		*task = *markedTask
	}
	if err != nil {
		return err
	}
	if !marked {
		return DeferKKAIOutboxUntil(time.Now().Add(taskBillingRecoveryPollInterval), errors.New("task changed during ambiguous recovery"))
	}
	return model.CompleteTaskBillingRecovery(ctx, task.ID)
}
