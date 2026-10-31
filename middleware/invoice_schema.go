package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Reads use the refreshed snapshot; writes revalidate the invoice schema.
func InvoiceSchemaGate(db *gorm.DB) gin.HandlerFunc {
	return invoiceSchemaGate(func(c *gin.Context) bool {
		if c.Request.Method != http.MethodGet {
			return service.InvoiceSchemaReady(c.Request.Context(), db)
		}
		return service.InvoiceReadSchemaReady(c.Request.Context(), db)
	})
}

func invoiceSchemaGate(ready func(*gin.Context) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !ready(c) {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"success": false, "code": "SCHEMA_NOT_READY", "message": "invoice center is not enabled",
			})
			return
		}
		c.Next()
	}
}
