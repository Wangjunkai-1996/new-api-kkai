package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetTenantSiteConfig(c *gin.Context) {
	tenant, _ := middleware.GetTenantContext(c)
	config, err := service.ResolveTenantSiteConfig(model.DB, tenant)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "tenant site is unavailable"})
		return
	}
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, config)
}

func GetTenantSiteLogo(c *gin.Context) {
	tenant, ok := middleware.GetTenantContext(c)
	if !ok || tenant.TenantID <= 0 {
		c.Status(http.StatusNotFound)
		return
	}
	var profile model.TenantProfile
	if model.DB == nil || model.DB.Where("tenant_id = ?", tenant.TenantID).Select("logo_data, logo_mime").First(&profile).Error != nil || len(profile.LogoData) == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, profile.LogoMIME, profile.LogoData)
}
