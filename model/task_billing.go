package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"gorm.io/gorm"
)

const (
	TaskBillingSourceWallet       = "wallet"
	TaskBillingSourceSubscription = "subscription"

	TaskBillingStatePending     = "pending"
	TaskBillingStateReserved    = "reserved"
	TaskBillingStateDispatching = "dispatching"
	TaskBillingStateAccepted    = "accepted"
	TaskBillingStateAmbiguous   = "ambiguous"
	TaskBillingStateRefunded    = "refunded"
	TaskBillingStateCompleted   = "completed"
)

var (
	ErrTaskBillingInvalidRequest           = errors.New("invalid task billing request")
	ErrTaskBillingStateConflict            = errors.New("task billing state conflict")
	ErrTaskBillingInsufficientWallet       = errors.New("insufficient wallet quota")
	ErrTaskBillingNoActiveSubscription     = errors.New("no active subscription")
	ErrTaskBillingInsufficientSubscription = errors.New("insufficient subscription quota")
	ErrTaskBillingInsufficientToken        = errors.New("insufficient token quota")
	ErrTaskBillingRefundNotAllowed         = errors.New("task billing refund is not allowed")
)

type TaskBillingReservationRequest struct {
	Source    string
	Quota     int
	TokenID   int
	TokenKey  string
	SkipToken bool
}

type TaskSubmissionAttempt struct {
	Platform          constant.TaskPlatform
	ChannelID         int
	Action            string
	OriginModelName   string
	UpstreamModelName string
}

type TaskSubmissionAcceptance struct {
	UpstreamTaskID string
	TaskData       []byte
	ChannelID      int
	Status         TaskStatus
	Progress       string
	FailReason     string
	FinishTime     int64
	OtherRatios    map[string]float64
	TargetQuota    int
}

type TaskSubmissionAmbiguity struct {
	Reason      string
	TargetQuota *int
}

type TaskBillingMutation struct {
	Task                    *Task
	Applied                 bool
	PreviousQuota           int
	CurrentQuota            int
	SubscriptionAmountTotal int64
	SubscriptionAmountUsed  int64
	SubscriptionPlanID      int
	SubscriptionPlanTitle   string
}

func ReserveTaskBilling(ctx context.Context, taskID int64, request TaskBillingReservationRequest) (*TaskBillingMutation, error) {
	if taskID <= 0 || request.Quota < 0 || (request.Source != TaskBillingSourceWallet && request.Source != TaskBillingSourceSubscription) {
		return nil, ErrTaskBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}

	mutation := &TaskBillingMutation{}
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskBillingRow(tx, taskID)
		if err != nil {
			return err
		}
		if task.PrivateData.BillingState == "" {
			task.PrivateData.BillingState = TaskBillingStatePending
		}
		if task.PrivateData.BillingState != TaskBillingStatePending {
			if task.PrivateData.BillingState == TaskBillingStateRefunded {
				return ErrTaskBillingStateConflict
			}
			mutation.Task = task
			mutation.PreviousQuota = task.Quota
			mutation.CurrentQuota = task.Quota
			return loadTaskSubscriptionSnapshot(tx, mutation)
		}
		effectiveQuota := request.Quota
		if request.Source == TaskBillingSourceSubscription && effectiveQuota <= 0 {
			effectiveQuota = 1
		}
		if billingContext := task.PrivateData.BillingContext; billingContext != nil && billingContext.MaxQuota != nil && effectiveQuota > *billingContext.MaxQuota {
			return fmt.Errorf("%w: quota %d exceeds max %d", ErrTaskBillingInvalidRequest, effectiveQuota, *billingContext.MaxQuota)
		}
		switch request.Source {
		case TaskBillingSourceWallet:
			if err := reserveTaskWallet(tx, task.UserId, effectiveQuota); err != nil {
				return err
			}
		case TaskBillingSourceSubscription:
			if err := reserveTaskSubscription(tx, task, effectiveQuota, mutation); err != nil {
				return err
			}
		}

		tokenQuota := 0
		if !request.SkipToken && request.TokenID > 0 && effectiveQuota > 0 {
			if err := reserveTaskToken(tx, task.UserId, request.TokenID, effectiveQuota); err != nil {
				return err
			}
			tokenQuota = effectiveQuota
		}

		task.Quota = effectiveQuota
		task.PrivateData.BillingSource = request.Source
		task.PrivateData.TokenId = request.TokenID
		task.PrivateData.TokenQuota = tokenQuota
		task.PrivateData.TokenBilling = tokenQuota > 0
		task.PrivateData.BillingState = TaskBillingStateReserved
		if err := persistTaskBillingMutation(tx, task); err != nil {
			return err
		}
		mutation.Task = task
		mutation.Applied = true
		mutation.CurrentQuota = effectiveQuota
		return nil
	})
	if err != nil {
		return nil, err
	}
	if mutation.Task != nil {
		reconcileTaskBillingCacheAfterCommit(ctx, mutation.Task.ID)
	}
	return mutation, nil
}

