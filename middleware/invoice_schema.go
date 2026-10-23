package middleware

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// InvoiceSchemaGate checks the canonical receipt and physical schema without
// performing DDL. It also revalidates success so schema drift fails closed.
func InvoiceSchemaGate(db *gorm.DB) gin.HandlerFunc {
	return invoiceSchemaGate(func(ctx context.Context) error {
		return kkaimigrate.CheckInvoiceSchema(ctx, db)
	}, time.Now)
}

func invoiceSchemaGate(check func(context.Context) error, now func() time.Time) gin.HandlerFunc {
	var mu sync.Mutex
	var ready bool
	var nextCheck time.Time
	return func(c *gin.Context) {
		mu.Lock()
		if !now().Before(nextCheck) {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
			ready = check(ctx) == nil
			cancel()
			nextCheck = now().Add(30 * time.Second)
		}
		allowed := ready
		mu.Unlock()
		if !allowed {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"success": false, "code": "SCHEMA_NOT_READY", "message": "invoice center is not enabled",
			})
			return
		}
		c.Next()
	}
}
