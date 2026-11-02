package model

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMembershipPaymentTest(t *testing.T) (User, *MembershipCatalog) {
	t.Helper()
	user, catalog := setupMembershipTest(t)
	enableMembershipPaymentTest(t)
	return user, catalog
}

func enableMembershipPaymentTest(t *testing.T) {
	t.Helper()
	db := DB
	require.NoError(t, db.AutoMigrate(MembershipPaymentModels()...))
	membershipPaymentSchemaChecker.RLock()
	previous := membershipPaymentSchemaChecker.check
	membershipPaymentSchemaChecker.RUnlock()
	RegisterMembershipPaymentSchemaChecker(func(candidate *gorm.DB) error {
		if candidate != db {
			return ErrMembershipPaymentSchema
		}
		return nil
	})
	t.Cleanup(func() { RegisterMembershipPaymentSchemaChecker(previous) })
}

func createMembershipPaymentTestOrder(t *testing.T, userID int, catalog *MembershipCatalog, trade string) *MembershipOrder {
	t.Helper()
	order, err := CreateMembershipOrder(MembershipOrderInput{UserID: userID, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: PaymentProviderEpay, PaymentMethod: "alipay", TradeNo: trade, ExpiresAt: common.GetTimestamp() + 600, MerchantID: "merchant", PaymentSnapshot: "encrypted-test-snapshot"})
	require.NoError(t, err)
	return order
}

func claimMembershipPaymentTest(t *testing.T, order *MembershipOrder, receipt string) MembershipVerifiedPaymentInput {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, WakeMembershipPaymentCheck(ctx, order.ID, "self"))
	check, err := ClaimMembershipPaymentCheck(ctx, order.ID, common.GetTimestamp())
	require.NoError(t, err)
	return MembershipVerifiedPaymentInput{OrderID: order.ID, ClaimToken: check.ClaimToken, MerchantID: "merchant", ReceiptID: receipt, AmountMinor: order.AmountMinor, Currency: order.Currency, ProviderPaidAt: order.CreatedAt}
}

func TestMembershipPaymentHintDoesNotCreatePaymentEvidence(t *testing.T) {
	user, catalog := setupMembershipPaymentTest(t)
	order := createMembershipPaymentTestOrder(t, user.Id, catalog, "hint-only")
	require.NoError(t, WakeMembershipPaymentCheck(context.Background(), order.ID, "callback"))
	check, err := GetMembershipPaymentCheck(context.Background(), order.ID)
	require.NoError(t, err)
	assert.Positive(t, check.CallbackReceivedAt)
	assert.Equal(t, MembershipPaymentPending, check.Status)
	assert.Equal(t, "encrypted-test-snapshot", check.EncryptedSnapshot)
	require.NoError(t, DB.First(order, order.ID).Error)
	assert.Zero(t, order.PaidAt)
	assert.Nil(t, order.ReceiptKey)
	for _, table := range []any{&MembershipPaymentEvidence{}, &MembershipPaymentReceipt{}, &MembershipAccount{}} {
		var count int64
		require.NoError(t, DB.Model(table).Count(&count).Error)
		assert.Zero(t, count)
	}
}

