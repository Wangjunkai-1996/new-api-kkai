package service

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

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
	accounting       *model.ImageGenerationAccountingPayload
	accountingStored bool
	requestedOutputs int
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
	request, ok := relayInfo.Request.(*dto.ImageRequest)
	if !ok {
		return ErrImageStudioQuoteStale
	}
	requestedOutputs := 1
	if request.N != nil {
		if *request.N < 1 || *request.N > uint(dto.MaxImageN) {
			return ErrImageStudioQuoteStale
		}
		requestedOutputs = int(*request.N)
	}
	outputCount := relayInfo.ImageOutputCount
	if outputCount == 0 {
		outputCount = requestedOutputs
	}
	if outputCount < 1 || outputCount > requestedOutputs {
		return ErrImageStudioQuoteStale
	}
	pricingActualCount, err := imagePricingActualCount(relayInfo)
	if err != nil {
		return err
	}
	if pricingActualCount > 0 && pricingActualCount != outputCount {
		return ErrImageStudioQuoteStale
	}
	accounting := model.ImageGenerationAccountingPayload{
		GenerationID: generationID, TargetQuota: params.Quota, CountStatistics: totalTokens > 0,
		Username: c.GetString("username"), UpstreamRequestID: c.GetString(common.UpstreamRequestIdKey),
		ClientIP: clientIP, NodeName: common.NodeName, LogParams: params,
		OutputCount:        outputCount,
		PricingSnapshot:    cloneImagePricingSnapshot(relayInfo.ImagePricingSnapshot),
		PricingActualCount: pricingActualCount,
	}
	guard.relayInfo = relayInfo
	guard.accounting = &accounting
	guard.requestedOutputs = requestedOutputs
	guard.billingPrepared = true
	return nil
}

func imagePricingActualCount(relayInfo *relaycommon.RelayInfo) (int, error) {
	if relayInfo == nil || relayInfo.ImagePricingSnapshot == nil {
		return 0, nil
	}
	count := relayInfo.ImagePricingSnapshot.RequestedCount
	actual, hasActual := relayInfo.PriceData.OtherRatios()["n"]
	if hasActual {
		if actual < 1 || actual > dto.MaxImageN || actual != math.Trunc(actual) {
			return 0, ErrImageStudioQuoteStale
		}
		count = int(actual)
	}
	if relayInfo.ImageOutputCount > 0 {
		if relayInfo.ImageOutputCount > dto.MaxImageN {
			return 0, ErrImageStudioQuoteStale
		}
		count = relayInfo.ImageOutputCount
	}
	return count, nil
}

func PrepareImageStudioDeliveryBilling(c *gin.Context, providerOutputCount int, deliveredOutputCount int) error {
	guard, ok := imageStudioBillingGuardFromContext(c)
	if !ok || !guard.billingPrepared || guard.billingCompleted || guard.accounting == nil ||
		providerOutputCount < 1 || providerOutputCount > guard.requestedOutputs ||
		deliveredOutputCount < 1 || deliveredOutputCount > providerOutputCount {
		return ErrImageStudioQuoteStale
	}
	if guard.relayInfo.ImageOutputCount > 0 && guard.relayInfo.ImageOutputCount != providerOutputCount {
		return ErrImageStudioQuoteStale
	}
	if guard.accountingStored {
		if guard.accounting.OutputCount != deliveredOutputCount {
			return ErrImageStudioQuoteStale
		}
		return nil
	}

	accounting := *guard.accounting
	quota := accounting.TargetQuota
	if accounting.PricingSnapshot != nil {
		var err error
		quota, err = imagepricing.CalculateQuotaStrict(accounting.PricingSnapshot, deliveredOutputCount)
		if err != nil {
			return ErrImageStudioQuoteStale
		}
		accounting.PricingActualCount = deliveredOutputCount
	} else if deliveredOutputCount < accounting.OutputCount {
		prorated := decimal.NewFromInt(int64(quota)).
			Mul(decimal.NewFromInt(int64(deliveredOutputCount))).
			Div(decimal.NewFromInt(int64(accounting.OutputCount)))
		var clamp *common.QuotaClamp
		quota, clamp = common.QuotaFromDecimalChecked(prorated)
		if clamp != nil {
			return clamp
		}
	}
	if quota < 0 || quota > guard.maxQuota {
		return ErrImageStudioQuoteStale
	}
	accounting.OutputCount = deliveredOutputCount
	accounting.TargetQuota = quota
	accounting.LogParams.Quota = quota
	if accounting.LogParams.Other == nil {
		accounting.LogParams.Other = make(map[string]interface{})
	}
	adminInfo, _ := accounting.LogParams.Other["admin_info"].(map[string]interface{})
	if adminInfo == nil {
		adminInfo = make(map[string]interface{})
	}
	adminInfo["image_outputs"] = map[string]int{
		"provider":  providerOutputCount,
		"delivered": deliveredOutputCount,
	}
	accounting.LogParams.Other["admin_info"] = adminInfo

	prepareContext, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 10*time.Second)
	defer cancel()
	if err := model.PrepareImageGenerationAccounting(
		prepareContext, model.DB, accounting.GenerationID, accounting,
	); err != nil {
		return err
	}
	guard.accounting = &accounting
	guard.accountingStored = true
	guard.finalQuota = quota
	return nil
}

func CommitImageStudioBilling(c *gin.Context) error {
	guard, ok := imageStudioBillingGuardFromContext(c)
	if !ok || !guard.billingPrepared || !guard.accountingStored || guard.relayInfo == nil {
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
	priceData *types.PriceData,
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
	maximumRatios := types.PriceData{}
	maximumRatios.ReplaceOtherRatios(priceData.OtherRatios())
	for name, ratio := range meta.BillingRatios {
		maximumRatios.AddOtherRatio(name, ratio)
	}
	quotaDecimal = maximumRatios.ApplyOtherRatiosToDecimal(quotaDecimal)
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
