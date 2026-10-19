package controller

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func AdminListResellers(c *gin.Context) {
	var applications []model.ResellerApplication
	if err := model.DB.Order("id DESC").Limit(200).Find(&applications).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var tenants []model.Tenant
	if err := model.DB.Order("id DESC").Find(&tenants).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var domains []model.TenantDomain
	if err := model.DB.Find(&domains).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	type tenantView struct {
		model.Tenant
		Domains []model.TenantDomain `json:"domains"`
	}
	views := make([]tenantView, 0, len(tenants))
	byTenant := make(map[int64][]model.TenantDomain)
	for _, domain := range domains {
		byTenant[domain.TenantID] = append(byTenant[domain.TenantID], domain)
	}
	for _, tenant := range tenants {
		views = append(views, tenantView{Tenant: tenant, Domains: byTenant[tenant.ID]})
	}
	common.ApiSuccess(c, gin.H{"applications": applications, "tenants": views})
}

func AdminReviewResellerApplication(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	var request struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err != nil || common.DecodeJson(c.Request.Body, &request) != nil || len(request.Reason) > 2000 || (request.Status != model.ResellerApplicationStatusApproved && request.Status != model.ResellerApplicationStatusRejected) {
		resellerBadRequest(c, "invalid application review")
		return
	}
	var application *model.ResellerApplication
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		application, err = model.ReviewResellerApplicationTx(tx, id, c.GetInt("id"), request.Status == model.ResellerApplicationStatusApproved, request.Reason)
		return err
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, application)
}

func AdminUpdateResellerStatus(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	var request struct {
		Status string `json:"status"`
	}
	if err != nil || common.DecodeJson(c.Request.Body, &request) != nil || (request.Status != model.TenantStatusActive && request.Status != model.TenantStatusDisabled) {
		resellerBadRequest(c, "invalid tenant status")
		return
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		tenant, err := model.LockTenantTx(tx, id)
		if err != nil {
			return err
		}
		if request.Status == model.TenantStatusActive {
			var domains int64
			if err := tx.Model(&model.TenantDomain{}).Where("tenant_id = ? AND status = ?", id, model.TenantDomainStatusActive).Count(&domains).Error; err != nil {
				return err
			}
			var entries []model.TenantCatalogEntry
			if err := tx.Where("tenant_id = ? AND status = ?", id, model.TenantCatalogStatusEnabled).Find(&entries).Error; err != nil {
				return err
			}
			if domains == 0 || len(entries) == 0 {
				return model.ErrResellerInvalidCatalog
			}
			for _, entry := range entries {
				var price model.TenantPriceVersion
				if err := tx.Where(map[string]any{"tenant_id": id, "model": entry.Model, "group": entry.SalesGroup}).Order("version DESC").First(&price).Error; err != nil {
					return err
				}
			}
		}
		return tx.Model(tenant).Update("status", request.Status).Error
	})
	if err != nil {
		resellerBadRequest(c, "active tenants require a provisioned domain, catalog and prices")
		return
	}
	common.ApiSuccess(c, gin.H{"status": request.Status})
}

func AdminProvisionResellerDomain(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	var request struct {
		Hostname string `json:"hostname"`
	}
	if err != nil || common.DecodeJson(c.Request.Body, &request) != nil {
		resellerBadRequest(c, "invalid domain")
		return
	}
	root, rootErr := model.NormalizeTenantHost(os.Getenv("TENANT_ROOT_DOMAIN"))
	host, hostErr := model.NormalizeTenantHost(request.Hostname)
	prefix := strings.TrimSuffix(host, "."+root)
	reserved := false
	for _, name := range []string{"www", "api", "api-origin", "dt-api", "cpa", "monitor", "admin", "mail", "status"} {
		reserved = reserved || prefix == name
	}
	for _, platform := range strings.Split(os.Getenv("TENANT_PLATFORM_HOSTS"), ",") {
		canonical, _ := model.NormalizeTenantHost(platform)
		reserved = reserved || canonical == host
	}
	if rootErr != nil || hostErr != nil || !strings.HasSuffix(host, "."+root) || prefix == "" || strings.Contains(prefix, ".") || reserved {
		resellerBadRequest(c, "a non-reserved subdomain under TENANT_ROOT_DOMAIN is required")
		return
	}
	var domain *model.TenantDomain
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		domain, err = model.ProvisionTenantDomainTx(tx, id, host)
		return err
	})
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "domain is unavailable"})
		return
	}
	common.ApiSuccess(c, domain)
}
