package controller

import (
	"bytes"
	"context"
	"errors"
	"image/color"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newImageAsyncAcceptanceFixture(t *testing.T) (*gorm.DB, service.ImageAsyncSubmissionRequest, *service.NormalizedImageStudioSubmission) {
	t.Helper()
	db, token := newImageStudioRelayTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.KKAIOutboxEvent{}, &model.KKAIImageExecution{}, &model.KKAIImageGenerationReference{}, &model.UserSubscription{}))
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", token.UserId).Update("quota", 1000).Error)
	oldAsync := image_studio_setting.Get().AsyncSubmissionEnabled
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"image_studio.async_submission_enabled": "true"}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"image_studio.async_submission_enabled": strconv.FormatBool(oldAsync)}))
	})
	request := service.ImageAsyncSubmissionRequest{TokenID: token.Id, Model: imageStudioEditTestModel, Prompt: "a lighthouse", Mode: service.ImageStudioModeGeneration}
	normalized, err := service.NormalizeAsyncImageSubmission(context.Background(), db, token.UserId, request)
	require.NoError(t, err)
	normalized.RelayRequest.Size = "1024x1024"
	snapshot := &imagepricing.Snapshot{PolicyVersion: "test", PolicyHash: strings.Repeat("a", 64), Model: request.Model, Size: normalized.RelayRequest.Size, Tier: "1k", UnitPrice: 3, QuotaPerUnit: 100, GroupRatio: 1, RequestedCount: 1}
	quote, err := service.NewImageStudioQuote(normalized, 300, map[string]float64{"n": 1}, snapshot)
	require.NoError(t, err)
	normalized.QuoteToken, request.QuoteToken = quote.QuoteToken, quote.QuoteToken
	return db, request, normalized
}

func TestImageAsyncAcceptanceRollbackLeavesNoReservationOrDebit(t *testing.T) {
	db, request, normalized := newImageAsyncAcceptanceFixture(t)
	failure := errors.New("execution insert failed")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("image-acceptance-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "kkai_image_executions" {
			tx.AddError(failure)
		}
	}))
	_, err := service.AcceptAsyncImageSubmission(context.Background(), db, request, normalized, "atomic-acceptance", "request-atomic", "127.0.0.1")
	require.ErrorIs(t, err, failure)
	var user model.User
	require.NoError(t, db.First(&user, normalized.UserID).Error)
	assert.EqualValues(t, 1000, user.Quota)
	for _, table := range []string{"kkai_image_generations", "kkai_idempotency_keys", "kkai_outbox", "kkai_image_executions"} {
		var count int64
		require.NoError(t, db.Table(table).Count(&count).Error)
		assert.Zero(t, count, table)
	}
	require.NoError(t, db.Callback().Create().Remove("image-acceptance-failure"))
	generation, err := service.AcceptAsyncImageSubmission(context.Background(), db, request, normalized, "atomic-acceptance", "request-atomic", "127.0.0.1")
	require.NoError(t, err)
	replay, err := service.AcceptAsyncImageSubmission(context.Background(), db, request, normalized, "atomic-acceptance", "request-retry", "127.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, generation.ID, replay.ID)
	require.NoError(t, db.First(&user, normalized.UserID).Error)
	assert.EqualValues(t, 700, user.Quota)
}

