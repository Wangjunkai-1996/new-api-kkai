package controller

import (
	"errors"
	"io"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type resellerApplicationRequest struct {
	BusinessName        string `json:"business_name"`
	DesiredDomainPrefix string `json:"desired_domain_prefix"`
	ContactName         string `json:"contact_name"`
	ContactEmail        string `json:"contact_email"`
	Notes               string `json:"notes"`
}

type resellerProfileRequest struct {
	SiteName        string `json:"site_name"`
	LogoURL         string `json:"logo_url"`
	Contact         string `json:"contact"`
	Announcement    string `json:"announcement"`
	HomepageContent string `json:"homepage_content"`
}

type resellerPriceRequest struct {
	SalesGroup      string `json:"sales_group"`
	Model           string `json:"model"`
	SaleMultiplier  string `json:"sale_multiplier"`
	Status          string `json:"status"`
	ExpectedVersion int64  `json:"expected_version"`
}

type resellerPricesPatchRequest struct {
	Prices []resellerPriceRequest `json:"prices"`
}

func resellerTenant(c *gin.Context) (model.TenantContext, *model.TenantMembership, *gorm.DB, bool) {
	context, ok := middleware.GetTenantContext(c)
	if !ok || context.TenantID <= model.PlatformTenantID || model.DB == nil {
		return model.TenantContext{}, nil, nil, false
	}
	membership, ok := middleware.GetResellerMembership(c)
	if !ok || model.ValidateTenantMembership(membership, context.TenantID, c.GetInt("id"), model.TenantMembershipRoleOwner) != nil {
		return model.TenantContext{}, nil, nil, false
	}
	return context, membership, model.DB, true
}

func resellerBadRequest(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": message})
}

func resellerNotReady(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "tenant store is unavailable"})
}

func resellerPageQuery(c *gin.Context) *common.PageInfo {
	page := common.GetPageQuery(c)
	// Preserve the first 200 rows for clients that predate pagination.
	if c.Query("p") == "" && c.Query("page_size") == "" && c.Query("ps") == "" && c.Query("size") == "" {
		page.PageSize = 200
	}
	if page.PageSize < 1 {
		page.PageSize = common.ItemsPerPage
	}
	if page.Page < 1 || page.Page-1 > int(^uint(0)>>1)/page.PageSize {
		page.Page = 1
	}
	return page
}

// CreateResellerApplication creates a pending application for the logged-in
// platform account. Tenant IDs are assigned only by platform review.
func CreateResellerApplication(c *gin.Context) {
	if model.DB == nil {
		resellerNotReady(c)
		return
	}
	var request resellerApplicationRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		resellerBadRequest(c, "invalid application")
		return
	}
	request.BusinessName = strings.TrimSpace(request.BusinessName)
	request.ContactName = strings.TrimSpace(request.ContactName)
	request.ContactEmail = model.NormalizeEmail(request.ContactEmail)
	request.Notes = strings.TrimSpace(request.Notes)
	address, emailErr := mail.ParseAddress(request.ContactEmail)
	if request.BusinessName == "" || len(request.BusinessName) > 128 || len(request.ContactName) > 64 || len(request.ContactEmail) > 254 || len(request.Notes) > 4096 || emailErr != nil || address.Address != request.ContactEmail {
		resellerBadRequest(c, "a site name and valid contact email are required")
		return
	}
	prefix, err := model.NormalizeResellerDomainPrefix(request.DesiredDomainPrefix)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "RESELLER_DOMAIN_PREFIX_INVALID", "message": "a valid non-reserved domain prefix is required"})
		return
	}
	if prefix != "" {
		if !model.ResellerApplicationPrefixEnabled(model.DB) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "code": "RESELLER_DOMAIN_PREFIX_UNAVAILABLE", "message": "domain prefix preferences are not available"})
			return
		}
		host, err := model.NormalizeResellerDomain(prefix+"."+strings.TrimSpace(os.Getenv("TENANT_ROOT_DOMAIN")), os.Getenv("TENANT_ROOT_DOMAIN"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "RESELLER_DOMAIN_PREFIX_INVALID", "message": "a valid non-reserved domain prefix is required"})
			return
		}
		for _, platform := range strings.Split(os.Getenv("TENANT_PLATFORM_HOSTS"), ",") {
			canonical, _ := model.NormalizeTenantHost(platform)
			if canonical == host {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "RESELLER_DOMAIN_PREFIX_INVALID", "message": "a valid non-reserved domain prefix is required"})
				return
			}
		}
		var assigned int64
		if err := model.DB.Model(&model.TenantDomain{}).Where("hostname = ?", host).Count(&assigned).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		if assigned > 0 {
			c.JSON(http.StatusConflict, gin.H{"success": false, "code": "RESELLER_DOMAIN_PREFIX_TAKEN", "message": "the requested domain is already assigned"})
			return
		}
	}
	var pending int64
	if err := model.DB.Model(&model.ResellerApplication{}).Where("applicant_user_id = ? AND status = ?", c.GetInt("id"), model.ResellerApplicationStatusPending).Count(&pending).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if pending > 0 {
		c.JSON(http.StatusConflict, gin.H{"success": false, "code": "RESELLER_APPLICATION_PENDING", "message": "a reseller application is already pending"})
		return
	}
	application := model.ResellerApplication{
		ApplicantUserID:     c.GetInt("id"),
		BusinessName:        request.BusinessName,
		DesiredDomainPrefix: prefix,
		ContactName:         request.ContactName,
		ContactEmail:        request.ContactEmail,
		Notes:               request.Notes,
		Status:              model.ResellerApplicationStatusPending,
	}
	if err := model.CreateResellerApplication(&application); err != nil {
		if errors.Is(err, model.ErrResellerInvalidApplication) {
			resellerBadRequest(c, "a site name and valid contact email are required")
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, application)
}

