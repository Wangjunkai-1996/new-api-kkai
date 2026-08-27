package service

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestImageStudioMaximumPreconsumeIncludesCompletionAndAllRatios(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	common.SetContextKey(c, constant.ContextKeyIsImageStudio, true)
	relayInfo := &relaycommon.RelayInfo{}
	price := types.PriceData{
		ModelRatio: 3, CompletionRatio: 2,
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.5},
	}
	price.AddOtherRatio("n", 2)

	require.NoError(t, ApplyImageStudioMaximumPreconsume(
		c, relayInfo, &price, 100, &types.TokenCountMeta{MaxTokens: 1_000},
	))
	require.Equal(t, 7_500, price.QuotaToPreConsume)
	require.Equal(t, price.QuotaToPreConsume, relayInfo.PriceData.QuotaToPreConsume)
}

func TestImageStudioMaximumPreconsumeUsesRequestCountWithoutChangingFinalRatioBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	common.SetContextKey(c, constant.ContextKeyIsImageStudio, true)
	relayInfo := &relaycommon.RelayInfo{}
	price := types.PriceData{
		ModelRatio: 1, CompletionRatio: 1,
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}

	require.NoError(t, ApplyImageStudioMaximumPreconsume(
		c, relayInfo, &price, 100, &types.TokenCountMeta{
			MaxTokens: 100, BillingRatios: map[string]float64{"n": 4},
		},
	))
	require.Equal(t, (common.PreConsumedQuota+100)*4, price.QuotaToPreConsume)
	require.Nil(t, price.OtherRatios())
	require.Nil(t, relayInfo.PriceData.OtherRatios())
}

func TestImageStudioMaximumPreconsumePreservesCompletePriceQuotes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	common.SetContextKey(c, constant.ContextKeyIsImageStudio, true)

	relayInfo := &relaycommon.RelayInfo{}
	price := types.PriceData{UsePrice: true, QuotaToPreConsume: 321}
	require.NoError(t, ApplyImageStudioMaximumPreconsume(
		c, relayInfo, &price, 100, &types.TokenCountMeta{MaxTokens: 1_000},
	))
	require.Equal(t, 321, price.QuotaToPreConsume)
	require.Equal(t, 321, relayInfo.PriceData.QuotaToPreConsume)
}

func TestImageStudioMaximumPreconsumeRejectsUnboundedTieredExpression(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	common.SetContextKey(c, constant.ContextKeyIsImageStudio, true)
	relayInfo := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{BillingMode: "tiered_expr"},
	}
	price := types.PriceData{QuotaToPreConsume: 654}
	require.ErrorIs(t, ApplyImageStudioMaximumPreconsume(
		c, relayInfo, &price, 100, &types.TokenCountMeta{MaxTokens: 1_000},
	), ErrImageModelBillingUnsupported)
}

func TestImagePricingActualCountKeepsSnapshotImmutable(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ImagePricingSnapshot: &imagepricing.Snapshot{RequestedCount: 2},
		ImageOutputCount:     1,
	}
	info.PriceData.AddOtherRatio("n", 1)

	actual, err := imagePricingActualCount(info)

	require.NoError(t, err)
	require.Equal(t, 1, actual)
	require.Equal(t, 2, info.ImagePricingSnapshot.RequestedCount)
}

func TestImagePricingActualCountRejectsFractionalValues(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ImagePricingSnapshot: &imagepricing.Snapshot{RequestedCount: 2},
		ImageOutputCount:     1,
	}
	info.PriceData.AddOtherRatio("n", 1.5)

	_, err := imagePricingActualCount(info)

	require.ErrorIs(t, err, ErrImageStudioQuoteStale)
}

func TestImageStudioDeliveryBillingProratesRatioQuotaByArchivedOutputs(t *testing.T) {
	c, relayInfo, db, generation := newImageStudioDeliveryBillingFixture(t, 9201, 4, 400)
	relayInfo.ImageOutputCount = 3

	require.NoError(t, PrepareImageStudioBillingCommit(c, relayInfo, 30, model.RecordConsumeLogParams{
		ChannelId: 3, ModelName: generation.Model, TokenId: generation.TokenID, Quota: 300,
	}))
	require.NoError(t, PrepareImageStudioDeliveryBilling(c, 3, 2))

	accounting, err := model.GetImageGenerationAccounting(context.Background(), db, generation.ID)
	require.NoError(t, err)
	require.Equal(t, 2, accounting.OutputCount)
	require.Equal(t, 200, accounting.TargetQuota)
	require.Equal(t, 200, accounting.LogParams.Quota)
	require.NoError(t, CommitImageStudioBilling(c))
	require.NoError(t, FinishImageGeneration(
		context.Background(), db, generation.ID, model.ImageGenerationStatusPartial,
		2, accounting.TargetQuota, "archive", "partial_archive", "some image results could not be archived",
	))

	var user model.User
	require.NoError(t, db.First(&user, 9201).Error)
	require.EqualValues(t, 800, user.Quota)
	require.EqualValues(t, 200, user.UsedQuota)
	require.EqualValues(t, 1, user.RequestCount)
}

