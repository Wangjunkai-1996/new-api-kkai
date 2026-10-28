package model

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func shopTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "shop.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	previousType := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() { common.SetMainDatabaseType(previousType); require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(append(ShopSchemaModels(), &User{}, &KKAIOutboxEvent{}, &KKAIJobLease{})...))
	require.NoError(t, db.Create(&User{Id: 1, Username: "buyer", AffCode: "buyer", TenantID: PlatformTenantID, Role: common.RoleCommonUser, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, db.Create(&User{Id: 2, Username: "operator", AffCode: "operator", TenantID: PlatformTenantID, Role: common.RoleAdminUser, Status: common.UserStatusEnabled}).Error)
	return db
}

func shopTestOrder(t *testing.T, db *gorm.DB) (ShopOrder, ShopPaymentAttempt, ShopPaymentReceipt) {
	t.Helper()
	product := ShopProduct{ShopRecord: ShopRecord{PublicID: "sp_test", Version: 1}, TenantID: PlatformTenantID, DeliveryType: "automatic"}
	require.NoError(t, db.Create(&product).Error)
	snapshot, err := common.Marshal(ShopOrderSnapshot{DeliveryType: "automatic", Retention: ShopRetentionPolicy{BuyerInputAfterClosureSeconds: 100}, Terms: ShopProductTerms{ViewPolicy: ShopViewPolicy{DurationSeconds: 100}}})
	require.NoError(t, err)
	order := ShopOrder{ShopRecord: ShopRecord{PublicID: "so_test", Version: 1, CreatedAt: 100, UpdatedAt: 100}, TenantID: PlatformTenantID, UserID: 1, ProductID: product.ID, SnapshotJSON: string(snapshot), AmountMinor: 100, Currency: "CNY", CurrencyExponent: 2, PurchaseState: "open", PaymentState: "unpaid", DeliveryState: "pending", InputCompleteAt: 100, MissingInputKeysJSON: "[]", ReservationExpiresAt: 500}
	require.NoError(t, db.Create(&order).Error)
	attempt := ShopPaymentAttempt{ShopRecord: ShopRecord{PublicID: "spt_test", Version: 1, CreatedAt: 100, UpdatedAt: 100}, OrderID: order.ID, TenantID: PlatformTenantID, UserID: 1, AttemptNo: 1, TradeNo: "shop-test", Provider: "epay", ProviderAccountRef: "merchant", Environment: "sandbox", BusinessKind: "shop", CreateState: "ready", ProviderOrderID: "provider-order", ProviderSessionID: "provider-session"}
	require.NoError(t, db.Create(&attempt).Error)
	require.NoError(t, db.Model(&order).Update("active_attempt_id", attempt.ID).Error)
	order.ActiveAttemptID = &attempt.ID
	receipt := ShopPaymentReceipt{Provider: "epay", ProviderAccountRef: "merchant", Environment: "sandbox", ProviderTransactionID: "transaction", OriginalTradeRef: attempt.TradeNo, ProviderOrderID: attempt.ProviderOrderID, ProviderSessionID: attempt.ProviderSessionID, AmountMinor: 100, Currency: "CNY", CurrencyExponent: 2, ProviderPaidAt: 150, VerificationSource: "provider_query", PricingVerified: true, EvidenceRef: "verified-query"}
	return order, attempt, receipt
}

func shopTestCommand(key string, version, now int64) ShopCommand {
	return ShopCommand{ActorID: 2, TenantID: PlatformTenantID, Admin: true, Key: key, RequestHash: strings.Repeat("a", 64), HashKeyVersion: "key1", ExpectedVersion: version, Now: now}
}

func TestShopVerifiedReceiptRequiresFullIdentityAndRecovers(t *testing.T) {
	db := shopTestDB(t)
	order, _, receipt := shopTestOrder(t, db)
	receipt.VerificationSource = "provider_webhook"
	receipt.ProviderSessionID = ""
	stored, err := SaveShopVerifiedReceipt(db, ShopVerifiedReceiptInput{Receipt: receipt, Verified: true, Now: 200})
	require.NoError(t, err)
	stored, err = MatchShopPaymentReceipt(db, stored.ID, 200)
	require.NoError(t, err)
	assert.Equal(t, "review", stored.ProcessingState)
	assert.Equal(t, "PAYMENT_IDENTITY_MISMATCH", stored.ExceptionCode)
	require.NoError(t, db.First(&order, order.ID).Error)
	assert.Equal(t, "unpaid", order.PaymentState)
	var count int64
	require.NoError(t, db.Model(&ShopEvent{}).Where("order_id = ? AND kind = ? AND visibility = ?", order.ID, "payment_received", "public").Count(&count).Error)
	assert.Zero(t, count, "a receipt with mismatched identity is not a public payment fact")
	receipt.VerificationSource = "provider_query"
	receipt.ProviderSessionID = "provider-session"
	recovered, err := SaveShopVerifiedReceipt(db, ShopVerifiedReceiptInput{Receipt: receipt, Verified: true, Now: 201})
	require.NoError(t, err)
	assert.Equal(t, stored.ID, recovered.ID)
	assert.Equal(t, "pending", recovered.ProcessingState)
	matched, err := MatchShopPaymentReceipt(db, recovered.ID, 201)
	require.NoError(t, err)
	assert.Equal(t, "matched", matched.ProcessingState)
	_, err = SaveShopVerifiedReceipt(db, ShopVerifiedReceiptInput{Receipt: receipt, Verified: true, Now: 202})
	require.NoError(t, err)
	_, err = MatchShopPaymentReceipt(db, matched.ID, 202)
	require.NoError(t, err)
	require.NoError(t, db.Model(&ShopPaymentReceipt{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	require.NoError(t, db.Model(&ShopEvent{}).Where("order_id = ? AND kind = ? AND visibility = ?", order.ID, "payment_received", "public").Count(&count).Error)
	assert.Equal(t, int64(1), count, "verified recovery and retries announce only one payment")
	require.NoError(t, db.Model(&KKAIOutboxEvent{}).Where("topic = ?", ShopTopicFulfillment).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	require.NoError(t, db.First(&order, order.ID).Error)
	assert.Equal(t, "paid", order.PaymentState)
	receipt.AmountMinor++
	_, err = SaveShopVerifiedReceipt(db, ShopVerifiedReceiptInput{Receipt: receipt, Verified: true, Now: 203})
	assert.ErrorIs(t, err, ErrShopEvidence)
	require.NoError(t, db.First(matched, matched.ID).Error)
	assert.Equal(t, int64(100), matched.AmountMinor)
}

func TestShopDistinctExtraReceiptKeepsItsOwnPaymentEvent(t *testing.T) {
	db := shopTestDB(t)
	order, _, receipt := shopTestOrder(t, db)
	first, err := SaveShopVerifiedReceipt(db, ShopVerifiedReceiptInput{Receipt: receipt, Verified: true, Now: 200})
	require.NoError(t, err)
	first, err = MatchShopPaymentReceipt(db, first.ID, 200)
	require.NoError(t, err)
	require.Equal(t, "matched", first.ProcessingState)
	receipt.ProviderTransactionID = "another-provider-transaction"
	extra, err := SaveShopVerifiedReceipt(db, ShopVerifiedReceiptInput{Receipt: receipt, Verified: true, Now: 201})
	require.NoError(t, err)
	extra, err = MatchShopPaymentReceipt(db, extra.ID, 201)
	require.NoError(t, err)
	require.Equal(t, "extra", extra.ProcessingState)
	_, err = MatchShopPaymentReceipt(db, extra.ID, 202)
	require.NoError(t, err)
	var events []ShopEvent
	require.NoError(t, db.Where("order_id = ? AND kind = ? AND visibility = ?", order.ID, "payment_received", "public").Order("id").Find(&events).Error)
	require.Len(t, events, 2, "two distinct provider transactions are two actual received payments")
	require.NotNil(t, events[0].ReceiptID)
	require.NotNil(t, events[1].ReceiptID)
	assert.Equal(t, first.ID, *events[0].ReceiptID)
	assert.Equal(t, extra.ID, *events[1].ReceiptID)
	require.NoError(t, db.First(&order, order.ID).Error)
	require.NotNil(t, order.EffectiveReceiptID)
	assert.Equal(t, first.ID, *order.EffectiveReceiptID)
	var count int64
	require.NoError(t, db.Model(&KKAIOutboxEvent{}).Where("topic = ?", ShopTopicFulfillment).Count(&count).Error)
	assert.Equal(t, int64(1), count, "extra payment cannot create another delivery")
}

func TestShopLateReceiptForRetiredBuyerRetainsResponsibility(t *testing.T) {
	db := shopTestDB(t)
	order, _, receipt := shopTestOrder(t, db)
	require.NoError(t, db.Create(&ShopUserState{UserID: 1, TenantID: PlatformTenantID, Version: 1, RetiredAt: 180}).Error)
	stored, err := SaveShopVerifiedReceipt(db, ShopVerifiedReceiptInput{Receipt: receipt, Verified: true, Now: 200})
	require.NoError(t, err)
	stored, err = MatchShopPaymentReceipt(db, stored.ID, 200)
	require.NoError(t, err)
	assert.Equal(t, "review", stored.ProcessingState)
	assert.Equal(t, "BUYER_RETIRED", stored.ExceptionCode)
	require.NotNil(t, stored.OrderID)
	assert.Equal(t, order.ID, *stored.OrderID)
	require.NoError(t, db.First(&order, order.ID).Error)
	assert.Equal(t, "unpaid", order.PaymentState)
	pending, err := shopOrderHasResponsibilities(db, &order, true)
	require.NoError(t, err)
	assert.True(t, pending)
}

func TestShopRefundUnknownCannotRetryAndCancellationReopensSafely(t *testing.T) {
	db := shopTestDB(t)
	_, _, receipt := shopTestOrder(t, db)
	stored, err := SaveShopVerifiedReceipt(db, ShopVerifiedReceiptInput{Receipt: receipt, Verified: true, Now: 200})
	require.NoError(t, err)
	stored, err = MatchShopPaymentReceipt(db, stored.ID, 200)
	require.NoError(t, err)
	evidence, err := RegisterShopPaymentEvidence(db, ShopEvidenceInput{Command: shopTestCommand("evidence-1", stored.Version, 210), ReceiptPublicID: stored.PublicID, Evidence: ShopPaymentEvidence{Source: "merchant_console", ProviderTransactionID: stored.ProviderTransactionID, AmountMinor: 100, Currency: "CNY", CurrencyExponent: 2, Reference: "merchant/transaction", Summary: "Not refunded", ObservedStatus: "not_refunded", ObservedAt: 210}})
	require.NoError(t, err)
	require.NoError(t, db.First(stored, stored.ID).Error)
	refund, err := ApproveShopRefund(db, ShopRefundApproveInput{Command: shopTestCommand("approve-1", stored.Version, 211), ReceiptPublicID: stored.PublicID, Kind: "goods", Reason: "approved", EvidenceIDs: []string{evidence.PublicID}})
	require.NoError(t, err)
	refund, err = MutateShopRefund(db, ShopRefundActionInput{Command: shopTestCommand("attempt-1", refund.Version, 212), RefundPublicID: refund.PublicID, Action: "attempt", Reason: "prepare", EvidenceIDs: []string{evidence.PublicID}})
	require.NoError(t, err)
	var attempt ShopRefundAttempt
	require.NoError(t, db.Where("refund_case_id = ?", refund.ID).First(&attempt).Error)
	refund, err = MutateShopRefund(db, ShopRefundActionInput{Command: shopTestCommand("cancel-1", refund.Version, 213), RefundPublicID: refund.PublicID, Action: "cancel", Reason: "not executed", EvidenceIDs: []string{evidence.PublicID}})
	require.NoError(t, err)
	require.NoError(t, db.First(&attempt, attempt.ID).Error)
	assert.Equal(t, "failed", attempt.State)
	assert.Equal(t, "cancelled_not_executed", attempt.FailureCode)
	require.NoError(t, db.First(stored, stored.ID).Error)
	refund, err = ApproveShopRefund(db, ShopRefundApproveInput{Command: shopTestCommand("approve-2", stored.Version, 214), ReceiptPublicID: stored.PublicID, Kind: "goods", Reason: "approved again", EvidenceIDs: []string{evidence.PublicID}})
	require.NoError(t, err)
	refund, err = MutateShopRefund(db, ShopRefundActionInput{Command: shopTestCommand("attempt-2", refund.Version, 215), RefundPublicID: refund.PublicID, Action: "attempt", Reason: "prepare again", EvidenceIDs: []string{evidence.PublicID}})
	require.NoError(t, err)
	attempt = ShopRefundAttempt{}
	require.NoError(t, db.Where("refund_case_id = ? AND attempt_no = ?", refund.ID, 2).First(&attempt).Error)
	refund, err = MutateShopRefund(db, ShopRefundActionInput{Command: shopTestCommand("record-2", refund.Version, 216), RefundPublicID: refund.PublicID, AttemptPublicID: attempt.PublicID, Action: "record", Reason: "channel operation", OperatedAt: 216, ProviderRefundRef: "refund-2"})
	require.NoError(t, err)
	assert.Equal(t, "unknown", refund.State)
	_, err = MutateShopRefund(db, ShopRefundActionInput{Command: shopTestCommand("attempt-3", refund.Version, 217), RefundPublicID: refund.PublicID, Action: "attempt", Reason: "must not repeat", EvidenceIDs: []string{evidence.PublicID}})
	assert.ErrorIs(t, err, ErrShopRefund)
	amounts, err := GetShopRefundableAmounts(db, ShopScope{UserID: 2, TenantID: PlatformTenantID, Admin: true}, stored.PublicID)
	require.NoError(t, err)
	assert.Equal(t, map[string]int64{"goods": 0, "extra": 0, "mismatch": 0}, amounts)
}

func TestShopRetentionHonorsViewWindowHoldsAndUnassignedInventory(t *testing.T) {
	db := shopTestDB(t)
	order, attempt, _ := shopTestOrder(t, db)
	require.NoError(t, db.Model(&attempt).Updates(map[string]any{"create_state": "closed", "close_state": "closed"}).Error)
	require.NoError(t, db.Model(&order).Updates(map[string]any{"purchase_state": "closed", "delivery_state": "delivered", "closed_at": 100, "buyer_input_ciphertext": "buyer-secret", "buyer_input_key_version": "v1"}).Error)
	stock := ShopStockUnit{ShopRecord: ShopRecord{PublicID: "stock-sold", Version: 1}, ProductID: order.ProductID, State: "sold", SoldOrderID: &order.ID, IdentityDomainHash: "domain", FingerprintKeyVersion: "fp1", IdentityFingerprint: "identity", SecretCiphertext: "stock-secret", SecretKeyVersion: "v1", SecretRetentionUntil: 200}
	require.NoError(t, db.Create(&stock).Error)
	unassigned := ShopStockUnit{ShopRecord: ShopRecord{PublicID: "stock-available", Version: 1}, ProductID: order.ProductID, State: "available", IdentityDomainHash: "domain", FingerprintKeyVersion: "fp1", IdentityFingerprint: "unassigned", SecretCiphertext: "available-secret", SecretKeyVersion: "v1", SecretRetentionUntil: 1}
	require.NoError(t, db.Create(&unassigned).Error)
	delivery := ShopDelivery{ShopRecord: ShopRecord{PublicID: "delivery", Version: 1}, OrderID: order.ID, VersionNo: 1, StockUnitID: &stock.ID, SecretRetentionUntil: 200, ViewExpiresAt: 300, DeliveredAt: 100, RetentionHold: true}
	require.NoError(t, db.Create(&delivery).Error)
	assert.ErrorIs(t, PurgeShopOrderSecrets(db, order.PublicID, 250), ErrShopResponsibilities)
	require.NoError(t, db.Model(&delivery).Update("retention_hold", false).Error)
	require.NoError(t, PurgeShopOrderSecrets(db, order.PublicID, 250))
	require.NoError(t, db.First(&stock, stock.ID).Error)
	assert.Equal(t, "stock-secret", stock.SecretCiphertext)
	require.NoError(t, PurgeShopOrderSecrets(db, order.PublicID, 300))
	require.NoError(t, db.First(&stock, stock.ID).Error)
	assert.Empty(t, stock.SecretCiphertext)
	assert.Equal(t, "identity", stock.IdentityFingerprint)
	require.NoError(t, db.First(&unassigned, unassigned.ID).Error)
	assert.Equal(t, "available-secret", unassigned.SecretCiphertext)
	require.NoError(t, db.First(&order, order.ID).Error)
	assert.Empty(t, order.BuyerInputCiphertext)
	assert.Equal(t, int64(250), order.BuyerInputPurgedAt)
}

func TestShopWorkerFencePreventsStaleBusinessWrites(t *testing.T) {
	db := shopTestDB(t)
	order, attempt, _ := shopTestOrder(t, db)
	now := time.Now().Unix()
	require.NoError(t, db.Create(&KKAIJobLease{LeaseName: "shop", Holder: "worker", Fence: 2, LeaseUntil: now + 3600, UpdatedAt: now}).Error)
	event := KKAIOutboxEvent{EventKey: "shop-test", Topic: ShopTopicReceipt, Status: KKAIOutboxStatusPending, LockedBy: "worker", LockedAt: now}
	require.NoError(t, db.Create(&event).Error)
	fence := ShopWorkerFence{EventID: event.ID, LockedBy: "worker", LockTimeout: time.Minute, LeaderName: "shop", LeaderHolder: "worker", LeaderFence: 1}
	err := HoldShopPaymentForReview(WithShopWorkerFence(db, fence), attempt.PublicID, "refund_signal", "provider-proof", now)
	assert.ErrorIs(t, err, ErrShopWorkerFenceLost)
	require.NoError(t, db.First(&order, order.ID).Error)
	assert.False(t, order.RiskHold)
	fence.LeaderFence = 2
	require.NoError(t, HoldShopPaymentForReview(WithShopWorkerFence(db, fence), attempt.PublicID, "refund_signal", "provider-proof", now))
	require.NoError(t, db.First(&order, order.ID).Error)
	assert.True(t, order.RiskHold)
	require.NoError(t, db.Model(&event).Update("locked_by", "replacement").Error)
	assert.ErrorIs(t, HoldShopPaymentForReview(WithShopWorkerFence(db, fence), attempt.PublicID, "stale", "stale-proof", now), ErrShopWorkerFenceLost)
	require.NoError(t, db.First(&order, order.ID).Error)
	assert.Equal(t, "refund_signal", order.RiskReasonCode)
}

func TestShopCommittedReplayRechecksActorAndTenant(t *testing.T) {
	db := shopTestDB(t)
	c := shopTestCommand("safe-replay", 1, 100)
	require.NoError(t, shopRemember(db, c, "order-note", "so_test", "order", "so_test", nil))
	scope := ShopScope{UserID: 2, TenantID: PlatformTenantID, Admin: true}
	result, err := GetShopIdempotency(db, scope, "order-note", "so_test", c.Key)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NoError(t, db.Model(&User{}).Where("id = ?", 2).Update("role", common.RoleCommonUser).Error)
	_, err = GetShopIdempotency(db, scope, "order-note", "so_test", c.Key)
	assert.ErrorIs(t, err, ErrShopNotFound)
	scope.Admin = false
	require.NoError(t, db.Model(&User{}).Where("id = ?", 2).Update("status", common.UserStatusDisabled).Error)
	_, err = GetShopIdempotency(db, scope, "order-note", "so_test", c.Key)
	assert.ErrorIs(t, err, ErrShopNotFound)
}

func TestShopOrderReadPreservesDatabaseErrorsAndHidesInactiveUsers(t *testing.T) {
	for _, tt := range []struct {
		name    string
		wantErr error
	}{
		{name: "cancelled_database_query", wantErr: context.Canceled},
		{name: "missing_user", wantErr: ErrShopNotFound},
		{name: "disabled_user", wantErr: ErrShopNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := shopTestDB(t)
			order, _, _ := shopTestOrder(t, db)
			scope := ShopScope{UserID: order.UserID, TenantID: PlatformTenantID}
			_, err := GetShopOrder(db, scope, order.PublicID)
			require.NoError(t, err)
			switch tt.name {
			case "cancelled_database_query":
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				db = db.WithContext(ctx)
			case "missing_user":
				require.NoError(t, db.Delete(&User{}, order.UserID).Error)
			case "disabled_user":
				require.NoError(t, db.Model(&User{}).Where("id = ?", order.UserID).Update("status", common.UserStatusDisabled).Error)
			}
			result, err := GetShopOrder(db, scope, order.PublicID)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, result)
		})
	}
}

func TestShopDeletionGateAllowsOldSchemaAndFailsClosedForPartialSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return CheckShopUserDeletionTx(tx, 1) }))
	require.NoError(t, db.AutoMigrate(&ShopUserState{}))
	shopSchemaChecker.RLock()
	previous := shopSchemaChecker.check
	shopSchemaChecker.RUnlock()
	t.Cleanup(func() { RegisterShopSchemaChecker(previous) })
	sentinel := errors.New("canonical schema mismatch")
	RegisterShopSchemaChecker(func(tx *gorm.DB) error { return sentinel })
	assert.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error { return CheckShopUserDeletionTx(tx, 1) }), sentinel)
}