func ClaimTaskSubmission(ctx context.Context, taskID int64, attempt *TaskSubmissionAttempt) (*Task, bool, error) {
	if taskID <= 0 {
		return nil, false, ErrTaskBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var claimedTask *Task
	claimed := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskBillingRow(tx, taskID)
		if err != nil {
			return err
		}
		claimedTask = task
		if task.PrivateData.BillingState != TaskBillingStateReserved {
			return nil
		}
		if attempt != nil {
			task.Platform = attempt.Platform
			task.ChannelId = attempt.ChannelID
			task.Action = attempt.Action
			task.Properties.OriginModelName = attempt.OriginModelName
			task.Properties.UpstreamModelName = attempt.UpstreamModelName
		}
		task.PrivateData.BillingState = TaskBillingStateDispatching
		if err := tx.Save(task).Error; err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return claimedTask, claimed, err
}

func ResetTaskSubmissionClaim(ctx context.Context, taskID int64) error {
	if taskID <= 0 {
		return ErrTaskBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskBillingRow(tx, taskID)
		if err != nil {
			return err
		}
		if task.PrivateData.BillingState != TaskBillingStateDispatching {
			return fmt.Errorf("%w: cannot reset submission from %s", ErrTaskBillingStateConflict, task.PrivateData.BillingState)
		}
		task.PrivateData.BillingState = TaskBillingStateReserved
		return tx.Save(task).Error
	})
}

func MarkTaskSubmissionAmbiguous(ctx context.Context, taskID int64, ambiguity TaskSubmissionAmbiguity) (*Task, bool, error) {
	if taskID <= 0 || (ambiguity.TargetQuota != nil && *ambiguity.TargetQuota < 0) {
		return nil, false, ErrTaskBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ambiguity.Reason == "" {
		ambiguity.Reason = "task submission outcome is unknown"
	}

	var ambiguousTask *Task
	marked := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskBillingRow(tx, taskID)
		if err != nil {
			return err
		}
		ambiguousTask = task
		if task.PrivateData.BillingState != TaskBillingStateDispatching &&
			task.PrivateData.BillingState != TaskBillingStateAmbiguous {
			return nil
		}
		if task.Status == TaskStatusSuccess || task.Status == TaskStatusFailure {
			return nil
		}

		if task.PrivateData.BillingState == TaskBillingStateDispatching {
			task.Status = TaskStatusUnknown
			task.Progress = "100%"
			task.FailReason = ambiguity.Reason
			task.FinishTime = time.Now().Unix()
		} else {
			if task.Status != TaskStatusUnknown {
				task.Status = TaskStatusUnknown
			}
			if task.Progress != "100%" {
				task.Progress = "100%"
			}
			if task.FailReason == "" {
				task.FailReason = ambiguity.Reason
			}
			if task.FinishTime == 0 {
				task.FinishTime = time.Now().Unix()
			}
		}
		task.PrivateData.BillingState = TaskBillingStateAmbiguous
		task.PrivateData.AccountingRequired = true
		if task.PrivateData.AccountingState == "" {
			task.PrivateData.AccountingState = TaskAccountingStatePending
		}
		if task.PrivateData.TargetQuota == nil && ambiguity.TargetQuota != nil {
			targetQuota := *ambiguity.TargetQuota
			task.PrivateData.TargetQuota = &targetQuota
		}
		if err := tx.Save(task).Error; err != nil {
			return err
		}
		if err := enqueueTaskAccounting(tx, task); err != nil {
			return err
		}
		marked = true
		return nil
	})
	return ambiguousTask, marked, err
}

