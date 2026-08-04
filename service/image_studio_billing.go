package service

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

var ErrImageStudioQuoteStale = errors.New("image studio quote is stale")

const imageStudioBillingGuardContextKey = "image_studio_billing_guard"

type imageStudioBillingGuard struct {
	maxQuota         int
	finalQuota       int
	billingCompleted bool
	billingPrepared  bool
	quoteExceeded    bool
	settlementErr    error
	relayInfo        *relaycommon.RelayInfo
}

func SetImageStudioBillingGuard(c *gin.Context, maxQuota int) error {
	if c == nil || maxQuota < 0 {
		return ErrImageStudioQuoteStale
	}
	c.Set(imageStudioBillingGuardContextKey, &imageStudioBillingGuard{maxQuota: maxQuota})
	return nil
}

func EnforceImageStudioPreconsumeLimit(c *gin.Context, quota int) error {
	guard, ok := imageStudioBillingGuardFromContext(c)
	if !ok {
		return nil
	}
	if quota < 0 || quota > guard.maxQuota {
		return ErrImageStudioQuoteStale
	}
	return nil
}

func ImageStudioFinalQuota(c *gin.Context) (quota int, settled bool, capped bool) {
	guard, ok := imageStudioBillingGuardFromContext(c)
	if !ok {
		return 0, false, false
	}
	return guard.finalQuota, guard.billingPrepared || guard.billingCompleted, guard.quoteExceeded
}

func PrepareImageStudioBillingCommit(
	c *gin.Context,
	relayInfo *relaycommon.RelayInfo,
	totalTokens int,
	params model.RecordConsumeLogParams,
) error {
	guard, ok := imageStudioBillingGuardFromContext(c)
	if !ok || relayInfo == nil || relayInfo.Billing == nil || totalTokens < 0 || params.Quota < 0 ||
		guard.quoteExceeded || guard.billingPrepared || guard.billingCompleted {
		return ErrImageStudioQuoteStale
	}
	generationID := ImageStudioGenerationID(c)
	if generationID <= 0 {
		return ErrImageStudioQuoteStale
	}
	clientIP := ""
	if userSetting, err := model.GetUserSetting(relayInfo.UserId, false); err == nil && userSetting.RecordIpLog {
		clientIP = c.ClientIP()
	}
	pricingActualCount, err := imagePricingActualCount(relayInfo)
	if err != nil {
		return err
	}
	accounting := model.ImageGenerationAccountingPayload{
		GenerationID: generationID, TargetQuota: params.Quota, CountStatistics: totalTokens > 0,
		Username: c.GetString("username"), UpstreamRequestID: c.GetString(common.UpstreamRequestIdKey),
		ClientIP: clientIP, NodeName: common.NodeName, LogParams: params,
		PricingSnapshot:    cloneImagePricingSnapshot(relayInfo.ImagePricingSnapshot),
		PricingActualCount: pricingActualCount,
	}
	prepareContext, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 10*time.Second)
	defer cancel()
	if err := model.PrepareImageGenerationAccounting(prepareContext, model.DB, generationID, accounting); err != nil {
		return err
	}
	guard.relayInfo = relayInfo
	guard.billingPrepared = true
	return nil
}

func imagePricingActualCount(relayInfo *relaycommon.RelayInfo) (int, error) {
	if relayInfo == nil || relayInfo.ImagePricingSnapshot == nil {
		return 0, nil
	}
	count := relayInfo.ImagePricingSnapshot.RequestedCount
	if actual, ok := relayInfo.PriceData.OtherRatios()["n"]; ok {
		if actual < 1 || actual > dto.MaxImageN || actual != math.Trunc(actual) {
			return 0, ErrImageStudioQuoteStale
		}
		count = int(actual)
	}
	return count, nil
}

func CommitImageStudioBilling(c *gin.Context) error {
	guard, ok := imageStudioBillingGuardFromContext(c)
	if !ok || !guard.billingPrepared || guard.relayInfo == nil {
		return ErrImageStudioQuoteStale
	}
	if guard.billingCompleted {
		return guard.settlementErr
	}
	settlementErr := SettleBilling(c, guard.relayInfo, guard.finalQuota)
	completeImageStudioBilling(c, settlementErr)
	if settlementErr != nil {
		return settlementErr
	}
	return nil
}

func applyImageStudioFinalQuota(c *gin.Context, quota int) (int, bool) {
	guard, ok := imageStudioBillingGuardFromContext(c)
	if !ok {
		return quota, false
	}
	guard.finalQuota = quota
	if quota > guard.maxQuota {
		guard.finalQuota = 0
		guard.quoteExceeded = true
	}
	return guard.finalQuota, guard.quoteExceeded
}

func completeImageStudioBilling(c *gin.Context, settlementErr error) bool {
	guard, ok := imageStudioBillingGuardFromContext(c)
	if !ok {
		return false
	}
	guard.settlementErr = settlementErr
	guard.billingCompleted = settlementErr == nil
	return true
}

func ApplyImageStudioMaximumPreconsume(
	c *gin.Context,
	relayInfo *relaycommon.RelayInfo,
	priceData *hosttypes.PriceData,
	promptTokens int,
	meta *types.TokenCountMeta,
) error {
	if c == nil || !common.GetContextKeyBool(c, constant.ContextKeyIsImageStudio) {
		return nil
	}
	if relayInfo == nil || priceData == nil || meta == nil || promptTokens < 0 || meta.MaxTokens < 0 {
		return ErrImageStudioQuoteStale
	}
	// Arbitrary tiered expressions cannot be proven monotonic over every image,
	// cache, audio, request, and time variable. First phase therefore fails
	// closed instead of spending provider cost against an unsafe quote.
	if relayInfo.TieredBillingSnapshot != nil {
		return ErrImageModelBillingUnsupported
	}
	// Fixed-price quotes already represent the complete request price. Only
	// ratio billing needs its token estimate expanded to maximum completion.
	if priceData.UsePrice || priceData.FreeModel {
		relayInfo.PriceData = *priceData
		return nil
	}
	inputTokens := promptTokens
	if inputTokens < common.PreConsumedQuota {
		inputTokens = common.PreConsumedQuota
	}
	weightedTokens := decimal.NewFromInt(int64(inputTokens)).Add(
		decimal.NewFromInt(int64(meta.MaxTokens)).Mul(
			decimal.NewFromFloat(priceData.CompletionRatio),
		),
	)
	quotaDecimal := weightedTokens.
		Mul(decimal.NewFromFloat(priceData.ModelRatio)).
		Mul(decimal.NewFromFloat(priceData.GroupRatioInfo.GroupRatio))
	quotaDecimal = priceData.ApplyOtherRatiosToDecimal(quotaDecimal)
	quota, clamp := common.QuotaFromDecimalChecked(quotaDecimal)
	if clamp != nil {
		return clamp
	}
	priceData.QuotaToPreConsume = quota
	relayInfo.PriceData = *priceData
	return nil
}

func imageStudioBillingGuardFromContext(c *gin.Context) (*imageStudioBillingGuard, bool) {
	if c == nil {
		return nil, false
	}
	value, exists := c.Get(imageStudioBillingGuardContextKey)
	if !exists {
		return nil, false
	}
	guard, ok := value.(*imageStudioBillingGuard)
	return guard, ok && guard != nil
}
