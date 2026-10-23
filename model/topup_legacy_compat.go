package model

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// InsertBeforeWalletPaymentExpand holds the top_ups table's transaction read
// lock through the schema check and insert. Concurrent expansion must finish
// before this transaction starts or wait until the legacy order is committed.
// The caller owns canonical receipt validation to preserve model's dependency
// direction; a physical column check alone cannot authorize legacy purchases.
func (topUp *TopUp) InsertBeforeWalletPaymentExpand(ctx context.Context, checkLegacySchema func(context.Context, *gorm.DB) error) error {
	if DB == nil || checkLegacySchema == nil || topUp.WalletLifecycleVersion != 0 {
		return ErrWalletPaymentSchema
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing []struct{ ID int }
		if err := tx.Table("top_ups").Select("id").Limit(1).Find(&existing).Error; err != nil {
			return err
		}
		if err := checkLegacySchema(ctx, tx); err != nil {
			return err
		}
		var user User
		if err := tx.Select("id", "tenant_id").First(&user, topUp.UserId).Error; err != nil {
			return err
		}
		if topUp.TenantID != user.TenantID || (user.TenantID > 0 && (topUp.PaymentProvider != PaymentProviderEpay || topUp.Currency != ResellerCurrencyCNY || topUp.PaidMinor <= 0)) {
			return ErrResellerFinanceInvalidInput
		}
		return tx.Create(topUp).Error
	})
}