func PersistTaskSubmissionAcceptance(ctx context.Context, taskID int64, acceptance TaskSubmissionAcceptance) (*Task, bool, error) {
	if taskID <= 0 || acceptance.UpstreamTaskID == "" || acceptance.TargetQuota < 0 ||
		(acceptance.Status != TaskStatusSubmitted && acceptance.Status != TaskStatusUnknown) {
		return nil, false, ErrTaskBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var acceptedTask *Task
	accepted := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskBillingRow(tx, taskID)
		if err != nil {
			return err
		}
		acceptedTask = task
		if task.PrivateData.BillingState == TaskBillingStateAccepted {
			if task.PrivateData.UpstreamTaskID != acceptance.UpstreamTaskID {
				return nil
			}
			accepted = true
			if acceptance.Status != TaskStatusUnknown || task.Status == TaskStatusSuccess || task.Status == TaskStatusFailure || task.Status == TaskStatusUnknown {
				return enqueueTaskAccounting(tx, task)
			}
		} else if task.PrivateData.BillingState != TaskBillingStateDispatching {
			return nil
		}

		applyTaskSubmissionAcceptance(task, acceptance)
		if err := tx.Save(task).Error; err != nil {
			return err
		}
		if err := enqueueTaskAccounting(tx, task); err != nil {
			return err
		}
		accepted = true
		return nil
	})
	return acceptedTask, accepted, err
}

func RecoverTaskSubmissionAcceptance(ctx context.Context, taskID int64, acceptance TaskSubmissionAcceptance) (*Task, bool, error) {
	if taskID <= 0 || acceptance.UpstreamTaskID == "" || acceptance.TargetQuota < 0 ||
		(acceptance.Status != TaskStatusSubmitted && acceptance.Status != TaskStatusUnknown) {
		return nil, false, ErrTaskBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var recoveredTask *Task
	recovered := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskBillingRow(tx, taskID)
		if err != nil {
			return err
		}
		recoveredTask = task
		if task.PrivateData.UpstreamTaskID != "" {
			if task.PrivateData.UpstreamTaskID != acceptance.UpstreamTaskID {
				return ErrTaskBillingStateConflict
			}
			recovered = task.PrivateData.BillingState == TaskBillingStateAccepted
			if recovered {
				return enqueueTaskAccounting(tx, task)
			}
			return nil
		}
		if task.Status == TaskStatusSuccess || task.Status == TaskStatusFailure || task.PrivateData.BillingState == TaskBillingStateRefunded {
			return nil
		}
		switch task.PrivateData.BillingState {
		case TaskBillingStateDispatching, TaskBillingStateAmbiguous, TaskBillingStateCompleted:
		default:
			return nil
		}

		applyTaskSubmissionAcceptance(task, acceptance)
		if err := tx.Save(task).Error; err != nil {
			return err
		}
		if err := enqueueTaskAccounting(tx, task); err != nil {
			return err
		}
		recovered = true
		return nil
	})
	return recoveredTask, recovered, err
}