func TestMembershipPaymentManagedOrdersRequireMerchantEvidence(t *testing.T) {
	user, catalog := setupMembershipPaymentTest(t)
	_, err := CreateMembershipOrder(MembershipOrderInput{UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: PaymentProviderEpay, TradeNo: "missing-snapshot", ExpiresAt: common.GetTimestamp() + 600})
	require.ErrorIs(t, err, ErrMembershipPaymentEvidence)
	order := createMembershipPaymentTestOrder(t, user.Id, catalog, "query-required")
	_, err = CompleteMembershipPayment(MembershipPaymentInput{TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: "unqueried-receipt", AmountMinor: order.AmountMinor, Currency: order.Currency, OneTime: true, Paid: true})
	require.ErrorIs(t, err, ErrMembershipPaymentEvidence)
	input := claimMembershipPaymentTest(t, order, "merchant-receipt")
	input.MerchantID = "foreign-merchant"
	_, err = PersistVerifiedMembershipPayment(context.Background(), input)
	require.ErrorIs(t, err, ErrMembershipPaymentEvidence)
	var count int64
	require.NoError(t, DB.Model(&MembershipPaymentEvidence{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, DB.Model(&MembershipAccount{}).Count(&count).Error)
	assert.Zero(t, count)
	input.MerchantID = "merchant"
	input.AmountMinor--
	evidence, err := PersistVerifiedMembershipPayment(context.Background(), input)
	require.NoError(t, err)
	paid, err := RetryVerifiedMembershipPayment(context.Background(), evidence.ID)
	require.NoError(t, err)
	assert.Equal(t, MembershipOrderException, paid.Status)
	assert.Equal(t, "payment amount or currency mismatch", paid.ExceptionReason)
	require.NoError(t, DB.First(evidence, evidence.ID).Error)
	assert.Equal(t, MembershipPaymentReview, evidence.Status)
	_, err = RetryVerifiedMembershipPayment(context.Background(), evidence.ID)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&MembershipAccount{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestMembershipPaymentWakeRespectsThirtySecondQueryFloor(t *testing.T) {
	user, catalog := setupMembershipPaymentTest(t)
	order := createMembershipPaymentTestOrder(t, user.Id, catalog, "query-floor")
	ctx := context.Background()
	now := common.GetTimestamp()
	require.NoError(t, DB.Model(&MembershipPaymentCheck{}).Where("order_id = ?", order.ID).Updates(map[string]any{"last_checked_at": now, "next_check_at": now + 300}).Error)
	require.NoError(t, WakeMembershipPaymentCheck(ctx, order.ID, "callback"))
	first, err := GetMembershipPaymentCheck(ctx, order.ID)
	require.NoError(t, err)
	assert.Equal(t, now+30, first.NextCheckAt)
	assert.Positive(t, first.CallbackReceivedAt)
	require.NoError(t, WakeMembershipPaymentCheck(ctx, order.ID, "self"))
	second, err := GetMembershipPaymentCheck(ctx, order.ID)
	require.NoError(t, err)
	assert.Equal(t, first.NextCheckAt, second.NextCheckAt)
	_, err = ClaimMembershipPaymentCheck(ctx, order.ID, now)
	require.ErrorIs(t, err, ErrMembershipPaymentClaim)
}

func TestMembershipPaymentWakeCannotReviveTerminalCheck(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   error
	}{{MembershipPaymentVerified, ErrMembershipConflict}, {MembershipPaymentComplete, ErrMembershipConflict}, {MembershipPaymentReview, ErrMembershipConflict}} {
		t.Run(tc.status, func(t *testing.T) {
			user, catalog := setupMembershipPaymentTest(t)
			order := createMembershipPaymentTestOrder(t, user.Id, catalog, "terminal-wake")
			require.NoError(t, DB.Model(&MembershipPaymentCheck{}).Where("order_id = ?", order.ID).Updates(map[string]any{"status": tc.status, "next_check_at": 0}).Error)
			err := WakeMembershipPaymentCheck(context.Background(), order.ID, "self")
			require.ErrorIs(t, err, tc.want)
			check, err := GetMembershipPaymentCheck(context.Background(), order.ID)
			require.NoError(t, err)
			assert.Equal(t, tc.status, check.Status)
			assert.Zero(t, check.NextCheckAt)
		})
	}
}

func TestMembershipPaymentWakeDuringQuerySurvivesBackoff(t *testing.T) {
	for _, wake := range []bool{false, true} {
		name := "normal backoff retained"
		if wake {
			name = "same-second wake retained"
		}
		t.Run(name, func(t *testing.T) {
			user, catalog := setupMembershipPaymentTest(t)
			order := createMembershipPaymentTestOrder(t, user.Id, catalog, "wake-during-query")
			ctx := context.Background()
			input := claimMembershipPaymentTest(t, order, "unused")
			if wake {
				require.NoError(t, WakeMembershipPaymentCheck(ctx, order.ID, "callback"))
			}
			fallback := common.GetTimestamp() + 3600
			require.NoError(t, FinishMembershipPaymentCheck(ctx, order.ID, input.ClaimToken, MembershipPaymentCheckResult{Status: MembershipPaymentPending, NextCheckAt: fallback}))
			check, err := GetMembershipPaymentCheck(ctx, order.ID)
			require.NoError(t, err)
			if wake {
				assert.Equal(t, check.LastCheckedAt+30, check.NextCheckAt)
			} else {
				assert.Equal(t, fallback, check.NextCheckAt)
			}
		})
	}
}

func TestMembershipPaymentAbandonedClaimRemainsDiscoverable(t *testing.T) {
	user, catalog := setupMembershipPaymentTest(t)
	order := createMembershipPaymentTestOrder(t, user.Id, catalog, "abandoned-claim")
	claimMembershipPaymentTest(t, order, "unused")
	now := common.GetTimestamp()
	require.NoError(t, DB.Model(&MembershipPaymentCheck{}).Where("order_id = ?", order.ID).Update("claim_until", now-1).Error)
	for i := 0; i < 21; i++ {
		check := MembershipPaymentCheck{OrderID: order.ID + int64(i) + 1, TradeNo: fmt.Sprintf("backlog-%d", i), UserID: user.Id, MerchantID: "merchant", EncryptedSnapshot: "encrypted-test-snapshot", Status: MembershipPaymentPending, NextCheckAt: now - 1}
		require.NoError(t, DB.Create(&check).Error)
	}
	due, err := ListDueMembershipPaymentChecks(context.Background(), now, 20)
	require.NoError(t, err)
	require.Len(t, due, 20)
	assert.Equal(t, order.ID, due[0].OrderID)
	_, err = ClaimMembershipPaymentCheck(context.Background(), order.ID, now)
	require.NoError(t, err)
}

func TestMembershipLegacyReviewIsUnpaidAndClearsAfterVerifiedImport(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	order, err := CreateMembershipOrder(MembershipOrderInput{UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: PaymentProviderEpay, PaymentMethod: "alipay", TradeNo: "historical-unproven", ExpiresAt: common.GetTimestamp() + 600})
	require.NoError(t, err)
	enableMembershipPaymentTest(t)
	require.NoError(t, RecordMembershipLegacyReview(context.Background(), order.ID, MembershipLegacyCredentialReview))
	require.NoError(t, DB.First(order, order.ID).Error)
	assert.Equal(t, MembershipOrderPending, order.Status)
	assert.Zero(t, order.PaidAt)
	assert.Nil(t, order.ReceiptKey)
	assert.Equal(t, MembershipLegacyCredentialReview, order.ExceptionReason)
	orders, err := ListMembershipOrders(user.Id, MembershipOrderException, 20)
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, order.ID, orders[0].ID)
	other := createReserveTestUser(t, 500)
	orders, err = ListMembershipOrders(other.Id, MembershipOrderException, 20)
	require.NoError(t, err)
	assert.Empty(t, orders)
	_, err = EnsureMembershipPaymentCheck(context.Background(), order.ID, "merchant", "encrypted-test-snapshot")
	require.NoError(t, err)
	require.NoError(t, DB.First(order, order.ID).Error)
	assert.Empty(t, order.ExceptionReason)
	orders, err = ListMembershipOrders(user.Id, MembershipOrderException, 20)
	require.NoError(t, err)
	assert.Empty(t, orders)
}

func TestMembershipPaymentVerifiedOrderCannotResumeCheckout(t *testing.T) {
	user, catalog := setupMembershipPaymentTest(t)
	order := createMembershipPaymentTestOrder(t, user.Id, catalog, "already-paid-awaiting-grant")
	_, claimed, err := ClaimMembershipCheckout(order.TradeNo, user.Id)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, SaveMembershipCheckout(order.TradeNo, `{"submit_url":"https://pay.example.test/submit.php"}`))
	evidence, err := PersistVerifiedMembershipPayment(context.Background(), claimMembershipPaymentTest(t, order, "verified-before-checkout"))
	require.NoError(t, err)
	_, claimed, err = ClaimMembershipCheckout(order.TradeNo, user.Id)
	require.ErrorIs(t, err, ErrMembershipPaymentReview)
	assert.False(t, claimed)
	var unchanged MembershipPaymentEvidence
	require.NoError(t, DB.First(&unchanged, evidence.ID).Error)
	assert.Equal(t, *evidence, unchanged)
	paid, err := RetryVerifiedMembershipPayment(context.Background(), evidence.ID)
	require.NoError(t, err)
	assert.Equal(t, MembershipOrderSuccess, paid.Status)
}

func TestMembershipPaymentReviewListPreservesUserOwnership(t *testing.T) {
	user, catalog := setupMembershipPaymentTest(t)
	own := createMembershipPaymentTestOrder(t, user.Id, catalog, "own-query-review")
	other := createReserveTestUser(t, 500)
	foreign := createMembershipPaymentTestOrder(t, other.Id, catalog, "foreign-query-review")
	require.NoError(t, DB.Model(&MembershipPaymentCheck{}).Where("order_id IN ?", []int64{own.ID, foreign.ID}).Updates(map[string]any{"status": MembershipPaymentReview, "last_error": "PAYMENT_SNAPSHOT_UNAVAILABLE"}).Error)
	orders, err := ListMembershipOrders(user.Id, MembershipOrderException, 20)
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, own.ID, orders[0].ID)
	assert.Equal(t, MembershipOrderPending, orders[0].Status)
	assert.Zero(t, orders[0].PaidAt, "a diagnostic review must not imply payment")
	admin, err := ListMembershipOrders(0, MembershipOrderException, 20)
	require.NoError(t, err)
	require.Len(t, admin, 2)
	assert.ElementsMatch(t, []int64{own.ID, foreign.ID}, []int64{admin[0].ID, admin[1].ID})
	unrelated := createReserveTestUser(t, 500)
	orders, err = ListMembershipOrders(unrelated.Id, MembershipOrderException, 20)
	require.NoError(t, err)
	assert.Empty(t, orders)
}

