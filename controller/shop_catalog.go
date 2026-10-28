package controller

import (
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type shopMoneyInput struct {
	AmountMinor string `json:"amount_minor"`
	Currency    string `json:"currency"`
	Exponent    int    `json:"exponent"`
}
type shopCoverInput struct {
	URL    string `json:"url"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Alt    string `json:"alt,omitempty"`
}
type shopProductInput struct {
	ExpectedVersion              string                    `json:"expected_version"`
	Name                         *string                   `json:"name"`
	Summary                      *string                   `json:"summary"`
	Specification                *string                   `json:"specification"`
	ProductType                  *string                   `json:"product_type"`
	DeliveryType                 *string                   `json:"delivery_type"`
	CategoryID                   *string                   `json:"category_id"`
	Cover                        *shopCoverInput           `json:"cover"`
	Price                        *shopMoneyInput           `json:"price"`
	SortOrder                    *int                      `json:"sort_order"`
	TemplateVersion              *string                   `json:"template_version"`
	DeliveryFields               *[]model.ShopField        `json:"delivery_fields"`
	BuyerInputSchema             *[]model.ShopField        `json:"buyer_input_schema"`
	UsageConditions              *string                   `json:"usage_conditions"`
	DeliveryPolicy               *model.ShopDeliveryPolicy `json:"delivery_policy"`
	ViewPolicy                   *model.ShopViewPolicy     `json:"view_policy"`
	WarrantyPolicy               *model.ShopPeriodPolicy   `json:"warranty_policy"`
	EntitlementPolicy            *model.ShopPeriodPolicy   `json:"entitlement_policy"`
	RefundPolicy                 *model.ShopRefundPolicy   `json:"refund_policy"`
	InventoryIdentityDomain      *string                   `json:"inventory_identity_domain"`
	IdentityNormalizationVersion *string                   `json:"identity_normalization_version"`
	ManualCapacity               *int64                    `json:"manual_capacity"`
	LowStockThreshold            *int64                    `json:"low_stock_threshold"`
}

func shopCategoryDTO(value model.ShopCategory) gin.H {
	return gin.H{"id": value.PublicID, "version": shopID(value.Version), "name": value.Name, "sort_order": value.SortOrder}
}
func shopProductDTO(value model.ShopProduct, category string, stock int64, enabled, admin bool) (gin.H, error) {
	var terms model.ShopProductTerms
	if value.TermsJSON != "" && common.UnmarshalJsonStr(value.TermsJSON, &terms) != nil {
		return nil, model.ErrShopInvalid
	}
	availability := "unavailable"
	if enabled && value.Status == "published" {
		availability = "available"
		if value.DeliveryType == "automatic" && stock == 0 {
			availability = "sold_out"
		}
		if value.DeliveryType == "manual" && value.ManualUsed >= value.ManualCapacity {
			availability = "capacity_full"
		}
	}
	out := gin.H{"id": value.PublicID, "version": shopID(value.Version), "name": value.Name, "summary": value.Summary, "category_id": nil, "cover": nil, "price": shopMoney(value.PriceMinor, value.Currency, value.CurrencyExponent), "delivery_type": value.DeliveryType, "availability": availability, "product_type": value.ProductKind, "specification": value.Specification, "usage_conditions": terms.UsageConditions, "delivery_fields": terms.DeliveryFields, "buyer_input_schema": terms.BuyerInputSchema, "delivery_policy": terms.DeliveryPolicy, "view_policy": terms.ViewPolicy, "warranty_policy": terms.WarrantyPolicy, "entitlement_policy": terms.EntitlementPolicy, "refund_policy": terms.RefundPolicy, "terms_version": shopID(value.TermsVersion), "template_version": shopID(terms.TemplateVersion), "can_purchase": availability == "available", "allowed_actions": []string{}}
	if terms.DeliveryFields == nil {
		out["delivery_fields"] = []model.ShopField{}
	}
	if terms.BuyerInputSchema == nil {
		out["buyer_input_schema"] = []model.ShopField{}
	}
	if category != "" {
		out["category_id"] = category
	}
	if value.CoverRef != "" {
		out["cover"] = gin.H{"url": value.CoverRef, "alt": value.Name}
	}
	if availability != "available" {
		out["unavailable_reason_code"] = "SHOP_NEW_ORDERS_PAUSED"
		if availability == "sold_out" {
			out["unavailable_reason_code"] = "SHOP_OUT_OF_STOCK"
		}
		if availability == "capacity_full" {
			out["unavailable_reason_code"] = "SHOP_CAPACITY_FULL"
		}
	}
	if admin {
		out["state"] = value.Status
		out["sort_order"] = value.SortOrder
		out["manual_capacity"] = value.ManualCapacity
		out["low_stock_threshold"] = value.LowStockThreshold
		out["inventory_identity_domain"] = value.InventoryIdentityDomain
		out["identity_normalization_version"] = shopID(value.IdentityNormalizationVersion)
		out["allowed_actions"] = []string{"edit", "publish", "unpublish", "archive"}
	}
	return out, nil
}
func ListShopCategories(c *gin.Context) {
	rows, err := model.ListShopCategories(shopDB(c))
	if err != nil {
		shopModelError(c, err)
		return
	}
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		items = append(items, shopCategoryDTO(row))
	}
	shopSuccess(c, 200, items)
}
func SaveShopCategory(c *gin.Context) {
	var in struct {
		Name            string `json:"name"`
		SortOrder       int    `json:"sort_order"`
		ExpectedVersion string `json:"expected_version"`
	}
	if !shopBind(c, &in, 16384) {
		return
	}
	id := c.Param("category_id")
	if id != "" && in.ExpectedVersion == "" {
		shopModelError(c, model.ErrShopInvalid)
		return
	}
	cmd, ok := shopCommand(c, in, "category", id, in.ExpectedVersion)
	if !ok {
		return
	}
	row, err := model.SaveShopCategory(shopDB(c), cmd, id, strings.TrimSpace(in.Name), in.SortOrder)
	if err != nil {
		shopModelError(c, err)
		return
	}
	shopSuccess(c, 200, shopCategoryDTO(*row))
}
func shopCatalogDTOs(c *gin.Context, rows []model.ShopProduct) ([]gin.H, error) {
	settings, err := model.GetShopSettings(shopDB(c))
	if err != nil {
		return nil, err
	}
	enabled := settings.SalesEnabled && service.ShopPaymentReady(settings)
	categories, err := model.ListShopCategories(shopDB(c))
	if err != nil {
		return nil, err
	}
	categoryIDs := map[int64]string{}
	for _, category := range categories {
		categoryIDs[category.ID] = category.PublicID
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	var counts []struct {
		ProductID int64
		Total     int64
	}
	if len(ids) > 0 {
		err = shopDB(c).Model(&model.ShopStockUnit{}).Select("product_id, COUNT(*) AS total").Where("product_id IN ? AND state = ?", ids, "available").Group("product_id").Scan(&counts).Error
		if err != nil {
			return nil, err
		}
	}
	stock := map[int64]int64{}
	for _, count := range counts {
		stock[count.ProductID] = count.Total
	}
	result := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		category := ""
		if row.CategoryID != nil {
			category = categoryIDs[*row.CategoryID]
		}
		item, err := shopProductDTO(row, category, stock[row.ID], enabled, shopScope(c).Admin)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}
func ListShopProducts(c *gin.Context) {
	page, size, ok := shopPagination(c)
	if !ok {
		return
	}
	var category *int64
	if value := c.Query("category_id"); value != "" {
		var row model.ShopCategory
		if err := shopDB(c).Where("public_id = ? AND tenant_id = ?", value, model.PlatformTenantID).First(&row).Error; err != nil {
			shopModelError(c, err)
			return
		}
		category = &row.ID
	}
	rows, total, err := model.ListShopProducts(shopDB(c), shopScope(c), c.Query("state"), category, c.Query("q"), page, size)
	if err != nil {
		shopModelError(c, err)
		return
	}
	items, err := shopCatalogDTOs(c, rows)
	if err != nil {
		shopModelError(c, err)
		return
	}
	shopSuccess(c, 200, shopPage(items, page, size, total))
}
func GetShopProduct(c *gin.Context) {
	row, err := model.GetShopProduct(shopDB(c), shopScope(c), c.Param("product_id"))
	if err != nil {
		shopModelError(c, err)
		return
	}
	items, err := shopCatalogDTOs(c, []model.ShopProduct{*row})
	if err != nil {
		shopModelError(c, err)
		return
	}
	shopSuccess(c, 200, items[0])
}
func SaveShopProduct(c *gin.Context) {
	var in shopProductInput
	if !shopBind(c, &in, 131072) {
		return
	}
	id := c.Param("product_id")
	if id != "" && in.ExpectedVersion == "" {
		shopModelError(c, model.ErrShopInvalid)
		return
	}
	cmd, ok := shopCommand(c, in, "product", id, in.ExpectedVersion)
	if !ok {
		return
	}
	var value model.ShopProduct
	var terms model.ShopProductTerms
	if id != "" {
		old, err := model.GetShopProduct(shopDB(c), shopScope(c), id)
		if err != nil {
			shopModelError(c, err)
			return
		}
		value = *old
		if old.TermsJSON != "" && common.UnmarshalJsonStr(old.TermsJSON, &terms) != nil {
			shopModelError(c, model.ErrShopInvalid)
			return
		}
	}
	if in.Name != nil {
		value.Name = strings.TrimSpace(*in.Name)
	}
	if in.Summary != nil {
		value.Summary = *in.Summary
	}
	if in.Specification != nil {
		value.Specification = *in.Specification
	}
	if in.ProductType != nil {
		value.ProductKind = *in.ProductType
	}
	if in.DeliveryType != nil {
		value.DeliveryType = *in.DeliveryType
	}
	if in.SortOrder != nil {
		value.SortOrder = *in.SortOrder
	}
	if in.CategoryID != nil {
		value.CategoryID = nil
		if *in.CategoryID != "" {
			var category model.ShopCategory
			if err := shopDB(c).Where("public_id = ? AND tenant_id = ?", *in.CategoryID, model.PlatformTenantID).First(&category).Error; err != nil {
				shopModelError(c, err)
				return
			}
			value.CategoryID = &category.ID
		}
	}
	if in.Cover != nil {
		value.CoverRef = in.Cover.URL
	}
	if in.Price != nil {
		minor, err := strconv.ParseInt(in.Price.AmountMinor, 10, 64)
		if err != nil || minor < 0 || strconv.FormatInt(minor, 10) != in.Price.AmountMinor {
			shopModelError(c, model.ErrShopInvalid)
			return
		}
		settings, err := model.GetShopSettings(shopDB(c))
		if err != nil {
			shopModelError(c, err)
			return
		}
		if in.Price.Currency != settings.Currency || in.Price.Exponent != settings.CurrencyExponent {
			shopModelError(c, model.ErrShopInvalid)
			return
		}
		value.PriceMinor = minor
		value.Currency = settings.Currency
		value.CurrencyExponent = settings.CurrencyExponent
	}
	if in.TemplateVersion != nil {
		n, err := shopVersion(*in.TemplateVersion)
		if err != nil {
			shopModelError(c, err)
			return
		}
		terms.TemplateVersion = n
	}
	if in.DeliveryFields != nil {
		terms.DeliveryFields = *in.DeliveryFields
	}
	if in.BuyerInputSchema != nil {
		terms.BuyerInputSchema = *in.BuyerInputSchema
	}
	if in.UsageConditions != nil {
		terms.UsageConditions = *in.UsageConditions
	}
	if in.DeliveryPolicy != nil {
		terms.DeliveryPolicy = *in.DeliveryPolicy
	}
	if in.ViewPolicy != nil {
		terms.ViewPolicy = *in.ViewPolicy
	}
	if in.WarrantyPolicy != nil {
		terms.WarrantyPolicy = *in.WarrantyPolicy
	}
	if in.EntitlementPolicy != nil {
		terms.EntitlementPolicy = *in.EntitlementPolicy
	}
	if in.RefundPolicy != nil {
		terms.RefundPolicy = *in.RefundPolicy
	}
	if in.InventoryIdentityDomain != nil {
		value.InventoryIdentityDomain = *in.InventoryIdentityDomain
	}
	if in.IdentityNormalizationVersion != nil {
		n, err := shopVersion(*in.IdentityNormalizationVersion)
		if err != nil {
			shopModelError(c, err)
			return
		}
		value.IdentityNormalizationVersion = n
	}
	if in.ManualCapacity != nil {
		value.ManualCapacity = *in.ManualCapacity
	}
	if in.LowStockThreshold != nil {
		value.LowStockThreshold = *in.LowStockThreshold
	}
	raw, err := common.Marshal(terms)
	if err != nil {
		shopModelError(c, err)
		return
	}
	value.TermsJSON = string(raw)
	row, err := model.SaveShopProduct(shopDB(c), cmd, id, value)
	if err != nil {
		shopModelError(c, err)
		return
	}
	items, err := shopCatalogDTOs(c, []model.ShopProduct{*row})
	if err != nil {
		shopModelError(c, err)
		return
	}
	shopSuccess(c, 200, items[0])
}
func ChangeShopProductStatus(c *gin.Context) {
	var in struct {
		ExpectedVersion string `json:"expected_version"`
		Reason          string `json:"reason"`
	}
	if !shopBind(c, &in, 16384) {
		return
	}
	if in.ExpectedVersion == "" {
		shopModelError(c, model.ErrShopInvalid)
		return
	}
	action := c.GetString("shop_action")
	cmd, ok := shopCommand(c, gin.H{"status": action, "input": in}, "product-status", c.Param("product_id"), in.ExpectedVersion)
	if !ok {
		return
	}
	previous, err := model.GetShopIdempotency(shopDB(c), shopScope(c), "product-status", c.Param("product_id"), cmd.Key)
	if err != nil {
		shopModelError(c, err)
		return
	}
	if action == "published" && previous == nil {
		settings, err := model.GetShopSettings(shopDB(c))
		if err != nil {
			shopModelError(c, err)
			return
		}
		if !service.ShopPaymentReady(settings) {
			shopError(c, 503, "SHOP_CHANNEL_UNAVAILABLE")
			return
		}
	}
	row, err := model.SetShopProductStatus(shopDB(c), cmd, c.Param("product_id"), action, in.Reason)
	if err != nil {
		shopModelError(c, err)
		return
	}
	items, err := shopCatalogDTOs(c, []model.ShopProduct{*row})
	if err != nil {
		shopModelError(c, err)
		return
	}
	shopSuccess(c, 200, items[0])
}
func GetShopConfig(c *gin.Context) {
	settings, err := model.GetShopSettings(shopDB(c))
	if err != nil {
		shopModelError(c, err)
		return
	}
	methods, err := service.ShopPaymentMethods(shopDB(c))
	if err != nil {
		shopModelError(c, err)
		return
	}
	enabled := settings.SalesEnabled && service.ShopPaymentReady(settings) && len(methods) > 0
	if !enabled {
		methods = []service.ShopPaymentMethod{}
	}
	var currency any
	if settings.Currency != "" {
		currency = settings.Currency
	}
	shopSuccess(c, 200, gin.H{"new_orders_enabled": enabled, "currency": currency, "payment_methods": methods, "server_time": shopTime(time.Now().Unix())})
}
