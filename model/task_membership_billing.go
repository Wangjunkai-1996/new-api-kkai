package model

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"gorm.io/gorm"
)

// Native task writers lock Task before User and Ledger. Manual accounting must
// use that same order, including an idempotent replay of an already closed review.
func lockTaskMembershipBillingReviewTx(tx *gorm.DB, record *MembershipBillingRecord) (*Task, error) {
	if record == nil || record.Scope != MembershipBillingScopeTask || !strings.HasPrefix(record.RequestID, "task:") {
		return nil, ErrMembershipInvalidInput
	}
	var matches []Task
	if err := tx.Where("task_id = ? AND user_id = ?", strings.TrimPrefix(record.RequestID, "task:"), record.UserID).Limit(2).Find(&matches).Error; err != nil {
		return nil, err
	}
	if len(matches) != 1 {
		return nil, ErrMembershipConflict
	}
	task, err := lockTaskBillingRow(tx, matches[0].ID)
	if err != nil {
		return nil, err
	}
	if task.UserId != record.UserID || task.PrivateData.MembershipBillingRequestID != record.RequestID || task.PrivateData.BillingSource != record.Source {
		return nil, ErrMembershipConflict
	}
	return task, nil
}

func resolveTaskMembershipBillingReviewTx(tx *gorm.DB, task *Task, record *MembershipBillingRecord, input MembershipBillingResolutionInput) (*MembershipBillingRecord, error) {
	if task == nil || record == nil || task.PrivateData.MembershipBillingRequestID != record.RequestID || record.State != "reserved" ||
		task.PrivateData.BillingState == TaskBillingStateRefunded || task.PrivateData.BillingState == TaskBillingStateCompleted ||
		task.PrivateData.AccountingState == TaskAccountingStateStatsRecorded || task.PrivateData.AccountingState == TaskAccountingStateCompleted {
		return nil, ErrMembershipConflict
	}
	var result *MembershipBillingRecord
	var err error
	if input.Action == "release" {
		result, err = ReleaseMembershipBillingTx(tx, record.RequestID)
	} else {
		result, err = SettleMembershipBillingTx(tx, record.RequestID, input.ActualQuota)
	}
	if err != nil {
		return nil, err
	}
	task.Quota = int(result.ChargedQuota) // bounded by the int32 reservation
	task.PrivateData.TargetQuota = nil
	task.PrivateData.MembershipCompletionQuota = nil
	task.PrivateData.MembershipAssessedQuota = result.ActualQuota
	task.PrivateData.MembershipExcessQuota = result.ExcessQuota
	task.PrivateData.AccountingQuota = task.Quota
	task.PrivateData.TokenQuota = task.Quota
	task.PrivateData.TokenBilling = result.TokenReserved > 0 && task.Quota > 0
	if task.Status != TaskStatusSuccess && task.Status != TaskStatusFailure {
		task.Status = TaskStatusUnknown // verified cost is not proof of a delivered result
		task.FailReason = "Task billing was reviewed; upstream result remains unknown"
	}
	task.Progress = "100%"
	if task.FinishTime == 0 {
		task.FinishTime = common.GetTimestamp()
	}
	if input.Action == "release" {
		task.PrivateData.BillingState = TaskBillingStateRefunded
		task.PrivateData.AccountingState = TaskAccountingStateCompleted
	} else {
		task.PrivateData.BillingState = TaskBillingStateCompleted
		task.PrivateData.AccountingRequired = true
		if err := recordTaskAccountingQuotaIncrease(tx, task, task.Quota); err != nil {
			return nil, err
		}
		if err := tx.Model(&User{}).Where("id = ?", task.UserId).Update("request_count", gorm.Expr("request_count + ?", 1)).Error; err != nil {
			return nil, err
		}
		task.PrivateData.AccountingState = TaskAccountingStateStatsRecorded
		if err := enqueueTaskAccounting(tx, task); err != nil {
			return nil, err
		}
	}
	if err := persistTaskBillingMutation(tx, task); err != nil {
		return nil, err
	}
	return result, nil
}

// MarkTaskMembershipBillingUncertain retains the native task and its original
// payer while making a missing result discoverable. It does not declare failure
// or stop polling a known upstream job merely because a local timer expired.
func MarkTaskMembershipBillingUncertain(ctx context.Context, taskID int64, reason string) error {
	if taskID <= 0 || strings.TrimSpace(reason) == "" || len(reason) > 512 {
		return ErrMembershipInvalidInput
	}
	return MembershipTransaction(ctx, func(tx *gorm.DB) error {
		task, err := lockTaskBillingRow(tx, taskID)
		if err != nil {
			return err
		}
		return markTaskMembershipBillingUncertainTx(tx, task, reason)
	})
}

func markTaskMembershipBillingUncertainTx(tx *gorm.DB, task *Task, reason string) error {
	if task.PrivateData.MembershipBillingRequestID == "" {
		return nil
	}
	if _, _, err := lockMembershipUserTx(tx, task.UserId); err != nil {
		return err
	}
	var record MembershipBillingRecord
	if err := lockForUpdate(tx).First(&record, "request_id = ?", task.PrivateData.MembershipBillingRequestID).Error; err != nil {
		return err
	}
	if record.Scope != MembershipBillingScopeTask || record.UserID != task.UserId {
		return ErrMembershipConflict
	}
	if record.State != "reserved" || record.ReviewStartedAt > 0 {
		return nil
	}
	if err := tx.Model(&record).Updates(map[string]any{"review_started_at": common.GetTimestamp(), "review_reason": reason, "review_channel_id": task.ChannelId}).Error; err != nil {
		return err
	}
	if task.Status != TaskStatusSuccess && task.Status != TaskStatusFailure {
		task.Status = TaskStatusUnknown
		task.FailReason = "Upstream task outcome requires verification"
		if task.PrivateData.UpstreamTaskID != "" && task.Progress == "100%" {
			task.Progress = ""
		} else if task.PrivateData.UpstreamTaskID == "" {
			task.Progress = "100%" // no upstream identifier exists to poll safely
		}
		return tx.Save(task).Error
	}
	return nil
}

