package service

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ReserveImageGenerationBillingForUserTx applies the existing funding preference
// entirely inside the acceptance transaction. It never reads model.DB or Redis.
func ReserveImageGenerationBillingForUserTx(ctx context.Context, tx *gorm.DB, generationID int64, quota int, preference string, free bool) (*model.ImageGenerationBillingMutation, error) {
	if tx == nil || generationID <= 0 || quota < 0 {
		return nil, model.ErrImageBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx = tx.WithContext(ctx)
	preference = common.NormalizeBillingPreference(preference)
	if free || preference == "wallet_only" {
		return reserveImageFundingAttemptTx(tx, generationID, model.TaskBillingSourceWallet, quota)
	}
	if preference == "subscription_only" {
		return reserveImageFundingAttemptTx(tx, generationID, model.TaskBillingSourceSubscription, quota)
	}
	if preference == "wallet_first" {
		mutation, err := reserveImageFundingAttemptTx(tx, generationID, model.TaskBillingSourceWallet, quota)
		if !errors.Is(err, model.ErrImageBillingInsufficientWallet) {
			return mutation, err
		}
		return reserveImageFundingAttemptTx(tx, generationID, model.TaskBillingSourceSubscription, quota)
	}
	var generation model.KKAIImageGeneration
	if err := tx.Select("id", "user_id").First(&generation, generationID).Error; err != nil {
		return nil, err
	}
	var activeCount int64
	active := tx.Model(&model.UserSubscription{}).Where("user_id = ? AND status = ? AND end_time > ?", generation.UserID, "active", common.GetTimestamp())
	if err := active.Count(&activeCount).Error; err != nil {
		return nil, err
	}
	if activeCount == 0 {
		return reserveImageFundingAttemptTx(tx, generationID, model.TaskBillingSourceWallet, quota)
	}
	mutation, err := reserveImageFundingAttemptTx(tx, generationID, model.TaskBillingSourceSubscription, quota)
	if !errors.Is(err, model.ErrImageBillingInsufficientSubscription) {
		return mutation, err
	}
	var strictCount int64
	if queryErr := active.Where("allow_wallet_overflow = ?", false).Count(&strictCount).Error; queryErr != nil {
		return nil, queryErr
	}
	if strictCount != 0 {
		return nil, err
	}
	return reserveImageFundingAttemptTx(tx, generationID, model.TaskBillingSourceWallet, quota)
}

// A failed source may have tentatively reset a subscription period. Roll back
// that attempt before considering another source within the acceptance transaction.
func reserveImageFundingAttemptTx(tx *gorm.DB, generationID int64, source string, quota int) (*model.ImageGenerationBillingMutation, error) {
	var mutation *model.ImageGenerationBillingMutation
	err := tx.Transaction(func(attempt *gorm.DB) error {
		var err error
		mutation, err = model.ReserveImageGenerationBillingTx(attempt, generationID, source, quota)
		return err
	})
	return mutation, err
}

// RestoreImageGenerationBilling reconnects the relay's billing interface to an
// already reserved generation; restoring never reserves or debits funds again.
func RestoreImageGenerationBilling(c *gin.Context, db *gorm.DB, generation *model.KKAIImageGeneration, info *relaycommon.RelayInfo, fence model.ImageExecutionFence) error {
	if c == nil || c.Request == nil || db == nil || generation == nil || generation.ID <= 0 || info == nil {
		return model.ErrImageBillingInvalidRequest
	}
	var stored model.KKAIImageGeneration
	var subscription model.UserSubscription
	var membershipRecord *model.MembershipBillingRecord
	err := db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if _, err := model.RequireImageExecutionFenceTx(tx, generation.ID, fence); err != nil {
			return err
		}
		if err := tx.First(&stored, generation.ID).Error; err != nil {
			return err
		}
		if (stored.Status != model.ImageGenerationStatusSubmitting && stored.Status != model.ImageGenerationStatusRecovering) ||
			(stored.BillingState != model.ImageGenerationBillingStateReserved && stored.BillingState != model.ImageGenerationBillingStateProcessing) ||
			stored.UserID != info.UserId || stored.TokenID != info.TokenId {
			return model.ErrImageBillingStateConflict
		}
		if stored.BillingSource == model.TaskBillingSourceSubscription {
			return tx.First(&subscription, stored.SubscriptionID).Error
		}
		if stored.BillingSource == model.MembershipSource || stored.BillingSource == model.ImageMembershipWalletSource {
			membershipRecord = &model.MembershipBillingRecord{}
			if err := tx.First(membershipRecord, "request_id = ?", model.MembershipImageBillingKey(stored.ID)).Error; err != nil {
				return err
			}
			if membershipRecord.State != "reserved" || membershipRecord.Scope != model.MembershipBillingScopeImage ||
				membershipRecord.UserID != stored.UserID || membershipRecord.TokenID != stored.TokenID ||
				membershipRecord.ReservedQuota != int64(stored.ReservedQuota) ||
				(stored.BillingSource == model.MembershipSource && membershipRecord.Source != model.MembershipSource) ||
				(stored.BillingSource == model.ImageMembershipWalletSource && membershipRecord.Source != model.MembershipWalletSource) ||
				(info.UsingGroup != "" && info.UsingGroup != membershipRecord.GroupKey) {
				return model.ErrImageBillingStateConflict
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	info.BillingSource = stored.BillingSource
	info.FinalPreConsumedQuota = stored.ReservedQuota
	info.SubscriptionId = stored.SubscriptionID
	info.SubscriptionPreConsumed = int64(stored.ReservedQuota)
	info.SubscriptionAmountTotal = subscription.AmountTotal
	info.SubscriptionAmountUsedAfterPreConsume = subscription.AmountUsed
	if membershipRecord != nil {
		if err := ApplyMembershipReservation(info, membershipRecord); err != nil {
			return err
		}
	}
	if guard, ok := imageStudioBillingGuardFromContext(c); ok {
		guard.maxQuota = stored.ReservedQuota
	}
	info.Billing = &imageGenerationBillingSession{
		db: db, generationID: stored.ID, relayInfo: info, preConsumed: stored.ReservedQuota,
		fence: &fence, expectedStatus: stored.Status,
	}
	return SetImageStudioGenerationID(c, stored.ID)
}