func TestShopIncompleteReplacementPausesOriginalSLAOnce(t *testing.T) {
	db := shopTestDB(t)
	order, _, _ := shopTestOrder(t, db)
	snapshot := ShopOrderSnapshot{DeliveryType: "manual", Terms: ShopProductTerms{BuyerInputSchema: []ShopField{{Key: "login", Required: true}}, DeliveryPolicy: ShopDeliveryPolicy{SLA: &ShopSLA{Clock: "elapsed", Timezone: "UTC", DurationMinutes: 10, PauseOnMissingInput: true}}}}
	encoded, err := common.Marshal(snapshot)
	require.NoError(t, err)
	require.NoError(t, db.Model(&order).Updates(map[string]any{"snapshot_json": string(encoded), "payment_state": "paid", "paid_at": 100, "sla_started_at": 100, "original_delivery_due_at": 700, "delivery_due_at": 700}).Error)
	c := shopTestCommand("input-incomplete", 1, 200)
	c.ActorID = 1
	c.Admin = false
	updated, err := UpdateShopBuyerInput(db, ShopBuyerInputUpdate{Command: c, OrderPublicID: order.PublicID, Secret: ShopSecret{Ciphertext: "replacement", KeyVersion: "v1"}})
	require.NoError(t, err)
	assert.Equal(t, "awaiting_input", updated.DeliveryState)
	assert.Equal(t, int64(100), updated.InputCompleteAt)
	assert.Equal(t, int64(200), updated.SLAPauseStartedAt)
	c.Key = "input-incomplete-again"
	c.ExpectedVersion = updated.Version
	c.Now = 210
	updated, err = UpdateShopBuyerInput(db, ShopBuyerInputUpdate{Command: c, OrderPublicID: order.PublicID, Secret: ShopSecret{Ciphertext: "replacement2", KeyVersion: "v1"}})
	require.NoError(t, err)
	assert.Equal(t, int64(200), updated.SLAPauseStartedAt)
	c.Key = "input-complete"
	c.ExpectedVersion = updated.Version
	c.Now = 230
	updated, err = UpdateShopBuyerInput(db, ShopBuyerInputUpdate{Command: c, OrderPublicID: order.PublicID, Secret: ShopSecret{Ciphertext: "complete", KeyVersion: "v1"}, InputKeys: []string{"login"}})
	require.NoError(t, err)
	assert.Equal(t, "pending", updated.DeliveryState)
	assert.Equal(t, int64(100), updated.SLAStartedAt)
	assert.Equal(t, int64(700), updated.OriginalDeliveryDueAt)
	assert.Equal(t, int64(730), updated.DeliveryDueAt)
	assert.Equal(t, int64(30), updated.SLAPausedSeconds)
	assert.Zero(t, updated.SLAPauseStartedAt)
}