func TestMembershipPaymentManualResolutionClosesReviewWithoutRegrant(t *testing.T) {
	for _, action := range []string{"grant", "dismiss"} {
		t.Run(action, func(t *testing.T) {
			user, catalog := setupMembershipPaymentTest(t)
			order := createMembershipPaymentTestOrder(t, user.Id, catalog, "review-resolve")
			now := common.GetTimestamp()
			require.NoError(t, DB.Model(order).Updates(map[string]any{"created_at": now - 120, "expires_at": now - 60}).Error)
			require.NoError(t, DB.First(order, order.ID).Error)
			input := claimMembershipPaymentTest(t, order, "review-receipt")
			input.ProviderPaidAt = now - 30 // Confirmed payment after the order deadline.
			evidence, err := PersistVerifiedMembershipPayment(context.Background(), input)
			require.NoError(t, err)
			paid, err := RetryVerifiedMembershipPayment(context.Background(), evidence.ID)
			require.NoError(t, err)
			assert.Equal(t, MembershipOrderException, paid.Status)
			require.NoError(t, DB.First(evidence, evidence.ID).Error)
			assert.Equal(t, MembershipPaymentReview, evidence.Status)
			before, err := ListMembershipOrders(user.Id, MembershipOrderException, 20)
			require.NoError(t, err)
			require.Len(t, before, 1)
			_, err = ResolveMembershipOrder(order.TradeNo, user.Id, "review-operation", action, "Reviewed verified provider payment")
			require.NoError(t, err)
			after, err := ListMembershipOrders(user.Id, MembershipOrderException, 20)
			require.NoError(t, err)
			assert.Empty(t, after, "resolved payment must leave the pending review list")
			due, err := ListDueMembershipPaymentEvidence(context.Background(), common.GetTimestamp(), 20)
			require.NoError(t, err)
			assert.Empty(t, due)
			account, err := GetMembershipSelf(user.Id)
			require.NoError(t, err)
			_, err = RetryVerifiedMembershipPayment(context.Background(), evidence.ID)
			require.NoError(t, err)
			again, err := GetMembershipSelf(user.Id)
			require.NoError(t, err)
			assert.Equal(t, account.Account, again.Account, "manual terminal state cannot be replayed by the scheduler")
			if action == "grant" {
				require.NotNil(t, again.Account)
				assert.EqualValues(t, 100, again.Account.AmountTotal)
			} else {
				assert.Nil(t, again.Account)
			}
		})
	}
}

