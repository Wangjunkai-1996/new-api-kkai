package service

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/tidwall/gjson"
)

// PrepareTenantBudget freezes the shared relay's estimated pre-consumption and
// pricing facts. Tenant cash settlement remains durable, but is independent of
// membership products and does not change the provider's optional parameters.
func PrepareTenantBudget(info *relaycommon.RelayInfo, price *types.PriceData, promptTokens int, meta *relaytypes.TokenCountMeta) error {
	if info == nil || info.TenantID <= 0 {
		return nil
	}
	if price == nil || meta == nil || promptTokens < 0 || meta.MaxTokens < 0 || price.QuotaToPreConsume < 0 || price.QuotaToPreConsume > math.MaxInt32 {
		return fmt.Errorf("tenant billing price is missing")
	}
	if snapshot := info.TenantBudget; snapshot != nil {
		*price = snapshot.Price
		price.ReplaceOtherRatios(snapshot.OtherRatios)
		info.PriceData = *price
		info.TieredBillingSnapshot = snapshot.Tiered
		return nil
	}
	if common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
		return fmt.Errorf("tenant quota conversion is invalid")
	}
	if previous := info.TieredBillingSnapshot; previous != nil {
		requestInput := billingexpr.RequestInput{}
		if info.BillingRequestInput != nil {
			requestInput = *info.BillingRequestInput
		}
		frozen, err := billingexpr.FreezeRequestPricing(previous.ExprString, requestInput)
		if err != nil {
			return err
		}
		snapshot := *previous
		snapshot.ExprString = frozen
		snapshot.ExprHash = billingexpr.ExprHashString(frozen)
		if snapshot.DisplayExprString == "" {
			snapshot.DisplayExprString = previous.ExprString
		}
		info.TieredBillingSnapshot = &snapshot
	}
	snapshot := &types.MembershipPricing{
		Price: *price, OtherRatios: price.OtherRatios(),
		Tiered: info.TieredBillingSnapshot, ModelName: info.GetBillingModelName(),
		QuotaPerUnit:          common.QuotaPerUnit,
		ToolPrices:            operation_setting.SnapshotToolPrices(info.GetBillingModelName()),
		GeminiAudioInputPrice: operation_setting.GetGeminiInputAudioPricePerMillionTokens(info.GetBillingModelName()),
	}
	// A zero base model price can still carry separately billed tools or audio.
	// Only genuinely free requests may use a reservation that cannot settle a
	// positive charge. This remains independent of the pre-consume preference.
	price.FreeModel = price.GroupRatioInfo.GroupRatio == 0
	if !price.FreeModel && info.TieredBillingSnapshot == nil && ((price.UsePrice && price.ModelPrice == 0) || (!price.UsePrice && price.ModelRatio == 0)) {
		paidExtras, err := tenantRequestHasPaidExtras(info, meta, snapshot, nil)
		if err != nil {
			return err
		}
		price.FreeModel = !paidExtras
	}
	snapshot.Price = *price
	snapshot.Price.ReplaceOtherRatios(snapshot.OtherRatios)
	info.TenantBudget = snapshot
	info.PriceData = *price
	return nil
}

