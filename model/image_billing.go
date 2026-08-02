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

const KKAIOutboxTopicImageBillingCacheReconcile = "image.billing.cache.reconcile.v1"

var (
	ErrImageBillingInvalidRequest           = errors.New("invalid image billing request")
	ErrImageBillingStateConflict            = errors.New("image billing state conflict")
	ErrImageBillingInsufficientWallet       = errors.New("insufficient wallet quota")
	ErrImageBillingNoActiveSubscription     = errors.New("no active subscription")
	ErrImageBillingInsufficientSubscription = errors.New("insufficient subscription quota")
)

type ImageBillingCacheReconcilePayload struct {
	GenerationID int64 `json:"generation_id"`
}

type ImageGenerationBillingMutation struct {
	Generation              *KKAIImageGeneration
	Applied                 bool
	PreviousQuota           int
	CurrentQuota            int
	SubscriptionAmountTotal int64
	SubscriptionAmountUsed  int64
}

func ReserveImageGenerationBilling(
	ctx context.Context,
	db *gorm.DB,
	generationID int64,
	source string,
	quota int,
) (*ImageGenerationBillingMutation, error) {
	if db == nil || generationID <= 0 || quota < 0 ||
		(source != TaskBillingSourceWallet && source != TaskBillingSourceSubscription) {
		return nil, ErrImageBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	mutation := &ImageGenerationBillingMutation{}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		generation, err := lockImageGenerationBillingRow(tx, generationID)
		if err != nil {
			return err
		}
		mutation.Generation = generation
		mutation.PreviousQuota = generation.ReservedQuota
		mutation.CurrentQuota = generation.ReservedQuota
		if generation.Status != ImageGenerationStatusSubmitting {
			return ErrImageBillingStateConflict
		}
		if generation.BillingState != ImageGenerationBillingStatePending {
			if generation.BillingState == ImageGenerationBillingStateReserved &&
				generation.BillingSource == source && generation.ReservedQuota == quota {
				return loadImageSubscriptionSnapshot(tx, mutation)
			}
			return ErrImageBillingStateConflict
		}

		effectiveQuota := quota
		if source == TaskBillingSourceSubscription && effectiveQuota <= 0 {
			effectiveQuota = 1
		}
		switch source {
		case TaskBillingSourceWallet:
			if err := reserveImageWallet(tx, generation.UserID, effectiveQuota); err != nil {
				return err
			}
		case TaskBillingSourceSubscription:
			subscriptionID, total, used, err := reserveImageSubscription(
				tx, generation.UserID, effectiveQuota,
			)
			if err != nil {
				return err
			}
			generation.SubscriptionID = subscriptionID
			mutation.SubscriptionAmountTotal = total
			mutation.SubscriptionAmountUsed = used
		}
		generation.BillingSource = source
		generation.BillingState = ImageGenerationBillingStateReserved
		generation.ReservedQuota = effectiveQuota
		generation.HeartbeatAt = time.Now().Unix()
		generation.UpdatedAt = generation.HeartbeatAt
		if err := persistImageGenerationBillingMutation(tx, generation); err != nil {
			return err
		}
		mutation.Applied = true
		mutation.CurrentQuota = effectiveQuota
		return nil
	})
	if err != nil {
		return nil, err
	}
	if mutation.Generation != nil {
		reconcileImageBillingCacheAfterCommit(ctx, db, mutation.Generation.ID)
	}
	return mutation, nil
}

func MarkImageGenerationDispatching(
	ctx context.Context, db *gorm.DB, generationID int64,
) error {
	if db == nil || generationID <= 0 {
		return ErrImageBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().Unix()
	updated := db.WithContext(ctx).Model(&KKAIImageGeneration{}).
		Where(
			"id = ? AND status = ? AND billing_state IN ?",
			generationID,
			ImageGenerationStatusSubmitting,
			[]string{ImageGenerationBillingStateReserved, ImageGenerationBillingStateProcessing},
		).
		Updates(map[string]any{
			"billing_state": ImageGenerationBillingStateProcessing,
			"heartbeat_at":  now,
			"updated_at":    now,
		})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrImageBillingStateConflict
	}
	return nil
}