func reserveTaskMembershipTx(tx *gorm.DB, task *Task, request TaskBillingReservationRequest, mutation *TaskBillingMutation) error {
	input := request.Membership
	if input == nil || input.UserID != task.UserId || input.TokenID != request.TokenID || input.SkipToken != request.SkipToken || input.RequestID != "task:"+task.TaskID {
		return ErrTaskBillingInvalidRequest
	}
	input.Scope = MembershipBillingScopeTask
	record, err := ReserveMembershipBillingTx(tx, *input)
	if err != nil {
		return err
	}
	if bc := task.PrivateData.BillingContext; bc != nil && bc.MaxQuota != nil && record.ReservedQuota > int64(*bc.MaxQuota) {
		return fmt.Errorf("%w: full task budget exceeds the authorized maximum", ErrTaskBillingInvalidRequest)
	}
	var pricing types.MembershipPricing
	if err := common.UnmarshalJsonStr(record.Snapshot, &pricing); err != nil || pricing.Version != 1 || pricing.GroupKey != record.GroupKey {
		return ErrTaskBillingInvalidRequest
	}
	task.Quota = int(record.ReservedQuota)
	task.PrivateData.BillingSource = record.Source
	task.PrivateData.MembershipBillingRequestID = record.RequestID
	task.PrivateData.MembershipPricing = &pricing
	task.PrivateData.TokenId = record.TokenID
	task.PrivateData.TokenQuota = int(record.TokenReserved)
	task.PrivateData.TokenBilling = record.TokenReserved > 0
	task.PrivateData.BillingState = TaskBillingStateReserved
	if task.PrivateData.BillingContext == nil {
		task.PrivateData.BillingContext = &TaskBillingContext{}
	}
	bc := task.PrivateData.BillingContext
	bc.GroupRatio = pricing.Price.GroupRatioInfo.GroupRatio
	bc.TieredSnapshot = pricing.Tiered
	bc.ModelPrice, bc.ModelRatio = pricing.Price.ModelPrice, pricing.Price.ModelRatio
	bc.OtherRatios = cloneTaskBillingRatios(pricing.OtherRatios)
	if err := persistTaskBillingMutation(tx, task); err != nil {
		return err
	}
	mutation.Task, mutation.Membership, mutation.Applied, mutation.CurrentQuota = task, record, true, task.Quota
	return nil
}

func settleTaskMembershipTx(tx *gorm.DB, task *Task, assessed int, audit TaskBillingAuditRequest, mutation *TaskBillingMutation) error {
	mutation.Task, mutation.PreviousQuota, mutation.CurrentQuota = task, task.Quota, task.Quota
	if task.PrivateData.BillingState == TaskBillingStateRefunded || task.PrivateData.BillingState == TaskBillingStateCompleted {
		return nil
	}
	if task.Status != TaskStatusSuccess {
		// Accepted and unknown tasks still own a reservation. Only a terminal
		// successful result can turn it into a charge; failures use Release.
		task.PrivateData.TargetQuota = common.GetPointer(assessed)
		return tx.Save(task).Error
	}
	if task.PrivateData.MembershipCompletionQuota != nil {
		assessed = *task.PrivateData.MembershipCompletionQuota
	}
	record, err := SettleMembershipBillingTx(tx, task.PrivateData.MembershipBillingRequestID, int64(assessed))
	if err != nil {
		return err
	}
	mutation.Membership = record
	if !record.Applied && task.PrivateData.TargetQuota == nil {
		return nil
	}
	if audit.TieredSnapshot != nil && task.PrivateData.BillingContext != nil {
		task.PrivateData.BillingContext.TieredSnapshot = audit.TieredSnapshot
	}
	task.Quota = int(record.ChargedQuota)
	task.PrivateData.MembershipAssessedQuota = record.ActualQuota
	task.PrivateData.MembershipExcessQuota = record.ExcessQuota
	if task.PrivateData.TokenBilling {
		task.PrivateData.TokenQuota = task.Quota
	}
	task.PrivateData.TargetQuota = nil
	task.PrivateData.AccountingQuota = task.Quota
	if err := persistTaskBillingMutation(tx, task); err != nil {
		return err
	}
	mutation.Membership, mutation.Applied, mutation.CurrentQuota = record, record.Applied, task.Quota
	return nil
}

func releaseTaskMembershipTx(tx *gorm.DB, task *Task, mutation *TaskBillingMutation) error {
	record, err := ReleaseMembershipBillingTx(tx, task.PrivateData.MembershipBillingRequestID)
	if err != nil {
		return err
	}
	task.Quota = 0
	task.PrivateData.TokenQuota = 0
	task.PrivateData.TokenBilling = false
	task.PrivateData.TargetQuota = nil
	task.PrivateData.AccountingQuota = 0
	task.PrivateData.BillingState = TaskBillingStateRefunded
	if err := persistTaskBillingMutation(tx, task); err != nil {
		return err
	}
	mutation.Membership, mutation.Applied, mutation.CurrentQuota = record, record.Applied, 0
	return nil
}