// ListResellerApplications returns only applications submitted by the current
// account. Platform review is guarded by separate AdminAuth routes.
func ListResellerApplications(c *gin.Context) {
	if model.DB == nil {
		resellerNotReady(c)
		return
	}
	var applications []model.ResellerApplication
	if err := model.DB.Where("applicant_user_id = ?", c.GetInt("id")).Order("id DESC").Find(&applications).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var domains []model.TenantDomain
	tenantIDs := make([]int64, 0, len(applications))
	for _, application := range applications {
		if application.TenantID > 0 {
			tenantIDs = append(tenantIDs, application.TenantID)
		}
	}
	if len(tenantIDs) > 0 {
		if err := model.DB.Where("tenant_id IN ? AND status = ?", tenantIDs, model.TenantDomainStatusActive).Find(&domains).Error; err != nil {
			common.ApiError(c, err)
			return
		}
	}
	type applicationView struct {
		model.ResellerApplication
		Domains []model.TenantDomain `json:"domains"`
	}
	views := make([]applicationView, 0, len(applications))
	for _, application := range applications {
		view := applicationView{ResellerApplication: application, Domains: []model.TenantDomain{}}
		for _, domain := range domains {
			if domain.TenantID == application.TenantID {
				view.Domains = append(view.Domains, domain)
			}
		}
		views = append(views, view)
	}
	common.ApiSuccess(c, views)
}

func GetResellerMe(c *gin.Context) {
	context, _, db, ok := resellerTenant(c)
	if !ok {
		resellerNotReady(c)
		return
	}
	var tenant model.Tenant
	if err := db.First(&tenant, context.TenantID).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var profile model.TenantProfile
	profileErr := db.Where("tenant_id = ?", context.TenantID).First(&profile).Error
	if errors.Is(profileErr, gorm.ErrRecordNotFound) {
		profile = model.TenantProfile{TenantID: context.TenantID, SiteName: tenant.DisplayName}
	} else if profileErr != nil {
		common.ApiError(c, profileErr)
		return
	}
	var domains []model.TenantDomain
	if err := db.Where("tenant_id = ? AND status = ?", context.TenantID, model.TenantDomainStatusActive).Find(&domains).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"tenant": tenant, "profile": profile, "domains": domains})
}

func GetResellerProfile(c *gin.Context) {
	context, _, db, ok := resellerTenant(c)
	if !ok {
		resellerNotReady(c)
		return
	}
	var profile model.TenantProfile
	err := db.Where("tenant_id = ?", context.TenantID).First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiSuccess(c, model.TenantProfile{TenantID: context.TenantID})
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, profile)
}

