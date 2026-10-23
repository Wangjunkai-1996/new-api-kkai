package model

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupWalletPaymentDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupTopUpFinalizeTestDB(t)
	require.NoError(t, db.AutoMigrate(&WalletTopUpFlow{}, &WalletPaymentReceipt{}, &WalletPaymentEvent{}, &WalletTopUpRun{}))
	require.NoError(t, db.Create(&User{Id: 41, Username: "wallet-user", AffCode: "wallet-user", Quota: 10, Group: "default"}).Error)
	return db
}
func walletTestOrder(trade string) *TopUp {
	return &TopUp{TradeNo: trade, PaidMinor: 100, Currency: "CNY", CurrencyExponent: 2, CreditedQuota: 500, CreditDisplayAmount: "1", CreditDisplayUnit: "USD", PricingMode: "fixed", PaymentProvider: PaymentProviderEpay, PaymentMethod: "alipay", ProviderAccountRef: "merchant-one", ProviderEnvironment: "test", CheckoutPayload: "private-encrypted-provider-snapshot"}
}
func TestWalletFlowRetainsResultAndOriginalIdempotentOrder(t *testing.T) {
	db := setupWalletPaymentDB(t)
	scope := WalletTopUpScope{UserID: 41}
	first, flow, replay, err := CreateWalletTopUp(scope, strings.Repeat("a", 64), strings.Repeat("b", 64), 0, walletTestOrder("wallet-a"))
	require.NoError(t, err)
	assert.False(t, replay)
	_, err = AcknowledgeWalletTopUp(scope, flow.FlowID, flow.Revision, first.TradeNo)
	require.ErrorIs(t, err, ErrTopUpStatusInvalid)
	_, _, _, err = CreateWalletTopUp(scope, strings.Repeat("c", 64), strings.Repeat("b", 64), flow.Revision, walletTestOrder("wallet-b"))
	require.ErrorIs(t, err, ErrWalletFlowActive)
	_, _, _, err = CreateWalletTopUp(scope, strings.Repeat("a", 64), strings.Repeat("c", 64), 0, walletTestOrder("wallet-a"))
	require.ErrorIs(t, err, ErrWalletIdempotencyConflict)
	first, err = SaveWalletTopUpSession(first.TradeNo, first.CreateClaimToken, "session-a", "provider-a", "private-checkout", common.GetTimestamp()+60)
	require.NoError(t, err)
	first, err = RequestWalletTopUpCancel(scope, first.TradeNo, first.Version, "user_cancelled")
	require.NoError(t, err)
	check, err := ClaimWalletTopUpCheck(first.TradeNo, common.GetTimestamp())
	require.NoError(t, err)
	_, err = ApplyWalletTopUpCheck(first.TradeNo, WalletTopUpCheckResult{ClaimToken: check.CheckClaimToken, ExpectedVersion: check.Version, ProviderPaymentState: "unpaid", ReliableClosed: true, CloseEvidenceRef: "authenticated-close-a", TerminalStatus: "expired"})
	require.NoError(t, err)
	state, err := GetWalletTopUpState(scope)
	require.NoError(t, err)
	require.NotNil(t, state.Order)
	assert.Equal(t, "expired", state.Order.Status, "the timeout result remains current until acknowledgement")
	state, err = AcknowledgeWalletTopUp(scope, flow.FlowID, flow.Revision, first.TradeNo)
	require.NoError(t, err)
	assert.Nil(t, state.Order)
	_, err = AcknowledgeWalletTopUp(scope, flow.FlowID, flow.Revision, first.TradeNo)
	require.NoError(t, err, "the exact acknowledgement can be replayed")
	_, err = AcknowledgeWalletTopUp(scope, flow.FlowID, flow.Revision+1, first.TradeNo)
	require.ErrorIs(t, err, ErrWalletFlowChanged, "acknowledgement replay still requires the original revision")
	second, newFlow, _, err := CreateWalletTopUp(scope, strings.Repeat("c", 64), strings.Repeat("d", 64), state.Flow.Revision, walletTestOrder("wallet-b"))
	require.NoError(t, err)
	original, current, replay, err := CreateWalletTopUp(scope, strings.Repeat("a", 64), strings.Repeat("b", 64), 0, walletTestOrder("unused"))
	require.NoError(t, err)
	assert.True(t, replay)
	assert.Equal(t, first.Id, original.Id)
	require.NotNil(t, current.TopUpID)
	assert.Equal(t, second.Id, *current.TopUpID)
	_, err = AcknowledgeWalletTopUp(scope, flow.FlowID, flow.Revision, first.TradeNo)
	require.ErrorIs(t, err, ErrWalletFlowChanged)
	var stored WalletTopUpFlow
	require.NoError(t, db.First(&stored, newFlow.ID).Error)
	require.NotNil(t, stored.TopUpID)
	assert.Equal(t, second.Id, *stored.TopUpID)
}