// tenantRequestHasPaidExtras inspects protocol tool declarations, never tool
// parameter schemas or user text. It classifies funding; it does not restrict
// which tools, media or provider fields may be forwarded.
func tenantRequestHasPaidExtras(info *relaycommon.RelayInfo, meta *relaytypes.TokenCountMeta, snapshot *types.MembershipPricing, data []byte) (bool, error) {
	if info.ChannelMeta != nil && len(info.ParamOverride) > 0 {
		return true, nil // Overrides may add billed features after reservation.
	}
	if request, ok := info.Request.(*dto.GeminiChatRequest); ok && request.CachedContent != "" {
		return true, nil // Opaque provider context cannot prove auxiliary usage is free.
	}
	if snapshot.GeminiAudioInputPrice > 0 {
		for _, file := range meta.Files {
			if file != nil && file.FileType == relaytypes.FileTypeAudio {
				return true, nil
			}
		}
	}
	if info.RelayMode != relayconstant.RelayModeResponses && strings.HasSuffix(info.GetBillingModelName(), "search-preview") && snapshot.ToolPrices[dto.BuildInToolWebSearchPreview] > 0 {
		return true, nil
	}
	if data == nil {
		var err error
		data, err = common.Marshal(info.Request)
		if err != nil {
			return false, err
		}
	}
	payload := gjson.ParseBytes(data)
	for _, field := range []string{"extra_body", "web_search_options", "search_parameters", "enable_search", "web_search"} {
		value := payload.Get(field)
		if value.Exists() && value.Type != gjson.Null && value.Type != gjson.False {
			return true, nil
		}
	}
	if payload.Get("previous_response_id").String() != "" || (payload.Get("conversation").Exists() && payload.Get("conversation").Type != gjson.Null) || payload.Get("cachedContent").String() != "" || payload.Get("cached_content").String() != "" {
		return true, nil
	}
	pending := []gjson.Result{payload.Get("tools"), payload.Get("functions")}
	for _, message := range payload.Get("messages").Array() {
		pending = append(pending, message.Get("tools"))
	}
	for _, input := range payload.Get("input").Array() {
		if input.Get("type").String() == "additional_tools" {
			pending = append(pending, input.Get("tools"))
		}
	}
	for len(pending) > 0 {
		tool := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if tool.IsArray() {
			pending = append(pending, tool.Array()...)
			continue
		}
		if !tool.IsObject() {
			continue
		}
		kind := tool.Get("type").String()
		if kind == "namespace" {
			pending = append(pending, tool.Get("tools"))
			continue
		}
		if kind == dto.BuildInToolImageGeneration || snapshot.ToolPrices[kind] > 0 {
			return true, nil
		}
		for _, declaration := range []string{"functionDeclarations", "function_declarations"} {
			pending = append(pending, tool.Get(declaration))
		}
		for _, search := range []string{"googleSearch", "google_search", "googleSearchRetrieval", "google_search_retrieval"} {
			if value := tool.Get(search); value.Exists() && value.Type != gjson.Null && snapshot.ToolPrices[dto.BuildInToolGoogleSearch] > 0 {
				return true, nil
			}
		}
		name := tool.Get("name").String()
		if function := tool.Get("function.name"); function.Exists() {
			name = function.String()
		} else if custom := tool.Get("custom.name"); custom.Exists() {
			name = custom.String()
		}
		if kind == "" || kind == "function" || kind == "custom" {
			switch name {
			case dto.BuildInToolWebSearch, dto.BuildInToolWebSearchPreview, dto.BuildInToolFileSearch, dto.BuildInToolGoogleSearch, dto.BuildInToolImageGeneration:
				continue // Caller functions cannot impersonate billed hosted tools.
			}
		}
		if snapshot.ToolPrices[name] > 0 {
			return true, nil
		}
	}
	return false, nil
}

// ValidateTenantOutbound runs after provider conversion and parameter overrides.
// Requests must retain their frozen pricing and an integrated relay transport.
func ValidateTenantOutbound(request *http.Request, info *relaycommon.RelayInfo) error {
	if info == nil || info.TenantID <= 0 {
		return nil
	}
	if err := ValidateTenantChannelTransport(info); err != nil {
		return err
	}
	if info.TenantBudget == nil {
		return fmt.Errorf("tenant request has no authorized billing budget")
	}
	if info.TenantBudget.Price.FreeModel && info.TenantBudget.Price.GroupRatioInfo.GroupRatio > 0 {
		if request == nil || request.GetBody == nil {
			return fmt.Errorf("cannot verify the final request for a free tenant reservation")
		}
		reader, err := request.GetBody()
		if err != nil {
			return err
		}
		defer reader.Close()
		data, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		meta := &relaytypes.TokenCountMeta{}
		if info.Request != nil {
			meta = info.Request.GetTokenCountMeta()
		}
		paid, err := tenantRequestHasPaidExtras(info, meta, info.TenantBudget, data)
		if err != nil {
			return err
		}
		if paid {
			return fmt.Errorf("upstream request adds paid features to a free tenant reservation")
		}
	}
	return nil
}

// ValidateTenantChannelTransport also runs before adaptor dispatch, because
// SDK and WebSocket adaptors do not pass through the shared HTTP client.
func ValidateTenantChannelTransport(info *relaycommon.RelayInfo) error {
	if info == nil || info.TenantID <= 0 {
		return nil
	}
	apiType, _ := common.ChannelType2APIType(info.GetChannelType())
	if !slices.Contains(constant.TenantTextChannelTypes(), info.GetChannelType()) || info.ChannelMeta == nil || info.ApiType != apiType {
		return fmt.Errorf("channel transport is unavailable on reseller sites")
	}
	return nil
}
