package service

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const imageStudioGenerationIDContextKey = "image_studio_generation_id"

type imageGenerationBillingSession struct {
	db             *gorm.DB
	generationID   int64
	relayInfo      *relaycommon.RelayInfo
	preConsumed    int
	settled        bool
	refunded       bool
	mu             sync.Mutex
	fence          *model.ImageExecutionFence
	expectedStatus string
}

func SetImageStudioGenerationID(c *gin.Context, generationID int64) error {
	if c == nil || generationID <= 0 {
		return ErrInvalidImageStudioSubmission
	}
	c.Set(imageStudioGenerationIDContextKey, generationID)
	return nil
}

func ImageStudioGenerationID(c *gin.Context) int64 {
	if c == nil {
		return 0
	}
	return c.GetInt64(imageStudioGenerationIDContextKey)
}

func MarkImageStudioGenerationDispatching(c *gin.Context) error {
	generationID := ImageStudioGenerationID(c)
	if generationID <= 0 {
		return nil
	}
	base := context.Background()
	if c != nil && c.Request != nil {
		base = context.WithoutCancel(c.Request.Context())
	}
	ctx, cancel := context.WithTimeout(base, 10*time.Second)
	defer cancel()
	if boundID, fence, ok := ImageExecutionFenceFromContext(base); ok {
		if boundID != generationID {
			return model.ErrImageExecutionFenceLost
		}
		return model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			execution, err := model.RequireImageExecutionFenceTx(tx, generationID, fence)
			if err != nil {
				return err
			}
			if execution.State != model.ImageGenerationExecutionQueued && execution.State != model.ImageGenerationExecutionPreparing {
				return model.ErrImageExecutionFenceLost
			}
			now := time.Now().Unix()
			if execution.ExecutionDeadlineAt == 0 {
				execution.ExecutionDeadlineAt = now + int64(image_studio_setting.Get().SubmissionTimeoutSecs)
			}
			if execution.ExecutionDeadlineAt <= now || c.GetInt("channel_id") <= 0 {
				return model.ErrImageBillingStateConflict
			}
			execution.State = model.ImageGenerationExecutionDispatching
			execution.DispatchChannelID = c.GetInt("channel_id")
			execution.DispatchStartedAt = now
			execution.UpdatedAt = now
			if err := tx.Save(execution).Error; err != nil {
				return err
			}
			return model.MarkImageGenerationDispatching(ctx, tx, generationID)
		})
	}
	return model.MarkImageGenerationDispatching(ctx, model.DB, generationID)
}

