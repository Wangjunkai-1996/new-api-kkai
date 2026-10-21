package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const ImageExecutionFailureContentPolicy = "upstream_content_policy"

// AdoptImageExecution never rewinds a dispatch. A new owner may finish durable
// output or reconcile an unknown result, but cannot send an already started POST.
func AdoptImageExecution(ctx context.Context, db *gorm.DB, generationID int64, fence model.ImageExecutionFence) (*model.KKAIImageExecution, *model.KKAIImageGeneration, error) {
	var execution *model.KKAIImageExecution
	var generation model.KKAIImageGeneration
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := model.LockImageStudioCapacityTx(tx); err != nil {
			return err
		}
		var existing model.KKAIImageExecution
		if err := tx.First(&existing, "generation_id = ?", generationID).Error; err != nil {
			return err
		}
		if existing.State == model.ImageGenerationExecutionDone {
			execution = &existing
			return nil
		}
		var err error
		execution, err = model.ClaimImageGenerationExecutionTx(tx, generationID, fence)
		if err != nil {
			return err
		}
		if err := tx.First(&generation, generationID).Error; err != nil {
			return err
		}
		if execution.State != model.ImageGenerationExecutionQueued && execution.State != model.ImageGenerationExecutionPreparing {
			return nil
		}
		now := time.Now()
		if execution.QueueDeadlineAt <= now.Unix() && execution.ExecutionDeadlineAt == 0 {
			return nil
		}
		if execution.ExecutionDeadlineAt == 0 {
			execution.ExecutionDeadlineAt = now.Add(ImageExecutionTimeout).Unix()
			execution.RecoveryDeadlineAt = now.Add(ImageExecutionTimeout + ImageArchiveTimeout + ImageRecoveryGrace).Unix()
		}
		execution.State = model.ImageGenerationExecutionPreparing
		execution.UpdatedAt = now.Unix()
		return tx.Save(execution).Error
	})
	return execution, &generation, err
}

func MarkImageExecutionUnknown(ctx context.Context, db *gorm.DB, generationID int64, fence model.ImageExecutionFence, upstreamID, failureCode string) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		execution, err := model.RequireImageExecutionFenceTx(tx, generationID, fence)
		if err != nil {
			return err
		}
		if execution.State != model.ImageGenerationExecutionDispatching && execution.State != model.ImageGenerationExecutionUnknown {
			return ErrImageGenerationConflict
		}
		if len(upstreamID) > 191 {
			upstreamID = upstreamID[:191]
		}
		now := time.Now().Unix()
		if code := imageGenerationErrorCode(failureCode); code != "" {
			var generation model.KKAIImageGeneration
			if err := lockRowsForUpdate(tx).Where("id = ? AND status = ? AND deleted_at = 0", generationID, model.ImageGenerationStatusSubmitting).First(&generation).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrImageGenerationConflict
				}
				return err
			}
			if err := tx.Model(&generation).Updates(map[string]any{"error_code": code, "error_message": "", "updated_at": now}).Error; err != nil {
				return err
			}
		}
		return tx.Model(execution).Updates(map[string]any{"state": model.ImageGenerationExecutionUnknown, "upstream_request_id": upstreamID, "updated_at": now}).Error
	})
}

// CompleteImageExecution is shared by normal completion and crash recovery. A
// durable accounting intent and exactly its ready output set are required to pay.
func CompleteImageExecution(ctx context.Context, db *gorm.DB, generationID int64, fence model.ImageExecutionFence, failureCode string) error {
	var completed model.KKAIImageGeneration
	var mutation *model.ImageGenerationBillingMutation
	err := model.MembershipTransactionWithDB(ctx, db, func(tx *gorm.DB) error {
		mutation = nil
		if err := model.LockImageStudioCapacityTx(tx); err != nil {
			return err
		}
		execution, err := model.RequireImageExecutionFenceTx(tx, generationID, fence)
		if err != nil {
			return err
		}
		var generation model.KKAIImageGeneration
		if err := tx.First(&generation, generationID).Error; err != nil {
			return err
		}
		if generation.Status != model.ImageGenerationStatusSubmitting {
			return ErrImageGenerationConflict
		}
		accounting, accountingErr := model.GetImageGenerationAccounting(ctx, tx, generationID)
		var ready, staging int64
		if err := tx.Model(&model.KKAIImageAsset{}).Where("generation_id = ? AND state = ? AND deleted_at = 0", generationID, model.ImageAssetStateReady).Count(&ready).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.KKAIImageAsset{}).Where("generation_id = ? AND state = ? AND deleted_at = 0", generationID, model.ImageAssetStateStaging).Count(&staging).Error; err != nil {
			return err
		}
		canSettle := accountingErr == nil && ready > 0 && ready <= int64(generation.RequestedCount) && staging == 0 && accounting.OutputCount == int(ready)
		if accountingErr != nil && !errors.Is(accountingErr, gorm.ErrRecordNotFound) && !errors.Is(accountingErr, model.ErrImageBillingInvalidRequest) {
			return accountingErr
		}
		status, stage, code, message := model.ImageGenerationStatusSucceeded, "", "", ""
		quota, count := 0, 0
		if canSettle {
			mutation, err = model.SettleImageGenerationBillingTx(tx, generationID, accounting.TargetQuota, generation.Status)
			if err != nil {
				return err
			}
			quota, count = accounting.TargetQuota, int(ready)
			if count < generation.RequestedCount {
				status = model.ImageGenerationStatusPartial
			}
		} else {
			if failureCode == "" {
				return model.ErrImageAccountingNotReady
			}
			if err := DiscardSubmittingImageGenerationAssets(ctx, tx, generationID); err != nil {
				return err
			}
			mutation, err = model.RefundImageGenerationBillingTx(tx, generationID, generation.Status)
			if err != nil {
				return err
			}
			status, stage, code, message = model.ImageGenerationStatusFailed, "prepare", failureCode, "image request could not be completed"
			if failureCode == ImageExecutionFailureContentPolicy {
				stage, message = "upstream", "image request was rejected by the provider content policy; reserved quota was refunded"
			} else if execution.State == model.ImageGenerationExecutionDispatching || execution.State == model.ImageGenerationExecutionUnknown || execution.State == model.ImageGenerationExecutionResultReady {
				status, stage, message = model.ImageGenerationStatusUnknown, "reconcile", "image provider outcome could not be recovered; reserved quota was refunded"
			}
			if failureCode == "upstream_outcome_unknown" {
				if storedCode := imageGenerationErrorCode(generation.ErrorCode); storedCode != "" {
					code = storedCode
				}
			}
		}
		if err := FinishImageGeneration(ctx, tx, generationID, status, count, quota, stage, code, message); err != nil {
			return err
		}
		if err := tx.Model(execution).Updates(map[string]any{"state": model.ImageGenerationExecutionDone, "updated_at": time.Now().Unix()}).Error; err != nil {
			return err
		}
		completed = generation
		completed.Status, completed.SucceededCount, completed.ErrorCode = status, count, code
		return tx.Where("generation_id = ?", generationID).Delete(&model.KKAIImageGenerationReference{}).Error
	})
	if err == nil {
		model.MembershipBillingAfterCommit(mutation.MembershipRecord)
		logger.LogInfo(ctx, fmt.Sprintf("event=image_async_completed generation_id=%d request_id=%q elapsed_ms=%d output_count=%d status=%s code=%s", completed.ID, completed.RequestID, time.Since(time.Unix(completed.CreatedAt, 0)).Milliseconds(), completed.SucceededCount, completed.Status, completed.ErrorCode))
	}
	return err
}