func UpdateResellerProfile(c *gin.Context) {
	context, _, db, ok := resellerTenant(c)
	if !ok {
		resellerNotReady(c)
		return
	}
	var request resellerProfileRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		resellerBadRequest(c, "invalid reseller profile")
		return
	}
	profile := model.TenantProfile{
		TenantID:        context.TenantID,
		SiteName:        strings.TrimSpace(request.SiteName),
		Contact:         strings.TrimSpace(request.Contact),
		Announcement:    strings.TrimSpace(request.Announcement),
		HomepageContent: strings.TrimSpace(request.HomepageContent),
		UpdatedBy:       c.GetInt("id"),
	}
	if err := model.ValidateTenantProfile(&profile); err != nil {
		resellerBadRequest(c, "invalid reseller profile")
		return
	}
	var current model.TenantProfile
	err := db.Where("tenant_id = ?", context.TenantID).First(&current).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err = db.Create(&profile).Error; err == nil {
			common.ApiSuccess(c, profile)
			return
		}
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	profile.ID, profile.CreatedAt = current.ID, current.CreatedAt
	profile.LogoURL = current.LogoURL
	if err = db.Model(&current).Where("tenant_id = ?", context.TenantID).Updates(map[string]any{
		"site_name": profile.SiteName, "contact": profile.Contact,
		"announcement": profile.Announcement, "homepage_content": profile.HomepageContent,
		"updated_by": profile.UpdatedBy, "updated_at": time.Now(),
	}).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, profile)
}

func UploadResellerLogo(c *gin.Context) {
	tenant, _, db, ok := resellerTenant(c)
	if !ok {
		resellerNotReady(c)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.MaxTenantLogoBytes+64*1024)
	if err := c.Request.ParseMultipartForm(service.MaxTenantLogoBytes); err != nil {
		resellerBadRequest(c, "invalid logo upload")
		return
	}
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	file, _, err := c.Request.FormFile("logo")
	if err != nil {
		resellerBadRequest(c, "logo is required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, service.MaxTenantLogoBytes+1))
	if err != nil {
		resellerBadRequest(c, "invalid logo upload")
		return
	}
	logoURL, err := service.SaveTenantLogo(db, tenant.TenantID, c.GetInt("id"), data)
	if err != nil {
		resellerBadRequest(c, "logo must be JPEG, PNG or WebP, at most 2MB and 1024 x 1024 pixels")
		return
	}
	common.ApiSuccess(c, gin.H{"logo_url": logoURL})
}

func GetResellerCatalog(c *gin.Context) {
	context, _, db, ok := resellerTenant(c)
	if !ok {
		resellerNotReady(c)
		return
	}
	entries, err := service.NewTenantPricingService(db).ResolveCatalog(c.Request.Context(), context.TenantID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, entries)
}

func UpdateResellerCatalog(c *gin.Context) {
	_, _, _, ok := resellerTenant(c)
	if !ok {
		resellerNotReady(c)
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "TENANT_GROUP_PRICING_REQUIRED", "message": "Set prices by group instead of model."})
}

func GetResellerPrices(c *gin.Context) {
	context, _, db, ok := resellerTenant(c)
	if !ok {
		resellerNotReady(c)
		return
	}
	pricing := service.NewTenantPricingService(db)
	groups, err := pricing.ListGroupPrices(c.Request.Context(), context.TenantID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if c.Query("scope") == "group" {
		common.ApiSuccess(c, groups)
		return
	}
	entries, err := pricing.ResolveCatalog(c.Request.Context(), context.TenantID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	byGroup := make(map[string]service.TenantGroupPrice, len(groups))
	for _, group := range groups {
		byGroup[group.Group] = group
	}
	prices := make([]model.TenantPriceVersion, 0, len(entries))
	for _, entry := range entries {
		group, exists := byGroup[entry.SalesGroup]
		if !exists {
			continue
		}
		cost, _, err := service.TenantSupplierUnitPrice(entry.Model, entry.SalesGroup)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		prices = append(prices, model.TenantPriceVersion{ID: group.ID, TenantID: context.TenantID, Model: entry.Model, Group: entry.SalesGroup, Version: group.Version, SalesMultiplier: group.SalesMultiplier, SupplierPriceMinor: cost})
	}
	common.ApiSuccess(c, prices)
}

func UpdateResellerPrices(c *gin.Context) {
	tenant, _, db, ok := resellerTenant(c)
	if !ok {
		resellerNotReady(c)
		return
	}
	var request resellerPricesPatchRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		resellerBadRequest(c, "invalid reseller prices")
		return
	}
	if len(request.Prices) == 0 {
		resellerBadRequest(c, "at least one group price update is required")
		return
	}
	updates := make([]service.TenantPriceUpdate, 0, len(request.Prices))
	for _, item := range request.Prices {
		if strings.TrimSpace(item.Model) != "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "TENANT_GROUP_PRICING_REQUIRED", "message": "Set prices by group instead of model."})
			return
		}
		updates = append(updates, service.TenantPriceUpdate{Group: strings.TrimSpace(item.SalesGroup), Multiplier: strings.TrimSpace(item.SaleMultiplier), ExpectedVersion: item.ExpectedVersion})
	}
	prices, err := service.NewTenantPricingService(db).SaveGroupPrices(c.Request.Context(), tenant.TenantID, updates)
	if errors.Is(err, service.ErrTenantPriceVersionConflict) {
		c.JSON(http.StatusConflict, gin.H{"success": false, "code": "TENANT_PRICE_VERSION_CONFLICT", "message": "price configuration changed; reload and retry"})
		return
	}
	if errors.Is(err, model.ErrResellerPriceBelowCost) {
		resellerBadRequest(c, "sale multiplier must be at least the platform multiplier")
		return
	}
	if errors.Is(err, model.ErrResellerInvalidPrice) || errors.Is(err, model.ErrResellerInvalidCatalog) {
		resellerBadRequest(c, "invalid reseller prices")
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, prices)
}

func GetResellerSupply(c *gin.Context) {
	_, _, db, ok := resellerTenant(c)
	if !ok {
		resellerNotReady(c)
		return
	}
	entries, err := service.ListTenantSupply(db)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, entries)
}

