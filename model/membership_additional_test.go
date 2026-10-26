package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMembershipCheckoutClaimAndAdditionalReceiptAudit(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	input := MembershipOrderInput{UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: PaymentProviderEpay, TradeNo: "checkout", ExpiresAt: common.GetTimestamp() + 600}
	order, err := CreateMembershipOrder(input)
	require.NoError(t, err)
	_, claimed, err := ClaimMembershipCheckout(order.TradeNo, user.Id)
	require.NoError(t, err)
	require.True(t, claimed)
	_, _, err = ClaimMembershipCheckout(order.TradeNo, user.Id)
	assert.ErrorIs(t, err, ErrMembershipConflict)
	require.NoError(t, SaveMembershipCheckout(order.TradeNo, `{"url":"https://payments.example/checkout"}`))
	reused, claimed, err := ClaimMembershipCheckout(order.TradeNo, user.Id)
	require.NoError(t, err)
	assert.False(t, claimed)
	assert.Contains(t, reused.CheckoutData, "payments.example")
	payment := MembershipPaymentInput{TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: "first", AmountMinor: 1000, Currency: "CNY", OneTime: true, Paid: true}
	paid, err := CompleteMembershipPayment(payment)
	require.NoError(t, err)
	require.Equal(t, MembershipOrderSuccess, paid.Status)
	payment.ReceiptID = "second"
	paid, err = CompleteMembershipPayment(payment)
	require.NoError(t, err)
	require.Equal(t, MembershipOrderSuccess, paid.Status)
	require.Len(t, paid.Payments, 2)
	assert.Equal(t, MembershipOrderException, paid.Payments[1].Status)
	_, err = CompleteMembershipPayment(payment)
	require.NoError(t, err)
	anomalies, err := ListMembershipOrders(user.Id, MembershipOrderException, 10)
	require.NoError(t, err)
	require.Len(t, anomalies, 1)
	self, err := GetMembershipSelf(user.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 100, self.Account.AmountTotal)
	_, err = ResolveMembershipOrder(order.TradeNo, 1, "dismiss-second", "dismiss", "Additional payment handled separately")
	require.NoError(t, err)
	anomalies, err = ListMembershipOrders(user.Id, MembershipOrderException, 10)
	require.NoError(t, err)
	assert.Empty(t, anomalies)
	self, err = GetMembershipSelf(user.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 100, self.Account.AmountTotal)
}