func applyTaskSubmissionAcceptance(task *Task, acceptance TaskSubmissionAcceptance) {
	task.PrivateData.UpstreamTaskID = acceptance.UpstreamTaskID
	if acceptance.ChannelID > 0 {
		task.ChannelId = acceptance.ChannelID
	}
	if task.PrivateData.BillingContext == nil {
		task.PrivateData.BillingContext = &TaskBillingContext{}
	}
	task.PrivateData.BillingContext.OtherRatios = cloneTaskBillingRatios(acceptance.OtherRatios)
	task.PrivateData.BillingState = TaskBillingStateAccepted
	task.PrivateData.AccountingRequired = true
	if task.PrivateData.AccountingState == "" {
		task.PrivateData.AccountingState = TaskAccountingStatePending
	}
	task.PrivateData.TargetQuota = common.GetPointer(acceptance.TargetQuota)
	if task.PrivateData.AccountingState == TaskAccountingStatePending {
		task.PrivateData.AccountingQuota = acceptance.TargetQuota
	}
	task.Data = append(json.RawMessage(nil), acceptance.TaskData...)
	task.Status = acceptance.Status
	task.Progress = acceptance.Progress
	task.FailReason = acceptance.FailReason
	task.FinishTime = acceptance.FinishTime
}

func AdjustTaskBilling(ctx context.Context, taskID int64, targetQuota int) (*TaskBillingMutation, error) {
	return AdjustTaskBillingWithAudit(ctx, taskID, targetQuota, TaskBillingAuditRequest{
		Reason: "task billing settlement",
	})
}

func AdjustTaskBillingWithAudit(ctx context.Context, taskID int64, targetQuota int, audit TaskBillingAuditRequest) (*TaskBillingMutation, error) {
	if taskID <= 0 || targetQuota < 0 {
		return nil, ErrTaskBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}

	mutation := &TaskBillingMutation{}
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskBillingRow(tx, taskID)
		if err != nil {
			return err
		}
		if billingContext := task.PrivateData.BillingContext; billingContext != nil && billingContext.MaxQuota != nil && targetQuota > *billingContext.MaxQuota {
			targetQuota = *billingContext.MaxQuota
		}
		mutation.Task = task
		mutation.PreviousQuota = task.Quota
		mutation.CurrentQuota = task.Quota
		if task.PrivateData.BillingState == TaskBillingStateRefunded {
			return nil
		}

		fundingDelta := targetQuota - task.Quota
		tokenTarget := 0
		if task.PrivateData.TokenBilling {
			tokenTarget = targetQuota
		}
		tokenDelta := tokenTarget - task.PrivateData.TokenQuota
		if fundingDelta == 0 && tokenDelta == 0 {
			task.PrivateData.TargetQuota = nil
			if task.PrivateData.AccountingState == TaskAccountingStatePending {
				task.PrivateData.AccountingQuota = targetQuota
			}
			return tx.Save(task).Error
		}
		if err := adjustTaskFunding(tx, task, fundingDelta); err != nil {
			return err
		}
		_, err = adjustTaskToken(tx, task, tokenDelta)
		if err != nil {
			return err
		}
		if fundingDelta > 0 && (task.PrivateData.AccountingState == TaskAccountingStateStatsRecorded ||
			task.PrivateData.AccountingState == TaskAccountingStateCompleted) {
			if err := recordTaskAccountingQuotaIncrease(tx, task, fundingDelta); err != nil {
				return err
			}
		}

		task.Quota = targetQuota
		task.PrivateData.TokenQuota = tokenTarget
		task.PrivateData.TargetQuota = nil
		if task.PrivateData.AccountingState == TaskAccountingStatePending {
			task.PrivateData.AccountingQuota = targetQuota
		}
		if err := persistTaskBillingMutation(tx, task); err != nil {
			return err
		}
		if fundingDelta != 0 {
			logType := LogTypeConsume
			logQuota := fundingDelta
			if fundingDelta < 0 {
				logType = LogTypeRefund
				logQuota = -fundingDelta
			}
			if err := enqueueTaskBillingAudit(tx, task, TaskBillingAuditPayload{
				Operation:     TaskBillingAuditOperationSettlement,
				LogType:       logType,
				Quota:         logQuota,
				Reason:        audit.Reason,
				PreviousQuota: mutation.PreviousQuota,
				CurrentQuota:  targetQuota,
				QuotaClamps:   audit.QuotaClamps,
			}); err != nil {
				return err
			}
		}
		mutation.Applied = true
		mutation.CurrentQuota = targetQuota
		return nil
	})
	if err != nil {
		return nil, err
	}
	if mutation.Task != nil {
		reconcileTaskBillingCacheAfterCommit(ctx, mutation.Task.ID)
	}
	return mutation, nil
}

