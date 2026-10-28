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
		return kkaimigrate.CheckShopSchema(ctx, db)
	})
}

var shopRuntimeSchema = walletPaymentSchemaGate{
	checkSchema: kkaimigrate.CheckShopSchema,
	now:         time.Now,
}

// The shared gate bounds inspection and keeps request cancellation from
// poisoning readiness for independent callers. It never installs schema.
func CheckShopRuntimeSchema(ctx context.Context, db *gorm.DB) error {
	return shopRuntimeSchema.check(ctx, db)
}
