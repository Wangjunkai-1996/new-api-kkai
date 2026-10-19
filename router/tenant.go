package router

import (
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// EnableTenantRouting installs the Host-to-tenant gate before route
// registration. It is an explicit application integration point so rollout
// can remain disabled until the platform host list and pilot domains are
// provisioned; TenantContext itself always fails closed when enabled.
func EnableTenantRouting(router *gin.Engine, db *gorm.DB, platformHosts ...string) {
	if router == nil {
		return
	}
	router.Use(middleware.TenantContext(db, platformHosts...), middleware.TenantFeatureGate())
}
