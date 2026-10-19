package service

import (
	"fmt"
	"math"
	"net/http"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/types"
)

// PrepareTenantBudget authorizes the complete provider-enforced request bound,
// including hidden input and cache usage, before a customer can incur a cost.
// Pilot models need documented aggregate bounds in the existing model catalog.
func PrepareTenantBudget(info *relaycommon.RelayInfo, price *types.PriceData, promptTokens int, meta *relaytypes.TokenCountMeta) error {
	if info == nil || info.TenantID <= 0 {
		return nil
	}
	if price == nil {
		return fmt.Errorf("tenant billing price is missing")
	}
	if snapshot := info.TenantBudget; snapshot != nil {
		*price = snapshot.Price
		info.PriceData = snapshot.Price
		info.TieredBillingSnapshot = snapshot.Tiered
		return nil
	}
	snapshot, err := prepareRequestBudget(info, price, promptTokens, meta)
	if err != nil {
		return err
	}
	maximum := snapshot.MaximumBeforeGroup * price.GroupRatioInfo.GroupRatio
	if maximum <= 0 || math.IsNaN(maximum) || math.IsInf(maximum, 0) {
		return fmt.Errorf("tenant request needs a finite positive maximum charge")
	}
	quota, err := common.QuotaFromFloatStrict(math.Ceil(maximum))
	if err != nil {
		return err
	}
	price.QuotaToPreConsume = quota
	price.Quota = quota
	price.FreeModel = false
	snapshot.Price = *price
	snapshot.OtherRatios = price.OtherRatios()
	snapshot.Tiered = info.TieredBillingSnapshot
	snapshot.ModelName = info.GetBillingModelName()
	info.TenantBudget = snapshot
	info.PriceData = *price
	return nil
}

// ValidateTenantOutbound runs after provider conversion and parameter overrides.
// A missing snapshot or removed/increased bound cannot reach the upstream.
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
	return validateRequestBudgetOutbound(request, info)
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
