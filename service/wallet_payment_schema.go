package service

import (
	"context"
	"sync"
	"time"

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

// CheckWalletPaymentRuntimeSchema shares the invoice gate's bounded recheck
// policy across requests and workers. Maintenance checks remain uncached.
func CheckWalletPaymentRuntimeSchema(ctx context.Context, db *gorm.DB) error {
	return walletRuntimeSchema.check(ctx, db)
}

func (gate *walletPaymentSchemaGate) check(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return kkaimigrate.ErrSchemaNotReady
	}
	gate.Lock()
	defer gate.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if gate.db == db && gate.now().Before(gate.nextCheck) {
		return gate.err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	gate.err = gate.checkSchema(checkCtx, db)
	gate.db = db
	gate.nextCheck = gate.now().Add(30 * time.Second)
	return gate.err
}