func TestWalletIdleStateExposesRecoverableOriginalSession(t *testing.T) {
	db := setupWalletPaymentDB(t)
	scope := WalletTopUpScope{UserID: 41}
	original := walletTestOrder("recoverable-checkout")
	original.UserId = scope.UserID
	original.WalletLifecycleVersion = 1
	original.CreateState = "ready"
	original.CreateTime = common.GetTimestamp()
	original.Status = common.TopUpStatusPending
	original.ProviderSessionID = "original-session"
	require.NoError(t, db.Create(original).Error)
	state, err := GetWalletTopUpState(scope)
	require.NoError(t, err)
	assert.Nil(t, state.Order)
	assert.True(t, state.HasRecoverableCandidate)
	_, _, _, err = CreateWalletTopUp(scope, strings.Repeat("a", 64), strings.Repeat("b", 64), 0, walletTestOrder("must-not-replace-original"))
	require.ErrorIs(t, err, ErrWalletFlowActive)
	state, err = RestoreWalletTopUp(scope, original.TradeNo)
	require.NoError(t, err)
	require.NotNil(t, state.Order)
	assert.Equal(t, original.Id, state.Order.Id)
	assert.Equal(t, "original-session", state.Order.ProviderSessionID)
}

func TestWalletConcurrentCreationHasOneAuthoritativeFlow(t *testing.T) {
	db := setupWalletPaymentDB(t)
	scope := WalletTopUpScope{UserID: 41}
	out := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			_, _, _, err := CreateWalletTopUp(scope, strings.Repeat(fmt.Sprint(i), 64), strings.Repeat("d", 64), 0, walletTestOrder(fmt.Sprintf("concurrent-%d", i)))
			out <- err
		}(i)
	}
	first, second := <-out, <-out
	if first == nil {
		require.ErrorIs(t, second, ErrWalletFlowActive)
	} else {
		require.ErrorIs(t, first, ErrWalletFlowActive)
		require.NoError(t, second)
	}
	var count int64
	require.NoError(t, db.Model(&TopUp{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}
func walletReceiptFor(order *TopUp, transaction string) WalletPaymentReceipt {
	return WalletPaymentReceipt{TopUpID: &order.Id, Provider: order.PaymentProvider, ProviderAccountRef: order.ProviderAccountRef, ProviderEnvironment: order.ProviderEnvironment, TransactionID: transaction, AmountMinor: 100, Currency: order.Currency, CurrencyExponent: order.CurrencyExponent, PaidAt: common.GetTimestamp(), EvidenceSource: "authenticated_query", EvidenceRef: "provider-evidence", VerificationStatus: "verified", SettlementKind: "paid"}
}
func TestWalletVerifiedLatePaymentCreditsOnceAndRetainsSecondPayment(t *testing.T) {
	db := setupWalletPaymentDB(t)
	scope := WalletTopUpScope{UserID: 41}
	order, _, _, err := CreateWalletTopUp(scope, strings.Repeat("a", 64), strings.Repeat("b", 64), 0, walletTestOrder("late-payment"))
	require.NoError(t, err)
	require.NoError(t, db.Model(order).Updates(map[string]any{"status": "expired", "closed_at": common.GetTimestamp(), "close_evidence_ref": "old-close", "provider_payment_state": "unpaid"}).Error)
	input := FinalizeVerifiedWalletTopUpInput{PricingVerified: true, TradeNo: order.TradeNo, Receipt: walletReceiptFor(order, "transaction-one")}
	result, err := FinalizeVerifiedWalletTopUp(input)
	require.NoError(t, err)
	assert.False(t, result.AlreadyCompleted)
	result, err = FinalizeVerifiedWalletTopUp(input)
	require.NoError(t, err)
	assert.True(t, result.AlreadyCompleted)
	input.Receipt.TransactionID = "transaction-two"
	_, err = FinalizeVerifiedWalletTopUp(input)
	require.ErrorIs(t, err, ErrWalletPaymentEvidence)
	var user User
	require.NoError(t, db.First(&user, 41).Error)
	assert.EqualValues(t, 510, user.Quota)
	var receipts []WalletPaymentReceipt
	require.NoError(t, db.Order("id").Find(&receipts).Error)
	require.Len(t, receipts, 2)
	assert.Equal(t, "credited", receipts[0].ProcessingStatus)
	assert.Equal(t, "review", receipts[1].ProcessingStatus)
	var events int64
	require.NoError(t, db.Model(&KKAIOutboxEvent{}).Count(&events).Error)
	assert.EqualValues(t, 1, events)
}
func TestWalletPaidReceiptSurvivesAccountingFailureAndCanRetry(t *testing.T) {
	db := setupWalletPaymentDB(t)
	scope := WalletTopUpScope{UserID: 41}
	order, _, _, err := CreateWalletTopUp(scope, strings.Repeat("a", 64), strings.Repeat("b", 64), 0, walletTestOrder("credit-retry"))
	require.NoError(t, err)
	failure := errors.New("outbox storage unavailable")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("wallet:credit-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "kkai_outbox" {
			tx.AddError(failure)
		}
	}))
	input := FinalizeVerifiedWalletTopUpInput{PricingVerified: true, TradeNo: order.TradeNo, Receipt: walletReceiptFor(order, "verified-retry")}
	_, err = FinalizeVerifiedWalletTopUp(input)
	require.ErrorIs(t, err, failure)
	var receipt WalletPaymentReceipt
	require.NoError(t, db.First(&receipt).Error)
	assert.Equal(t, "review", receipt.ProcessingStatus)
	assert.Equal(t, "payment-operations", receipt.ReviewOwner)
	assert.EqualValues(t, 100, receipt.AmountMinor)
	var user User
	require.NoError(t, db.First(&user, 41).Error)
	assert.EqualValues(t, 10, user.Quota)
	var saved TopUp
	require.NoError(t, db.First(&saved, order.Id).Error)
	assert.Equal(t, "credit_review", saved.Status)
	require.NoError(t, db.Callback().Create().Remove("wallet:credit-failure"))
	_, err = FinalizeVerifiedWalletTopUp(input)
	require.NoError(t, err)
	require.NoError(t, db.First(&receipt).Error)
	assert.Equal(t, "credited", receipt.ProcessingStatus)
	assert.Empty(t, receipt.ReviewOwner)
	require.NoError(t, db.First(&user, 41).Error)
	assert.EqualValues(t, 510, user.Quota)
}
func TestWalletHistoryIncludesAllAgeUnfinishedAndUsesSameCounts(t *testing.T) {
	db := setupWalletPaymentDB(t)
	now := common.GetTimestamp()
	orders := []TopUp{
		{UserId: 41, TradeNo: "old-paid", Status: "success", CreateTime: now - 40*86400},
		{UserId: 41, TradeNo: "old-pending", Status: "pending", CreateTime: now - 40*86400},
		{UserId: 41, TradeNo: "old-failed-unverified", Status: "failed", CreateTime: now - 40*86400},
		{UserId: 41, TradeNo: "recent-paid", Status: "success", CreateTime: now - 60},
		{UserId: 41, TradeNo: "recent-closed", Status: "expired", CreateTime: now - 60, ClosedAt: now - 1, CloseEvidenceRef: "close", ProviderPaymentState: "unpaid"},
	}
	require.NoError(t, db.Create(&orders).Error)
	result, err := QueryWalletTopUps(WalletTopUpHistoryQuery{Scope: WalletTopUpScope{UserID: 41}, QueryScope: "wallet", Limit: 100})
	require.NoError(t, err)
	assert.EqualValues(t, 4, result.Total)
	assert.EqualValues(t, 4, result.Counts["all"])
	assert.EqualValues(t, 1, result.Counts["completed"])
	assert.EqualValues(t, 3, result.Counts["closed"], "old pending and unverified failed attempts are site-expired but remain visible")
	assert.Zero(t, result.Counts["review"])
	result, err = QueryWalletTopUps(WalletTopUpHistoryQuery{Scope: WalletTopUpScope{UserID: 41}, QueryScope: "wallet", StatusGroup: "closed", Limit: 100})
	require.NoError(t, err)
	assert.EqualValues(t, 3, result.Total)
	require.Len(t, result.Orders, 3)
	assert.Equal(t, "recent-closed", result.Orders[0].TradeNo)
	assert.EqualValues(t, 4, result.Counts["all"])
	result, err = QueryWalletTopUps(WalletTopUpHistoryQuery{Scope: WalletTopUpScope{UserID: 41}, QueryScope: "recent", ExactTradeNo: "old-paid", Limit: 100})
	require.NoError(t, err)
	assert.Equal(t, "exact", result.QueryScope)
	assert.EqualValues(t, 1, result.Total)
}

