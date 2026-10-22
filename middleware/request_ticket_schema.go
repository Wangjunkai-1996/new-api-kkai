package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Readiness is fixed for this process. Applying v16 requires replacing the
// serving processes before endpoints can open; startup never performs DDL.
func RequestTicketSchemaGate(db *gorm.DB) gin.HandlerFunc {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	ready := kkaimigrate.CheckRequestTicketSchema(ctx, db) == nil
	cancel()
	return func(c *gin.Context) {
		if !ready {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"success": false, "code": "REQUEST_TICKET_SCHEMA_NOT_READY",
				"message": "request tickets are not enabled",
			})
			return
		}
		c.Next()
	}
}
