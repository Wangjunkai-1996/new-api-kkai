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

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const imageStudioGenerationIDContextKey = "image_studio_generation_id"

type imageGenerationBillingSession struct {
	db           *gorm.DB
	generationID int64
	relayInfo    *relaycommon.RelayInfo
	preConsumed  int
	settled      bool
	refunded     bool
	mu           sync.Mutex
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
	reserve := func(source string) (*model.ImageGenerationBillingMutation, error) {
		return model.ReserveImageGenerationBilling(
			context.WithoutCancel(c.Request.Context()),
			db,
			generationID,
			source,
			preConsumedQuota,
		)
	}
	preference := common.NormalizeBillingPreference(relayInfo.UserSetting.BillingPreference)
	var mutation *model.ImageGenerationBillingMutation
	var err error
	if relayInfo.PriceData.FreeModel {
		mutation, err = reserve(model.TaskBillingSourceWallet)
	} else {
		switch preference {
		case "wallet_only":
			mutation, err = reserve(model.TaskBillingSourceWallet)
		case "subscription_only":
			mutation, err = reserve(model.TaskBillingSourceSubscription)
		case "wallet_first":
			mutation, err = reserve(model.TaskBillingSourceWallet)
			if errors.Is(err, model.ErrImageBillingInsufficientWallet) {
				mutation, err = reserve(model.TaskBillingSourceSubscription)
			}
		default:
			hasSubscription, queryErr := model.HasActiveUserSubscription(relayInfo.UserId)
			if queryErr != nil {
				return types.NewError(
					queryErr, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry(),
				)
			}
			if hasSubscription {
				mutation, err = reserve(model.TaskBillingSourceSubscription)
				if errors.Is(err, model.ErrImageBillingInsufficientSubscription) {
					allowWallet, allowErr := model.UserActiveSubscriptionsAllowWalletOverflow(
						relayInfo.UserId,
					)
					if allowErr != nil {
						return types.NewError(
							allowErr,
							types.ErrorCodeQueryDataError,
							types.ErrOptionWithSkipRetry(),
						)
					}
					if allowWallet {
						mutation, err = reserve(model.TaskBillingSourceWallet)
					}
				}
			} else {
				mutation, err = reserve(model.TaskBillingSourceWallet)
			}
		}
	}
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
	mutation, err := model.SettleImageGenerationBilling(ctx, session.db, session.generationID, actualQuota)
	if err != nil {
		return err
	}
	session.settled = true
	session.preConsumed = actualQuota
	if mutation != nil && mutation.Generation != nil {
		session.relayInfo.FinalPreConsumedQuota = mutation.CurrentQuota
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
	mutation, err := model.RefundImageGenerationBilling(
		ctx, session.db, session.generationID,
	)
	if err != nil {
		common.SysLog("failed to refund durable image generation billing: " + err.Error())
		return
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