func TestWalletReceiptConflictDoesNotPoisonDurablePricingProofRetry(t *testing.T) {
	db := setupWalletPaymentDB(t)
	scope := WalletTopUpScope{UserID: 41}
	proposed := walletTestOrder("durable-pricing")
	proposed.PricingMode = "channel_adjusted"
	proposed.PaymentProvider = PaymentProviderStripe
	order, _, _, err := CreateWalletTopUp(scope, strings.Repeat("a", 64), strings.Repeat("b", 64), 0, proposed)
	require.NoError(t, err)
	original := walletReceiptFor(order, "same-transaction")
	original.AmountMinor = 90
	original.PricingVerified = true
	receipt, err := SaveWalletPaymentReceipt(&original)
	require.NoError(t, err)
	conflict := original
	conflict.AmountMinor = 80
	_, err = SaveWalletPaymentReceipt(&conflict)
	require.ErrorIs(t, err, ErrWalletPaymentEvidence)
	var before WalletPaymentReceipt
	require.NoError(t, db.First(&before, receipt.ID).Error)
	assert.Empty(t, before.ExceptionReason)
	assert.EqualValues(t, 90, before.AmountMinor)
	assert.True(t, before.PricingVerified)
	result, err := RetryVerifiedWalletTopUpReceipt(receipt.ID)
	require.NoError(t, err)
	assert.Equal(t, "success", result.TopUp.Status)
	var user User
	require.NoError(t, db.First(&user, 41).Error)
	assert.EqualValues(t, 510, user.Quota)
	var events []WalletPaymentEvent
	require.NoError(t, db.Where("kind = ?", "receipt_conflict").Find(&events).Error)
	require.Len(t, events, 1)
	require.NotNil(t, events[0].ReceiptID)
	assert.Equal(t, receipt.ID, *events[0].ReceiptID)
	assert.NotEmpty(t, events[0].EvidenceHash)
}

