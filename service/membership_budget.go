package service

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// PrepareMembershipBudget reserves an upper bound before contacting upstream.
// An omitted output/tool bound is not permission to subsidize an unbounded call.
func PrepareMembershipBudget(c *gin.Context, info *relaycommon.RelayInfo, price *types.PriceData, promptTokens int, meta *relaytypes.TokenCountMeta) error {
	if !model.MembershipEnabled() || info.IsChannelTest {
		return nil
	}
	enabled, err := model.ShouldUseMembershipBilling(info.UserId)
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	if info.RelayMode == relayconstant.RelayModeRealtime {
		info.MembershipRealtime = true
		return nil
	}

	if info.MembershipBillingRequestID != "" {
		return nil
	}
	snapshot, err := prepareRequestBudget(info, price, promptTokens, meta)
	if err != nil {
		return err
	}
	violationFee := 0.0
	if settings := model_setting.GetGrokSettings(); settings != nil && settings.ViolationDeductionEnabled {
		violationFee = settings.ViolationDeductionAmount * common.QuotaPerUnit
		if violationFee < 0 || math.IsNaN(violationFee) || math.IsInf(violationFee, 0) {
			return model.ErrMembershipInvalidInput
		}
	}
	snapshot.MaximumBeforeGroup += violationFee
	if math.IsInf(snapshot.MaximumBeforeGroup, 0) {
		return model.ErrMembershipInvalidInput
	}
	snapshot.ViolationFeeBeforeGroup = violationFee
	info.MembershipPricing = snapshot
	return nil
}