func RefundTaskBilling(ctx context.Context, taskID int64) (*TaskBillingMutation, error) {
	return RefundTaskBillingWithAudit(ctx, taskID, TaskBillingAuditRequest{
		Reason: "task billing refund",
	})
}

func RefundTaskBillingWithAudit(ctx context.Context, taskID int64, audit TaskBillingAuditRequest) (*TaskBillingMutation, error) {
	if taskID <= 0 {
		return nil, ErrTaskBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}

	mutation := &TaskBillingMutation{}
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskBillingRow(tx, taskID)
		if err != nil {
			return err
		}
		mutation.Task = task
		mutation.PreviousQuota = task.Quota
		mutation.CurrentQuota = task.Quota
		previousState := task.PrivateData.BillingState
		if task.PrivateData.BillingState == TaskBillingStateRefunded {
			return nil
		}
		if !taskBillingRefundAllowed(task) {
			return ErrTaskBillingRefundNotAllowed
		}

		fundingDelta := -task.Quota
		tokenDelta := -task.PrivateData.TokenQuota
		if err := adjustTaskFunding(tx, task, fundingDelta); err != nil {
			return err
		}
		_, err = adjustTaskToken(tx, task, tokenDelta)
		if err != nil {
			return err
		}

		task.Quota = 0
		task.PrivateData.TokenQuota = 0
		task.PrivateData.TokenBilling = false
		task.PrivateData.TargetQuota = nil
		task.PrivateData.BillingState = TaskBillingStateRefunded
		if err := persistTaskBillingMutation(tx, task); err != nil {
			return err
		}
		if mutation.PreviousQuota > 0 {
			if err := enqueueTaskBillingAudit(tx, task, TaskBillingAuditPayload{
				Operation:     TaskBillingAuditOperationRefund,
				LogType:       LogTypeRefund,
				Quota:         mutation.PreviousQuota,
				Reason:        audit.Reason,
				PreviousQuota: mutation.PreviousQuota,
				CurrentQuota:  0,
			}); err != nil {
				return err
			}
		}
		mutation.Applied = mutation.PreviousQuota != 0 || tokenDelta != 0 || previousState != TaskBillingStatePending
		mutation.CurrentQuota = 0
		return nil
	})
	if err != nil {
		return nil, err
	}
	if mutation.Task != nil {
		reconcileTaskBillingCacheAfterCommit(ctx, mutation.Task.ID)
	}
	return mutation, nil
}

func CompleteTaskBillingRecovery(ctx context.Context, taskID int64) error {
	if taskID <= 0 {
		return ErrTaskBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskBillingRow(tx, taskID)
		if err != nil {
			return err
		}
		if task.PrivateData.BillingState == TaskBillingStateRefunded || task.PrivateData.BillingState == TaskBillingStateCompleted {
			return nil
		}
		task.PrivateData.BillingState = TaskBillingStateCompleted
		return tx.Save(task).Error
	})
}

