package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMembershipTest(t *testing.T) (User, *MembershipCatalog) {
	t.Helper()
	db := setupTopUpFinalizeTestDB(t)
	require.NoError(t, db.AutoMigrate(append(MembershipModels(), &Token{})...))
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	oldEnabled := membershipRuntimeEnabled.Load()
	membershipRuntimeEnabled.Store(true)
	oldGroups := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"gpt":0.2,"new":0.5}`))
	t.Cleanup(func() {
		membershipRuntimeEnabled.Store(oldEnabled)
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
	})
	user := createReserveTestUser(t, 500)
	catalog, err := PublishMembershipCatalog(MembershipCatalog{SalesEnabled: true, Tiers: []MembershipTier{
		{ID: "gold", Name: "黄金", Rank: 1, Currency: "CNY", PriceMinor: 1000, Quota: 100, Enabled: true, Discounts: map[string]float64{"gpt": 0.95, "new": 1}},
		{ID: "diamond", Name: "钻石", Rank: 2, Currency: "CNY", PriceMinor: 2000, Quota: 200, Enabled: true, Discounts: map[string]float64{"gpt": 0.9, "new": 1}},
		{ID: "king", Name: "王者", Rank: 3, Currency: "CNY", PriceMinor: 3000, Quota: 300, Enabled: true, Discounts: map[string]float64{"gpt": 0.8, "new": 1}},
	}, Groups: []MembershipGroup{{Key: "gpt", DisplayName: "GPT", Enabled: true, Published: true}, {Key: "new", DisplayName: "New", Enabled: true}}}, 0, 1)
	require.NoError(t, err)
	return user, catalog
}

func TestValidateMembershipCatalogAcceptsFiveThousandUnitTier(t *testing.T) {
	_, catalog := setupMembershipTest(t)
	catalog.Tiers[2].PriceMinor = 500000
	catalog.Tiers[2].Quota = 2_500_000_000
	require.NoError(t, ValidateMembershipCatalog(catalog))
}

func buyMembershipTest(t *testing.T, userID int, catalog *MembershipCatalog, tierID, trade string) (*MembershipOrder, *MembershipAccount) {
	t.Helper()
	self, err := GetMembershipSelf(userID)
	require.NoError(t, err)
	input := MembershipOrderInput{UserID: userID, TierID: tierID, ReleaseID: catalog.ReleaseID, Provider: PaymentProviderEpay, PaymentMethod: "alipay", TradeNo: trade, ExpiresAt: common.GetTimestamp() + 600}
	if self.Account != nil && self.Account.ActiveAt(common.GetTimestamp()) {
		input.ExpectedAccountID, input.ExpectedRevision = self.Account.ID, self.Account.Revision
	}
	order, err := CreateMembershipOrder(input)
	require.NoError(t, err)
	order, err = CompleteMembershipPayment(MembershipPaymentInput{TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: "receipt-" + trade, AmountMinor: order.AmountMinor, Currency: order.Currency, OneTime: true, Paid: true})
	require.NoError(t, err)
	require.Equal(t, MembershipOrderSuccess, order.Status)
	self, err = GetMembershipSelf(userID)
	require.NoError(t, err)
	require.NotNil(t, self.Account)
	return order, self.Account
}

func membershipReservation(t *testing.T, userID int, key string, sub, wallet int64) MembershipReservationInput {
	t.Helper()
	quote, err := GetMembershipQuote(userID, "gpt", 0.2, 0.2)
	require.NoError(t, err)
	return MembershipReservationInput{RequestID: key, UserID: userID, GroupKey: "gpt", SubscriptionQuota: sub, WalletQuota: wallet, ExpectedAccountID: quote.AccountID, ExpectedRevision: quote.Revision, ExpectedReleaseID: quote.ReleaseID, SubscriptionSnapshot: `{"ratio":0.19}`, WalletSnapshot: `{"ratio":0.2}`, SkipToken: true}
}

func TestMembershipSnapshotUpgradeAndOriginalReservation(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	order, account := buyMembershipTest(t, user.Id, catalog, "gold", "gold-1")
	input := membershipReservation(t, user.Id, "call-1", 80, 90)
	reserved, err := ReserveMembershipBilling(input)
	require.NoError(t, err)
	assert.Equal(t, MembershipSource, reserved.Source)
	assert.EqualValues(t, 20, reserved.RemainingQuota)
	self, err := GetMembershipSelf(user.Id)
	require.NoError(t, err)
	assert.Equal(t, MembershipActive, self.Account.Status)
	assert.Zero(t, self.Account.AmountUsed)
	_, upgraded := buyMembershipTest(t, user.Id, catalog, "diamond", "diamond-1")
	assert.Equal(t, account.ID, upgraded.ID)
	assert.EqualValues(t, 300, upgraded.AmountTotal)
	assert.EqualValues(t, 80, upgraded.AmountReserved)
	settled, err := SettleMembershipBilling("call-1", 50)
	require.NoError(t, err)
	assert.EqualValues(t, 50, settled.ChargedQuota)
	assert.EqualValues(t, 250, settled.RemainingQuota)
	assert.Equal(t, reserved.Snapshot, settled.Snapshot)
	assert.Equal(t, reserved.AccountRevision, settled.AccountRevision)
	duplicate, err := SettleMembershipBilling("call-1", 50)
	require.NoError(t, err)
	assert.False(t, duplicate.Applied)
	_, err = SettleMembershipBilling("call-1", 49)
	assert.ErrorIs(t, err, ErrMembershipConflict)
	// A duplicate callback cannot restore the old level or issue quota again.
	_, err = CompleteMembershipPayment(MembershipPaymentInput{TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: order.ReceiptID, AmountMinor: order.AmountMinor, Currency: order.Currency, OneTime: true, Paid: true})
	require.NoError(t, err)
	self, err = GetMembershipSelf(user.Id)
	require.NoError(t, err)
	assert.Equal(t, "diamond", self.Account.TierID)
	assert.EqualValues(t, 300, self.Account.AmountTotal)
}

func TestMembershipWalletFallbackTokenAtomicityAndExcess(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	_, account := buyMembershipTest(t, user.Id, catalog, "gold", "gold-2")
	token := Token{UserId: user.Id, Key: "member-token", Status: common.TokenStatusEnabled, RemainQuota: 100, ExpiredTime: -1}
	require.NoError(t, DB.Create(&token).Error)
	input := membershipReservation(t, user.Id, "too-big", 110, 120)
	input.SkipToken = false
	input.TokenID = token.Id
	input.TokenKey = token.Key
	_, err := ReserveMembershipBilling(input)
	assert.ErrorIs(t, err, ErrTaskBillingInsufficientToken)
	var unchanged User
	require.NoError(t, DB.First(&unchanged, user.Id).Error)
	assert.EqualValues(t, 500, unchanged.Quota)
	var count int64
	require.NoError(t, DB.Model(&MembershipBillingRecord{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, DB.Model(&token).Update("remain_quota", 200).Error)
	reserved, err := ReserveMembershipBilling(input)
	require.NoError(t, err)
	assert.Equal(t, MembershipWalletSource, reserved.Source)
	assert.EqualValues(t, 120, reserved.ReservedQuota)
	settled, err := SettleMembershipBilling("too-big", 150)
	require.NoError(t, err)
	assert.EqualValues(t, 120, settled.ChargedQuota)
	assert.EqualValues(t, 30, settled.ExcessQuota)
	require.NoError(t, DB.First(&unchanged, user.Id).Error)
	assert.EqualValues(t, 380, unchanged.Quota)
	require.NoError(t, DB.First(account, account.ID).Error)
	assert.EqualValues(t, 100, account.Available())
	require.NoError(t, DB.First(&token, token.Id).Error)
	assert.Equal(t, 80, token.RemainQuota)
	assert.Equal(t, 120, token.UsedQuota)
}

func TestMembershipPreauthorizationIsNotExhaustionAndOldRefundIsIsolated(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	_, old := buyMembershipTest(t, user.Id, catalog, "gold", "gold-3")
	_, err := ReserveMembershipBilling(membershipReservation(t, user.Id, "old-call", 100, 110))
	require.NoError(t, err)
	self, err := GetMembershipSelf(user.Id)
	require.NoError(t, err)
	assert.Equal(t, MembershipActive, self.Account.Status)
	require.NoError(t, DB.Model(old).Update("expires_at", common.GetTimestamp()-1).Error)
	_, current := buyMembershipTest(t, user.Id, catalog, "gold", "gold-4")
	require.NotEqual(t, old.ID, current.ID)
	released, err := ReleaseMembershipBilling("old-call")
	require.NoError(t, err)
	assert.Equal(t, old.ID, released.AccountID)
	_, err = ReleaseMembershipBilling("old-call")
	require.NoError(t, err)
	require.NoError(t, DB.First(old, old.ID).Error)
	assert.Equal(t, MembershipExpired, old.Status)
	assert.Zero(t, old.AmountReserved)
	require.NoError(t, DB.First(current, current.ID).Error)
	assert.EqualValues(t, 100, current.Available())
}

func TestMembershipGroupDiscountsFollowLatestReleaseAndOrderTermsStayImmutable(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	order, err := CreateMembershipOrder(MembershipOrderInput{UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: PaymentProviderEpay, TradeNo: "pending-first", ExpiresAt: common.GetTimestamp() + 600})
	require.NoError(t, err)
	catalog.Tiers[0].PriceMinor = 9999
	catalog.Tiers[0].Quota = 999
	catalog.Tiers[0].Enabled = false
	catalog.Groups[1].Published = true
	for i := range catalog.Tiers {
		catalog.Tiers[i].Discounts["new"] = 0.9 - float64(i)*0.1
	}
	second, err := PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
	require.NoError(t, err)
	paid, err := CompleteMembershipPayment(MembershipPaymentInput{TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: "pending-receipt", AmountMinor: 1000, Currency: "CNY", OneTime: true, Paid: true})
	require.NoError(t, err)
	require.Equal(t, MembershipOrderSuccess, paid.Status)
	self, err := GetMembershipSelf(user.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 100, self.Account.AmountTotal)
	quote, err := GetMembershipQuote(user.Id, "new", 0.5, 0.5)
	require.NoError(t, err)
	assert.InDelta(t, 0.45, quote.MembershipRatio, 1e-9)
	for i := range second.Tiers {
		second.Tiers[i].Discounts["new"] = 0.6 - float64(i)*0.1
	}
	_, err = PublishMembershipCatalog(*second, second.ReleaseID, 1)
	require.NoError(t, err)
	quote, err = GetMembershipQuote(user.Id, "new", 0.5, 0.5)
	require.NoError(t, err)
	assert.InDelta(t, 0.6, quote.Discount, 1e-9)
	assert.InDelta(t, 0.3, quote.MembershipRatio, 1e-9)
	quote, err = GetMembershipQuote(user.Id, "gpt", 0.2, 0.15)
	require.NoError(t, err)
	assert.Equal(t, 0.15, quote.MembershipRatio)
}

func TestMembershipPaidExceptionsCommitAndCanBeResolvedOnce(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	order, err := CreateMembershipOrder(MembershipOrderInput{UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: PaymentProviderEpay, TradeNo: "closed-paid", ExpiresAt: common.GetTimestamp() + 600})
	require.NoError(t, err)
	_, err = CloseMembershipOrder(order.TradeNo, user.Id)
	require.NoError(t, err)
	paid, err := CompleteMembershipPayment(MembershipPaymentInput{TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: "late-receipt", AmountMinor: order.AmountMinor, Currency: order.Currency, OneTime: true, Paid: true})
	require.NoError(t, err)
	require.Equal(t, MembershipOrderException, paid.Status)
	assert.NotZero(t, paid.PaidAt)
	self, err := GetMembershipSelf(user.Id)
	require.NoError(t, err)
	assert.Nil(t, self.Account)
	resolved, err := ResolveMembershipOrder(order.TradeNo, 1, "resolve-1", "grant", "Verified late receipt")
	require.NoError(t, err)
	assert.Equal(t, MembershipOrderSuccess, resolved.Status)
	_, err = ResolveMembershipOrder(order.TradeNo, 1, "resolve-1", "grant", "Verified late receipt")
	require.NoError(t, err)
	_, err = ResolveMembershipOrder(order.TradeNo, 1, "resolve-1", "dismiss", "Verified late receipt")
	assert.ErrorIs(t, err, ErrMembershipConflict)
	var accounts int64
	require.NoError(t, DB.Model(&MembershipAccount{}).Count(&accounts).Error)
	assert.EqualValues(t, 1, accounts)
}

func TestMembershipCatalogConflictsAndDisabledSalesPreserveEntitlements(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	buyMembershipTest(t, user.Id, catalog, "gold", "gold-5")
	catalog.Tiers[1].Discounts["gpt"] = 0.99
	_, err := PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
	assert.ErrorIs(t, err, ErrMembershipInvalidInput)
	catalog.Tiers[1].Discounts["gpt"] = 0.9
	catalog.SalesEnabled = false
	saved, err := PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
	require.NoError(t, err)
	assert.True(t, saved.TopupDiscountsDisabled)
	quote, err := GetMembershipQuote(user.Id, "gpt", 0.2, 0.2)
	require.NoError(t, err)
	assert.True(t, quote.Active)
	_, err = PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
	assert.ErrorIs(t, err, ErrMembershipConflict)
	_, err = ReserveMembershipBilling(membershipReservation(t, user.Id, "after-sales-close", 10, 11))
	require.NoError(t, err)
	_, err = SettleMembershipBilling("after-sales-close", 10)
	require.NoError(t, err)
}

func TestMembershipBillingTransactionRollsBackWithOwningTask(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	buyMembershipTest(t, user.Id, catalog, "gold", "gold-6")
	input := membershipReservation(t, user.Id, "task-rollback", 20, 25)
	err := DB.Transaction(func(tx *gorm.DB) error {
		_, err := ReserveMembershipBillingTx(tx, input)
		require.NoError(t, err)
		return gorm.ErrInvalidTransaction
	})
	require.ErrorIs(t, err, gorm.ErrInvalidTransaction)
	self, err := GetMembershipSelf(user.Id)
	require.NoError(t, err)
	assert.Zero(t, self.Account.AmountReserved)
	var count int64
	require.NoError(t, DB.Model(&MembershipBillingRecord{}).Where("request_id = ?", input.RequestID).Count(&count).Error)
	assert.Zero(t, count)
}