func TestWalletIncompletePaidProofIsDurableAndCanBeEnriched(t *testing.T) {
	db := setupWalletPaymentDB(t)
	scope := WalletTopUpScope{UserID: 41}
	order, _, _, err := CreateWalletTopUp(scope, strings.Repeat("a", 64), strings.Repeat("b", 64), 0, walletTestOrder("incomplete-proof"))
	require.NoError(t, err)
	receipt := walletReceiptFor(order, "incomplete-transaction")
	receipt.PaidAt = 0
	_, err = FinalizeVerifiedWalletTopUp(FinalizeVerifiedWalletTopUpInput{TradeNo: order.TradeNo, Receipt: receipt})
	require.ErrorIs(t, err, ErrWalletPaymentProofIncomplete)
	var stored WalletPaymentReceipt
	require.NoError(t, db.First(&stored).Error)
	assert.Zero(t, stored.PaidAt)
	assert.Equal(t, "PAYMENT_PROOF_INCOMPLETE", stored.ExceptionReason)
	claim, err := ClaimWalletTopUpCheck(order.TradeNo, common.GetTimestamp())
	require.NoError(t, err)
	held, err := ApplyWalletTopUpCheck(order.TradeNo, WalletTopUpCheckResult{ClaimToken: claim.CheckClaimToken, ExpectedVersion: claim.Version, ProviderPaymentState: "unpaid", ReliableClosed: true, CloseEvidenceRef: "stale-negative-query", TerminalStatus: "expired"})
	require.NoError(t, err)
	assert.Equal(t, "credit_review", held.Status)
	assert.Zero(t, held.ClosedAt)
	_, err = RetryVerifiedWalletTopUpReceipt(stored.ID)
	require.ErrorIs(t, err, ErrWalletPaymentProofIncomplete, "a retry must not invent pricing or payment time")
	receipt.PaidAt = common.GetTimestamp()
	result, err := FinalizeVerifiedWalletTopUp(FinalizeVerifiedWalletTopUpInput{TradeNo: order.TradeNo, Receipt: receipt, PricingVerified: true})
	require.NoError(t, err)
	assert.Equal(t, "success", result.TopUp.Status)
	require.NoError(t, db.First(&stored).Error)
	assert.True(t, stored.PricingVerified)
	assert.Equal(t, "credited", stored.ProcessingStatus)
	unmatched := walletReceiptFor(order, "unmatched-transaction")
	unmatched.TopUpID = nil
	saved, err := SaveWalletPaymentReceipt(&unmatched)
	require.NoError(t, err)
	assert.Equal(t, "review", saved.ProcessingStatus)
	assert.Equal(t, "UNMATCHED_PAYMENT", saved.ExceptionReason)
	assert.Equal(t, "payment-operations", saved.ReviewOwner)
	assert.Zero(t, saved.NextRetryAt)
}