// UpdateWithStatusPreservingBilling applies poller-owned fields only when the
// complete previously observed snapshot is still current.
func (task *Task) UpdateWithStatusPreservingBilling(from TaskSnapshot) (bool, error) {
	if task == nil || task.ID <= 0 {
		return false, ErrTaskBillingInvalidRequest
	}
	won := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		current, err := lockTaskBillingRow(tx, task.ID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !current.Snapshot().Equal(from) || !from.Allows(task.Snapshot()) {
			return nil
		}
		privateData := current.PrivateData
		privateData.ResultURL = task.PrivateData.ResultURL
		privateData.ArchiveSource = task.PrivateData.ArchiveSource
		task.PrivateData = privateData
		task.UpdatedAt = time.Now().Unix()
		result := tx.Model(&Task{}).
			Where("id = ? AND status = ?", task.ID, from.Status).
			Updates(map[string]any{
				"status":       task.Status,
				"progress":     task.Progress,
				"submit_time":  task.SubmitTime,
				"start_time":   task.StartTime,
				"finish_time":  task.FinishTime,
				"fail_reason":  task.FailReason,
				"private_data": task.PrivateData,
				"data":         task.Data,
				"updated_at":   task.UpdatedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		won = result.RowsAffected > 0
		return nil
	})
	return won, err
}

// FailTaskBeforeSubmission terminalizes only states that cannot have an active
// upstream dispatcher. It intentionally leaves quota clearing to the refund transaction.
func FailTaskBeforeSubmission(ctx context.Context, taskID int64, reason string) (*Task, bool, error) {
	if taskID <= 0 {
		return nil, false, ErrTaskBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if reason == "" {
		reason = "task submission failed"
	}

	var failedTask *Task
	failed := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskBillingRow(tx, taskID)
		if err != nil {
			return err
		}
		failedTask = task
		switch task.PrivateData.BillingState {
		case "", TaskBillingStatePending, TaskBillingStateReserved, TaskBillingStateRefunded:
		default:
			return nil
		}
		if task.Status == TaskStatusSuccess || task.Status == TaskStatusUnknown {
			return nil
		}
		if task.Status == TaskStatusFailure {
			failed = true
			return enqueueTaskAccounting(tx, task)
		}
		task.Status = TaskStatusFailure
		task.Progress = "100%"
		task.FinishTime = time.Now().Unix()
		task.FailReason = reason
		if err := tx.Save(task).Error; err != nil {
			return err
		}
		if err := enqueueTaskAccounting(tx, task); err != nil {
			return err
		}
		failed = true
		return nil
	})
	return failedTask, failed, err
}

func cloneTaskBillingRatios(ratios map[string]float64) map[string]float64 {
	if len(ratios) == 0 {
		return nil
	}
	cloned := make(map[string]float64, len(ratios))
	for key, ratio := range ratios {
		cloned[key] = ratio
	}
	return cloned
}

func lockTaskBillingRow(tx *gorm.DB, taskID int64) (*Task, error) {
	var task Task
	if err := lockForUpdate(tx).Where("id = ?", taskID).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func reserveTaskWallet(tx *gorm.DB, userID int, quota int) error {
	if quota <= 0 {
		return nil
	}
	updated := tx.Model(&User{}).
		Where("id = ? AND quota >= ?", userID, quota).
		Update("quota", gorm.Expr("quota - ?", quota))
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected == 0 {
		return ErrTaskBillingInsufficientWallet
	}
	return nil
}

func reserveTaskSubscription(tx *gorm.DB, task *Task, quota int, mutation *TaskBillingMutation) error {
	now := common.GetTimestamp()
	var subscriptions []UserSubscription
	if err := lockForUpdate(tx).
		Where("user_id = ? AND status = ? AND end_time > ?", task.UserId, "active", now).
		Order("end_time asc, id asc").
		Find(&subscriptions).Error; err != nil {
		return err
	}
	if len(subscriptions) == 0 {
		return ErrTaskBillingNoActiveSubscription
	}
	for index := range subscriptions {
		subscription := &subscriptions[index]
		plan, err := getSubscriptionPlanByIdTx(tx, subscription.PlanId)
		if err != nil {
			return err
		}
		if err := maybeResetUserSubscriptionWithPlanTx(tx, subscription, plan, now); err != nil {
			return err
		}
		if subscription.AmountTotal > 0 && subscription.AmountTotal-subscription.AmountUsed < int64(quota) {
			continue
		}
		subscription.AmountUsed += int64(quota)
		if err := tx.Save(subscription).Error; err != nil {
			return err
		}
		task.PrivateData.SubscriptionId = subscription.Id
		mutation.SubscriptionAmountTotal = subscription.AmountTotal
		mutation.SubscriptionAmountUsed = subscription.AmountUsed
		mutation.SubscriptionPlanID = subscription.PlanId
		mutation.SubscriptionPlanTitle = plan.Title
		return nil
	}
	return ErrTaskBillingInsufficientSubscription
}

func reserveTaskToken(tx *gorm.DB, userID int, tokenID int, quota int) error {
	var token Token
	if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", tokenID, userID).First(&token).Error; err != nil {
		return err
	}
	if !token.UnlimitedQuota && token.RemainQuota < quota {
		return ErrTaskBillingInsufficientToken
	}
	token.RemainQuota -= quota
	token.UsedQuota += quota
	token.AccessedTime = common.GetTimestamp()
	return tx.Save(&token).Error
}