func TouchImageGeneration(
	ctx context.Context, db *gorm.DB, generationID int64,
) (bool, error) {
	if db == nil || generationID <= 0 {
		return false, ErrImageBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().Unix()
	updated := db.WithContext(ctx).Model(&KKAIImageGeneration{}).
		Where("id = ? AND status = ?", generationID, ImageGenerationStatusSubmitting).
		Updates(map[string]any{"heartbeat_at": now, "updated_at": now})
	return updated.RowsAffected == 1, updated.Error
}

func SettleImageGenerationBilling(
	ctx context.Context, db *gorm.DB, generationID int64, targetQuota int,
) (*ImageGenerationBillingMutation, error) {
	return settleImageGenerationBilling(
		ctx, db, generationID, targetQuota, ImageGenerationStatusSubmitting,
	)
}

func SettleRecoveringImageGenerationBilling(
	ctx context.Context, db *gorm.DB, generationID int64, targetQuota int,
) (*ImageGenerationBillingMutation, error) {
	return settleImageGenerationBilling(
		ctx, db, generationID, targetQuota, ImageGenerationStatusRecovering,
	)
}

func settleImageGenerationBilling(
	ctx context.Context,
	db *gorm.DB,
	generationID int64,
	targetQuota int,
	expectedStatus string,
) (*ImageGenerationBillingMutation, error) {
	if db == nil || generationID <= 0 || targetQuota < 0 {
		return nil, ErrImageBillingInvalidRequest
	}
	if expectedStatus != ImageGenerationStatusSubmitting &&
		expectedStatus != ImageGenerationStatusRecovering {
		return nil, ErrImageBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	mutation := &ImageGenerationBillingMutation{}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		generation, err := lockImageGenerationBillingRow(tx, generationID)
		if err != nil {
			return err
		}
		mutation.Generation = generation
		mutation.PreviousQuota = generation.ReservedQuota
		mutation.CurrentQuota = generation.ReservedQuota
		if generation.Status != expectedStatus {
			return ErrImageBillingStateConflict
		}
		if generation.BillingState == ImageGenerationBillingStateSettled {
			if generation.ReservedQuota != targetQuota {
				return ErrImageBillingStateConflict
			}
			return loadImageSubscriptionSnapshot(tx, mutation)
		}
		accounting, err := imageGenerationAccountingInTransaction(tx, generationID)
		if err != nil || accounting.TargetQuota != targetQuota {
			if err != nil {
				return err
			}
			return ErrImageBillingStateConflict
		}
		if generation.BillingState != ImageGenerationBillingStateReserved &&
			generation.BillingState != ImageGenerationBillingStateProcessing {
			return ErrImageBillingStateConflict
		}
		delta := targetQuota - generation.ReservedQuota
		if err := adjustImageFunding(tx, generation, delta); err != nil {
			return err
		}
		now := time.Now().Unix()
		generation.BillingState = ImageGenerationBillingStateSettled
		generation.ReservedQuota = targetQuota
		generation.FinalQuota = targetQuota
		generation.HeartbeatAt = now
		generation.UpdatedAt = now
		if err := persistImageGenerationBillingMutation(tx, generation); err != nil {
			return err
		}
		if err := applyImageGenerationAccountingStatistics(tx, generation, accounting); err != nil {
			return err
		}
		mutation.Applied = true
		mutation.CurrentQuota = targetQuota
		return loadImageSubscriptionSnapshot(tx, mutation)
	})
	if err != nil {
		return nil, err
	}
	if mutation.Generation != nil {
		reconcileImageBillingCacheAfterCommit(ctx, db, mutation.Generation.ID)
	}
	return mutation, nil
}

func RefundImageGenerationBilling(
	ctx context.Context, db *gorm.DB, generationID int64,
) (*ImageGenerationBillingMutation, error) {
	return refundImageGenerationBilling(
		ctx, db, generationID, ImageGenerationStatusSubmitting,
	)
}