func TestWalletRunUnknownScopeAndCheckpointAreBounded(t *testing.T) {
	db := setupWalletPaymentDB(t)
	require.NoError(t, db.AutoMigrate(&SystemTaskLock{}))
	now := common.GetTimestamp()
	require.NoError(t, db.Create(&TopUp{Id: 100, UserId: 41, TradeNo: "unknown-order", Status: "pending", CreateTime: now - 60}).Error)
	require.NoError(t, db.Create(&TopUp{Id: 101, UserId: 41, TradeNo: "stripe-order", Status: "pending", PaymentProvider: PaymentProviderStripe, CreateTime: now - 60}).Error)
	scope := WalletRunScope{PaymentProvider: "unknown", AccountRef: "unknown", Environment: "unknown", TenantID: 0, CreatedBefore: now}
	max, total, missing, err := ReportWalletTopUpRun(scope)
	require.NoError(t, err)
	assert.EqualValues(t, 100, max)
	assert.EqualValues(t, 1, total)
	assert.EqualValues(t, 1, missing)
	run := &WalletTopUpRun{ID: "bounded-run", ScopeJSON: "{}", ScopeDigest: strings.Repeat("a", 64), MaxOrderID: 100, ControlVersion: 1, DesiredState: "running", Status: "running"}
	require.NoError(t, db.Create(run).Error)
	require.NoError(t, db.Create(&SystemTaskLock{Type: "wallet_payment_history", TaskID: "history-task", LockedBy: "runner", LockedUntil: now + 60}).Error)
	_, err = CheckpointWalletRun(run, "history-task", "runner", 101, "credited")
	require.ErrorIs(t, err, ErrResellerFinanceInvalidInput)
	ok, err := CheckpointWalletRun(run, "history-task", "runner", 100, "credited")
	require.NoError(t, err)
	assert.True(t, ok)
	_, err = CheckpointWalletRun(run, "history-task", "runner", 100, "credited")
	require.ErrorIs(t, err, ErrResellerFinanceInvalidInput, "the same checkpoint cannot be counted twice")
	var stored WalletTopUpRun
	require.NoError(t, db.First(&stored, "id = ?", run.ID).Error)
	assert.EqualValues(t, 1, stored.ScannedCount)
	assert.EqualValues(t, stored.ScannedCount, stored.CreditedCount+stored.ClosedCount+stored.ReviewCount+stored.ErrorCount)
	require.NoError(t, db.Model(&SystemTaskLock{}).Where("task_id = ?", "history-task").Update("locked_until", 0).Error)
	_, err = UpdateWalletTopUpRun(run.ID, stored.ControlVersion, map[string]any{"status": "completed"}, WalletRunTaskLease{TaskID: "history-task", Runner: "runner"})
	require.ErrorIs(t, err, ErrWalletOrderChanged)
	require.NoError(t, db.First(&stored, "id = ?", run.ID).Error)
	assert.Equal(t, "running", stored.Status)
}

func TestWalletHistoryRunsRotateByLastProgress(t *testing.T) {
	db := setupWalletPaymentDB(t)
	runs := []WalletTopUpRun{
		{ID: "slow-first", Status: "running", CreatedAt: 1, UpdatedAt: 3},
		{ID: "waiting-next", Status: "queued", CreatedAt: 2, UpdatedAt: 2},
		{ID: "already-completed", Status: "completed", CreatedAt: 1, UpdatedAt: 1},
	}
	require.NoError(t, db.Create(&runs).Error)
	active, err := FindActiveWalletRuns()
	require.NoError(t, err)
	require.Len(t, active, 2)
	assert.Equal(t, "waiting-next", active[0].ID)
	assert.Equal(t, "slow-first", active[1].ID)
	require.NoError(t, db.Model(&WalletTopUpRun{}).Where("id = ?", active[0].ID).Update("updated_at", 4).Error)
	active, err = FindActiveWalletRuns()
	require.NoError(t, err)
	assert.Equal(t, "slow-first", active[0].ID)
}

