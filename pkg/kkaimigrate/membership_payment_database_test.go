package kkaimigrate

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// Exercise the exact membership/payment DDL and real row locks without
// bootstrapping unrelated schemas. Every engine uses a disposable namespace.
func TestMembershipPaymentRealDatabaseRecovery(t *testing.T) {
	if !model.MembershipEnabled() {
		t.Skip("KKAI_MEMBERSHIP_ENABLED=true is required")
	}
	for _, dialect := range []string{DialectMySQL, DialectPostgres} {
		t.Run(dialect, func(t *testing.T) {
			db := newShopSchemaDatabaseFixture(t, dialect)
			ctx := context.Background()
			require.NoError(t, db.Migrator().CreateTable(&AppliedMigration{}))
			for _, item := range planItemsForDialect(dialect, MembershipPaymentSchemaVersion) {
				require.NoError(t, db.Create(&AppliedMigration{Version: item.Version, Name: item.Name, Checksum: storedMigrationChecksum(item)}).Error)
			}
			require.NoError(t, executeMigrationSchema(db, dialect, migrationSet()[MembershipSchemaVersion-1]))
			require.NoError(t, executeMigrationSchema(db, dialect, migrationSet()[MembershipPaymentSchemaVersion-1]))
			require.NoError(t, CheckMembershipPaymentRuntimeSchema(ctx, db))
			if dialector, ok := db.Dialector.(*mysql.Dialector); ok {
				dialector.DefaultStringSize = 191
			}
			require.NoError(t, db.AutoMigrate(&model.User{}))
			previousDB, previousRedis := model.DB, common.RedisEnabled
			previousGroups := ratio_setting.GroupRatio2JSONString()
			model.DB, common.RedisEnabled = db, false
			model.RegisterMembershipPaymentSchemaChecker(func(candidate *gorm.DB) error { return CheckMembershipPaymentRuntimeSchema(ctx, candidate) })
			t.Cleanup(func() {
				model.DB, common.RedisEnabled = previousDB, previousRedis
				model.RegisterMembershipPaymentSchemaChecker(nil)
				require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroups))
			})
			require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"gpt":0.2}`))
			user := model.User{Id: 1, Username: "membership-payment-recovery", Group: "gpt", Status: common.UserStatusEnabled, Quota: 321}
			require.NoError(t, db.Create(&user).Error)
			catalog, err := model.PublishMembershipCatalog(model.MembershipCatalog{SalesEnabled: true,
				Groups: []model.MembershipGroup{{Key: "gpt", DisplayName: "GPT", Enabled: true, Published: true}},
				Tiers:  []model.MembershipTier{{ID: "gold", Name: "Gold", Rank: 1, Currency: "CNY", PriceMinor: 1000, Quota: 100, Enabled: true, Discounts: map[string]float64{"gpt": 0.95}}},
			}, 0, user.Id)
			require.NoError(t, err)
			order, err := model.CreateMembershipOrder(model.MembershipOrderInput{UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID,
				Provider: model.PaymentProviderEpay, PaymentMethod: "alipay", TradeNo: "MEMBER-real-db", ExpiresAt: common.GetTimestamp() + 600,
				MerchantID: "original-merchant", PaymentSnapshot: "test-encrypted-snapshot",
			})
			require.NoError(t, err)
			require.NoError(t, model.WakeMembershipPaymentCheck(ctx, order.ID, "callback"))
			claim, err := model.ClaimMembershipPaymentCheck(ctx, order.ID, common.GetTimestamp())
			require.NoError(t, err)
			require.NoError(t, model.WakeMembershipPaymentCheck(ctx, order.ID, "self"))
			require.NoError(t, model.FinishMembershipPaymentCheck(ctx, order.ID, claim.ClaimToken, model.MembershipPaymentCheckResult{Status: model.MembershipPaymentPending, NextCheckAt: common.GetTimestamp() + 3600}))
			check, err := model.GetMembershipPaymentCheck(ctx, order.ID)
			require.NoError(t, err)
			assert.Equal(t, check.LastCheckedAt+30, check.NextCheckAt, "in-flight wake survives a long unpaid backoff")
			require.NoError(t, db.Model(&model.MembershipPaymentCheck{}).Where("order_id = ?", order.ID).Update("next_check_at", common.GetTimestamp()).Error)
			claim, err = model.ClaimMembershipPaymentCheck(ctx, order.ID, common.GetTimestamp())
			require.NoError(t, err)
			evidence, err := model.PersistVerifiedMembershipPayment(ctx, model.MembershipVerifiedPaymentInput{OrderID: order.ID, ClaimToken: claim.ClaimToken,
				MerchantID: "original-merchant", ReceiptID: "real-db-provider-receipt", AmountMinor: order.AmountMinor, Currency: order.Currency, ProviderPaidAt: order.CreatedAt,
			})
			require.NoError(t, err)
			failure := errors.New("isolated account persistence failure")
			const callback = "membership-payment-real-db-failure"
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "kkai_membership_accounts" {
					tx.AddError(failure)
				}
			}))
			_, err = model.RetryVerifiedMembershipPayment(ctx, evidence.ID)
			require.ErrorIs(t, err, failure)
			require.NoError(t, db.Callback().Create().Remove(callback))
			require.NoError(t, db.First(evidence, evidence.ID).Error)
			assert.Equal(t, model.MembershipPaymentPending, evidence.Status)
			var receipts int64
			require.NoError(t, db.Model(&model.MembershipPaymentReceipt{}).Count(&receipts).Error)
			assert.Zero(t, receipts, "failed entitlement transaction leaves durable evidence but no processed receipt")
			start, done := make(chan struct{}), make(chan error, 2)
			for range 2 {
				go func() { <-start; _, err := model.RetryVerifiedMembershipPayment(ctx, evidence.ID); done <- err }()
			}
			close(start)
			require.NoError(t, <-done)
			require.NoError(t, <-done)
			var accounts int64
			require.NoError(t, db.Model(&model.MembershipAccount{}).Count(&accounts).Error)
			assert.EqualValues(t, 1, accounts)
			require.NoError(t, db.Model(&model.MembershipPaymentReceipt{}).Count(&receipts).Error)
			assert.EqualValues(t, 1, receipts)
			require.NoError(t, db.First(evidence, evidence.ID).Error)
			assert.Equal(t, model.MembershipPaymentComplete, evidence.Status)
			require.NoError(t, db.First(order, order.ID).Error)
			assert.Equal(t, model.MembershipOrderSuccess, order.Status)
			var account model.MembershipAccount
			require.NoError(t, db.First(&account, order.AccountID).Error)
			assert.EqualValues(t, 100, account.AmountTotal)
			assert.EqualValues(t, 1, account.Revision)
			original := account
			_, err = model.RetryVerifiedMembershipPayment(ctx, evidence.ID)
			require.NoError(t, err)
			require.NoError(t, db.First(&account, order.AccountID).Error)
			assert.Equal(t, original, account, "replay cannot add quota or renew expiry")
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.EqualValues(t, 321, user.Quota)
		})
	}
}