func TestMembershipPaymentOrderAndCheckAreAtomic(t *testing.T) {
	user, catalog := setupMembershipPaymentTest(t)
	failure := errors.New("injected payment check persistence failure")
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register("membership-payment-check-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "kkai_membership_payment_checks" {
			tx.AddError(failure)
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Create().Remove("membership-payment-check-failure") })
	_, err := CreateMembershipOrder(MembershipOrderInput{UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: PaymentProviderEpay, TradeNo: "atomic-check", ExpiresAt: common.GetTimestamp() + 600, MerchantID: "merchant", PaymentSnapshot: "encrypted-test-snapshot"})
	require.ErrorIs(t, err, failure)
	var orders int64
	require.NoError(t, DB.Model(&MembershipOrder{}).Count(&orders).Error)
	assert.Zero(t, orders)
	var state MembershipUserState
	err = DB.First(&state, "user_id = ?", user.Id).Error
	if err == nil {
		assert.Zero(t, state.PendingOrderID)
	} else {
		require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	}
}

func TestMembershipPaymentEvidenceSurvivesFailureAndConcurrentRetry(t *testing.T) {
	user, catalog := setupMembershipPaymentTest(t)
	order := createMembershipPaymentTestOrder(t, user.Id, catalog, "recover")
	ctx := context.Background()
	evidence, err := PersistVerifiedMembershipPayment(ctx, claimMembershipPaymentTest(t, order, "recover-receipt"))
	require.NoError(t, err)
	_, err = CreateMembershipOrder(MembershipOrderInput{UserID: user.Id, TierID: "diamond", ReleaseID: catalog.ReleaseID, Provider: PaymentProviderEpay, TradeNo: "blocked-by-paid", ExpiresAt: common.GetTimestamp() + 600, MerchantID: "merchant", PaymentSnapshot: "encrypted-test-snapshot"})
	require.ErrorIs(t, err, ErrMembershipPaymentReview)
	failure := errors.New("injected membership account failure")
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register("membership-payment-account-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "kkai_membership_accounts" {
			tx.AddError(failure)
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Create().Remove("membership-payment-account-failure") })
	_, err = RetryVerifiedMembershipPayment(ctx, evidence.ID)
	require.ErrorIs(t, err, failure)
	require.NoError(t, DB.First(evidence, evidence.ID).Error)
	assert.Equal(t, MembershipPaymentPending, evidence.Status)
	assert.Equal(t, "recover-receipt", evidence.ReceiptID)
	assert.Equal(t, "membership fulfillment failed", evidence.LastError)
	var receipts int64
	require.NoError(t, DB.Model(&MembershipPaymentReceipt{}).Count(&receipts).Error)
	assert.Zero(t, receipts)
	require.NoError(t, DB.Callback().Create().Remove("membership-payment-account-failure"))
	connection, err := DB.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(2)
	start, done := make(chan struct{}), make(chan error, 2)
	for range 2 {
		go func() { <-start; _, err := RetryVerifiedMembershipPayment(ctx, evidence.ID); done <- err }()
	}
	close(start)
	require.NoError(t, <-done)
	require.NoError(t, <-done)
	require.NoError(t, DB.First(evidence, evidence.ID).Error)
	assert.Equal(t, MembershipPaymentComplete, evidence.Status)
	self, err := GetMembershipSelf(user.Id)
	require.NoError(t, err)
	require.NotNil(t, self.Account)
	assert.EqualValues(t, 100, self.Account.AmountTotal)
	var accounts int64
	require.NoError(t, DB.Model(&MembershipAccount{}).Count(&accounts).Error)
	assert.EqualValues(t, 1, accounts)
	require.NoError(t, DB.Model(&MembershipPaymentReceipt{}).Count(&receipts).Error)
	assert.EqualValues(t, 1, receipts)
}

