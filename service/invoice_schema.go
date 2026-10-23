package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"gorm.io/gorm"
)

var invoiceSchemaCache invoiceSchemaReadiness

type invoiceSchemaReadiness struct {
	sync.Mutex
	db        *gorm.DB
	ready     bool
	expiresAt time.Time
}

// InvoiceSchemaReady shares the read-only canonical/schema check between policy
// and financial routes. Expired success is never used if revalidation fails.
func InvoiceSchemaReady(ctx context.Context, db *gorm.DB) bool {
	return invoiceSchemaCache.check(ctx, db, kkaimigrate.CheckInvoiceSchema, time.Now)
}

func (state *invoiceSchemaReadiness) check(ctx context.Context, db *gorm.DB, check func(context.Context, *gorm.DB) error, now func() time.Time) bool {
	if db == nil || ctx == nil || ctx.Err() != nil {
		return false
	}
	state.Lock()
	defer state.Unlock()
	if state.db == db && now().Before(state.expiresAt) {
		return state.ready
	}
	// This is shared process state: one browser closing a request must not cancel
	// the check for every customer. The complete runtime audit needs its own budget.
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	started := time.Now()
	err := check(checkCtx, db.WithContext(checkCtx))
	contextErr := checkCtx.Err()
	ready := err == nil && contextErr == nil
	ttl := 5 * time.Second
	if ready {
		ttl = 30 * time.Second
	} else {
		category := "inspection_failed"
		switch {
		case errors.Is(contextErr, context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
			category = "deadline_exceeded"
		case errors.Is(err, context.Canceled):
			category = "canceled"
		case errors.Is(err, kkaimigrate.ErrSchemaNotReady):
			category = "schema_not_ready"
		}
		// Driver errors can contain SQL parameters or credentials. Log only fixed
		// categories and duration; even transient failures continue to fail closed.
		common.SysError(fmt.Sprintf("invoice schema check failed: category=%s duration_ms=%d", category, time.Since(started).Milliseconds()))
	}
	state.db, state.ready, state.expiresAt = db, ready, now().Add(ttl)
	return ready
}