func GetResellerCustomers(c *gin.Context) {
	context, _, db, ok := resellerTenant(c)
	if !ok {
		resellerNotReady(c)
		return
	}
	page := resellerPageQuery(c)
	memberships := db.Model(&model.TenantMembership{}).Select("user_id").Where("tenant_id = ? AND role = ? AND status = ?", context.TenantID, model.TenantMembershipRoleCustomer, model.TenantMembershipStatusActive)
	query := db.Model(&model.User{}).Where("tenant_id = ? AND id IN (?)", context.TenantID, memberships)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	type customerView struct {
		ID                 int    `json:"id"`
		Username           string `json:"username"`
		DisplayName        string `json:"display_name"`
		Status             int    `json:"status"`
		CreatedAt          int64  `json:"created_at"`
		Quota              int64  `json:"quota"`
		UsedQuota          int64  `json:"used_quota"`
		TotalCreditedQuota int64  `json:"total_credited_quota"`
		TotalRefundedQuota int64  `json:"total_refunded_quota"`
	}
	customers := make([]customerView, 0)
	if err := query.Select("id, username, display_name, status, created_at, quota, used_quota").Order("id ASC").Offset(page.GetStartIdx()).Limit(page.PageSize).Find(&customers).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if len(customers) > 0 {
		customerIDs := make([]int, len(customers))
		byCustomerID := make(map[int]int, len(customers))
		for index, customer := range customers {
			customerIDs[index] = customer.ID
			byCustomerID[customer.ID] = index
		}
		var totals []struct {
			CustomerID         int
			TotalCreditedQuota int64
			TotalRefundedQuota int64
		}
		// Lots record successfully credited quota, including later refunds.
		// Payment cash and remaining lot quota have different meanings.
		if err := db.Model(&model.CustomerPaymentLot{}).
			Select("customer_id, SUM(credited_quota) AS total_credited_quota, SUM(refunded_quota) AS total_refunded_quota").
			Where("tenant_id = ? AND customer_id IN (?)", context.TenantID, customerIDs).
			Group("customer_id").Scan(&totals).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		for _, total := range totals {
			index := byCustomerID[total.CustomerID]
			customers[index].TotalCreditedQuota = total.TotalCreditedQuota
			customers[index].TotalRefundedQuota = total.TotalRefundedQuota
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": customers, "total": total, "page": page.Page, "page_size": page.PageSize})
}