func TestMembershipConcurrentCallbackAndFundingSelection(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	connection, err := DB.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(2)
	order, err := CreateMembershipOrder(MembershipOrderInput{UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: PaymentProviderEpay, TradeNo: "concurrent", ExpiresAt: common.GetTimestamp() + 600})
	require.NoError(t, err)
	payment := MembershipPaymentInput{TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: "one-receipt", AmountMinor: 1000, Currency: "CNY", OneTime: true, Paid: true}
	start := make(chan struct{})
	done := make(chan error, 2)
	for range 2 {
		go func() { <-start; _, err := CompleteMembershipPayment(payment); done <- err }()
	}
	close(start)
	require.NoError(t, <-done)
	require.NoError(t, <-done)
	var count int64
	require.NoError(t, DB.Model(&MembershipAccount{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, DB.Model(&MembershipPaymentReceipt{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	first := membershipReservation(t, user.Id, "parallel-a", 80, 90)
	second := membershipReservation(t, user.Id, "parallel-b", 80, 90)
	type reservationResult struct {
		record *MembershipBillingRecord
		err    error
	}
	results := make(chan reservationResult, 2)
	start = make(chan struct{})
	for _, input := range []MembershipReservationInput{first, second} {
		go func(input MembershipReservationInput) {
			<-start
			record, err := ReserveMembershipBilling(input)
			results <- reservationResult{record, err}
		}(input)
	}
	close(start)
	sources := map[string]int{}
	for range 2 {
		result := <-results
		require.NoError(t, result.err)
		sources[result.record.Source]++
	}
	assert.Equal(t, map[string]int{MembershipSource: 1, MembershipWalletSource: 1}, sources)
	self, err := GetMembershipSelf(user.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 80, self.Account.AmountReserved)
	var wallet User
	require.NoError(t, DB.First(&wallet, user.Id).Error)
	assert.EqualValues(t, 410, wallet.Quota)
}

func TestMembershipTokenRefundUsesOriginalLimitAndSurvivesAdministrativeReset(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	buyMembershipTest(t, user.Id, catalog, "gold", "token-admin")
	token := Token{UserId: user.Id, Key: "unlimited-original", UnlimitedQuota: true, RemainQuota: 100, Status: common.TokenStatusEnabled, ExpiredTime: -1}
	require.NoError(t, DB.Create(&token).Error)
	input := membershipReservation(t, user.Id, "unlimited-refund", 40, 50)
	input.TokenID = token.Id
	input.SkipToken = false
	input.TokenKey = token.Key
	_, err := ReserveMembershipBilling(input)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&token).Updates(map[string]any{"unlimited_quota": false, "used_quota": 0}).Error)
	released, err := ReleaseMembershipBilling(input.RequestID)
	require.NoError(t, err)
	assert.EqualValues(t, 40, released.TokenUsedRefundUnapplied)
	require.NoError(t, DB.First(&token, token.Id).Error)
	assert.Equal(t, 100, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)
	self, err := GetMembershipSelf(user.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 100, self.Account.Available())
}

func TestMembershipNoDiscountAndActualExhaustion(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	_, old := buyMembershipTest(t, user.Id, catalog, "gold", "exhaust-old")
	quote, err := GetMembershipQuote(user.Id, "new", 0.5, 0.7)
	require.NoError(t, err)
	assert.Equal(t, 1.0, quote.Discount)
	assert.Equal(t, 0.7, quote.MembershipRatio)
	input := membershipReservation(t, user.Id, "exhaust-all", 100, 110)
	_, err = ReserveMembershipBilling(input)
	require.NoError(t, err)
	_, err = SettleMembershipBilling(input.RequestID, 100)
	require.NoError(t, err)
	self, err := GetMembershipSelf(user.Id)
	require.NoError(t, err)
	assert.Equal(t, MembershipExhausted, self.Account.Status)
	_, current := buyMembershipTest(t, user.Id, catalog, "gold", "exhaust-new")
	assert.NotEqual(t, old.ID, current.ID)
	input = membershipReservation(t, user.Id, "insufficient-both", 110, 501)
	_, err = ReserveMembershipBilling(input)
	assert.ErrorIs(t, err, ErrMembershipInsufficientQuota)
	_, err = GetMembershipBillingRecord(input.RequestID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestMembershipCatalogCurrencyAndGroupTombstone(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	buyMembershipTest(t, user.Id, catalog, "gold", "group-delete")
	catalog.Tiers[0].Currency = "JPY"
	_, err := PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
	assert.ErrorIs(t, err, ErrMembershipInvalidInput)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = previousOptions })
	require.NoError(t, UpdateOption("GroupRatio", `{"new":0.5}`))
	_, err = GetMembershipQuote(user.Id, "gpt", 0.2, 0.2)
	assert.ErrorIs(t, err, ErrMembershipInvalidInput)
	var tombstone MembershipGroupRecord
	require.NoError(t, DB.First(&tombstone, "group_key = ?", "gpt").Error)
	assert.False(t, tombstone.Enabled)
	assert.Equal(t, catalog.ReleaseID, tombstone.FirstReleaseID)
	require.NoError(t, UpdateOption("GroupRatio", `{"new":0.5,"gpt":0.3}`))
	quote, err := GetMembershipQuote(user.Id, "gpt", 0.3, 0.3)
	require.NoError(t, err)
	assert.InDelta(t, 0.285, quote.MembershipRatio, 1e-9)
	require.NoError(t, DB.First(&tombstone, "group_key = ?", "gpt").Error)
	assert.True(t, tombstone.Enabled)
	assert.Equal(t, catalog.ReleaseID, tombstone.FirstReleaseID)
}

func TestMembershipLegacyContractsAndReverseCreationGuard(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	require.NoError(t, DB.AutoMigrate(&UserSubscription{}, &SubscriptionOrder{}))
	legacy := UserSubscription{UserId: user.Id, Status: "active", EndTime: common.GetTimestamp() + 600, AmountTotal: 100}
	require.NoError(t, DB.Create(&legacy).Error)
	use, err := ShouldUseMembershipBilling(user.Id)
	require.NoError(t, err)
	assert.False(t, use)
	_, err = CreateMembershipOrder(MembershipOrderInput{UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: PaymentProviderEpay, TradeNo: "legacy-conflict", ExpiresAt: common.GetTimestamp() + 600})
	assert.ErrorIs(t, err, ErrMembershipConflict)
	require.NoError(t, DB.Model(&legacy).Update("status", "expired").Error)
	buyMembershipTest(t, user.Id, catalog, "gold", "post-legacy")
	use, err = ShouldUseMembershipBilling(user.Id)
	require.NoError(t, err)
	assert.True(t, use)
	order := SubscriptionOrder{UserId: user.Id, PlanId: 1, TradeNo: "legacy-new", Status: common.TopUpStatusPending}
	assert.ErrorIs(t, order.Insert(), ErrMembershipConflict)
	err = DB.Transaction(func(tx *gorm.DB) error {
		_, err := CreateUserSubscriptionFromPlanTx(tx, user.Id, &SubscriptionPlan{Id: 1}, "admin")
		return err
	})
	assert.ErrorIs(t, err, ErrMembershipConflict)
	require.NoError(t, DB.Model(&legacy).Update("status", "active").Error)
	_, err = ShouldUseMembershipBilling(user.Id)
	assert.ErrorIs(t, err, ErrMembershipConflict)
}

func TestMembershipBillingRequiresActiveAccount(t *testing.T) {
	if !MembershipEnabled() {
		t.Skip("requires KKAI_MEMBERSHIP_ENABLED=true")
	}
	user, catalog := setupMembershipTest(t)
	use, err := ShouldUseMembershipBilling(user.Id)
	require.NoError(t, err)
	assert.False(t, use)

	_, account := buyMembershipTest(t, user.Id, catalog, "gold", "billing-selector")
	use, err = ShouldUseMembershipBilling(user.Id)
	require.NoError(t, err)
	assert.True(t, use)

	require.NoError(t, DB.Model(account).Update("expires_at", common.GetTimestamp()-1).Error)
	use, err = ShouldUseMembershipBilling(user.Id)
	require.NoError(t, err)
	assert.False(t, use)
}

func TestMembershipOwningTransactionHonorsCancellation(t *testing.T) {
	setupMembershipTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := MembershipTransaction(ctx, func(tx *gorm.DB) error { return tx.Create(&MembershipUserState{UserID: 200}).Error })
	require.ErrorIs(t, err, context.Canceled)
}

func TestMembershipLegacyPendingCheckoutAndLateLegacyPayment(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &SubscriptionOrder{}, &UserSubscription{}))
	plan := SubscriptionPlan{Title: "legacy", Enabled: true, DurationUnit: SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 50}
	require.NoError(t, DB.Create(&plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	t.Cleanup(func() { InvalidateSubscriptionPlanCache(plan.Id) })
	legacy := SubscriptionOrder{UserId: user.Id, PlanId: plan.Id, TradeNo: "legacy-pending", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
	require.NoError(t, legacy.Insert())
	_, err := CreateMembershipOrder(MembershipOrderInput{UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: PaymentProviderEpay, TradeNo: "wait-legacy", ExpiresAt: common.GetTimestamp() + 600})
	require.ErrorIs(t, err, ErrMembershipConflict)
	require.NoError(t, ExpireSubscriptionOrder(legacy.TradeNo, PaymentProviderEpay))
	buyMembershipTest(t, user.Id, catalog, "gold", "after-legacy-close")
	// Simulate a pre-cutover/administrative inconsistency. Verified money must
	// remain recorded as an exception instead of rolling the callback back.
	require.NoError(t, DB.Model(&legacy).Update("status", common.TopUpStatusPending).Error)
	require.NoError(t, CompleteSubscriptionOrder(legacy.TradeNo, `{"verified":true}`, PaymentProviderEpay, "alipay"))
	require.NoError(t, CompleteSubscriptionOrder(legacy.TradeNo, `{"verified":true}`, PaymentProviderEpay, "alipay"))
	require.NoError(t, DB.First(&legacy, legacy.Id).Error)
	assert.Equal(t, MembershipOrderException, legacy.Status)
	assert.NotZero(t, legacy.CompleteTime)
	assert.Contains(t, legacy.ProviderPayload, "membership_conflict")
	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestMembershipGrantDoesNotDismissAnAdditionalPayment(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	order, err := CreateMembershipOrder(MembershipOrderInput{UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: PaymentProviderEpay, TradeNo: "two-late-receipts", ExpiresAt: common.GetTimestamp() + 600})
	require.NoError(t, err)
	_, err = CloseMembershipOrder(order.TradeNo, user.Id)
	require.NoError(t, err)
	payment := MembershipPaymentInput{TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: "primary-late", AmountMinor: order.AmountMinor, Currency: order.Currency, OneTime: true, Paid: true}
	_, err = CompleteMembershipPayment(payment)
	require.NoError(t, err)
	payment.ReceiptID = "extra-late"
	_, err = CompleteMembershipPayment(payment)
	require.NoError(t, err)
	_, err = ResolveMembershipOrder(order.TradeNo, 1, "grant-late", "grant", "Verified primary payment")
	require.NoError(t, err)
	outstanding, err := ListMembershipOrders(user.Id, MembershipOrderException, 10)
	require.NoError(t, err)
	require.Len(t, outstanding, 1)
	require.Len(t, outstanding[0].Payments, 2)
	assert.Equal(t, "resolved", outstanding[0].Payments[0].Status)
	assert.Equal(t, MembershipOrderException, outstanding[0].Payments[1].Status)
	_, err = ResolveMembershipOrder(order.TradeNo, 1, "dismiss-extra", "dismiss", "Extra receipt handled manually")
	require.NoError(t, err)
	outstanding, err = ListMembershipOrders(user.Id, MembershipOrderException, 10)
	require.NoError(t, err)
	assert.Empty(t, outstanding)
}

func TestMembershipRankChangeCannotRepurchaseSameActiveTier(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	_, account := buyMembershipTest(t, user.Id, catalog, "gold", "original-gold")
	for i := range catalog.Tiers {
		catalog.Tiers[i].Rank += 10
	}
	updated, err := PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
	require.NoError(t, err)
	_, err = CreateMembershipOrder(MembershipOrderInput{
		UserID: user.Id, TierID: "gold", ReleaseID: updated.ReleaseID, Provider: PaymentProviderEpay,
		TradeNo: "same-gold", ExpiresAt: common.GetTimestamp() + 600,
		ExpectedAccountID: account.ID, ExpectedRevision: account.Revision,
	})
	assert.ErrorIs(t, err, ErrMembershipConflict)
	// A checkout created before this guard existed still cannot issue a second
	// quota grant when the verified payment arrives after the fix.
	terms, err := common.Marshal(MembershipOrderTerms{
		Tier: updated.Tiers[0], ReleaseID: updated.ReleaseID, DurationSeconds: MembershipDurationSeconds,
		Action: "upgrade", ExpectedAccountID: account.ID, ExpectedRevision: account.Revision,
	})
	require.NoError(t, err)
	oldCheckout := MembershipOrder{
		TradeNo: "pre-fix-same-gold", UserID: user.Id, TierID: "gold", ReleaseID: updated.ReleaseID,
		AmountMinor: updated.Tiers[0].PriceMinor, Currency: "CNY", Provider: PaymentProviderEpay,
		Status: MembershipOrderPending, ExpiresAt: common.GetTimestamp() + 600, Terms: string(terms),
	}
	require.NoError(t, DB.Create(&oldCheckout).Error)
	paid, err := CompleteMembershipPayment(MembershipPaymentInput{
		TradeNo: oldCheckout.TradeNo, Provider: oldCheckout.Provider, ReceiptID: "same-tier-payment",
		AmountMinor: oldCheckout.AmountMinor, Currency: oldCheckout.Currency, OneTime: true, Paid: true,
	})
	require.NoError(t, err)
	assert.Equal(t, MembershipOrderException, paid.Status)
	assert.NotZero(t, paid.PaidAt)
	_, err = ResolveMembershipOrder(oldCheckout.TradeNo, 1, "same-tier-grant", "grant", "Verified payment cannot bypass same-tier restriction")
	assert.ErrorIs(t, err, ErrMembershipConflict)
	var current MembershipAccount
	require.NoError(t, DB.First(&current, account.ID).Error)
	assert.Equal(t, *account, current)
}
