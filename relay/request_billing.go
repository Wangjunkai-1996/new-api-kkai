package relay

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

// PrepareRequestBilling estimates and reserves one request's charge. Transports
// provide the current request body through BodyStorage or BillingRequestInput;
// channel retries retain the resulting billing session and pricing snapshot.
func PrepareRequestBilling(c *gin.Context, info *relaycommon.RelayInfo, beforePreConsume func() error) *types.NewAPIError {
	if err := service.PrepareImagePricing(c, info); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	meta := &types.TokenCountMeta{TokenType: types.TokenTypeTokenizer}
	if info.Request != nil && (needSensitiveCheck || constant.CountToken || model.MembershipEnabled()) {
		meta = info.Request.GetTokenCountMeta()
	} else {
		// Avoid building CombineText when only the pricing quantities are needed.
		switch request := info.Request.(type) {
		case *dto.GeneralOpenAIRequest:
			meta.MaxTokens = int(max(lo.FromPtr(request.MaxTokens), lo.FromPtr(request.MaxCompletionTokens)))
		case *dto.OpenAIResponsesRequest:
			meta.MaxTokens = int(lo.FromPtr(request.MaxOutputTokens))
		case *dto.ClaudeRequest:
			meta.MaxTokens = int(lo.FromPtr(request.MaxTokens))
		case *dto.ImageRequest:
			meta = request.GetTokenCountMeta()
		}
	}

	if needSensitiveCheck && meta != nil {
		if contains, words := service.CheckSensitiveText(meta.CombineText); contains {
			service.RequestPolicy(c).AddEvent(service.PolicyEvent{ErrorCode: string(types.ErrorCodeSensitiveWordsDetected), ErrorSource: "local", Decision: service.PolicyDecision{Action: "stop", Reason: "local_rejection", Source: "global"}, Health: "unchanged"})
			message := fmt.Sprintf("user sensitive words detected: %d match(es)", len(words))
			logger.LogWarn(c, message)
			return types.NewErrorWithStatusCode(errors.New("prompt blocked"), types.ErrorCodeSensitiveWordsDetected, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, info)
	if model.MembershipEnabled() {
		tokens, err = service.CountRequestToken(c, meta, info)
	}
	if err != nil {
		return types.NewError(err, types.ErrorCodeCountTokenFailed)
	}
	info.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, info, tokens, meta)
	if err != nil {
		return types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
	}
	if err := service.ApplyImageStudioMaximumPreconsume(c, info, &priceData, tokens, meta); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeModelPriceError, http.StatusBadRequest)
	}
	if err := service.PrepareMembershipBudget(c, info, &priceData, tokens, meta); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if err := service.EnforceImageStudioPreconsumeLimit(c, priceData.QuotaToPreConsume); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeQuoteStale, http.StatusConflict, types.ErrOptionWithSkipRetry())
	}
	if beforePreConsume != nil {
		if err := beforePreConsume(); err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
		}
	}
	if info.MembershipRealtime {
		return nil // Each response.create obtains its own complete authorization.
	}
	if priceData.FreeModel && info.MembershipPricing == nil {
		logger.LogInfo(c, fmt.Sprintf("模型 %s 免费，跳过预扣费", info.OriginModelName))
		if service.ImageStudioGenerationID(c) > 0 {
			return service.PreConsumeBilling(c, 0, info)
		}
		return nil
	}
	return service.PreConsumeBilling(c, priceData.QuotaToPreConsume, info)
}

// RefundFailedRequestBilling applies the common final-failure policy after all
// eligible attempts have ended. A settled BillingSession never refunds again.
func RefundFailedRequestBilling(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) *types.NewAPIError {
	if apiErr == nil {
		return nil
	}
	if info.MembershipRealtime {
		return apiErr // Each realtime response owns its own authorization.
	}
	apiErr = service.NormalizeViolationFeeError(apiErr)
	if info.MembershipBillingRequestID != "" && service.FinalizeMembershipPartialUsage(c, info, apiErr) {
		return apiErr
	}
	if info.MembershipBillingRequestID != "" && info.MembershipOutcomeUnknown {
		types.ErrOptionWithSkipRetry()(apiErr)
		if err := model.MarkMembershipBillingUncertain(info.MembershipBillingRequestID, info.ChannelId, "upstream_outcome_unknown"); err != nil {
			logger.LogError(c, "failed to flag membership billing for review: "+err.Error())
		}
		logger.LogWarn(c, "membership billing outcome unknown; reservation retained for review: "+info.MembershipBillingRequestID)
		return apiErr
	}
	if info.MembershipBillingRequestID != "" && service.ChargeViolationFeeIfNeeded(c, info, apiErr) {
		return apiErr
	}
	if info.Billing != nil {
		info.Billing.Refund(c)
	}
	service.ChargeViolationFeeIfNeeded(c, info, apiErr)
	return apiErr
}