func TestImageStudioDeliveryBillingUsesCanonicalShortResponseWithoutAdapterCount(t *testing.T) {
	c, relayInfo, db, generation := newImageStudioDeliveryBillingFixture(t, 9202, 4, 400)
	relayInfo.ImagePricingSnapshot = &imagepricing.Snapshot{
		PolicyVersion: "test-v1", PolicyHash: strings.Repeat("a", 64),
		Model: generation.Model, Size: "1024x1024", Tier: "1k",
		UnitPrice: 1, QuotaPerUnit: 100, GroupRatio: 1, RequestedCount: 4,
	}
	relayInfo.PriceData.UsePrice = true
	relayInfo.PriceData.AddOtherRatio("n", 4)

	require.Zero(t, relayInfo.ImageOutputCount)
	require.NoError(t, PrepareImageStudioBillingCommit(c, relayInfo, 30, model.RecordConsumeLogParams{
		ChannelId: 3, ModelName: generation.Model, TokenId: generation.TokenID, Quota: 400,
	}))
	// The canonical response contained two usable outputs even though the
	// adapter did not publish ImageOutputCount for the four-output request.
	require.NoError(t, PrepareImageStudioDeliveryBilling(c, 2, 2))

	accounting, err := model.GetImageGenerationAccounting(context.Background(), db, generation.ID)
	require.NoError(t, err)
	require.Equal(t, 2, accounting.OutputCount)
	require.Equal(t, 2, accounting.PricingActualCount)
	require.Equal(t, 200, accounting.TargetQuota)
	require.Equal(t, 200, accounting.LogParams.Quota)
	require.NoError(t, CommitImageStudioBilling(c))
	require.NoError(t, FinishImageGeneration(
		context.Background(), db, generation.ID, model.ImageGenerationStatusPartial,
		2, accounting.TargetQuota, "", "", "",
	))

	var user model.User
	require.NoError(t, db.First(&user, 9202).Error)
	require.EqualValues(t, 800, user.Quota)
	require.EqualValues(t, 200, user.UsedQuota)
	require.EqualValues(t, 1, user.RequestCount)
}

func TestImageStudioDeliveryBillingProratesCanonicalShortRatioResponseWithoutAdapterCount(t *testing.T) {
	c, relayInfo, db, generation := newImageStudioDeliveryBillingFixture(t, 9203, 4, 400)

	require.Zero(t, relayInfo.ImageOutputCount)
	require.NoError(t, PrepareImageStudioBillingCommit(c, relayInfo, 30, model.RecordConsumeLogParams{
		ChannelId: 3, ModelName: generation.Model, TokenId: generation.TokenID, Quota: 400,
	}))
	require.NoError(t, PrepareImageStudioDeliveryBilling(c, 2, 2))

	accounting, err := model.GetImageGenerationAccounting(context.Background(), db, generation.ID)
	require.NoError(t, err)
	require.Equal(t, 2, accounting.OutputCount)
	require.Equal(t, 200, accounting.TargetQuota)
	require.Equal(t, 200, accounting.LogParams.Quota)
	require.NoError(t, CommitImageStudioBilling(c))
	require.NoError(t, FinishImageGeneration(
		context.Background(), db, generation.ID, model.ImageGenerationStatusPartial,
		2, accounting.TargetQuota, "", "", "",
	))

	var user model.User
	require.NoError(t, db.First(&user, 9203).Error)
	require.EqualValues(t, 800, user.Quota)
	require.EqualValues(t, 200, user.UsedQuota)
	require.EqualValues(t, 1, user.RequestCount)
}

func TestImageStudioDeliveryBillingKeepsUsageQuotaForShortCountedResponse(t *testing.T) {
	c, relayInfo, db, generation := newImageStudioDeliveryBillingFixture(t, 9204, 4, 400)
	relayInfo.ImageOutputCount = 2

	require.NoError(t, PrepareImageStudioBillingCommit(c, relayInfo, 516, model.RecordConsumeLogParams{
		ChannelId: 3, ModelName: generation.Model, TokenId: generation.TokenID, Quota: 200,
	}))
	require.NoError(t, PrepareImageStudioDeliveryBilling(c, 2, 2))

	accounting, err := model.GetImageGenerationAccounting(context.Background(), db, generation.ID)
	require.NoError(t, err)
	require.Equal(t, 2, accounting.OutputCount)
	require.Equal(t, 200, accounting.TargetQuota)
	require.Equal(t, 200, accounting.LogParams.Quota)
	require.NoError(t, CommitImageStudioBilling(c))

	var user model.User
	require.NoError(t, db.First(&user, 9204).Error)
	require.EqualValues(t, 800, user.Quota)
	require.EqualValues(t, 200, user.UsedQuota)
}

func newImageStudioDeliveryBillingFixture(
	t *testing.T, userID int, requestedOutputs int, reservedQuota int,
) (*gin.Context, *relaycommon.RelayInfo, *gorm.DB, model.KKAIImageGeneration) {
	t.Helper()
	db := newImageAccountingRecoveryTestDB(t)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	require.NoError(t, db.Create(&model.User{
		Id: userID, Username: "image-delivery", Password: "password", Quota: 1_000,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id: 3, Name: "image-delivery-channel", Key: "test-key", Status: 1, Type: 1,
	}).Error)
	generation := seedRecoverableImageGeneration(t, db, userID, requestedOutputs)
	_, err := model.ReserveImageGenerationBilling(
		context.Background(), db, generation.ID, model.TaskBillingSourceWallet, reservedQuota,
	)
	require.NoError(t, err)
	require.NoError(t, model.MarkImageGenerationDispatching(context.Background(), db, generation.ID))

	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
	c.Set("username", "image-delivery")
	require.NoError(t, SetImageStudioGenerationID(c, generation.ID))
	require.NoError(t, SetImageStudioBillingGuard(c, reservedQuota))
	requested := uint(requestedOutputs)
	relayInfo := &relaycommon.RelayInfo{
		UserId: userID, TokenId: generation.TokenID, OriginModelName: generation.Model,
		Request: &dto.ImageRequest{Model: generation.Model, Prompt: generation.Prompt, N: &requested},
	}
	relayInfo.Billing = &imageGenerationBillingSession{
		db: db, generationID: generation.ID, relayInfo: relayInfo, preConsumed: reservedQuota,
	}
	return c, relayInfo, db, generation
}
