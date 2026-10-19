package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// TenantBusinessSchemaGate freezes enablement at router startup. An expanded
// bridge must keep reseller mutations disabled even on the platform Host.
// Finalizing DDL does not silently open business on already running processes;
// they must be replaced after final-schema and rollback acceptance.
func TenantBusinessSchemaGate(db *gorm.DB) gin.HandlerFunc {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	ready := kkaimigrate.CheckTenantOnlineSchema(ctx, db) == nil
	cancel()
	return func(c *gin.Context) {
		if !ready {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"success": false, "code": "TENANT_SCHEMA_NOT_READY",
				"message": "reseller sites are not enabled",
			})
			return
		}
		c.Next()
	}
}