func adjustTaskFunding(tx *gorm.DB, task *Task, delta int) error {
	if delta == 0 {
		return nil
	}
	if task.PrivateData.BillingSource == TaskBillingSourceSubscription {
		var subscription UserSubscription
		if err := lockForUpdate(tx).Where("id = ?", task.PrivateData.SubscriptionId).First(&subscription).Error; err != nil {
			return err
		}
		newUsed := subscription.AmountUsed + int64(delta)
		if newUsed < 0 {
			newUsed = 0
		}
		if subscription.AmountTotal > 0 && newUsed > subscription.AmountTotal {
			return ErrTaskBillingInsufficientSubscription
		}
		subscription.AmountUsed = newUsed
		return tx.Save(&subscription).Error
	}
	if delta > 0 {
		updated := tx.Model(&User{}).
			Where("id = ? AND quota >= ?", task.UserId, delta).
			Update("quota", gorm.Expr("quota - ?", delta))
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected == 0 {
			return ErrTaskBillingInsufficientWallet
		}
		return nil
	}
	return increaseUserQuotaWithDB(tx, task.UserId, int64(-delta))
}

func adjustTaskToken(tx *gorm.DB, task *Task, delta int) (string, error) {
	if delta == 0 || task.PrivateData.TokenId <= 0 {
		return "", nil
	}
	var token Token
	query := lockForUpdate(tx).Where("id = ?", task.PrivateData.TokenId).First(&token)
	if errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if query.Error != nil {
		return "", query.Error
	}
	update := tx.Model(&Token{}).Where("id = ?", token.Id)
	if delta > 0 && !token.UnlimitedQuota {
		update = update.Where("remain_quota >= ? AND unlimited_quota = ?", delta, false)
	}
	update = update.Updates(map[string]any{
		"remain_quota":  gorm.Expr("remain_quota - ?", delta),
		"used_quota":    gorm.Expr("used_quota + ?", delta),
		"accessed_time": common.GetTimestamp(),
	})
	if update.Error != nil {
		return "", update.Error
	}
	if update.RowsAffected == 0 && delta > 0 && !token.UnlimitedQuota {
		return "", ErrTaskBillingInsufficientToken
	}
	return token.Key, nil
}

func taskBillingRefundAllowed(task *Task) bool {
	switch task.PrivateData.BillingState {
	case "", TaskBillingStatePending, TaskBillingStateReserved:
		return true
	case TaskBillingStateDispatching, TaskBillingStateAccepted:
		return task.Status == TaskStatusFailure
	case TaskBillingStateAmbiguous:
		return task.Status == TaskStatusFailure
	default:
		return false
	}
}

func loadTaskSubscriptionSnapshot(tx *gorm.DB, mutation *TaskBillingMutation) error {
	if mutation == nil || mutation.Task == nil || mutation.Task.PrivateData.BillingSource != TaskBillingSourceSubscription || mutation.Task.PrivateData.SubscriptionId <= 0 {
		return nil
	}
	var subscription UserSubscription
	if err := tx.Where("id = ?", mutation.Task.PrivateData.SubscriptionId).First(&subscription).Error; err != nil {
		return nil
	}
	mutation.SubscriptionAmountTotal = subscription.AmountTotal
	mutation.SubscriptionAmountUsed = subscription.AmountUsed
	mutation.SubscriptionPlanID = subscription.PlanId
	plan, err := getSubscriptionPlanByIdTx(tx, subscription.PlanId)
	if err == nil && plan != nil {
		mutation.SubscriptionPlanTitle = plan.Title
	}
	return nil
}

func TaskBillingRecoveryTime(now time.Time, delay time.Duration) int64 {
	if delay <= 0 {
		return now.Unix()
	}
	return now.Add(delay).Unix()
}