func TestShopOrderSummariesApplyFundsFencesAndPagination(t *testing.T) {
	db := shopTestDB(t)
	order, _, receipt := shopTestOrder(t, db)
	stored, err := SaveShopVerifiedReceipt(db, ShopVerifiedReceiptInput{Receipt: receipt, Verified: true, Now: 200})
	require.NoError(t, err)
	stored, err = MatchShopPaymentReceipt(db, stored.ID, 200)
	require.NoError(t, err)
	require.NoError(t, db.Model(&order).Update("delivery_state", "exception").Error)
	scope := ShopScope{UserID: 2, TenantID: PlatformTenantID, Admin: true}
	rows, count, err := ListShopOrderSummaries(db, scope, ShopOrderFilter{StatusGroup: "delivery", Limit: 1})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(1), count)
	assert.False(t, ShopOrderDeliveryBlocked(&rows[0]))
	assert.Contains(t, ShopAdminOrderActions(&rows[0]), "retry-delivery")
	amounts, err := GetShopRefundableAmounts(db, scope, stored.PublicID)
	require.NoError(t, err)
	assert.Equal(t, int64(100), amounts["goods"])
	adjustment := ShopPaymentAdjustment{ShopRecord: ShopRecord{PublicID: "adjustment", Version: 1}, ReceiptID: stored.ID, AdjustmentKey: "adjustment-key", AmountMinor: 40, Currency: "CNY", CurrencyExponent: 2, State: "disputed", Kind: "chargeback"}
	require.NoError(t, db.Create(&adjustment).Error)
	rows, count, err = ListShopOrderSummaries(db, scope, ShopOrderFilter{StatusGroup: "delivery", Limit: 1})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.True(t, ShopOrderDeliveryBlocked(&rows[0]))
	assert.NotContains(t, ShopAdminOrderActions(&rows[0]), "retry-delivery")
	_, err = MutateShopOrder(db, ShopOrderActionInput{Command: shopTestCommand("retry-blocked", rows[0].Order.Version, 201), OrderPublicID: order.PublicID, Action: "retry-delivery", Reason: "retry"})
	assert.ErrorIs(t, err, ErrShopRevealBlocked)
	amounts, err = GetShopRefundableAmounts(db, scope, stored.PublicID)
	require.NoError(t, err)
	assert.Zero(t, amounts["goods"])
	rows, count, err = ListShopOrderSummaries(db, scope, ShopOrderFilter{StatusGroup: "delivered", Limit: 1})
	require.NoError(t, err)
	assert.Empty(t, rows)
	assert.Zero(t, count)
}