func TestWalletEndedAttemptRetainsResultAndLatePaymentCannotReplaceNewFlow(t *testing.T) {
	db := setupWalletPaymentDB(t)
	scope := WalletTopUpScope{UserID: 41}
	first, flow, _, err := CreateWalletTopUp(scope, strings.Repeat("a", 64), strings.Repeat("b", 64), 0, walletTestOrder("ended-first"))
	require.NoError(t, err)
	first, err = SaveWalletTopUpSession(first.TradeNo, first.CreateClaimToken, "first-session", "first-provider-order", "frozen-checkout", first.CreateTime+3600)
	require.NoError(t, err)
	assert.Equal(t, first.CreateTime+WalletTopUpAttemptTTLSeconds, first.PayDeadlineAt, "a provider's longer session cannot extend the site's deadline")
	_, err = EndWalletTopUpAttempt(scope, first.TradeNo, first.Version+1, "user_ended")
	require.ErrorIs(t, err, ErrWalletOrderChanged)
	first, err = EndWalletTopUpAttempt(scope, first.TradeNo, first.Version, "user_ended")
	require.NoError(t, err)
	assert.Positive(t, first.LocalEndedAt)
	assert.Equal(t, "user_ended", first.LocalEndReason)
	assert.Equal(t, common.TopUpStatusPending, first.Status, "site ending does not fabricate a payment status")
	assert.Zero(t, first.ClosedAt)
	assert.Empty(t, first.CloseEvidenceRef)
	assert.True(t, WalletTopUpLocalEnded(first, common.GetTimestamp()))
	assert.False(t, WalletTopUpReliableTerminal(first))
	state, err := GetWalletTopUpState(scope)
	require.NoError(t, err)
	require.NotNil(t, state.Order, "the ended result remains current until acknowledgement")
	_, _, _, err = CreateWalletTopUp(scope, strings.Repeat("c", 64), strings.Repeat("d", 64), flow.Revision, walletTestOrder("blocked-until-ack"))
	require.ErrorIs(t, err, ErrWalletFlowActive)
	state, err = AcknowledgeWalletTopUp(scope, flow.FlowID, flow.Revision, first.TradeNo)
	require.NoError(t, err)
	assert.False(t, state.HasRecoverableCandidate)
	assert.Zero(t, state.OtherUnfinishedCount)
	_, err = RestoreWalletTopUp(scope, first.TradeNo)
	require.ErrorIs(t, err, ErrTopUpStatusInvalid)
	second, secondFlow, _, err := CreateWalletTopUp(scope, strings.Repeat("c", 64), strings.Repeat("d", 64), state.Flow.Revision, walletTestOrder("new-second"))
	require.NoError(t, err)
	historical := walletTestOrder("active-historical-operation")
	historical.UserId, historical.CreateTime, historical.Status = scope.UserID, common.GetTimestamp(), common.TopUpStatusPending
	require.NoError(t, db.Create(historical).Error)
	historical, err = EndWalletTopUpAttempt(scope, historical.TradeNo, historical.Version, "user_ended")
	require.NoError(t, err, "an owned historical operation can end without replacing the current operation")
	assert.Equal(t, "user_ended", historical.LocalEndReason)
	_, err = EndWalletTopUpAttempt(scope, first.TradeNo, first.Version, "user_ended")
	require.NoError(t, err, "ending a historical operation cannot affect the new flow")
	_, err = FinalizeVerifiedWalletTopUp(FinalizeVerifiedWalletTopUpInput{TradeNo: first.TradeNo, Receipt: walletReceiptFor(first, "late-first-payment"), PricingVerified: true})
	require.NoError(t, err)
	state, err = GetWalletTopUpState(scope)
	require.NoError(t, err)
	require.NotNil(t, state.Order)
	assert.Equal(t, second.Id, state.Order.Id)
	assert.Equal(t, secondFlow.FlowID, state.Flow.FlowID)
	original, current, replay, err := CreateWalletTopUp(scope, strings.Repeat("a", 64), strings.Repeat("b", 64), 0, walletTestOrder("unused"))
	require.NoError(t, err)
	assert.True(t, replay)
	assert.Equal(t, first.Id, original.Id)
	assert.Equal(t, second.Id, *current.TopUpID)
	var user User
	require.NoError(t, db.First(&user, scope.UserID).Error)
	assert.EqualValues(t, 510, user.Quota)
}