func TestMembershipPaymentTimeAndReviewBoundaries(t *testing.T) {
	cases := []struct {
		name        string
		status      string
		closeReason string
		paidOffset  int64
		validation  string
		manual      bool
		grant       bool
	}{
		{name: "paid before deadline discovered late", status: MembershipOrderPending, paidOffset: -90, grant: true},
		{name: "only automatic timeout closed", status: MembershipOrderClosed, closeReason: "expired", paidOffset: -90, grant: true},
		{name: "explicit cancellation", status: MembershipOrderClosed, closeReason: "cancelled", paidOffset: -90},
		{name: "unexplained historical closure", status: MembershipOrderClosed, paidOffset: -90},
		{name: "paid after deadline", status: MembershipOrderPending, paidOffset: -30},
		{name: "paid exactly at deadline", status: MembershipOrderPending, paidOffset: -60},
		{name: "unknown paid time", status: MembershipOrderPending},
		{name: "future paid time", status: MembershipOrderPending, paidOffset: 60},
		{name: "paid before order creation", status: MembershipOrderPending, paidOffset: -180},
		{name: "validation conflict retained", status: MembershipOrderPending, paidOffset: -90, validation: "payment method mismatch"},
		{name: "manual terminal state", status: MembershipOrderPending, paidOffset: -90, manual: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user, catalog := setupMembershipPaymentTest(t)
			order := createMembershipPaymentTestOrder(t, user.Id, catalog, "time-boundary")
			now := common.GetTimestamp()
			updates := map[string]any{"created_at": now - 120, "expires_at": now - 60, "status": tc.status}
			if tc.manual {
				updates["resolved_at"], updates["resolution"] = now-30, `{"action":"dismiss"}`
			}
			require.NoError(t, DB.Model(order).Updates(updates).Error)
			require.NoError(t, DB.Model(&MembershipPaymentCheck{}).Where("order_id = ?", order.ID).Update("close_reason", tc.closeReason).Error)
			if tc.closeReason == "cancelled" {
				require.NoError(t, DB.Model(&MembershipPaymentCheck{}).Where("order_id = ?", order.ID).Update("close_reason", "expired").Error)
				_, err := CloseMembershipOrder(order.TradeNo, user.Id)
				require.NoError(t, err)
				check, err := GetMembershipPaymentCheck(context.Background(), order.ID)
				require.NoError(t, err)
				assert.Equal(t, "cancelled", check.CloseReason)
			}
			require.NoError(t, DB.First(order, order.ID).Error)
			input := claimMembershipPaymentTest(t, order, "time-receipt")
			input.ProviderPaidAt = 0
			if tc.paidOffset != 0 {
				input.ProviderPaidAt = now + tc.paidOffset
			}
			input.ValidationError = tc.validation
			evidence, err := PersistVerifiedMembershipPayment(context.Background(), input)
			require.NoError(t, err)
			paid, err := RetryVerifiedMembershipPayment(context.Background(), evidence.ID)
			require.NoError(t, err)
			require.NoError(t, DB.First(evidence, evidence.ID).Error)
			assert.Equal(t, input.ProviderPaidAt, evidence.ProviderPaidAt)
			if tc.validation != "" {
				assert.Equal(t, tc.validation, evidence.ValidationError)
			}
			if tc.grant {
				assert.Equal(t, MembershipOrderSuccess, paid.Status)
				assert.Equal(t, MembershipPaymentComplete, evidence.Status)
				self, err := GetMembershipSelf(user.Id)
				require.NoError(t, err)
				require.NotNil(t, self.Account)
				assert.GreaterOrEqual(t, self.Account.ExpiresAt, now+MembershipDurationSeconds)
			} else {
				assert.Equal(t, MembershipOrderException, paid.Status)
				assert.Equal(t, MembershipPaymentReview, evidence.Status)
				_, err := RetryVerifiedMembershipPayment(context.Background(), evidence.ID)
				require.NoError(t, err)
				var accounts int64
				require.NoError(t, DB.Model(&MembershipAccount{}).Count(&accounts).Error)
				assert.Zero(t, accounts)
			}
		})
	}
}

