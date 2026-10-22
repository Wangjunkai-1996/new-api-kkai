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

// A canonical online expansion can enable tickets without restarting serving
// processes. Failed checks are throttled; no request performs DDL.
func RequestTicketSchemaGate(db *gorm.DB) gin.HandlerFunc {
	return requestTicketSchemaGate(func(ctx context.Context) error {
		return kkaimigrate.CheckRequestTicketSchema(ctx, db)
	}, time.Now)
}

func requestTicketSchemaGate(check func(context.Context) error, now func() time.Time) gin.HandlerFunc {
	var mu sync.Mutex
	var ready bool
	var nextCheck time.Time
	return func(c *gin.Context) {
		mu.Lock()
		if !ready && !now().Before(nextCheck) {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
			ready = check(ctx) == nil
			cancel()
			nextCheck = now().Add(30 * time.Second)
		}
		allowed := ready
		mu.Unlock()
		if !allowed {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"success": false, "code": "REQUEST_TICKET_SCHEMA_NOT_READY",
				"message": "request tickets are not enabled",
			})
			return
		}
		c.Next()
	}
}
