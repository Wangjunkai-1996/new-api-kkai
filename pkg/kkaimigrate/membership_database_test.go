package kkaimigrate

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// Exercise the shipped DDL and real row locks in disposable MySQL/PostgreSQL
// namespaces. The same DSNs and cleanup policy as the image migration apply.
func TestMembershipRealDatabaseTransactions(t *testing.T) {
	if !model.MembershipEnabled() {
		t.Skip("KKAI_MEMBERSHIP_ENABLED=true is required")
	}
	for _, dialect := range []string{DialectMySQL, DialectPostgres} {
		t.Run(dialect, func(t *testing.T) {
			db := newImageAsyncDatabaseFixture(t, dialect)
			ctx := context.Background()
			_, err := applyThroughVersion(ctx, db, Options{}, ImageAsyncSchemaVersion, KnownCatalogVersion)
			require.NoError(t, err)
			_, err = ApplyImageAsyncExpand(ctx, db, Options{})
			require.NoError(t, err)
			var baseline []AppliedMigration
			require.NoError(t, db.Order("version").Find(&baseline).Error)
			_, err = ApplyMembershipExpand(ctx, db, Options{})
			require.NoError(t, err)
			require.NoError(t, CheckMembershipSchema(ctx, db))
			require.NoError(t, CheckImageAsyncSchema(ctx, db))
			_, err = ApplyMembershipExpand(ctx, db, Options{})
			require.NoError(t, err)
			var preserved []AppliedMigration
			require.NoError(t, db.Where("version <= ?", ImageAsyncSchemaVersion).Order("version").Find(&preserved).Error)
			assert.Equal(t, baseline, preserved)

			// Only base application models are completed by the fixture. Membership
			// tables remain exactly as the explicit production migration created them.
			if dialector, ok := db.Dialector.(*mysql.Dialector); ok {
				// This driver otherwise maps strings with only uniqueIndex tags to LONGTEXT.
				dialector.DefaultStringSize = 191
			}
			require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}))
			previousDB, previousRedis := model.DB, common.RedisEnabled
			previousGroups := ratio_setting.GroupRatio2JSONString()
			model.DB, common.RedisEnabled = db, false
			require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"gpt":0.2}`))
			t.Cleanup(func() {
				model.DB, common.RedisEnabled = previousDB, previousRedis
				require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroups))
			})
			user := model.User{Id: 1, Username: "membership-db-test", Group: "gpt", Status: common.UserStatusEnabled}
			token := model.Token{Id: 1, UserId: 1, Key: "local-test-token", Status: common.TokenStatusEnabled, RemainQuota: 1000}
			channel := model.Channel{Id: 1, Name: "local-test-channel", Type: 1, Key: "fixture", Status: 1}
			require.NoError(t, db.Create(&user).Error)
			require.NoError(t, db.Create(&token).Error)
			require.NoError(t, db.Create(&channel).Error)
			catalog, err := model.PublishMembershipCatalog(model.MembershipCatalog{
				SalesEnabled: true,
				Groups:       []model.MembershipGroup{{Key: "gpt", DisplayName: "GPT", Enabled: true, Published: true}},
				Tiers: []model.MembershipTier{
					{ID: "gold", Name: "Gold", Rank: 1, Currency: "CNY", PriceMinor: 1000, Quota: 100, Enabled: true, Discounts: map[string]float64{"gpt": 0.95}},
					{ID: "diamond", Name: "Diamond", Rank: 2, Currency: "CNY", PriceMinor: 2000, Quota: 200, Enabled: true, Discounts: map[string]float64{"gpt": 0.9}},
					{ID: "king", Name: "King", Rank: 3, Currency: "CNY", PriceMinor: 3000, Quota: 300, Enabled: true, Discounts: map[string]float64{"gpt": 0.8}},
				},
			}, 0, 1)
			require.NoError(t, err)
			order, err := model.CreateMembershipOrder(model.MembershipOrderInput{
				UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: model.PaymentProviderEpay,
				PaymentMethod: "alipay", TradeNo: "database-purchase", ExpiresAt: common.GetTimestamp() + 600,
			})
			require.NoError(t, err)
			payment := model.MembershipPaymentInput{TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: "database-receipt", AmountMinor: order.AmountMinor, Currency: order.Currency, OneTime: true, Paid: true}
			paymentErrors := make(chan error, 2)
			start := make(chan struct{})
			var workers sync.WaitGroup
			for range 2 {
				workers.Add(1)
				go func() {
					defer workers.Done()
					<-start
					_, err := model.CompleteMembershipPayment(payment)
					paymentErrors <- err
				}()
			}
			close(start)
			workers.Wait()
			close(paymentErrors)
			for err := range paymentErrors {
				require.NoError(t, err)
			}
			self, err := model.GetMembershipSelf(user.Id)
			require.NoError(t, err)
			require.NotNil(t, self.Account)
			account := self.Account
			assert.EqualValues(t, 100, account.AmountTotal, "concurrent callback delivery grants only once")
			quote, err := model.GetMembershipQuote(user.Id, "gpt", 0.2, 0.2)
			require.NoError(t, err)
			input := model.MembershipReservationInput{
				UserID: user.Id, GroupKey: "gpt", SubscriptionQuota: 60, WalletQuota: 80,
				ExpectedAccountID: quote.AccountID, ExpectedRevision: quote.Revision, ExpectedReleaseID: quote.ReleaseID,
				SubscriptionSnapshot: `{"ratio":0.19}`, WalletSnapshot: `{"ratio":0.2}`, TokenID: token.Id, TokenKey: token.Key,
			}
			staleTx := db.Begin()
			require.NoError(t, staleTx.Error)
			defer staleTx.Rollback()
			var snapshotAccount model.MembershipAccount
			// Establish MySQL's repeatable-read snapshot before another connection publishes.
			require.NoError(t, staleTx.First(&snapshotAccount, account.ID).Error)
			catalog, err = model.PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
			require.NoError(t, err)
			staleInput := input
			staleInput.RequestID = "database-stale-pricing"
			_, err = model.ReserveMembershipBillingTx(staleTx, staleInput)
			require.NoError(t, staleTx.Rollback().Error)
			require.ErrorIs(t, err, model.ErrMembershipQuoteChanged, "authorization must see a publication newer than the transaction snapshot")
			quote, err = model.GetMembershipQuote(user.Id, "gpt", 0.2, 0.2)
			require.NoError(t, err)
			input.ExpectedReleaseID = quote.ReleaseID
			type reservationResult struct {
				record *model.MembershipBillingRecord
				err    error
			}
			reservations := make(chan reservationResult, 2)
			start = make(chan struct{})
			for i := range 2 {
				workers.Add(1)
				go func() {
					defer workers.Done()
					<-start
					request := input
					request.RequestID = fmt.Sprintf("database-race-%d", i)
					record, err := model.ReserveMembershipBilling(request)
					reservations <- reservationResult{record, err}
				}()
			}
			close(start)
			workers.Wait()
			close(reservations)
			var winner *model.MembershipBillingRecord
			denied := 0
			for result := range reservations {
				if result.err != nil {
					require.ErrorIs(t, result.err, model.ErrMembershipInsufficientQuota)
					denied++
				} else {
					require.Nil(t, winner, "two reservations cannot spend the same available quota")
					winner = result.record
				}
			}
			require.NotNil(t, winner)
			assert.Equal(t, 1, denied)
			require.NoError(t, db.First(account, account.ID).Error)
			assert.EqualValues(t, 60, account.AmountReserved)
			assert.Zero(t, account.AmountUsed)
			settled, err := model.SettleMembershipRelayBilling(winner.RequestID, 90, channel.Id, true)
			require.NoError(t, err)
			assert.EqualValues(t, 60, settled.ChargedQuota)
			assert.EqualValues(t, 30, settled.ExcessQuota)
			replay, err := model.SettleMembershipRelayBilling(winner.RequestID, 90, channel.Id, true)
			require.NoError(t, err)
			assert.False(t, replay.Applied)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Zero(t, user.Quota)
			assert.EqualValues(t, 60, user.UsedQuota)
			assert.Equal(t, 1, user.RequestCount)
			require.NoError(t, db.First(&channel, channel.Id).Error)
			assert.EqualValues(t, 60, channel.UsedQuota)

			input.RequestID, input.SubscriptionQuota = "database-rollback", 40
			callback := "test:membership_ledger_create_failure"
			abort := errors.New("ledger insert unavailable")
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "kkai_membership_billing" {
					tx.AddError(abort)
				}
			}))
			_, err = model.ReserveMembershipBilling(input)
			require.ErrorIs(t, err, abort)
			require.NoError(t, db.Callback().Create().Remove(callback))
			require.NoError(t, db.First(account, account.ID).Error)
			assert.Zero(t, account.AmountReserved)
			assert.EqualValues(t, 60, account.AmountUsed)
			require.NoError(t, db.First(&token, token.Id).Error)
			assert.Equal(t, 940, token.RemainQuota)
			assert.Equal(t, 60, token.UsedQuota)

			input.RequestID = "database-old-account"
			_, err = model.ReserveMembershipBilling(input)
			require.NoError(t, err)
			require.NoError(t, db.Model(account).Update("expires_at", common.GetTimestamp()-1).Error)
			order, err = model.CreateMembershipOrder(model.MembershipOrderInput{
				UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: model.PaymentProviderEpay,
				PaymentMethod: "alipay", TradeNo: "database-repurchase", ExpiresAt: common.GetTimestamp() + 600,
			})
			require.NoError(t, err)
			payment.TradeNo, payment.ReceiptID = order.TradeNo, "database-repurchase-receipt"
			_, err = model.CompleteMembershipPayment(payment)
			require.NoError(t, err)
			_, err = model.ReleaseMembershipBilling(input.RequestID)
			require.NoError(t, err)
			self, err = model.GetMembershipSelf(user.Id)
			require.NoError(t, err)
			require.NotNil(t, self.Account)
			assert.NotEqual(t, account.ID, self.Account.ID)
			assert.EqualValues(t, 100, self.Account.Available())
			require.NoError(t, db.First(account, account.ID).Error)
			assert.Zero(t, account.AmountReserved)
			assert.EqualValues(t, 60, account.AmountUsed)

			require.NoError(t, db.Model(&user).Update("quota", 200).Error)
			input.RequestID, input.SubscriptionQuota = "database-wallet-fallback", 110
			input.ExpectedAccountID, input.ExpectedRevision = self.Account.ID, self.Account.Revision
			wallet, err := model.ReserveMembershipBilling(input)
			require.NoError(t, err)
			assert.Equal(t, model.MembershipWalletSource, wallet.Source)
			assert.EqualValues(t, 80, wallet.ReservedQuota)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.EqualValues(t, 120, user.Quota)
			_, err = model.ReleaseMembershipBilling(wallet.RequestID)
			require.NoError(t, err)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.EqualValues(t, 200, user.Quota)
			require.NoError(t, db.First(self.Account, self.Account.ID).Error)
			assert.EqualValues(t, 100, self.Account.Available())
		})
	}
}