func TestMembershipPaymentClaimFencesStaleWorkers(t *testing.T) {
	user, catalog := setupMembershipPaymentTest(t)
	order := createMembershipPaymentTestOrder(t, user.Id, catalog, "fenced")
	ctx := context.Background()
	stale := claimMembershipPaymentTest(t, order, "stale-receipt")
	require.NoError(t, DB.Model(&MembershipPaymentCheck{}).Where("order_id = ?", order.ID).Update("claim_until", common.GetTimestamp()-1).Error)
	current, err := ClaimMembershipPaymentCheck(ctx, order.ID, common.GetTimestamp())
	require.NoError(t, err)
	require.NotEqual(t, stale.ClaimToken, current.ClaimToken)
	_, err = PersistVerifiedMembershipPayment(ctx, stale)
	assert.ErrorIs(t, err, ErrMembershipPaymentClaim)
	err = FinishMembershipPaymentCheck(ctx, order.ID, stale.ClaimToken, MembershipPaymentCheckResult{Status: MembershipPaymentReview, Reason: "stale failure"})
	assert.ErrorIs(t, err, ErrMembershipPaymentClaim)
	require.NoError(t, FinishMembershipPaymentCheck(ctx, order.ID, current.ClaimToken, MembershipPaymentCheckResult{Status: MembershipPaymentPending, NextCheckAt: common.GetTimestamp() + 300}))
	check, err := GetMembershipPaymentCheck(ctx, order.ID)
	require.NoError(t, err)
	assert.Equal(t, MembershipPaymentPending, check.Status)
	assert.Empty(t, check.LastError)
	assert.Equal(t, 2, check.Attempts)
	var count int64
	require.NoError(t, DB.Model(&MembershipPaymentEvidence{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestMembershipPaymentCannotReuseReceiptForAnotherUser(t *testing.T) {
	user, catalog := setupMembershipPaymentTest(t)
	first := createMembershipPaymentTestOrder(t, user.Id, catalog, "receipt-owner")
	evidence, err := PersistVerifiedMembershipPayment(context.Background(), claimMembershipPaymentTest(t, first, "unique-receipt"))
	require.NoError(t, err)
	_, err = RetryVerifiedMembershipPayment(context.Background(), evidence.ID)
	require.NoError(t, err)
	other := createReserveTestUser(t, 500)
	second := createMembershipPaymentTestOrder(t, other.Id, catalog, "other-user")
	_, err = PersistVerifiedMembershipPayment(context.Background(), claimMembershipPaymentTest(t, second, "unique-receipt"))
	require.ErrorIs(t, err, ErrMembershipPaymentEvidence)
	self, err := GetMembershipSelf(other.Id)
	require.NoError(t, err)
	assert.Nil(t, self.Account)
	require.NoError(t, DB.First(evidence, evidence.ID).Error)
	assert.Equal(t, MembershipPaymentComplete, evidence.Status)
	assert.Equal(t, first.ID, evidence.OrderID)
}

func TestMembershipPaymentHistoricalGrantIsNotIssuedTwice(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	order, original := buyMembershipTest(t, user.Id, catalog, "gold", "manual-history")
	enableMembershipPaymentTest(t)
	// A historical verified query may finish after a prior operator fulfilled
	// the original receipt. Its retry must preserve the exact account revision.
	_, err := EnsureMembershipPaymentCheck(context.Background(), order.ID, "merchant", "encrypted-test-snapshot")
	require.NoError(t, err)
	require.NoError(t, DB.Model(&MembershipPaymentCheck{}).Where("order_id = ?", order.ID).Update("status", MembershipPaymentPending).Error)
	evidence, err := PersistVerifiedMembershipPayment(context.Background(), claimMembershipPaymentTest(t, order, order.ReceiptID))
	require.NoError(t, err)
	_, err = RetryVerifiedMembershipPayment(context.Background(), evidence.ID)
	require.NoError(t, err)
	self, err := GetMembershipSelf(user.Id)
	require.NoError(t, err)
	assert.Equal(t, original.ID, self.Account.ID)
	assert.Equal(t, original.AmountTotal, self.Account.AmountTotal)
	assert.Equal(t, original.Revision, self.Account.Revision)
	assert.Equal(t, original.ExpiresAt, self.Account.ExpiresAt)
}

func TestMembershipPaymentUpgradePreservesUsageAndRevisionGuard(t *testing.T) {
	for _, changed := range []bool{false, true} {
		name := "usage preserved"
		if changed {
			name = "revision conflict reviewed"
		}
		t.Run(name, func(t *testing.T) {
			user, catalog := setupMembershipTest(t)
			_, account := buyMembershipTest(t, user.Id, catalog, "gold", "before-upgrade")
			enableMembershipPaymentTest(t)
			_, err := ReserveMembershipBilling(membershipReservation(t, user.Id, "consume-during-query", 40, 40))
			require.NoError(t, err)
			order, err := CreateMembershipOrder(MembershipOrderInput{UserID: user.Id, TierID: "diamond", ReleaseID: catalog.ReleaseID, Provider: PaymentProviderEpay, PaymentMethod: "alipay", TradeNo: "verified-upgrade", ExpiresAt: common.GetTimestamp() + 600, ExpectedAccountID: account.ID, ExpectedRevision: account.Revision, MerchantID: "merchant", PaymentSnapshot: "encrypted-test-snapshot"})
			require.NoError(t, err)
			evidence, err := PersistVerifiedMembershipPayment(context.Background(), claimMembershipPaymentTest(t, order, "upgrade-receipt"))
			require.NoError(t, err)
			_, err = SettleMembershipBilling("consume-during-query", 30)
			require.NoError(t, err)
			_, err = ReserveMembershipBilling(membershipReservation(t, user.Id, "still-reserved", 15, 15))
			require.NoError(t, err)
			if changed {
				require.NoError(t, DB.Model(account).Update("revision", account.Revision+1).Error)
			}
			paid, err := RetryVerifiedMembershipPayment(context.Background(), evidence.ID)
			require.NoError(t, err)
			self, err := GetMembershipSelf(user.Id)
			require.NoError(t, err)
			assert.Equal(t, account.ID, self.Account.ID)
			assert.EqualValues(t, 30, self.Account.AmountUsed)
			assert.EqualValues(t, 15, self.Account.AmountReserved)
			if changed {
				assert.Equal(t, MembershipOrderException, paid.Status)
				assert.EqualValues(t, 100, self.Account.AmountTotal)
			} else {
				assert.Equal(t, MembershipOrderSuccess, paid.Status)
				assert.EqualValues(t, 300, self.Account.AmountTotal)
			}
		})
	}
}
