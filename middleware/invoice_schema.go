package middleware

import (
	"context"
	"net/http"

	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// InvoiceSchemaGate checks the canonical receipt and physical schema without
// performing DDL. It also revalidates success so schema drift fails closed.
func InvoiceSchemaGate(db *gorm.DB) gin.HandlerFunc {
	return invoiceSchemaGate(func(ctx context.Context) bool {
		return service.InvoiceSchemaReady(ctx, db)
	})
}

func invoiceSchemaGate(ready func(context.Context) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !ready(c.Request.Context()) {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"success": false, "code": "SCHEMA_NOT_READY", "message": "invoice center is not enabled",
			})
			return
		}
		c.Next()
	}
}