// prepareRequestBudget uses verified aggregate provider bounds for every billed
// usage category. Wallet and membership authorizations share this calculation.
func prepareRequestBudget(info *relaycommon.RelayInfo, price *types.PriceData, promptTokens int, meta *relaytypes.TokenCountMeta) (*types.MembershipPricing, error) {
	if promptTokens < 0 || meta == nil || meta.MaxTokens < 0 {
		return nil, model.ErrMembershipInvalidInput
	}
	tools := operation_setting.SnapshotToolPrices(info.GetBillingModelName())
	audioPrice := operation_setting.GetGeminiInputAudioPricePerMillionTokens(info.GetBillingModelName())
	maximum := 0.0
	input := float64(promptTokens)
	output := float64(meta.MaxTokens)
	copies := 1
	needsOutput := false
	providerOutputBound := false
	toolCost := 0.0
	toolLimits := make(map[string]int)
	catalog, err := model.GetMembershipCatalog()
	if err != nil {
		return nil, err
	}
	limits := catalog.ModelLimits[info.GetBillingModelName()]
	switch request := info.Request.(type) {
	case *dto.GeneralOpenAIRequest:
		switch info.RelayMode {
		case relayconstant.RelayModeEmbeddings, relayconstant.RelayModeModerations, relayconstant.RelayModeRerank:
		default:
			needsOutput = true
		}
		if request.N != nil {
			copies = *request.N
		}
		if strings.HasSuffix(info.GetBillingModelName(), "search-preview") {
			if limits.MaxToolCalls <= 0 {
				return nil, fmt.Errorf("configure the provider's verified maximum search calls for this model")
			}
			toolCost += tools[dto.BuildInToolWebSearchPreview] * float64(limits.MaxToolCalls) / 1000
		}
	case *dto.OpenAIResponsesRequest:
		needsOutput = true
		maximumToolPrice := 0.0
		for _, tool := range request.GetToolsMap() {
			name, _ := tool["type"].(string)
			price := tools[name] / 1000
			if name == dto.BuildInToolImageGeneration {
				price = operation_setting.GPTImage1High1024x1536
			}
			maximumToolPrice = max(maximumToolPrice, price)
		}
		if maximumToolPrice > 0 {
			if request.MaxToolCalls == nil || *request.MaxToolCalls == 0 || *request.MaxToolCalls > math.MaxInt32/2 {
				return nil, fmt.Errorf("max_tool_calls must specify a bounded positive limit for paid server tools")
			}
			toolCost += maximumToolPrice * float64(*request.MaxToolCalls)
			toolLimits["max_tool_calls"] = int(*request.MaxToolCalls)
		}
	case *dto.ClaudeRequest:
		needsOutput = true
		_, searches := dto.ProcessTools(request.GetTools())
		for _, tool := range searches {
			if tool.MaxUses <= 0 {
				return nil, fmt.Errorf("web_search.max_uses must specify a positive limit for membership billing")
			}
			toolCost += tools[dto.BuildInToolWebSearch] * float64(tool.MaxUses) / 1000
			toolLimits["web_search"] += tool.MaxUses
		}
	case *dto.GeminiChatRequest:
		needsOutput = true
		if request.GenerationConfig.CandidateCount != nil {
			copies = *request.GenerationConfig.CandidateCount
		}
		if strings.Contains(string(request.Tools), "google_search") || strings.Contains(string(request.Tools), "googleSearch") {
			if limits.MaxToolCalls <= 0 {
				return nil, fmt.Errorf("configure the provider's verified maximum search calls for this model")
			}
			toolCost += tools[dto.BuildInToolGoogleSearch] * float64(limits.MaxToolCalls) / 1000
		}
	case *dto.ImageRequest:
		providerOutputBound = true
		count, err := request.ImageCount(false)
		if err != nil {
			return nil, err
		}
		copies = count
	case *dto.AudioRequest:
		// Speech/transcription APIs expose no max_tokens field. Their aggregate
		// generated usage still needs a verified provider bound in the catalog.
		providerOutputBound = true
	}
	if copies < 1 || copies > dto.MaxImageN {
		return nil, fmt.Errorf("output count is outside the supported billing bound")
	}
	if needsOutput && output <= 0 && !price.UsePrice {
		return nil, fmt.Errorf("set max_tokens, max_completion_tokens or max_output_tokens so the full request can be authorized before dispatch")
	}
	// Local tokenization is an estimate, especially for media and server-side
	// context. The catalog records a provider-enforced aggregate request bound;
	// it must include every internal tool/model turn, not just a context window.
	if info.ImagePricingSnapshot == nil && (!price.UsePrice || info.TieredBillingSnapshot != nil) {
		if strings.TrimSpace(limits.Evidence) == "" || limits.MaxInputTokens <= 0 || ((needsOutput || providerOutputBound) && limits.MaxOutputTokens <= 0) {
			return nil, fmt.Errorf("configure verified aggregate input/output token limits for membership model %q", info.GetBillingModelName())
		}
		if int64(promptTokens) > limits.MaxInputTokens || int64(meta.MaxTokens) > limits.MaxOutputTokens && meta.MaxTokens > 0 {
			return nil, fmt.Errorf("request exceeds this model's verified membership token limits")
		}
		input = float64(limits.MaxInputTokens)
		output = float64(limits.MaxOutputTokens)
	}
	output *= float64(copies)
	if snapshot := info.TieredBillingSnapshot; snapshot != nil {
		requestInput := billingexpr.RequestInput{}
		if info.BillingRequestInput != nil {
			requestInput = *info.BillingRequestInput
		}
		frozen, err := billingexpr.FreezeRequestPricing(snapshot.ExprString, requestInput)
		if err != nil {
			return nil, err
		}
		upper := *snapshot
		upper.ExprString = frozen
		upper.ExprHash = billingexpr.ExprHashString(frozen)
		if upper.DisplayExprString == "" {
			upper.DisplayExprString = snapshot.ExprString
		}
		upper.GroupRatio = 1
		maximum, err = billingexpr.MaximumTokenQuota(&upper, billingexpr.TokenParams{P: input, C: output, Len: input, CR: input, CC: input, CC1h: input, Img: input, ImgCR: input, ImgO: output, AI: input, AO: output}, copies)
		if err != nil {
			return nil, err
		}
		upper.GroupRatio = price.GroupRatioInfo.GroupRatio
		info.TieredBillingSnapshot = &upper

	} else if image := info.ImagePricingSnapshot; image != nil {
		maximum = image.UnitPrice * image.QuotaPerUnit * float64(image.RequestedCount)
	} else if price.UsePrice {
		maximum = price.ApplyOtherRatiosToFloat(price.ModelPrice * common.QuotaPerUnit)
	} else {
		// Cache categories may overlap in provider usage. Bounding each category is
		// intentionally conservative; actual settlement retains normal normalization.
		inputWeight := 1 + max(0, price.CacheRatio) + max(0, price.CacheCreationRatio) + max(0, price.CacheCreation5mRatio) + max(0, price.CacheCreation1hRatio) + max(0, price.ImageRatio) + max(0, price.AudioRatio)
		outputWeight := max(0, price.CompletionRatio) + max(0, price.AudioRatio*price.AudioCompletionRatio)
		maximum = price.ApplyOtherRatiosToFloat((input*inputWeight+output*outputWeight)*price.ModelRatio + input*audioPrice/1_000_000*common.QuotaPerUnit)
	}
	maximum += toolCost * common.QuotaPerUnit
	if maximum < 0 || math.IsNaN(maximum) || math.IsInf(maximum, 0) {
		return nil, model.ErrMembershipInvalidInput
	}
	// Capture auxiliary prices in the same step that computes their upper bound.
	return &types.MembershipPricing{MaxInputTokens: int(limits.MaxInputTokens), MaxOutputTokens: meta.MaxTokens, OutputCopies: copies, ToolCallLimits: toolLimits, RequiresOutputBound: needsOutput && !price.UsePrice, MaximumBeforeGroup: maximum, ToolPrices: tools, QuotaPerUnit: common.QuotaPerUnit, GeminiAudioInputPrice: audioPrice}, nil
}

