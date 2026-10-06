package model

import (
	"slices"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupSubscriptionGroupTestDB(t *testing.T) {
	t.Helper()
	db := setupTopUpFinalizeTestDB(t)
	require.NoError(t, db.AutoMigrate(&SubscriptionPlan{}, &SubscriptionOrder{}, &UserSubscription{}, &Log{}))
	previousLogDB := LOG_DB
	LOG_DB = db
	t.Cleanup(func() { LOG_DB = previousLogDB })
}

func TestSubscriptionUpgradeRefreshesAuthoritativeGroup(t *testing.T) {
	for _, operation := range []string{"payment", "admin", "balance"} {
		t.Run(operation, func(t *testing.T) {
			setupSubscriptionGroupTestDB(t)
			useUserCacheMiniRedis(t)
			user := createReserveTestUser(t, 3_000_000_000)
			_, err := GetUserCache(user.Id)
			require.NoError(t, err)
			plan := SubscriptionPlan{
				Title: "Group transition", Enabled: true, PriceAmount: 1,
				DurationUnit: SubscriptionDurationMonth, DurationValue: 1,
				UpgradeGroup: "vip", TotalAmount: 1000,
			}
			require.NoError(t, DB.Create(&plan).Error)
			// Other test databases reuse plan IDs; discard their process cache.
			InvalidateSubscriptionPlanCache(plan.Id)
			t.Cleanup(func() { InvalidateSubscriptionPlanCache(plan.Id) })

			// Move the user again after the subscription commits but before its
			// cache write. A stale plan snapshot must not undo this newer state.
			moved := false
			callback := "test:subscription_group_moved"
			require.NoError(t, DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
				if moved || tx.Statement.Table != "users" || !slices.Contains(tx.Statement.Selects, "auth_version") {
					return
				}
				moved = true
				require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("group", "manually-moved").Error)
			}))
			t.Cleanup(func() { require.NoError(t, DB.Callback().Query().Remove(callback)) })

			switch operation {
			case "payment":
				order := SubscriptionOrder{
					UserId: user.Id, PlanId: plan.Id, TradeNo: "group-refresh-" + common.GetRandomString(8),
					PaymentProvider: PaymentProviderEpay, PaymentMethod: "alipay", Status: common.TopUpStatusPending, Money: 1,
				}
				require.NoError(t, DB.Create(&order).Error)
				require.NoError(t, CompleteSubscriptionOrder(order.TradeNo, "", PaymentProviderEpay, "alipay"))
			case "admin":
				message, err := AdminBindSubscription(user.Id, plan.Id, "")
				require.NoError(t, err)
				assert.Contains(t, message, "vip")
			case "balance":
				require.NoError(t, PurchaseSubscriptionWithBalance(user.Id, plan.Id))
			}
			require.True(t, moved)
			cache, err := GetUserCache(user.Id)
			require.NoError(t, err)
			assert.Equal(t, "manually-moved", cache.Group)
			var stored User
			require.NoError(t, DB.First(&stored, user.Id).Error)
			assert.Equal(t, stored.Group, cache.Group)
			assert.Equal(t, stored.Quota, cache.Quota)
			assert.Greater(t, stored.Quota, int64(2_147_483_647))
			assert.Equal(t, user.AuthVersion, stored.AuthVersion)
			var subscription UserSubscription
			require.NoError(t, DB.Where("user_id = ?", user.Id).First(&subscription).Error)
			assert.Equal(t, "default", subscription.PrevUserGroup)
			assert.Equal(t, "vip", subscription.UpgradeGroup)
		})
	}
}

func TestAdminBindSubscriptionAlreadyInGroupDoesNotReportUpgrade(t *testing.T) {
	setupSubscriptionGroupTestDB(t)
	user := createReserveTestUser(t, 100)
	plan := SubscriptionPlan{
		Title: "Same group", Enabled: true,
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, UpgradeGroup: user.Group,
	}
	require.NoError(t, DB.Create(&plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	t.Cleanup(func() { InvalidateSubscriptionPlanCache(plan.Id) })
	message, err := AdminBindSubscription(user.Id, plan.Id, "")
	require.NoError(t, err)
	assert.Empty(t, message)
	var subscription UserSubscription
	require.NoError(t, DB.Where("user_id = ?", user.Id).First(&subscription).Error)
	assert.Empty(t, subscription.PrevUserGroup)
}
