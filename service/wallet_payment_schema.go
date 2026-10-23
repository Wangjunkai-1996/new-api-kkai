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

type walletPaymentSchemaGate struct {
	sync.Mutex
	db          *gorm.DB
	err         error
	nextCheck   time.Time
	checkSchema func(context.Context, *gorm.DB) error
	now         func() time.Time
}

var walletRuntimeSchema = walletPaymentSchemaGate{
	checkSchema: kkaimigrate.CheckWalletPaymentSchema,
	now:         time.Now,
}

// CheckWalletPaymentRuntimeSchema shares a bounded, cancellation-independent
// check across requests and workers. Maintenance checks remain uncached.
func CheckWalletPaymentRuntimeSchema(ctx context.Context, db *gorm.DB) error {
	return walletRuntimeSchema.check(ctx, db)
}

func (gate *walletPaymentSchemaGate) check(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return kkaimigrate.ErrSchemaNotReady
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	gate.Lock()
	defer gate.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if gate.db == db && gate.now().Before(gate.nextCheck) {
		return gate.err
	}
	// A browser closing its request must not poison readiness for every user.
	// Complete canonical/schema inspection has its own bounded audit budget.
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	started := time.Now()
	err := gate.checkSchema(checkCtx, db)
	if contextErr := checkCtx.Err(); contextErr != nil {
		err = contextErr
	}
	ttl := 30 * time.Second
	if err != nil {
		ttl = 5 * time.Second
		category := "inspection_failed"
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			category = "deadline_exceeded"
		case errors.Is(err, context.Canceled):
			category = "canceled"
		case errors.Is(err, kkaimigrate.ErrSchemaNotReady):
			category = "schema_not_ready"
		}
		// Driver errors may contain credentials or SQL parameters; log fixed
		// categories and duration only, and continue to fail closed.
		common.SysError(fmt.Sprintf("wallet payment schema check failed: category=%s duration_ms=%d", category, time.Since(started).Milliseconds()))
	}
	gate.err = err
	gate.db = db
	gate.nextCheck = gate.now().Add(ttl)
	return gate.err
}