// ValidateMembershipOutbound checks the effective request after conversion and
// parameter overrides, using a replay reader so dispatch sees the same bytes.
func ValidateMembershipOutbound(request *http.Request, info *relaycommon.RelayInfo) error {
	if info == nil || info.MembershipBillingRequestID == "" || info.MembershipPricing == nil {
		return nil
	}
	return validateRequestBudgetOutbound(request, info)
}

func validateRequestBudgetOutbound(request *http.Request, info *relaycommon.RelayInfo) error {
	snapshot := info.RequestBudget()
	if !snapshot.RequiresOutputBound && len(snapshot.ToolCallLimits) == 0 && snapshot.OutputCopies <= 0 {
		return nil
	}
	if request != nil && strings.HasPrefix(request.Header.Get("Content-Type"), "multipart/") && !snapshot.RequiresOutputBound && len(snapshot.ToolCallLimits) == 0 {
		// Multipart image pricing validates the normalized fields in the image
		// adapter; audio input files have no request-side output/token limits.
		return nil
	}
	if request == nil || request.GetBody == nil {
		return fmt.Errorf("cannot verify the authorized output limits of this upstream request")
	}
	reader, err := request.GetBody()
	if err != nil {
		return err
	}
	defer reader.Close()
	limit := int64(max(constant.MaxRequestBodyMB, 1)) << 20
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > limit || !gjson.ValidBytes(data) {
		return fmt.Errorf("cannot verify the upstream billing limits")
	}
	// Passthrough bodies may retain duplicate keys. Parsers disagree on which
	// occurrence wins, so no funding authorization may depend on such a body.
	pending := []gjson.Result{gjson.ParseBytes(data)}
	for len(pending) > 0 {
		value := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		keys := make(map[string]bool)
		duplicate := false
		value.ForEach(func(key, child gjson.Result) bool {
			if value.IsObject() {
				if keys[key.String()] {
					duplicate = true
					return false
				}
				keys[key.String()] = true
			}
			if child.IsObject() || child.IsArray() {
				pending = append(pending, child)
			}
			return true
		})
		if duplicate {
			return fmt.Errorf("upstream JSON contains duplicate keys")
		}
	}
	if info.TenantID > 0 {
		if err := ValidateTenantTextPayload(data, info.GetBillingModelName()); err != nil {
			return err
		}
	}
	found := false
	for _, path := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens", "max_tokens_to_sample", "max_new_tokens", "maxTokens", "generationConfig.maxOutputTokens", "generation_config.max_output_tokens", "parameters.max_tokens", "parameters.max_new_tokens", "options.num_predict"} {
		if !snapshot.RequiresOutputBound {
			break
		}
		value := gjson.GetBytes(data, path)
		if !value.Exists() {
			continue
		}
		n := value.Float()
		if value.Type != gjson.Number || n <= 0 || n != math.Trunc(n) || n > float64(snapshot.MaxOutputTokens) {
			return fmt.Errorf("upstream %s exceeds the authorized output limit", path)
		}
		found = true
	}
	if snapshot.RequiresOutputBound && !found {
		return fmt.Errorf("upstream conversion removed the authorized output limit")
	}
	for _, path := range []string{"n", "candidate_count", "generationConfig.candidateCount", "generation_config.candidate_count"} {
		value := gjson.GetBytes(data, path)
		if value.Exists() && (value.Type != gjson.Number || value.Float() < 1 || value.Float() > float64(snapshot.OutputCopies) || value.Float() != math.Trunc(value.Float())) {
			return fmt.Errorf("upstream output count exceeds authorization")
		}
	}
	if bound := snapshot.ToolCallLimits["max_tool_calls"]; bound > 0 {
		value := gjson.GetBytes(data, "max_tool_calls")
		if value.Type != gjson.Number || value.Float() < 1 || value.Float() > float64(bound) {
			return fmt.Errorf("upstream tool call limit exceeds authorization")
		}
	}
	if bound := snapshot.ToolCallLimits["web_search"]; bound > 0 {
		total := 0.0
		for _, tool := range gjson.GetBytes(data, "tools").Array() {
			if strings.HasPrefix(tool.Get("type").String(), "web_search") {
				value := tool.Get("max_uses")
				if value.Type != gjson.Number || value.Float() < 1 {
					return fmt.Errorf("upstream web search limit is missing")
				}
				total += value.Float()
			}
		}
		if total > float64(bound) {
			return fmt.Errorf("upstream web search count exceeds authorization")
		}
	}
	return nil
}
