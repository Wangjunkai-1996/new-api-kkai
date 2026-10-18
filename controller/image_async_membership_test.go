package controller

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func membershipImageAsyncAcceptanceFixture(t *testing.T, memberQuota int64, wallet int) (*gorm.DB, service.ImageAsyncSubmissionRequest, *service.NormalizedImageStudioSubmission, *model.MembershipAccount) {
	t.Helper()
	if !model.MembershipEnabled() {
		t.Skip("requires KKAI_MEMBERSHIP_ENABLED=true")
	}
	db, request, normalized := newImageAsyncAcceptanceFixture(t)
	require.NoError(t, db.AutoMigrate(model.MembershipModels()...))
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", normalized.UserID).Update("quota", wallet).Error)
	catalog, err := model.PublishMembershipCatalog(model.MembershipCatalog{
		SalesEnabled: true,
		Tiers:        []model.MembershipTier{{ID: "gold", Name: "Gold", Rank: 1, Style: "gold", Currency: "USD", PriceMinor: 1000, Quota: memberQuota, Enabled: true, Discounts: map[string]float64{service.ImageStudioTokenGroup: 0.5}}},
		Groups:       []model.MembershipGroup{{Key: service.ImageStudioTokenGroup, Enabled: true, Published: true}},
	}, 0, normalized.UserID)
	require.NoError(t, err)
	order, err := model.CreateMembershipOrder(model.MembershipOrderInput{UserID: normalized.UserID, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: model.PaymentProviderEpay, PaymentMethod: "alipay", TradeNo: "image-async-member", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	require.NoError(t, err)
	_, err = model.CompleteMembershipPayment(model.MembershipPaymentInput{TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: "image-async-paid", AmountMinor: order.AmountMinor, Currency: order.Currency, OneTime: true, Paid: true})
	require.NoError(t, err)
	self, err := model.GetMembershipSelf(normalized.UserID)
	require.NoError(t, err)
	require.NotNil(t, self.Account)
	claims, err := service.ValidateImageStudioQuote(normalized, time.Now())
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{UserId: normalized.UserID, TokenId: normalized.TokenID, IsPlayground: true, UsingGroup: service.ImageStudioTokenGroup, OriginModelName: normalized.Model, ChannelMeta: &relaycommon.ChannelMeta{}, ImagePricingSnapshot: claims.ImagePricingSnapshot, PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}}
	info.PriceData.AddOtherRatio("n", 1)
	input, err := service.PrepareMembershipReservation(info, 300)
	require.NoError(t, err)
	var snapshot types.MembershipPricing
	require.NoError(t, common.UnmarshalJsonStr(input.WalletSnapshot, &snapshot))
	quote, err := service.NewImageStudioQuote(normalized, int(input.WalletQuota), claims.OtherRatios, snapshot.Image, &snapshot)
	require.NoError(t, err)
	request.QuoteToken, normalized.QuoteToken = quote.QuoteToken, quote.QuoteToken
	return db, request, normalized, self.Account
}

func TestMembershipImageAsyncAcceptanceIsAtomicAndReplayIndependent(t *testing.T) {
	db, request, normalized, account := membershipImageAsyncAcceptanceFixture(t, 1000, 0)
	failure := errors.New("execution insert rolled back")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("member-image-acceptance-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "kkai_image_executions" {
			tx.AddError(failure)
		}
	}))
	_, err := service.AcceptAsyncImageSubmission(context.Background(), db, request, normalized, "member-atomic", "member-request", "127.0.0.1")
	require.ErrorIs(t, err, failure)
	require.NoError(t, db.Callback().Create().Remove("member-image-acceptance-failure"))
	require.NoError(t, db.First(account, account.ID).Error)
	assert.Zero(t, account.AmountReserved)
	for _, table := range []string{"kkai_membership_billing", "kkai_image_generations", "kkai_idempotency_keys", "kkai_outbox", "kkai_image_executions"} {
		var count int64
		require.NoError(t, db.Table(table).Count(&count).Error)
		assert.Zero(t, count, table)
	}
	generation, err := service.AcceptAsyncImageSubmission(context.Background(), db, request, normalized, "member-atomic", "member-request", "127.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, model.MembershipSource, generation.BillingSource)
	assert.Equal(t, 150, generation.ReservedQuota)
	require.NoError(t, db.Model(account).Updates(map[string]any{"expires_at": time.Now().Add(-time.Hour).Unix(), "revision": account.Revision + 1}).Error)
	normalized.QuoteToken = "expired-and-invalid"
	replayed, err := service.AcceptAsyncImageSubmission(context.Background(), db, request, normalized, "member-atomic", "retry", "127.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, generation.ID, replayed.ID)
	require.NoError(t, db.First(account, account.ID).Error)
	assert.EqualValues(t, 150, account.AmountReserved)
	var count int64
	require.NoError(t, db.Model(&model.MembershipBillingRecord{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestMembershipImageAsyncRejectsStaleEntitlementAndInsufficientFunds(t *testing.T) {
	for _, scenario := range []string{"revision_changed", "wallet_short", "old_unsigned_membership_quote"} {
		t.Run(scenario, func(t *testing.T) {
			db, request, normalized, account := membershipImageAsyncAcceptanceFixture(t, 100, 299)
			expected := model.ErrMembershipInsufficientQuota
			if scenario == "revision_changed" {
				require.NoError(t, db.Model(account).Update("revision", account.Revision+1).Error)
				expected = model.ErrMembershipQuoteChanged
			} else if scenario == "old_unsigned_membership_quote" {
				claims, err := service.ValidateImageStudioQuote(normalized, time.Now())
				require.NoError(t, err)
				quote, err := service.NewImageStudioQuote(normalized, claims.Quota, claims.OtherRatios, claims.ImagePricingSnapshot)
				require.NoError(t, err)
				normalized.QuoteToken, request.QuoteToken = quote.QuoteToken, quote.QuoteToken
				expected = service.ErrImageStudioQuoteStale
			}
			_, err := service.AcceptAsyncImageSubmission(context.Background(), db, request, normalized, "rejected", "request", "127.0.0.1")
			require.ErrorIs(t, err, expected)
			var count int64
			require.NoError(t, db.Model(&model.KKAIImageGeneration{}).Count(&count).Error)
			assert.Zero(t, count)
			require.NoError(t, db.Model(&model.MembershipBillingRecord{}).Count(&count).Error)
			assert.Zero(t, count)
			require.NoError(t, db.First(account, account.ID).Error)
			assert.Zero(t, account.AmountReserved)
			var user model.User
			require.NoError(t, db.First(&user, normalized.UserID).Error)
			assert.EqualValues(t, 299, user.Quota)
		})
	}
}

func TestMembershipImageAsyncRecoveryUsesOriginalSourceAndFence(t *testing.T) {
	for _, tc := range []struct {
		name             string
		memberQuota      int64
		wallet, reserved int
	}{
		{"membership", 1000, 0, 150},
		{"wallet", 100, 1000, 300},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, request, normalized, account := membershipImageAsyncAcceptanceFixture(t, tc.memberQuota, tc.wallet)
			generation, err := service.AcceptAsyncImageSubmission(context.Background(), db, request, normalized, "recover", "recovery", "127.0.0.1")
			require.NoError(t, err)
			now := time.Now().Unix()
			leader := model.KKAIJobLease{LeaseName: "member-image-test", Holder: "writer", Fence: 2, LeaseUntil: now + 300}
			require.NoError(t, db.Create(&leader).Error)
			var event model.KKAIOutboxEvent
			require.NoError(t, db.Where("topic = ?", service.ImageGenerateTopic).First(&event).Error)
			require.NoError(t, db.Model(&event).Updates(map[string]any{"locked_at": now, "locked_by": "claim-1"}).Error)
			require.NoError(t, db.Model(&model.KKAIImageExecution{}).Where("generation_id = ?", generation.ID).Updates(map[string]any{"state": model.ImageGenerationExecutionPreparing, "fence": "claim-1"}).Error)
			fence := model.ImageExecutionFence{EventID: event.ID, LockedBy: "claim-1", LockTimeout: 2 * time.Minute, LeaderName: leader.LeaseName, LeaderHolder: leader.Holder, LeaderFence: leader.Fence}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/internal/image", nil)
			require.NoError(t, service.SetImageStudioBillingGuard(c, 300))
			info := &relaycommon.RelayInfo{UserId: generation.UserID, TokenId: generation.TokenID, UsingGroup: service.ImageStudioTokenGroup}
			require.NoError(t, service.RestoreImageGenerationBilling(c, db, generation, info, fence))
			require.NoError(t, db.Model(account).Updates(map[string]any{"expires_at": now - 1, "revision": account.Revision + 1}).Error)
			require.NoError(t, service.RestoreImageGenerationBilling(c, db, generation, info, fence))
			assert.Equal(t, tc.reserved, info.FinalPreConsumedQuota)
			assert.Equal(t, tc.reserved, info.Billing.GetPreConsumedQuota())
			ratio := 1.0
			if tc.name == "membership" {
				ratio = 0.5
			}
			assert.Equal(t, ratio, info.ImagePricingSnapshot.GroupRatio)
			require.NoError(t, db.Model(&event).Update("locked_by", "claim-2").Error)
			info.Billing.Refund(c)
			require.ErrorIs(t, info.Billing.Settle(100), model.ErrImageExecutionFenceLost)
			record, err := model.GetMembershipBillingRecord(info.MembershipBillingRequestID)
			require.NoError(t, err)
			assert.Equal(t, "reserved", record.State)
			// A dispatched request is never re-sent. Without any durable delivered
			// output, Image Studio releases the original funding after its deadline.
			require.NoError(t, db.Model(&model.KKAIImageExecution{}).Where("generation_id = ?", generation.ID).Updates(map[string]any{"state": model.ImageGenerationExecutionUnknown, "payload": "invalid-on-purpose", "recovery_deadline_at": now - 1}).Error)
			require.NoError(t, db.Model(&event).Updates(map[string]any{"locked_by": "", "locked_at": 0}).Error)
			processor := service.NewKKAIOutboxProcessor(db, "member-recovery")
			require.NoError(t, processor.Register(service.ImageGenerateTopic, runImageGenerationEvent))
			ctx := service.WithImageWorkerLease(context.Background(), leader)
			batch, err := processor.ProcessBatch(ctx, 1)
			require.NoError(t, err)
			require.Equal(t, 1, batch.Delivered)
			require.NoError(t, db.First(generation, generation.ID).Error)
			assert.Equal(t, model.ImageGenerationStatusUnknown, generation.Status)
			assert.Equal(t, model.ImageGenerationBillingStateRefunded, generation.BillingState)
			record, err = model.GetMembershipBillingRecord(info.MembershipBillingRequestID)
			require.NoError(t, err)
			assert.Equal(t, "released", record.State)
			assert.Zero(t, record.ChargedQuota)
			require.NoError(t, db.First(account, account.ID).Error)
			assert.Zero(t, account.AmountReserved)
			assert.Zero(t, account.AmountUsed)
			var user model.User
			require.NoError(t, db.First(&user, normalized.UserID).Error)
			assert.EqualValues(t, tc.wallet, user.Quota)
		})
	}
}
