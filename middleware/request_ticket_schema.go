package middleware

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Reads use the refreshed snapshot; writes use a bounded ticket-only check.
// Online expansion enables tickets without restarting; no request performs DDL.
func RequestTicketSchemaGate(db *gorm.DB) gin.HandlerFunc {
	return requestTicketSchemaGate(func(ctx context.Context) error {
		return kkaimigrate.CheckRequestTicketRuntimeSchema(ctx, db)
	}, func(ctx context.Context) bool {
		return service.RequestTicketReadSchemaReady(ctx, db)
	}, time.Now)
}

func requestTicketSchemaGate(check func(context.Context) error, readReady func(context.Context) bool, now func() time.Time) gin.HandlerFunc {
	var mu sync.Mutex
	var ready bool
	var nextCheck time.Time
	return func(c *gin.Context) {
		allowed := false
		if c.Request.Method == http.MethodGet {
			allowed = readReady(c.Request.Context())
		} else if c.Request.Context().Err() == nil {
			mu.Lock()
			if !now().Before(nextCheck) {
				// One canceled browser request must not poison shared readiness.
				ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 3*time.Second)
				ready = check(ctx) == nil && ctx.Err() == nil
				cancel()
				ttl := 5 * time.Second
				if ready {
					ttl = 30 * time.Second
				}
				nextCheck = now().Add(ttl)
			}
			allowed = ready && c.Request.Context().Err() == nil
			mu.Unlock()
		}
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
