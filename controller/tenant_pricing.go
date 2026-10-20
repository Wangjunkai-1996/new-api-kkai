package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

type tenantCustomerPricing struct {
	model.Pricing
	GroupRatio    map[string]float64 `json:"group_ratio"`
	TenantPricing bool               `json:"tenant_pricing"`
}

func tenantCustomerPrices(c *gin.Context) ([]tenantCustomerPricing, error) {
	prices, err := service.NewTenantPricingService(model.DB).ResolveCustomerPrices(c.Request.Context(), common.GetContextKeyInt64(c, constant.ContextKeyTenantID))
	if err != nil {
		return nil, err
	}
	byName := make(map[string]model.Pricing)
	for _, row := range model.GetPricing() {
		byName[row.ModelName] = row
	}
	limited := common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled)
	limits, _ := common.GetContextKeyType[map[string]bool](c, constant.ContextKeyTokenModelLimit)
	result := make([]tenantCustomerPricing, 0, len(prices))
	indices := make(map[string]int, len(prices))
	for _, price := range prices {
		row, ok := byName[price.Model]
		if !ok || !row.PricingConfigured {
			continue
		}
		if limited && limits[ratio_setting.RoutingMatchModelName(price.Model)] != true {
			continue
		}
		if index, exists := indices[price.Model]; exists {
			result[index].EnableGroup = append(result[index].EnableGroup, price.Group)
			result[index].GroupRatio[price.Group] = price.SalesRatio
			version, _ := strconv.ParseInt(result[index].PricingVersion, 10, 64)
			result[index].PricingVersion = strconv.FormatInt(max(version, price.VersionID), 10)
			continue
		}
		endpoints := make([]relaytypes.EndpointType, 0, len(row.SupportedEndpointTypes))
		for _, endpoint := range row.SupportedEndpointTypes {
			switch endpoint {
			case relaytypes.EndpointTypeOpenAI, relaytypes.EndpointTypeOpenAIResponse, relaytypes.EndpointTypeAnthropic, relaytypes.EndpointTypeEmbeddings:
				endpoints = append(endpoints, endpoint)
			}
		}
		if len(endpoints) == 0 {
			continue
		}
		row.SupportedEndpointTypes = endpoints
		row.EnableGroup = []string{price.Group}
		row.BillingPluginVariants = nil
		row.PricingVersion = strconv.FormatInt(price.VersionID, 10)
		indices[price.Model] = len(result)
		result = append(result, tenantCustomerPricing{Pricing: row, TenantPricing: true, GroupRatio: map[string]float64{price.Group: price.SalesRatio}})
	}
	c.Header("Cache-Control", "no-store")
	return result, nil
}

func getTenantPricing(c *gin.Context) {
	prices, err := tenantCustomerPrices(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	groups := map[string]string{}
	ratios := map[string]float64{}
	for _, price := range prices {
		for group, ratio := range price.GroupRatio {
			groups[group] = setting.GetUsableGroupDescription(group)
			ratios[group] = ratio
		}
	}
	vendors := make([]model.PricingVendor, 0)
	usedVendors := make(map[int]bool)
	for _, price := range prices {
		usedVendors[price.VendorID] = true
	}
	for _, vendor := range model.GetVendors() {
		if usedVendors[vendor.ID] {
			vendors = append(vendors, vendor)
		}
	}
	endpoints := map[string]common.EndpointInfo{}
	for _, price := range prices {
		for _, endpoint := range price.SupportedEndpointTypes {
			if info, ok := model.GetSupportedEndpointMap()[string(endpoint)]; ok {
				endpoints[string(endpoint)] = info
			}
		}
	}
	displayNames := make(map[string]string, len(groups))
	for group, description := range groups {
		displayNames[group] = setting.GetGroupDisplayNameWithFallback(group, description)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": prices, "vendors": vendors, "group_ratio": ratios, "usable_group": groups, "group_display_names": displayNames, "supported_endpoint": endpoints, "auto_groups": []string{}, "auto_group_chains": map[string][]string{}, "membership_group_quotes": []service.MembershipGroupQuote{}})
}

func tenantModelNames(c *gin.Context) ([]string, error) {
	prices, err := tenantCustomerPrices(c)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(prices))
	for _, price := range prices {
		names = append(names, price.ModelName)
	}
	return names, nil
}

func listTenantModels(c *gin.Context, modelType int) {
	prices, err := tenantCustomerPrices(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	models := make([]dto.OpenAIModels, 0, len(prices))
	for _, price := range prices {
		item := buildOpenAIModel(price.ModelName, nil)
		item.SupportedEndpointTypes = price.SupportedEndpointTypes
		models = append(models, item)
	}
	respondModelList(c, modelType, models)
}
