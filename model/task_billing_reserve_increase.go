package model

import (
	"context"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// IncreaseTaskBillingReservation funds a more expensive retry before dispatch.
// It keeps the persisted funding identity and commits quota and cache outbox in
// one transaction. Accepted/ambiguous tasks must use the settlement path.
func IncreaseTaskBillingReservation(ctx context.Context, taskID int64, targetQuota int) (*TaskBillingMutation, error) {
	if taskID <= 0 || targetQuota < 0 || int64(targetQuota) > math.MaxInt32 {
		return nil, ErrTaskBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	mutation := &TaskBillingMutation{}
	err := MembershipTransaction(ctx, func(tx *gorm.DB) error {
		*mutation = TaskBillingMutation{}
		task, err := lockTaskBillingRow(tx, taskID)
		if err != nil {
			return err
		}
		if task.PrivateData.BillingState != TaskBillingStateReserved {
			return ErrTaskBillingStateConflict
		}
		mutation.Task, mutation.PreviousQuota, mutation.CurrentQuota = task, task.Quota, task.Quota
		if targetQuota <= task.Quota {
			return loadTaskSubscriptionSnapshot(tx, mutation)
		}
		if task.PrivateData.MembershipBillingRequestID != "" || task.PrivateData.BillingSource == MembershipSource {
			return fmt.Errorf("%w: membership reservation is frozen", ErrTaskBillingInvalidRequest)
		}
		if contract := task.PrivateData.BillingContext; contract != nil && contract.MaxQuota != nil && targetQuota > *contract.MaxQuota {
			return fmt.Errorf("%w: retry exceeds frozen maximum quota", ErrTaskBillingInvalidRequest)
		}
		delta := targetQuota - task.Quota
		switch task.PrivateData.BillingSource {
		case TaskBillingSourceWallet:
			if err := reserveTaskWallet(tx, task.UserId, delta); err != nil {
				return err
			}
		case TaskBillingSourceSubscription:
			var subscription UserSubscription
			if err := lockForUpdate(tx).Where("id = ? AND user_id = ? AND status = ? AND end_time > ?", task.PrivateData.SubscriptionId, task.UserId, "active", common.GetTimestamp()).First(&subscription).Error; err != nil {
				return err
			}
			if subscription.AmountUsed > math.MaxInt64-int64(delta) || (subscription.AmountTotal > 0 && subscription.AmountTotal-subscription.AmountUsed < int64(delta)) {
				return ErrTaskBillingInsufficientSubscription
			}
			subscription.AmountUsed += int64(delta)
			if err := tx.Save(&subscription).Error; err != nil {
				return err
			}
		default:
			return ErrTaskBillingInvalidRequest
		}
		if task.PrivateData.TokenBilling {
			if task.PrivateData.TokenId <= 0 || task.PrivateData.TokenQuota > math.MaxInt32-delta {
				return ErrTaskBillingInvalidRequest
			}
			if err := reserveTaskToken(tx, task.UserId, task.PrivateData.TokenId, delta); err != nil {
				return err
			}
			task.PrivateData.TokenQuota += delta
		}
		task.Quota = targetQuota
		if err := persistTaskBillingMutation(tx, task); err != nil {
			return err
		}
		mutation.Applied, mutation.CurrentQuota = true, targetQuota
		return loadTaskSubscriptionSnapshot(tx, mutation)
	})
	if err != nil {
		return nil, err
	}
	if mutation.Applied {
		reconcileTaskBillingCacheAfterCommit(ctx, taskID)
	}
	return mutation, nil
}