func TestShopRefundEvidenceAndAdjustmentsPreserveMoneyIdentity(t *testing.T) {
	db := shopTestDB(t)
	_, _, receipt := shopTestOrder(t, db)
	stored, err := SaveShopVerifiedReceipt(db, ShopVerifiedReceiptInput{Receipt: receipt, Verified: true, Now: 200})
	require.NoError(t, err)
	first, err := shopAcceptAdjustment(db, shopTestCommand("unused-key", 1, 201), stored, "refund", "provider-refund", 60, "succeeded", nil, "provider_query", 201)
	require.NoError(t, err)
	duplicate, err := shopAcceptAdjustment(db, shopTestCommand("unused-key", 1, 202), stored, "refund", "provider-refund", 60, "succeeded", nil, "provider_query", 202)
	require.NoError(t, err)
	assert.Equal(t, first.ID, duplicate.ID)
	_, err = shopAcceptAdjustment(db, shopTestCommand("unused-key", 1, 203), stored, "refund", "provider-refund", 50, "succeeded", nil, "provider_query", 203)
	assert.ErrorIs(t, err, ErrShopEvidence)
	_, err = shopAcceptAdjustment(db, shopTestCommand("unused-key", 1, 203), stored, "refund", "second-refund", 41, "succeeded", nil, "provider_query", 203)
	assert.ErrorIs(t, err, ErrShopRefund)
	_, err = shopAcceptAdjustment(db, shopTestCommand("unused-key", 1, 203), stored, "refund", "wrapped-refund", -1, "succeeded", nil, "provider_query", 203)
	assert.ErrorIs(t, err, ErrShopRefund)
	var count int64
	require.NoError(t, db.Model(&ShopPaymentAdjustment{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	evidence := ShopPaymentEvidence{ShopRecord: ShopRecord{PublicID: "proof", Version: 1}, ReceiptID: stored.ID, Source: "merchant_console", Provider: stored.Provider, ProviderAccountRef: stored.ProviderAccountRef, Environment: stored.Environment, ProviderTransactionID: stored.ProviderTransactionID, AmountMinor: 100, Currency: "CNY", CurrencyExponent: 2, ObservedStatus: "not_refunded", ObservedAt: 200, Reference: "proof"}
	require.NoError(t, db.Create(&evidence).Error)
	require.NoError(t, shopCheckFundsEvidence(db, stored, []string{evidence.PublicID}, 100, "", []string{"not_refunded"}))
	corrected := evidence
	corrected.ShopRecord = ShopRecord{PublicID: "proof-correction", Version: 1}
	corrected.CorrectionOfID = &evidence.ID
	require.NoError(t, db.Create(&corrected).Error)
	assert.ErrorIs(t, shopCheckFundsEvidence(db, stored, []string{evidence.PublicID}, 100, "", []string{"not_refunded"}), ErrShopEvidence)
	other := *stored
	other.ID++
	assert.ErrorIs(t, shopCheckFundsEvidence(db, &other, []string{corrected.PublicID}, 100, "", []string{"not_refunded"}), ErrShopEvidence)
}

func TestShopDeletionAllowsSettledRefundButBlocksUnresolvedPayment(t *testing.T) {
	db := shopTestDB(t)
	order, _, receipt := shopTestOrder(t, db)
	stored, err := SaveShopVerifiedReceipt(db, ShopVerifiedReceiptInput{Receipt: receipt, Verified: true, Now: 200})
	require.NoError(t, err)
	stored, err = MatchShopPaymentReceipt(db, stored.ID, 200)
	require.NoError(t, err)
	shopSchemaChecker.RLock()
	previous := shopSchemaChecker.check
	shopSchemaChecker.RUnlock()
	RegisterShopSchemaChecker(func(tx *gorm.DB) error { return nil })
	t.Cleanup(func() { RegisterShopSchemaChecker(previous) })
	assert.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error { return CheckShopUserDeletionTx(tx, 1) }), ErrShopResponsibilities)
	adjustment := ShopPaymentAdjustment{ShopRecord: ShopRecord{PublicID: "full-refund", Version: 1}, ReceiptID: stored.ID, AdjustmentKey: "full-refund", AmountMinor: 100, Currency: "CNY", CurrencyExponent: 2, State: "succeeded", Kind: "refund"}
	require.NoError(t, db.Create(&adjustment).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return CheckShopUserDeletionTx(tx, 1) }))
	var state ShopUserState
	require.NoError(t, db.First(&state, 1).Error)
	assert.Positive(t, state.RetiredAt)
	receipt.ProviderTransactionID = "late-extra"
	extra, err := SaveShopVerifiedReceipt(db, ShopVerifiedReceiptInput{Receipt: receipt, Verified: true, Now: 201})
	require.NoError(t, err)
	extra, err = MatchShopPaymentReceipt(db, extra.ID, 201)
	require.NoError(t, err)
	require.NotNil(t, extra.OrderID)
	assert.Equal(t, order.ID, *extra.OrderID)
	assert.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error { return CheckShopUserDeletionTx(tx, 1) }), ErrShopResponsibilities)
}

func TestShopPaymentUnknownReminderSurvivesRepeatedChecks(t *testing.T) {
	db := shopTestDB(t)
	order, attempt, _ := shopTestOrder(t, db)
	require.NoError(t, db.Create(&ShopSettings{ID: 1, ReminderPolicyJSON: `{"payment_unknown_seconds":60,"delivery_exception_seconds":60,"refund_verification_seconds":60}`}).Error)
	require.NoError(t, db.Model(&attempt).Updates(map[string]any{"create_state": "unknown", "updated_at": 199}).Error)
	recorded, err := RecordShopReminder(db, order.PublicID, "payment_unknown", 200)
	require.NoError(t, err)
	assert.True(t, recorded)
	require.NoError(t, db.Model(&attempt).Update("updated_at", 201).Error)
	recorded, err = RecordShopReminder(db, order.PublicID, "payment_unknown", 202)
	require.NoError(t, err)
	assert.False(t, recorded)
	var count int64
	require.NoError(t, db.Model(&ShopEvent{}).Where("kind = ?", "reminder_payment_unknown").Count(&count).Error)
	assert.Equal(t, int64(1), count)
}