func TestWalletSiteTimeoutRetainsOldOrdersButRejectsManualCompletionAndRestore(t *testing.T) {
	db := setupWalletPaymentDB(t)
	now := common.GetTimestamp()
	scope := WalletTopUpScope{UserID: 41}
	old := walletTestOrder("very-old-pending")
	old.UserId, old.Status, old.CreateTime, old.Amount = scope.UserID, common.TopUpStatusPending, now-90*86400, 1
	require.NoError(t, db.Create(old).Error)
	assert.True(t, WalletTopUpLocalEnded(old, now))
	_, err := RestoreWalletTopUp(scope, old.TradeNo)
	require.ErrorIs(t, err, ErrTopUpStatusInvalid)
	require.ErrorIs(t, ManualCompleteTopUp(old.TradeNo, "127.0.0.1"), ErrWalletAttemptEnded)
	history, err := QueryWalletTopUps(WalletTopUpHistoryQuery{Scope: scope, QueryScope: "wallet", StatusGroup: "closed", Limit: 100})
	require.NoError(t, err)
	require.Len(t, history.Orders, 1, "site-expired orders remain visible beyond the recent history window")
	assert.Equal(t, old.Id, history.Orders[0].Id)
	unfinished, err := QueryWalletTopUps(WalletTopUpHistoryQuery{Scope: scope, QueryScope: "unfinished", Limit: 100})
	require.NoError(t, err)
	assert.Zero(t, unfinished.Total)
	maxID, total, _, err := ReportWalletTopUpRun(WalletRunScope{PaymentProvider: PaymentProviderEpay, AccountRef: old.ProviderAccountRef, Environment: "test", CreatedBefore: now, TenantID: 0})
	require.NoError(t, err)
	assert.Zero(t, maxID)
	assert.Zero(t, total, "manual historical batches cannot revive site-expired orders")
	_, err = ScheduleWalletTopUpCheck(scope, old.TradeNo)
	require.NoError(t, err, "authenticated callbacks can still schedule their independent background check")
	var stored TopUp
	require.NoError(t, db.First(&stored, old.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, stored.Status)
	assert.Zero(t, stored.ClosedAt)
	assert.Empty(t, stored.CloseEvidenceRef)
	var user User
	require.NoError(t, db.First(&user, scope.UserID).Error)
	assert.EqualValues(t, 10, user.Quota)
	current, flow, _, err := CreateWalletTopUp(scope, strings.Repeat("a", 64), strings.Repeat("b", 64), 0, walletTestOrder("current-timed-out"))
	require.NoError(t, err)
	require.NoError(t, db.Model(current).Update("create_time", now-WalletTopUpAttemptTTLSeconds).Error)
	state, err := GetWalletTopUpState(scope)
	require.NoError(t, err)
	require.NotNil(t, state.Order, "the timeout result is retained rather than silently releasing the form")
	assert.True(t, WalletTopUpLocalEnded(state.Order, now))
	_, err = RequestWalletTopUpCancel(scope, current.TradeNo, current.Version, "user_cancel")
	require.ErrorIs(t, err, ErrWalletAttemptEnded)
	state, err = AcknowledgeWalletTopUp(scope, flow.FlowID, flow.Revision, current.TradeNo)
	require.NoError(t, err)
	assert.Nil(t, state.Order)
	assert.False(t, state.HasRecoverableCandidate)
}

func TestWalletLocalDeadlineNeverDiscardsKnownPaymentResponsibility(t *testing.T) {
	const now int64 = 1700000000
	for _, test := range []struct {
		name  string
		order TopUp
		ended bool
	}{
		{name: "at site deadline", order: TopUp{CreateTime: now - WalletTopUpAttemptTTLSeconds}, ended: true},
		{name: "before deadline", order: TopUp{CreateTime: now - WalletTopUpAttemptTTLSeconds + 1}},
		{name: "earlier provider expiry", order: TopUp{CreateTime: now - 60, ProviderExpiresAt: now}, ended: true},
		{name: "missing historical time remains unavailable", order: TopUp{}},
		{name: "old unpaid review", order: TopUp{Status: "review_required", CreateTime: 1}, ended: true},
		{name: "credited", order: TopUp{Status: "success", CreateTime: 1, LocalEndedAt: now - 10}},
		{name: "credit failure", order: TopUp{Status: "credit_review", CreateTime: 1, LocalEndedAt: now - 10}},
		{name: "verified paid", order: TopUp{ProviderPaymentState: "paid", CreateTime: 1, LocalEndedAt: now - 10}},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.ended, WalletTopUpLocalEnded(&test.order, now))
		})
	}
}