func TestImageAsyncAcceptanceAndAdoptionHaveNoTaskCountLimit(t *testing.T) {
	db, request, normalized := newImageAsyncAcceptanceFixture(t)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", normalized.UserID).Update("quota", 20_000).Error)
	generations := make([]model.KKAIImageGeneration, 0, 33)
	for index := 0; index < 33; index++ {
		key := "unlimited-image-" + strconv.Itoa(index)
		generation, err := service.AcceptAsyncImageSubmission(context.Background(), db, request, normalized, key, key, "127.0.0.1")
		require.NoError(t, err)
		generations = append(generations, *generation)
	}
	// Retrying an accepted request must still reuse its reservation and task.
	replay, err := service.AcceptAsyncImageSubmission(context.Background(), db, request, normalized, "unlimited-image-32", "retry", "127.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, generations[32].ID, replay.ID)
	var count int64
	require.NoError(t, db.Model(&model.KKAIImageGeneration{}).Where("user_id = ?", normalized.UserID).Count(&count).Error)
	assert.EqualValues(t, 33, count)
	var user model.User
	require.NoError(t, db.First(&user, normalized.UserID).Error)
	assert.EqualValues(t, 20_000-33*300, user.Quota)

	now := time.Now().Unix()
	for index, state := range []string{model.ImageGenerationExecutionUnknown, model.ImageGenerationExecutionDispatching, model.ImageGenerationExecutionPreparing} {
		require.NoError(t, db.Model(&model.KKAIImageExecution{}).Where("generation_id = ?", generations[index].ID).Updates(map[string]any{
			"state": state, "execution_deadline_at": now + 1200, "recovery_deadline_at": now + 1440,
		}).Error)
	}
	leader := model.KKAIJobLease{LeaseName: "unlimited-image-worker", Holder: "writer", Fence: 1, LeaseUntil: now + 300}
	require.NoError(t, db.Create(&leader).Error)
	var event model.KKAIOutboxEvent
	require.NoError(t, db.Where("topic = ? AND aggregate_id = ?", service.ImageGenerateTopic, strconv.FormatInt(generations[3].ID, 10)).Take(&event).Error)
	require.NoError(t, db.Model(&event).Updates(map[string]any{"locked_by": "unlimited-image-claim", "locked_at": now}).Error)
	fence := model.ImageExecutionFence{EventID: event.ID, LockedBy: "unlimited-image-claim", LockTimeout: 2 * time.Minute, LeaderName: leader.LeaseName, LeaderHolder: leader.Holder, LeaderFence: leader.Fence}
	execution, generation, err := service.AdoptImageExecution(context.Background(), db, generations[3].ID, fence)
	require.NoError(t, err)
	assert.Equal(t, generations[3].ID, generation.ID)
	assert.Equal(t, model.ImageGenerationExecutionPreparing, execution.State)
	assert.Greater(t, execution.ExecutionDeadlineAt, now)
	assert.Greater(t, execution.RecoveryDeadlineAt, execution.ExecutionDeadlineAt)
	var unknown model.KKAIImageExecution
	require.NoError(t, db.First(&unknown, "generation_id = ?", generations[0].ID).Error)
	assert.Equal(t, model.ImageGenerationExecutionUnknown, unknown.State)
}

func TestImageAsyncReplayPrecedesQuoteTokenAssetAndFeatureValidation(t *testing.T) {
	db, request, normalized := newImageAsyncAcceptanceFixture(t)
	asset := model.KKAIImageAsset{OwnerUserID: normalized.UserID, Kind: model.ImageAssetKindReference, State: model.ImageAssetStateReady, SHA256: strings.Repeat("b", 64), SizeBytes: 120, ObjectKey: "reference-replay"}
	require.NoError(t, db.Create(&asset).Error)
	request.Mode, request.AssetIDs = service.ImageStudioModeEdit, []int64{asset.ID}
	claims, err := service.ValidateImageStudioQuote(normalized, time.Now())
	require.NoError(t, err)
	normalized, err = service.NormalizeAsyncImageSubmission(context.Background(), db, normalized.UserID, request)
	require.NoError(t, err)
	normalized.RelayRequest.Size = claims.ImagePricingSnapshot.Size
	quote, err := service.NewImageStudioQuote(normalized, claims.Quota, claims.OtherRatios, claims.ImagePricingSnapshot)
	require.NoError(t, err)
	normalized.QuoteToken, request.QuoteToken = quote.QuoteToken, quote.QuoteToken
	generation, err := service.AcceptAsyncImageSubmission(context.Background(), db, request, normalized, "lost-response", "request-replay", "127.0.0.1")
	require.NoError(t, err)
	require.NoError(t, db.Model(&asset).Update("state", model.ImageAssetStateDeleted).Error)
	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", request.TokenID).Update("status", common.TokenStatusDisabled).Error)
	require.NoError(t, db.Model(&model.KKAIImageModelProfile{}).Where("id = ?", normalized.ProfileID).Update("enabled", false).Error)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"image_studio.async_submission_enabled": "false"}))
	request.QuoteToken = "expired-or-replaced-quote"
	body, err := common.Marshal(request)
	require.NoError(t, err)
	c, recorder := newImageStudioRelayContext(http.MethodPost, "/pg/images/generations/async", body)
	c.Request.Header.Set("Idempotency-Key", "lost-response")
	SubmitAsyncImageStudioGeneration(c)
	require.Equal(t, http.StatusAccepted, recorder.Code, recorder.Body.String())
	assert.Equal(t, "/api/image-studio/generations/"+strconv.FormatInt(generation.ID, 10), recorder.Header().Get("Location"))
	var response struct {
		Data service.ImageGenerationView `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, generation.ID, response.Data.ID)
	request.Prompt = "a different intent"
	body, err = common.Marshal(request)
	require.NoError(t, err)
	c, recorder = newImageStudioRelayContext(http.MethodPost, "/pg/images/generations/async", body)
	c.Request.Header.Set("Idempotency-Key", "lost-response")
	SubmitAsyncImageStudioGeneration(c)
	assert.Equal(t, http.StatusConflict, recorder.Code)
}

func newImageAsyncWorkerFixture(t *testing.T, state string) (*gorm.DB, model.KKAIImageGeneration, model.KKAIOutboxEvent, context.Context) {
	t.Helper()
	db, request, normalized := newImageAsyncAcceptanceFixture(t)
	generation, err := service.AcceptAsyncImageSubmission(context.Background(), db, request, normalized, "worker-recovery", "request-worker", "127.0.0.1")
	require.NoError(t, err)
	require.NoError(t, model.MarkImageGenerationDispatching(context.Background(), db, generation.ID))
	now := time.Now().Unix()
	leader := model.KKAIJobLease{LeaseName: "image-worker-test", Holder: "writer", Fence: 4, LeaseUntil: now + 300}
	require.NoError(t, db.Create(&leader).Error)
	var event model.KKAIOutboxEvent
	require.NoError(t, db.Where("topic = ?", service.ImageGenerateTopic).First(&event).Error)
	require.NoError(t, db.Model(&model.KKAIImageExecution{}).Where("generation_id = ?", generation.ID).Updates(map[string]any{"state": state, "payload": "invalid-on-purpose", "execution_deadline_at": now + 120, "recovery_deadline_at": now + 240}).Error)
	return db, *generation, event, service.WithImageWorkerLease(context.Background(), leader)
}

func TestImageAsyncWorkerPreservesRecoveryBudgetAndRefundsUnknownAfterDeadline(t *testing.T) {
	db, generation, event, ctx := newImageAsyncWorkerFixture(t, model.ImageGenerationExecutionUnknown)
	require.NoError(t, db.Model(&event).Updates(map[string]any{"attempts": 11}).Error)
	panicNext := false
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("image-recovery-db-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "kkai_image_executions" {
			if panicNext {
				panic("interrupted image recovery")
			}
			tx.AddError(errors.New("temporary database error"))
		}
	}))
	processor := service.NewKKAIOutboxProcessor(db, "test-recovery")
	require.NoError(t, processor.Register(service.ImageGenerateTopic, runImageGenerationEvent))
	result, err := processor.ProcessBatch(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Deferred)
	panicNext = true
	require.NoError(t, db.Model(&event).Update("available_at", time.Now().Unix()).Error)
	result, err = processor.ProcessBatch(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Deferred)
	require.NoError(t, db.Callback().Query().Remove("image-recovery-db-failure"))
	require.NoError(t, db.First(&event, event.ID).Error)
	assert.Equal(t, model.KKAIOutboxStatusPending, event.Status)
	assert.Equal(t, 11, event.Attempts)
	// The stored dispatch marker wins over the invalid payload: recovery must not POST again.
	require.NoError(t, db.Model(&event).Update("available_at", time.Now().Unix()).Error)
	result, err = processor.ProcessBatch(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Deferred)
	// An unusable accounting record cannot strand the reservation indefinitely.
	require.NoError(t, db.Create(&model.KKAIOutboxEvent{EventKey: "image-accounting:" + strconv.FormatInt(generation.ID, 10), Topic: model.KKAIOutboxTopicImageAccounting, AggregateID: strconv.FormatInt(generation.ID, 10), Payload: "{}", Status: model.KKAIOutboxStatusPending}).Error)
	require.NoError(t, db.Model(&model.KKAIImageExecution{}).Where("generation_id = ?", generation.ID).Update("recovery_deadline_at", time.Now().Add(-time.Second).Unix()).Error)
	require.NoError(t, db.Model(&event).Update("available_at", time.Now().Unix()).Error)
	result, err = processor.ProcessBatch(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Delivered)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	assert.Equal(t, model.ImageGenerationStatusUnknown, generation.Status)
	assert.Equal(t, model.ImageGenerationBillingStateRefunded, generation.BillingState)
	var user model.User
	require.NoError(t, db.First(&user, generation.UserID).Error)
	assert.EqualValues(t, 1000, user.Quota)
}

func TestImageAsyncWorkerSettlesDurableResultWithoutExecutingPayload(t *testing.T) {
	db, generation, event, ctx := newImageAsyncWorkerFixture(t, model.ImageGenerationExecutionResultReady)
	var channel model.Channel
	require.NoError(t, db.First(&channel).Error)
	require.NoError(t, model.PrepareImageGenerationAccounting(ctx, db, generation.ID, model.ImageGenerationAccountingPayload{TargetQuota: 200, OutputCount: 1, LogParams: model.RecordConsumeLogParams{ChannelId: channel.Id, ModelName: generation.Model, TokenId: generation.TokenID, Quota: 200}}))
	asset := model.KKAIImageAsset{GenerationID: &generation.ID, OwnerUserID: generation.UserID, Scope: model.ImageAssetScopeUser, Kind: model.ImageAssetKindOutput, State: model.ImageAssetStateReady, ObjectKey: "ready-result"}
	require.NoError(t, db.Create(&asset).Error)
	processor := service.NewKKAIOutboxProcessor(db, "test-result")
	require.NoError(t, processor.Register(service.ImageGenerateTopic, runImageGenerationEvent))
	result, err := processor.ProcessBatch(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Delivered)
	require.NoError(t, db.First(&generation, generation.ID).Error)
	assert.Equal(t, model.ImageGenerationStatusSucceeded, generation.Status)
	assert.Equal(t, 200, generation.FinalQuota)
	// A committed completion followed by a lost outbox acknowledgement is harmless.
	require.NoError(t, db.Model(&event).Updates(map[string]any{"status": model.KKAIOutboxStatusPending, "locked_by": "", "locked_at": 0}).Error)
	result, err = processor.ProcessBatch(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Delivered)
	var user model.User
	require.NoError(t, db.First(&user, generation.UserID).Error)
	assert.EqualValues(t, 800, user.Quota)
}

func TestImageAsyncWorkerEditMultipartRestoresPricingSnapshot(t *testing.T) {
	_, request, normalized := newImageAsyncAcceptanceFixture(t)
	normalized.Mode = service.ImageStudioModeEdit
	payload := imageStudioEditTestPNG(t, color.RGBA{R: 255, A: 255})
	archive, err := service.NewHTTPImageArchiveFetcher(t.TempDir()).Ingest(bytes.NewReader(payload), "image/png", 1<<20, 100)
	require.NoError(t, err)
	defer archive.Remove()
	c, _ := newImageStudioRelayContext(http.MethodPost, "/internal/images", nil)
	require.NoError(t, prepareAsyncImageContext(c, normalized))
	require.NoError(t, replaceImageStudioEditRelayBody(c, normalized.RelayRequest, []*service.FetchedImageArchive{archive}))
	defer common.CleanupBodyStorage(c)
	parsed, err := helper.GetAndValidateRequest(c, relaytypes.RelayFormatOpenAIImage)
	require.NoError(t, err)
	assert.Equal(t, "/v1/images/edits", c.Request.URL.Path)
	require.Len(t, c.Request.MultipartForm.File["image"], 1)
	claims, err := service.ValidateImageStudioQuote(normalized, time.Now())
	require.NoError(t, err)
	require.NoError(t, service.SetTrustedImagePricingSnapshot(c, *claims.ImagePricingSnapshot))
	info, err := relaycommon.GenRelayInfo(c, relaytypes.RelayFormatOpenAIImage, parsed, nil)
	require.NoError(t, err)
	require.NoError(t, service.PrepareImagePricing(c, info))
	_, err = helper.ModelPriceHelper(c, info, 0, parsed.GetTokenCountMeta())
	require.NoError(t, err)
	assert.Equal(t, request.Model, info.OriginModelName)
	assert.Equal(t, claims.ImagePricingSnapshot, info.ImagePricingSnapshot)
	assert.Equal(t, "1024x1024", c.Request.PostForm.Get("size"))
	assert.Equal(t, "1", c.Request.PostForm.Get("n"))
}