func RefundRecoveringImageGenerationBilling(
	ctx context.Context, db *gorm.DB, generationID int64,
) (*ImageGenerationBillingMutation, error) {
	return refundImageGenerationBilling(
		ctx, db, generationID, ImageGenerationStatusRecovering,
	)
}

func refundImageGenerationBilling(
	ctx context.Context,
	db *gorm.DB,
	generationID int64,
	expectedStatus string,
) (*ImageGenerationBillingMutation, error) {
	if db == nil || generationID <= 0 {
		return nil, ErrImageBillingInvalidRequest
	}
	if expectedStatus != ImageGenerationStatusSubmitting &&
		expectedStatus != ImageGenerationStatusRecovering {
		return nil, ErrImageBillingInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	mutation := &ImageGenerationBillingMutation{}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		generation, err := lockImageGenerationBillingRow(tx, generationID)
		if err != nil {
			return err
		}
		mutation.Generation = generation
		mutation.PreviousQuota = generation.ReservedQuota
		mutation.CurrentQuota = generation.ReservedQuota
		if generation.Status != expectedStatus {
			return ErrImageBillingStateConflict
		}
		if generation.BillingState == ImageGenerationBillingStateRefunded {
			return nil
		}
		if generation.BillingState != ImageGenerationBillingStatePending &&
			generation.BillingState != ImageGenerationBillingStateReserved &&
			generation.BillingState != ImageGenerationBillingStateProcessing {
			return ErrImageBillingStateConflict
		}
		if generation.BillingState != ImageGenerationBillingStatePending && generation.ReservedQuota > 0 {
			if err := adjustImageFunding(tx, generation, -generation.ReservedQuota); err != nil {
				return err
			}
		}
		now := time.Now().Unix()
		generation.BillingState = ImageGenerationBillingStateRefunded
		generation.ReservedQuota = 0
		generation.FinalQuota = 0
		generation.HeartbeatAt = now
		generation.UpdatedAt = now
		if err := persistImageGenerationBillingMutation(tx, generation); err != nil {
			return err
		}
		mutation.Applied = true
		mutation.CurrentQuota = 0
		return nil
	})
	if err != nil {
		return nil, err
	}
	if mutation.Generation != nil {
		reconcileImageBillingCacheAfterCommit(ctx, db, mutation.Generation.ID)
	}
	return mutation, nil
}

func ReconcileImageGenerationBillingCache(
	ctx context.Context, db *gorm.DB, generationID int64,
) error {
	if db == nil || generationID <= 0 {
		return ErrImageBillingInvalidRequest
	}
	if !common.RedisEnabled {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var generation KKAIImageGeneration
	if err := db.WithContext(ctx).Select("id", "user_id", "billing_source").First(
		&generation, generationID,
	).Error; err != nil {
		return err
	}
	if generation.BillingSource != TaskBillingSourceWallet {
		return nil
	}
	var quota int64
	if err := db.WithContext(ctx).Model(&User{}).Where("id = ?", generation.UserID).
		Select("quota").Scan(&quota).Error; err != nil {
		return err
	}
	return updateUserQuotaCache(generation.UserID, quota)
}

func lockImageGenerationBillingRow(
	tx *gorm.DB, generationID int64,
) (*KKAIImageGeneration, error) {
	var generation KKAIImageGeneration
	if err := lockForUpdate(tx).Where("id = ?", generationID).First(&generation).Error; err != nil {
		return nil, err
	}
	return &generation, nil
}

func reserveImageWallet(tx *gorm.DB, userID int, quota int) error {
	if quota <= 0 {
		return nil
	}
	updated := tx.Model(&User{}).Where("id = ? AND quota >= ?", userID, quota).
		Update("quota", gorm.Expr("quota - ?", quota))
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrImageBillingInsufficientWallet
	}
	return nil
}

