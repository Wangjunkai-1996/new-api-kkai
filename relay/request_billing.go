package relay

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
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
	"github.com/tidwall/gjson"
)

// PrepareRequestBilling estimates and reserves one request's charge. Transports
// provide the current request body through BodyStorage or BillingRequestInput;
// channel retries retain the resulting billing session and pricing snapshot.
func PrepareRequestBilling(c *gin.Context, info *relaycommon.RelayInfo, beforePreConsume func() error) *types.NewAPIError {
	if err := service.PrepareTenantPricing(c, info); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeModelPriceError, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	if err := service.PrepareImagePricing(c, info); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	membershipBilling := false
	if info.TenantID == 0 && model.MembershipEnabled() && !info.IsChannelTest {
		var err error
		membershipBilling, err = model.ShouldUseMembershipBilling(info.UserId)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		info.MembershipBillingEnabled = &membershipBilling
	}
	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	meta := &types.TokenCountMeta{TokenType: types.TokenTypeTokenizer}
	if info.Request != nil && (info.TenantID > 0 || needSensitiveCheck || constant.CountToken || membershipBilling) {
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
	if membershipBilling && (model.MembershipPilotHoldEnabled(info.UserId) || model.MembershipPilotEarlyReleaseEnabled(info.UserId)) {
		info.MembershipPilotTextHTTP = membershipTextHTTPRequest(c, info, meta)
	}
	if needSensitiveCheck && meta != nil {
		if contains, words := service.CheckSensitiveText(meta.CombineText); contains {
			service.RequestPolicy(c).AddEvent(service.PolicyEvent{ErrorCode: string(types.ErrorCodeSensitiveWordsDetected), ErrorSource: "local", Decision: service.PolicyDecision{Action: "stop", Reason: "local_rejection", Source: "global"}, Health: "unchanged"})
			message := fmt.Sprintf("user sensitive words detected: %d match(es)", len(words))
			logger.LogWarn(c, message)
			return types.NewErrorWithStatusCode(errors.New("prompt blocked"), types.ErrorCodeSensitiveWordsDetected, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
	}

	var tokens int
	var err error
	if membershipBilling {
		tokens, err = service.CountRequestToken(c, meta, info)
	} else {
		tokens, err = service.EstimateRequestToken(c, meta, info)
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
	if membershipBilling {
		if err := service.PrepareMembershipBudget(c, info, &priceData, tokens, meta); err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
	} else if info.TenantID > 0 {
		if err := service.PrepareTenantBudget(info, &priceData, tokens, meta); err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
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
	if priceData.FreeModel && info.MembershipPricing == nil && info.TenantID == 0 {
		logger.LogInfo(c, common.LogText("model %s is free, skipping pre-consume", info.OriginModelName))
		if service.ImageStudioGenerationID(c) > 0 {
			return service.PreConsumeBilling(c, 0, info)
		}
		return nil
	}
	return service.PreConsumeBilling(c, priceData.QuotaToPreConsume, info)
}

// Use the original JSON because passthrough channels can forward fields the
// DTO omits, including background Responses and provider-specific tools.
func membershipTextHTTPRequest(c *gin.Context, info *relaycommon.RelayInfo, meta *types.TokenCountMeta) bool {
	if !info.MembershipHTTPRelay || info.IsChannelTest || info.IsPlayground || info.TenantID != 0 ||
		info.DurableImageExecution || info.MembershipRealtime || c.Request.Method != http.MethodPost ||
		meta == nil || len(meta.Files) != 0 || meta.ToolsCount != 0 || strings.HasSuffix(info.GetBillingModelName(), "search-preview") ||
		len(common.GetContextKeyStringMap(c, constant.ContextKeyChannelParamOverride)) != 0 {
		return false
	}
	path := c.Request.URL.Path
	switch path {
	case "/v1/chat/completions":
		if info.RelayFormat != types.RelayFormatOpenAI {
			return false
		}
	case "/v1/messages":
		if info.RelayFormat != types.RelayFormatClaude {
			return false
		}
	case "/v1/responses":
		if info.RelayFormat != types.RelayFormatOpenAIResponses {
			return false
		}
	default:
		return false
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil || storage.IsDisk() || storage.Size() > 1<<20 {
		return false // Keep the pilot away from large or disk-backed payloads.
	}
	data, err := storage.Bytes()
	if err != nil {
		return false
	}
	fields := membershipObjectFields(gjson.ParseBytes(data))
	inputField := "messages"
	if path == "/v1/responses" {
		inputField = "input"
	}
	for field, value := range fields {
		switch field {
		case "model", "stream", "temperature", "top_p", "top_k", "max_tokens", "max_completion_tokens", "max_output_tokens", "reasoning_effort":
			if value.IsObject() || value.IsArray() {
				return false
			}
		case "stream_options":
			options := membershipObjectFields(value)
			if len(options) != 1 || (options["include_usage"].Type != gjson.True && options["include_usage"].Type != gjson.False) {
				return false
			}
		case "n":
			if value.Type != gjson.Number || value.Float() != 1 {
				return false
			}
		case "tools":
			if !value.IsArray() || len(value.Array()) != 0 {
				return false
			}
		case "store":
			if value.Type != gjson.False {
				return false
			}
		case "system", "instructions":
			if !membershipTextContent(value) {
				return false
			}
		case inputField:
		default:
			return false
		}
	}
	input := fields[inputField]
	if input.Type == gjson.String {
		return inputField == "input"
	}
	if !input.IsArray() {
		return false
	}
	messages := input.Array()
	for _, message := range messages {
		fields := membershipObjectFields(message)
		if inputField == "input" && fields["type"].String() == "message" {
			delete(fields, "type")
		}
		switch fields["role"].String() {
		case "system", "developer", "user", "assistant":
		default:
			return false
		}
		if len(fields) != 2 || !membershipTextContent(fields["content"]) {
			return false
		}
	}
	return len(messages) > 0
}

// The pilot must classify the same fields as every upstream parser, even on
// passthrough channels. An ambiguous object keeps the normal full reservation.
func membershipObjectFields(value gjson.Result) map[string]gjson.Result {
	if !value.IsObject() {
		return nil
	}
	fields := make(map[string]gjson.Result)
	value.ForEach(func(key, field gjson.Result) bool {
		name := key.String()
		if _, exists := fields[name]; exists {
			fields = nil
			return false
		}
		fields[name] = field
		return true
	})
	return fields
}

func membershipTextContent(content gjson.Result) bool {
	if content.Type == gjson.String {
		return true
	}
	if !content.IsArray() {
		return false
	}
	parts := content.Array()
	for _, part := range parts {
		fields := membershipObjectFields(part)
		if len(fields) != 2 || fields["text"].Type != gjson.String {
			return false
		}
		switch fields["type"].String() {
		case "text", "input_text", "output_text":
		default:
			return false
		}
	}
	return len(parts) > 0
}

func membershipFinalReviewReason(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) string {
	const ordinary = "upstream_outcome_unknown"
	if apiErr == nil || !info.MembershipPilotTextHTTP || info.MembershipPilotStoppedError != apiErr ||
		info.BillingSource != model.MembershipSource || !model.MembershipPilotEarlyReleaseEnabled(info.UserId) ||
		(info.ChannelMeta != nil && len(info.ParamOverride) != 0) ||
		c.Writer.Written() || info.ReceivedResponseCount != 0 || info.SendResponseCount != 0 ||
		info.BillingHasReportedUsage || info.BillingFailureUsage != nil || info.MembershipHasBillableUsage ||
		info.MembershipChargedQuota != 0 || info.MembershipAssessedQuota != 0 || info.MembershipViolationFee != 0 ||
		info.ImageOutputCount != 0 || apiErr.GetPolicyEvidence() != "" ||
		service.IsViolationFeeCode(apiErr.GetErrorCode()) || service.HasCSAMViolationMarker(apiErr) {
		return ordinary
	}
	if info.ResponsesUsageInfo != nil {
		if len(info.ResponsesUsageInfo.ImageGenerationCalls) > 0 {
			return ordinary
		}
		for _, tool := range info.ResponsesUsageInfo.BuiltInTools {
			if tool != nil && tool.CallCount > 0 {
				return ordinary
			}
		}
	}
	return model.MembershipPilotEarlyReviewReason
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
	if info.HasDurableBilling() && service.FinalizeFailedRequestUsage(c, info, apiErr) {
		return apiErr
	}
	if info.TenantID > 0 && info.HasDurableBilling() && info.BillingOutcomeUnknown {
		types.ErrOptionWithSkipRetry()(apiErr)
		if err := model.MarkTenantConsumptionUncertain(model.DB, info.RequestId, info.ChannelId); err != nil {
			logger.LogError(c, "failed to flag tenant billing for review: "+err.Error())
		}
		return apiErr
	}
	if info.MembershipBillingRequestID != "" && info.BillingOutcomeUnknown {
		types.ErrOptionWithSkipRetry()(apiErr)
		reason := membershipFinalReviewReason(c, info, apiErr)
		if err := service.MarkMembershipBillingUncertain(info.MembershipBillingRequestID, info.ChannelId, reason); err != nil {
			logger.LogError(c, "failed to flag membership billing for review: "+err.Error())
		}
		logger.LogWarn(c, fmt.Sprintf("membership billing outcome unknown: request=%s reason=%s", info.MembershipBillingRequestID, reason))
		return apiErr
	}
	if info.MembershipBillingRequestID != "" && service.ChargeViolationFeeIfNeeded(c, info, apiErr) {
		return apiErr
	}
	if info.Billing != nil {
		info.Billing.Refund(c)
	}
	if info.TenantID == 0 {
		service.ChargeViolationFeeIfNeeded(c, info, apiErr)
	}
	return apiErr
}
