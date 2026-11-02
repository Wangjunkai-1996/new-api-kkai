package service

import (
	"context"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"gorm.io/gorm"
)

var membershipPaymentSchema = struct {
	sync.Mutex
	db        *gorm.DB
	err       error
	nextCheck time.Time
}{}

func init() {
	model.RegisterMembershipPaymentSchemaChecker(func(db *gorm.DB) error {
		return CheckMembershipPaymentRuntimeSchema(context.Background(), db)
	})
}

// Readiness checks only this explicit expansion and its canonical receipts.
// Requests and workers share a bounded cache; a cancelled caller cannot cache
// cancellation as a failure for other orders.
func CheckMembershipPaymentRuntimeSchema(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return model.ErrMembershipPaymentSchema
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	membershipPaymentSchema.Lock()
	defer membershipPaymentSchema.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if membershipPaymentSchema.db == db && time.Now().Before(membershipPaymentSchema.nextCheck) {
		return membershipPaymentSchema.err
	}
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	err := kkaimigrate.CheckMembershipPaymentRuntimeSchema(checkCtx, db)
	if checkCtx.Err() != nil {
		err = checkCtx.Err()
	}
	ttl := 30 * time.Second
	if err != nil {
		ttl = 5 * time.Second
	}
	membershipPaymentSchema.db, membershipPaymentSchema.err = db, err
	membershipPaymentSchema.nextCheck = time.Now().Add(ttl)
	return err
}
