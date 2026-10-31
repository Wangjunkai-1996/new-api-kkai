package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"gorm.io/gorm"
)

func init() {
	model.RegisterShopSchemaChecker(func(db *gorm.DB) error {
		ctx := db.Statement.Context
		if ctx == nil {
			ctx = context.Background()
		}
		checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		return kkaimigrate.CheckShopRuntimeSchema(checkCtx, db.WithContext(checkCtx))
	})
}

var shopRuntimeSchema = walletPaymentSchemaGate{
	checkSchema: kkaimigrate.CheckShopRuntimeSchema,
	now:         time.Now,
}

// The shared gate bounds inspection and keeps request cancellation from
// poisoning readiness for independent callers. It never installs schema.
func CheckShopRuntimeSchema(ctx context.Context, db *gorm.DB) error {
	return shopRuntimeSchema.check(ctx, db)
}