func PreConsumeImageGenerationBilling(
	c *gin.Context,
	db *gorm.DB,
	generationID int64,
	preConsumedQuota int,
	relayInfo *relaycommon.RelayInfo,
) *types.NewAPIError {
	if c == nil || db == nil || generationID <= 0 || preConsumedQuota < 0 || relayInfo == nil {
		return types.NewError(
			model.ErrImageBillingInvalidRequest,
			types.ErrorCodeInvalidRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if relayInfo.MembershipPricing != nil {
		input, err := PrepareMembershipReservation(relayInfo, relayInfo.MembershipPricing.MaximumBeforeGroup)
		if err != nil {
			return membershipBillingError(err)
		}
		if guard, ok := imageStudioBillingGuardFromContext(c); ok && input.WalletQuota > int64(guard.maxQuota) {
			return membershipBillingError(ErrImageStudioQuoteStale)
		}
		record, err := model.ReserveMembershipImageGenerationBilling(context.WithoutCancel(c.Request.Context()), db, generationID, input)
		if err != nil {
			return membershipBillingError(err)
		}
		if err := ApplyMembershipReservation(relayInfo, record); err != nil {
			return membershipBillingError(err)
		}
		if guard, ok := imageStudioBillingGuardFromContext(c); ok {
			guard.maxQuota = int(record.ReservedQuota)
		}
		relayInfo.Billing = &imageGenerationBillingSession{db: db, generationID: generationID, relayInfo: relayInfo, preConsumed: int(record.ReservedQuota)}
		return nil
	}
	var mutation *model.ImageGenerationBillingMutation
	err := db.WithContext(context.WithoutCancel(c.Request.Context())).Transaction(func(tx *gorm.DB) error {
		var err error
		mutation, err = ReserveImageGenerationBillingForUserTx(
			tx.Statement.Context, tx, generationID, preConsumedQuota,
			relayInfo.UserSetting.BillingPreference, relayInfo.PriceData.FreeModel,
		)
		return err
	})
	if err != nil {
		return imageGenerationBillingError(err)
	}
	if mutation == nil || mutation.Generation == nil {
		return types.NewError(
			model.ErrImageBillingStateConflict,
			types.ErrorCodeUpdateDataError,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if err := model.ReconcileImageGenerationBillingCache(context.WithoutCancel(c.Request.Context()), db, generationID); err != nil {
		common.SysLog("failed to reconcile image billing cache: " + err.Error())
	}
	relayInfo.BillingSource = mutation.Generation.BillingSource
	relayInfo.FinalPreConsumedQuota = mutation.CurrentQuota
	relayInfo.SubscriptionId = mutation.Generation.SubscriptionID
	relayInfo.SubscriptionPreConsumed = int64(mutation.CurrentQuota)
	relayInfo.SubscriptionAmountTotal = mutation.SubscriptionAmountTotal
	relayInfo.SubscriptionAmountUsedAfterPreConsume = mutation.SubscriptionAmountUsed
	relayInfo.Billing = &imageGenerationBillingSession{
		db: db, generationID: generationID, relayInfo: relayInfo,
		preConsumed: mutation.CurrentQuota,
	}
	return nil
}

func (session *imageGenerationBillingSession) Settle(actualQuota int) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.settled {
		return nil
	}
	if session.refunded || actualQuota < 0 {
		return model.ErrImageBillingStateConflict
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	charge := actualQuota
	if session.relayInfo.MembershipBillingRequestID != "" {
		charge = min(actualQuota, session.preConsumed)
	}
	var mutation *model.ImageGenerationBillingMutation
	var err error
	if session.fence == nil {
		mutation, err = model.SettleImageGenerationBilling(ctx, session.db, session.generationID, charge)
	} else {
		err = model.MembershipTransactionWithDB(ctx, session.db, func(tx *gorm.DB) error {
			if _, err := model.RequireImageExecutionFenceTx(tx, session.generationID, *session.fence); err != nil {
				return err
			}
			var err error
			mutation, err = model.SettleImageGenerationBillingTx(tx, session.generationID, charge, session.expectedStatus)
			return err
		})
	}
	if err != nil {
		return err
	}
	if session.fence != nil {
		model.MembershipBillingAfterCommit(mutation.MembershipRecord)
	}
	session.settled = true
	session.preConsumed = mutation.CurrentQuota
	if mutation != nil && mutation.Generation != nil {
		if session.relayInfo.MembershipBillingRequestID == "" {
			session.relayInfo.FinalPreConsumedQuota = mutation.CurrentQuota
		}
		session.relayInfo.SubscriptionAmountTotal = mutation.SubscriptionAmountTotal
		session.relayInfo.SubscriptionAmountUsedAfterPreConsume = mutation.SubscriptionAmountUsed
	}
	return nil
}

func (session *imageGenerationBillingSession) Refund(c *gin.Context) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.settled || session.refunded {
		return
	}
	base := context.Background()
	if c != nil && c.Request != nil {
		base = context.WithoutCancel(c.Request.Context())
	}
	ctx, cancel := context.WithTimeout(base, 10*time.Second)
	defer cancel()
	var mutation *model.ImageGenerationBillingMutation
	var err error
	if session.fence == nil {
		mutation, err = model.RefundImageGenerationBilling(ctx, session.db, session.generationID)
	} else {
		err = model.MembershipTransactionWithDB(ctx, session.db, func(tx *gorm.DB) error {
			if _, err := model.RequireImageExecutionFenceTx(tx, session.generationID, *session.fence); err != nil {
				return err
			}
			var err error
			mutation, err = model.RefundImageGenerationBillingTx(tx, session.generationID, session.expectedStatus)
			return err
		})
	}
	if err != nil {
		common.SysLog("failed to refund durable image generation billing: " + err.Error())
		return
	}
	if session.fence != nil {
		model.MembershipBillingAfterCommit(mutation.MembershipRecord)
	}
	session.refunded = true
	session.preConsumed = 0
	if mutation != nil {
		session.relayInfo.FinalPreConsumedQuota = mutation.CurrentQuota
	}
}

func (session *imageGenerationBillingSession) NeedsRefund() bool {
	session.mu.Lock()
	defer session.mu.Unlock()
	return !session.settled && !session.refunded && session.preConsumed > 0
}

func (session *imageGenerationBillingSession) GetPreConsumedQuota() int {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.preConsumed
}

func (session *imageGenerationBillingSession) Reserve(targetQuota int) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if targetQuota <= session.preConsumed {
		return nil
	}
	return ErrImageStudioQuoteStale
}

func imageGenerationBillingError(err error) *types.NewAPIError {
	switch {
	case errors.Is(err, model.ErrImageBillingInsufficientWallet),
		errors.Is(err, model.ErrImageBillingNoActiveSubscription),
		errors.Is(err, model.ErrImageBillingInsufficientSubscription):
		return types.NewErrorWithStatusCode(
			err,
			types.ErrorCodeInsufficientUserQuota,
			http.StatusForbidden,
			types.ErrOptionWithSkipRetry(),
			types.ErrOptionWithNoRecordErrorLog(),
		)
	case errors.Is(err, model.ErrImageBillingInvalidRequest),
		errors.Is(err, model.ErrImageBillingStateConflict):
		return types.NewError(
			err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry(),
		)
	default:
		return types.NewError(
			err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry(),
		)
	}
}