func TestWalletPaidReviewAndUnknownTimestampKeepTheirDistinctHistoryStates(t *testing.T) {
	db := setupWalletPaymentDB(t)
	scope := WalletTopUpScope{UserID: 41}
	order, _, _, err := CreateWalletTopUp(scope, strings.Repeat("a", 64), strings.Repeat("b", 64), 0, walletTestOrder("paid-review-after-local-end"))
	require.NoError(t, err)
	require.NoError(t, db.Model(order).Updates(map[string]any{"status": "credit_review", "provider_payment_state": "paid", "review_owner": "payment-operations", "local_ended_at": common.GetTimestamp() - 60, "create_time": 1}).Error)
	_, err = EndWalletTopUpAttempt(scope, order.TradeNo, order.Version, "user_ended")
	require.ErrorIs(t, err, ErrTopUpStatusInvalid)
	unknown := walletTestOrder("unknown-create-time")
	unknown.UserId, unknown.Status, unknown.Amount = scope.UserID, common.TopUpStatusPending, 1
	require.NoError(t, db.Create(unknown).Error)
	require.ErrorIs(t, ManualCompleteTopUp(unknown.TradeNo, "127.0.0.1"), ErrWalletAttemptEnded)
	_, err = EndWalletTopUpAttempt(scope, unknown.TradeNo, unknown.Version, "timeout")
	require.ErrorIs(t, err, ErrTopUpStatusInvalid, "an unknown timestamp cannot invent a timeout moment")
	history, err := QueryWalletTopUps(WalletTopUpHistoryQuery{Scope: scope, QueryScope: "wallet", Limit: 100})
	require.NoError(t, err)
	assert.EqualValues(t, 2, history.Counts["all"])
	assert.EqualValues(t, 1, history.Counts["review"])
	assert.Zero(t, history.Counts["closed"], "neither known paid money nor an unknown time can be called a site timeout")
}

func TestWalletNegativeChecksCannotErasePaidFactsOrReceiptReview(t *testing.T) {
	for _, test := range []struct {
		name          string
		paymentState  string
		status        string
		receiptAmount int64
	}{
		{name: "persisted paid flag", paymentState: "paid", status: "credit_review", receiptAmount: 100},
		{name: "receipt committed before credit", status: "pending", receiptAmount: 100},
		{name: "mismatched receipt hold is not paid", paymentState: "unknown", status: "credit_review", receiptAmount: 99},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := setupWalletPaymentDB(t)
			order, _, _, err := CreateWalletTopUp(WalletTopUpScope{UserID: 41}, strings.Repeat("a", 64), strings.Repeat("b", 64), 0, walletTestOrder("held-receipt"))
			require.NoError(t, err)
			receipt := walletReceiptFor(order, "accepted-payment")
			receipt.AmountMinor, receipt.PricingVerified = test.receiptAmount, true
			saved, err := SaveWalletPaymentReceipt(&receipt)
			require.NoError(t, err)
			require.NoError(t, db.Model(order).Updates(map[string]any{"status": test.status, "provider_payment_state": test.paymentState}).Error)
			for _, state := range []string{"unpaid", "unknown"} {
				claim, err := ClaimWalletTopUpCheck(order.TradeNo, common.GetTimestamp())
				require.NoError(t, err)
				wrongAmount := int64(1)
				held, err := ApplyWalletTopUpCheck(order.TradeNo, WalletTopUpCheckResult{ClaimToken: claim.CheckClaimToken, ExpectedVersion: claim.Version, ProviderPaymentState: state, ReasonCode: "REVIEW_REQUIRED", ReliableClosed: state == "unpaid", CloseEvidenceRef: "later-negative-query", TerminalStatus: "expired", PayableMinor: &wrongAmount})
				require.NoError(t, err)
				assert.Equal(t, test.paymentState, held.ProviderPaymentState)
				assert.Equal(t, "credit_review", held.Status)
				assert.Empty(t, held.CheckClaimToken)
				assert.Zero(t, held.CheckClaimUntil)
				assert.Positive(t, held.NextCheckAt)
				assert.Zero(t, held.ClosedAt)
				assert.Empty(t, held.CloseEvidenceRef)
				assert.Nil(t, held.PayableMinor)
			}
			var user User
			require.NoError(t, db.First(&user, 41).Error)
			assert.EqualValues(t, 10, user.Quota, "holding a receipt never credits money")
			_, err = RetryVerifiedWalletTopUpReceipt(saved.ID)
			if test.receiptAmount == 100 {
				require.NoError(t, err, "a later trusted settlement still uses the original receipt")
				require.NoError(t, db.First(&user, 41).Error)
				assert.EqualValues(t, 510, user.Quota)
			} else {
				require.ErrorIs(t, err, ErrWalletPaymentEvidence)
				require.NoError(t, db.First(&user, 41).Error)
				assert.EqualValues(t, 10, user.Quota)
			}
		})
	}
}
