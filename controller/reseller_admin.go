package controller

import (
	"errors"
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
		Domains    []model.TenantDomain     `json:"domains"`
		Activation model.ResellerActivation `json:"activation"`
	}
	tenantIDs := make([]int64, 0, len(tenants))
	for _, tenant := range tenants {
		tenantIDs = append(tenantIDs, tenant.ID)
	}
	activations, err := model.GetResellerActivations(model.DB, tenantIDs)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	views := make([]tenantView, 0, len(tenants))
	byTenant := make(map[int64][]model.TenantDomain)
	for _, domain := range domains {
		byTenant[domain.TenantID] = append(byTenant[domain.TenantID], domain)
	}
	for _, tenant := range tenants {
		tenantDomains := byTenant[tenant.ID]
		if tenantDomains == nil {
			tenantDomains = []model.TenantDomain{}
		}
		views = append(views, tenantView{Tenant: tenant, Domains: tenantDomains, Activation: activations[tenant.ID]})
	}
	root, _ := model.NormalizeTenantHost(os.Getenv("TENANT_ROOT_DOMAIN"))
	common.ApiSuccess(c, gin.H{"applications": applications, "tenants": views, "root_domain": root})
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
	var activation model.ResellerActivation
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		tenant, err := model.LockTenantTx(tx, id)
		if err != nil {
			return err
		}
		if request.Status == model.TenantStatusActive {
			activations, err := model.GetResellerActivations(tx, []int64{id})
			if err != nil {
				return err
			}
			activation = activations[id]
			if !activation.Ready {
				return model.ErrResellerActivationIncomplete
			}
		}
		return tx.Model(tenant).Update("status", request.Status).Error
	})
	if err != nil {
		if errors.Is(err, model.ErrResellerActivationIncomplete) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "RESELLER_ACTIVATION_INCOMPLETE", "message": err.Error(), "activation": activation})
			return
		}
		common.ApiError(c, err)
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
	host, hostErr := model.NormalizeResellerDomain(request.Hostname, os.Getenv("TENANT_ROOT_DOMAIN"))
	reserved := false
	for _, platform := range strings.Split(os.Getenv("TENANT_PLATFORM_HOSTS"), ",") {
		canonical, _ := model.NormalizeTenantHost(platform)
		reserved = reserved || canonical == host
	}
	if hostErr != nil || reserved {
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
		if errors.Is(err, model.ErrResellerDomainAlreadyAssigned) {
			c.JSON(http.StatusConflict, gin.H{"success": false, "code": "RESELLER_DOMAIN_ALREADY_ASSIGNED", "message": err.Error()})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "domain is unavailable"})
		return
	}
	common.ApiSuccess(c, domain)
}