func reserveImageSubscription(
	tx *gorm.DB, userID int, quota int,
) (int, int64, int64, error) {
	now := common.GetTimestamp()
	var subscriptions []UserSubscription
	if err := lockForUpdate(tx).
		Where("user_id = ? AND status = ? AND end_time > ?", userID, "active", now).
		Order("end_time asc, id asc").Find(&subscriptions).Error; err != nil {
		return 0, 0, 0, err
	}
	if len(subscriptions) == 0 {
		return 0, 0, 0, ErrImageBillingNoActiveSubscription
	}
	for index := range subscriptions {
		subscription := &subscriptions[index]
		plan, err := getSubscriptionPlanByIdTx(tx, subscription.PlanId)
		if err != nil {
			return 0, 0, 0, err
		}
		if err := maybeResetUserSubscriptionWithPlanTx(tx, subscription, plan, now); err != nil {
			return 0, 0, 0, err
		}
		if subscription.AmountTotal > 0 &&
			subscription.AmountTotal-subscription.AmountUsed < int64(quota) {
			continue
		}
		subscription.AmountUsed += int64(quota)
		if err := tx.Save(subscription).Error; err != nil {
			return 0, 0, 0, err
		}
		return subscription.Id, subscription.AmountTotal, subscription.AmountUsed, nil
	}
	return 0, 0, 0, ErrImageBillingInsufficientSubscription
}

func adjustImageFunding(tx *gorm.DB, generation *KKAIImageGeneration, delta int) error {
	if delta == 0 {
		return nil
	}
	if generation.BillingSource == TaskBillingSourceSubscription {
		var subscription UserSubscription
		if err := lockForUpdate(tx).Where("id = ?", generation.SubscriptionID).
			First(&subscription).Error; err != nil {
			return err
		}
		used := subscription.AmountUsed + int64(delta)
		if used < 0 {
			used = 0
		}
		if subscription.AmountTotal > 0 && used > subscription.AmountTotal {
			return ErrImageBillingInsufficientSubscription
		}
		subscription.AmountUsed = used
		return tx.Save(&subscription).Error
	}
	if generation.BillingSource != TaskBillingSourceWallet {
		return ErrImageBillingStateConflict
	}
	if delta > 0 {
		return reserveImageWallet(tx, generation.UserID, delta)
	}
	return increaseUserQuotaWithDB(tx, generation.UserID, int64(-delta))
}

func persistImageGenerationBillingMutation(
	tx *gorm.DB, generation *KKAIImageGeneration,
) error {
	if tx == nil || generation == nil || generation.ID <= 0 {
		return ErrImageBillingInvalidRequest
	}
	if err := tx.Save(generation).Error; err != nil {
		return err
	}
	payload, err := common.Marshal(ImageBillingCacheReconcilePayload{
		GenerationID: generation.ID,
	})
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	event := KKAIOutboxEvent{
		EventKey: "image-billing-cache:" + strconv.FormatInt(generation.ID, 10) + ":" +
			generation.BillingState + ":" + strconv.Itoa(generation.ReservedQuota),
		Topic:       KKAIOutboxTopicImageBillingCacheReconcile,
		AggregateID: strconv.FormatInt(generation.ID, 10),
		Payload:     string(payload), Status: KKAIOutboxStatusPending,
		AvailableAt: now, CreatedAt: now,
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&event).Error
}

func loadImageSubscriptionSnapshot(
	tx *gorm.DB, mutation *ImageGenerationBillingMutation,
) error {
	if mutation == nil || mutation.Generation == nil ||
		mutation.Generation.BillingSource != TaskBillingSourceSubscription ||
		mutation.Generation.SubscriptionID <= 0 {
		return nil
	}
	var subscription UserSubscription
	if err := tx.Where("id = ?", mutation.Generation.SubscriptionID).
		First(&subscription).Error; err != nil {
		return err
	}
	mutation.SubscriptionAmountTotal = subscription.AmountTotal
	mutation.SubscriptionAmountUsed = subscription.AmountUsed
	return nil
}

func reconcileImageBillingCacheAfterCommit(
	ctx context.Context, db *gorm.DB, generationID int64,
) {
	if err := ReconcileImageGenerationBillingCache(
		context.WithoutCancel(ctx), db, generationID,
	); err != nil {
		common.SysLog(fmt.Sprintf(
			"failed to reconcile image billing cache for generation %d: %s",
			generationID, err.Error(),
		))
	}
}
