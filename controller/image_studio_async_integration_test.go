package controller

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/image_pricing_setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectAsyncImageChannelPreservesUnavailableDiagnostics(t *testing.T) {
	db, _, normalized := newImageAsyncAcceptanceFixture(t)
	previousMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCache })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/internal/image-quote", nil)
	ctx.Set("id", normalized.UserID)
	require.NoError(t, prepareAsyncImageContext(ctx, normalized))
	require.NoError(t, db.Model(&model.Ability{}).Where("1 = 1").Update("enabled", false).Error)

	err := selectAsyncImageChannel(ctx, normalized.Model)

	var apiErr *relaytypes.NewAPIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, relaytypes.ErrorCodeModelNotFound, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	assert.Equal(t, "image_service_unavailable", service.ClassifyImageGenerationError(err))
}

func TestImageAsyncWorkerPolicyRejectionRefundsWithoutResubmitting(t *testing.T) {
	if model.MembershipEnabled() {
		t.Skip("legacy fixture runs with membership disabled")
	}
	// Isolate the process-cached R2 client without adding a test-only reset API.
	const childEnvironment = "KKAI_IMAGE_ASYNC_POLICY_CHILD"
	if os.Getenv(childEnvironment) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$", "-test.timeout=45s")
		command.Env = append(os.Environ(), childEnvironment+"=1")
		output, err := command.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return
	}
	service.InitHttpClient()
	service.InitTokenEncoders()
	t.Setenv("VIDEO_STUDIO_R2_ENDPOINT", "http://127.0.0.1:1")
	t.Setenv("VIDEO_STUDIO_R2_REGION", "auto")
	t.Setenv("VIDEO_STUDIO_R2_BUCKET", "image-test")
	t.Setenv("VIDEO_STUDIO_R2_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("VIDEO_STUDIO_R2_SECRET_ACCESS_KEY", "test-secret-key")
	t.Setenv("VIDEO_STUDIO_R2_ACCESS_KEY_ID_FILE", "")
	t.Setenv("VIDEO_STUDIO_R2_SECRET_ACCESS_KEY_FILE", "")
	for _, tc := range []struct {
		name             string
		status           int
		body             string
		confirmedFailure bool
		mapping          string
		wantCode         string
		errorLogs        int
	}{
		{"http_policy", http.StatusBadRequest, `{"error":{"type":"invalid_request_error","code":"content_policy","message":"private-provider-diagnostic"}}`, true, "", service.ImageExecutionFailureContentPolicy, 1},
		{"in_band_policy", http.StatusOK, `{"error":{"type":"invalid_request_error","code":"content_policy_violation","message":"private-provider-diagnostic"}}`, true, "", service.ImageExecutionFailureContentPolicy, 1},
		{"mapped_in_band_policy_uses_original_status", http.StatusOK, `{"error":{"type":"upstream_error","code":"content_policy","message":"private-provider-diagnostic"}}`, true, `{"200":"502"}`, service.ImageExecutionFailureContentPolicy, 1},
		{"provider_error", http.StatusBadGateway, `{"error":{"type":"upstream_error","code":"image_provider_error","message":"private-provider-diagnostic"}}`, true, "", "image_service_unavailable", 1},
		{"provider_error_after_keepalive", http.StatusOK, `{"error":{"type":"upstream_error","code":"image_provider_error","message":"private-provider-diagnostic"}}`, true, "", "image_service_unavailable", 1},
		{"mapped_provider_error_uses_original_status", http.StatusBadGateway, `{"error":{"type":"upstream_error","code":"image_provider_error","message":"private-provider-diagnostic"}}`, true, `{"502":"400"}`, "image_service_unavailable", 1},
		{"mapped_keepalive_provider_error_uses_original_status", http.StatusOK, `{"error":{"type":"upstream_error","code":"image_provider_error","message":"private-provider-diagnostic"}}`, true, `{"200":"502"}`, "image_service_unavailable", 1},
		{"client_error_marker_is_ambiguous", http.StatusBadRequest, `{"error":{"type":"upstream_error","code":"image_provider_error","message":"private-provider-diagnostic"}}`, false, "", "image_service_unavailable", 1},
		{"gateway_policy_code_is_ambiguous", http.StatusBadGateway, `{"error":{"type":"server_error","code":"content_policy","message":"private-provider-diagnostic"}}`, false, "", "image_service_unavailable", 1},
		{"mapped_gateway_is_still_ambiguous", http.StatusBadGateway, `{"error":{"type":"server_error","code":"content_policy","message":"private-provider-diagnostic"}}`, false, `{"502":"400"}`, "image_service_unavailable", 1},
		{"mapped_gateway_success_is_still_ambiguous", http.StatusBadGateway, `{"error":{"type":"server_error","code":"content_policy","message":"private-provider-diagnostic"}}`, false, `{"502":"200"}`, "image_service_unavailable", 1},
		{"message_is_not_evidence", http.StatusBadRequest, `{"error":{"type":"invalid_request_error","code":"upstream_error","message":"content_policy private-provider-diagnostic"}}`, false, "", "image_invalid_request", 1},
		{"invalid_success_response", http.StatusOK, `{}`, false, "", "image_result_unavailable", 1},
		{"code_only_success_error_is_ambiguous", http.StatusOK, `{"error":{"code":"content_policy","message":"private-provider-diagnostic"}}`, false, "", "image_result_unavailable", 1},
		{"transport_failure_after_dispatch", 0, "", false, "", "image_service_unavailable", 1},
		{"provider_quota_is_not_user_quota", http.StatusBadRequest, `{"error":{"type":"invalid_request_error","code":"insufficient_quota","message":"private-provider-diagnostic https://advert.example top up upstream"}}`, false, "", "image_service_unavailable", 1},
		{"provider_impersonates_user_quota", http.StatusForbidden, `{"error":{"type":"invalid_request_error","code":"insufficient_user_quota","message":"private-provider-diagnostic"}}`, false, "", "image_service_unavailable", 1},
		{"provider_rate_limit", http.StatusTooManyRequests, `{"error":{"type":"invalid_request_error","code":"rate_limit_exceeded","message":"private-provider-diagnostic"}}`, false, "", "image_rate_limited", 1},
		{"provider_reference_rejected", http.StatusBadRequest, `{"error":{"type":"invalid_request_error","code":"invalid_image_format","message":"private-provider-diagnostic"}}`, false, "", "image_reference_invalid", 1},
		{"archive_all_outputs_failed", http.StatusOK, `{"data":[{"b64_json":"bm90LWFuLWltYWdl"}]}`, false, "", "image_result_unavailable", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, request, normalized := newImageAsyncAcceptanceFixture(t)
			require.NoError(t, db.AutoMigrate(&model.Log{}))
			previousLogDB, previousErrorLog := model.LOG_DB, constant.ErrorLogEnabled
			previousMemoryCache := common.MemoryCacheEnabled
			model.LOG_DB, constant.ErrorLogEnabled, common.MemoryCacheEnabled = db, true, false
			t.Cleanup(func() {
				model.LOG_DB, constant.ErrorLogEnabled, common.MemoryCacheEnabled = previousLogDB, previousErrorLog, previousMemoryCache
			})
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				if tc.status == 0 {
					_, _ = io.Copy(io.Discard, request.Body)
					connection, _, err := writer.(http.Hijacker).Hijack()
					if assert.NoError(t, err) {
						_ = connection.Close()
					}
					return
				}
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(tc.status)
				_, _ = writer.Write([]byte(tc.body))
			}))
			t.Cleanup(upstream.Close)
			require.NoError(t, db.Model(&model.Channel{}).Where("1 = 1").Updates(map[string]any{"base_url": upstream.URL, "status_code_mapping": tc.mapping}).Error)
			generation, err := service.AcceptAsyncImageSubmission(context.Background(), db, request, normalized, "policy-test", "policy-request", "127.0.0.1")
			require.NoError(t, err)
			leader := model.KKAIJobLease{LeaseName: "image-policy-worker", Holder: "writer", Fence: 1, LeaseUntil: time.Now().Add(time.Minute).Unix()}
			require.NoError(t, db.Create(&leader).Error)
			ctx := service.WithImageWorkerLease(context.Background(), leader)
			processor := service.NewKKAIOutboxProcessor(db, "image-policy-test")
			require.NoError(t, processor.Register(service.ImageGenerateTopic, runImageGenerationEvent))
			batch, err := processor.ProcessBatch(ctx, 1)
			require.NoError(t, err)
			require.EqualValues(t, 1, calls.Load(), "worker must reach the provider exactly once")
			require.NoError(t, db.First(generation, generation.ID).Error)
			assert.Equal(t, tc.wantCode, generation.ErrorCode)
			assert.NotContains(t, generation.ErrorMessage, "private-provider-diagnostic")
			var execution model.KKAIImageExecution
			require.NoError(t, db.First(&execution, "generation_id = ?", generation.ID).Error)
			var user model.User
			require.NoError(t, db.First(&user, generation.UserID).Error)
			if tc.confirmedFailure {
				assert.Equal(t, 1, batch.Delivered)
				assert.Zero(t, batch.Deferred)
				assert.Equal(t, model.ImageGenerationStatusFailed, generation.Status)
				assert.Equal(t, "upstream", generation.FailureStage)
				assert.Equal(t, tc.wantCode, generation.ErrorCode)
				assert.NotContains(t, generation.ErrorMessage, "private-provider-diagnostic")
				assert.Equal(t, model.ImageGenerationBillingStateRefunded, generation.BillingState)
				assert.EqualValues(t, 1000, user.Quota)
				assert.Equal(t, model.ImageGenerationExecutionDone, execution.State)
			} else {
				assert.Equal(t, 1, batch.Deferred)
				assert.Equal(t, model.ImageGenerationStatusSubmitting, generation.Status)
				assert.Equal(t, model.ImageGenerationBillingStateProcessing, generation.BillingState)
				assert.EqualValues(t, 700, user.Quota)
				assert.Equal(t, model.ImageGenerationExecutionUnknown, execution.State)
			}
			var logs []model.Log
			require.NoError(t, db.Where("type = ?", model.LogTypeError).Find(&logs).Error)
			require.Len(t, logs, tc.errorLogs)
			for _, log := range logs {
				assert.NotContains(t, log.Content, "private-provider-diagnostic")
			}
			replay, err := service.AcceptAsyncImageSubmission(ctx, db, request, normalized, "policy-test", "policy-retry", "127.0.0.1")
			require.NoError(t, err)
			assert.Equal(t, generation.ID, replay.ID)
			// Both terminal redelivery and unknown recovery must preserve the single POST.
			require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where("id = ?", execution.EventID).Updates(map[string]any{"status": model.KKAIOutboxStatusPending, "locked_by": "", "locked_at": 0, "available_at": time.Now().Unix()}).Error)
			_, err = processor.ProcessBatch(ctx, 1)
			require.NoError(t, err)
			assert.EqualValues(t, 1, calls.Load())
			quotaAfterFirstExecution := user.Quota
			require.NoError(t, db.First(&user, generation.UserID).Error)
			assert.Equal(t, quotaAfterFirstExecution, user.Quota, "redelivery must not debit or refund twice")
		})
	}
}

func TestImageAsyncWorkerEditExecutesAndArchivesExactlyOnce(t *testing.T) {
	if model.MembershipEnabled() {
		t.Skip("legacy fixture runs with membership disabled")
	}
	testImageAsyncWorkerEditFunding(t, 0, 3_000_000, 1)
}

func TestMembershipImageAsyncWorkerEditFundingAndPartialDelivery(t *testing.T) {
	if !model.MembershipEnabled() {
		t.Skip("requires KKAI_MEMBERSHIP_ENABLED=true")
	}
	for _, tc := range []struct {
		name          string
		memberQuota   int64
		wallet, count int
	}{
		{"member_zero_wallet_partial", 1_000_000, 0, 2},
		{"whole_wallet_fallback", 100, 3_000_000, 1},
	} {
		t.Run(tc.name, func(t *testing.T) { testImageAsyncWorkerEditFunding(t, tc.memberQuota, tc.wallet, tc.count) })
	}
}

func testImageAsyncWorkerEditFunding(t *testing.T, memberQuota int64, startingQuota, requestedCount int) {
	t.Helper()
	// The runtime R2 provider caches its client. Isolate its test endpoint from
	// other tests without adding a production-only cache reset hook.
	const childEnvironment = "KKAI_IMAGE_ASYNC_INTEGRATION_CHILD"
	if os.Getenv(childEnvironment) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$", "-test.timeout=45s")
		command.Env = append(os.Environ(), childEnvironment+"=1")
		output, err := command.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return
	}

	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	service.InitTokenEncoders()
	previousGrok := *model_setting.GetGrokSettings()
	*model_setting.GetGrokSettings() = model_setting.GrokSettings{ViolationDeductionEnabled: true, ViolationDeductionAmount: 0.05}
	t.Cleanup(func() { *model_setting.GetGrokSettings() = previousGrok })
	db := setupImageStudioIntegrationState(t)
	enableImageStudioEditIntegrationModel(t, db)
	require.NoError(t, db.AutoMigrate(&model.KKAIImageExecution{}, &model.KKAIImageGenerationReference{}))
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"image_studio.async_submission_enabled": "true"}))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"图片工作室":1.5}`))
	pricing := image_pricing_setting.DefaultConfig()
	pricing.Enabled = true
	pricingJSON, err := common.Marshal(pricing)
	require.NoError(t, err)
	require.NoError(t, image_pricing_setting.UpdateByJSONString(string(pricingJSON)))
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 407).Update("quota", startingQuota).Error)
	var account *model.MembershipAccount
	if memberQuota > 0 {
		require.NoError(t, db.AutoMigrate(model.MembershipModels()...))
		catalog, err := model.PublishMembershipCatalog(model.MembershipCatalog{
			SalesEnabled: true,
			Tiers:        []model.MembershipTier{{ID: "gold", Name: "Gold", Rank: 1, Style: "gold", Currency: "USD", PriceMinor: 1000, Quota: memberQuota, Enabled: true, Discounts: map[string]float64{service.ImageStudioTokenGroup: 0.5}}},
			Groups:       []model.MembershipGroup{{Key: service.ImageStudioTokenGroup, Enabled: true, Published: true}},
		}, 0, 407)
		require.NoError(t, err)
		order, err := model.CreateMembershipOrder(model.MembershipOrderInput{UserID: 407, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: model.PaymentProviderEpay, PaymentMethod: "alipay", TradeNo: "image-member", ExpiresAt: time.Now().Add(time.Hour).Unix()})
		require.NoError(t, err)
		_, err = model.CompleteMembershipPayment(model.MembershipPaymentInput{TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: "image-paid", AmountMinor: order.AmountMinor, Currency: order.Currency, OneTime: true, Paid: true})
		require.NoError(t, err)
		self, err := model.GetMembershipSelf(407)
		require.NoError(t, err)
		account = self.Account
		require.NotNil(t, account)
	}

	referenceImages := [][]byte{
		imageStudioEditTestPNG(t, color.RGBA{R: 0xf0, A: 0xff}),
		imageStudioEditTestPNG(t, color.RGBA{B: 0xb0, A: 0xff}),
	}
	outputImage := imageStudioEditTestPNG(t, color.RGBA{G: 0x80, A: 0xff})
	objects := make(map[string][]byte)
	assetIDs := make([]int64, len(referenceImages))
	for position, body := range referenceImages {
		key := fmt.Sprintf("image-studio/users/407/references/%d", position)
		asset := model.KKAIImageAsset{
			OwnerUserID: 407, Scope: model.ImageAssetScopeUser, Kind: model.ImageAssetKindReference,
			State: model.ImageAssetStateReady, ObjectKey: key, MIMEType: "image/png",
			SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), SizeBytes: int64(len(body)),
		}
		require.NoError(t, db.Create(&asset).Error)
		assetIDs[position], objects[key] = asset.ID, body
	}
	var objectsMu sync.Mutex
	var storagePuts atomic.Int32
	storage := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		key := strings.TrimPrefix(request.URL.Path, "/image-test/")
		switch request.Method {
		case http.MethodGet:
			objectsMu.Lock()
			body, found := objects[key]
			objectsMu.Unlock()
			if !found {
				http.NotFound(writer, request)
				return
			}
			writer.Header().Set("Content-Type", "image/png")
			writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
			_, _ = writer.Write(body)
		case http.MethodPut:
			body, readErr := io.ReadAll(request.Body)
			if !assert.NoError(t, readErr) {
				http.Error(writer, "read failed", http.StatusBadRequest)
				return
			}
			assert.Equal(t, "image/png", request.Header.Get("Content-Type"))
			storagePuts.Add(1)
			objectsMu.Lock()
			objects[key] = body
			objectsMu.Unlock()
			writer.Header().Set("ETag", `"image-result"`)
			writer.WriteHeader(http.StatusOK)
		default:
			http.Error(writer, "unsupported storage operation", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(storage.Close)
	t.Setenv("VIDEO_STUDIO_R2_ENDPOINT", storage.URL)
	t.Setenv("VIDEO_STUDIO_R2_REGION", "auto")
	t.Setenv("VIDEO_STUDIO_R2_BUCKET", "image-test")
	t.Setenv("VIDEO_STUDIO_R2_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("VIDEO_STUDIO_R2_SECRET_ACCESS_KEY", "test-secret-key")
	t.Setenv("VIDEO_STUDIO_R2_ACCESS_KEY_ID_FILE", "")
	t.Setenv("VIDEO_STUDIO_R2_SECRET_ACCESS_KEY_FILE", "")

	type providerRequest struct {
		Path, Model, Prompt, Size, Count string
		Images                           [][]byte
	}
	observed := make(chan providerRequest, 2)
	providerResponse, err := common.Marshal(map[string]any{
		"created": 1, "data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(outputImage)}},
		"usage": map[string]int{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2},
	})
	require.NoError(t, err)
	var providerCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		providerCalls.Add(1)
		if request.URL.Path == "/v1/images/generations" {
			var payload dto.ImageRequest
			if !assert.NoError(t, common.DecodeJson(request.Body, &payload)) || !assert.NotNil(t, payload.N) {
				http.Error(writer, "invalid generation", http.StatusBadRequest)
				return
			}
			observed <- providerRequest{Path: request.URL.Path, Model: payload.Model, Prompt: payload.Prompt, Size: payload.Size, Count: strconv.Itoa(int(*payload.N))}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write(providerResponse)
			return
		}
		if !assert.NoError(t, request.ParseMultipartForm(1<<20)) {
			http.Error(writer, "invalid multipart", http.StatusBadRequest)
			return
		}
		defer request.MultipartForm.RemoveAll()
		call := providerRequest{Path: request.URL.Path, Model: request.FormValue("model"), Prompt: request.FormValue("prompt"), Size: request.FormValue("size"), Count: request.FormValue("n")}
		for _, header := range request.MultipartForm.File["image[]"] {
			file, openErr := header.Open()
			if !assert.NoError(t, openErr) {
				http.Error(writer, "file unavailable", http.StatusBadRequest)
				return
			}
			body, readErr := io.ReadAll(file)
			closeErr := file.Close()
			if !assert.NoError(t, readErr) || !assert.NoError(t, closeErr) {
				http.Error(writer, "file unreadable", http.StatusBadRequest)
				return
			}
			assert.Equal(t, "image/png", header.Header.Get("Content-Type"))
			call.Images = append(call.Images, body)
		}
		select {
		case observed <- call:
		default:
			t.Error("unexpected repeated upstream call")
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(providerResponse)
	}))
	t.Cleanup(upstream.Close)
	var channel model.Channel
	require.NoError(t, db.First(&channel).Error)
	require.NoError(t, db.Model(&channel).Update("base_url", upstream.URL).Error)
	model.InitChannelCache()

	var token model.Token
	require.NoError(t, db.First(&token).Error)
	submission := service.ImageAsyncSubmissionRequest{
		TokenID: token.Id, Model: imageStudioEditTestModel, Prompt: "Combine the two references in this order",
		Parameters: map[string]any{"count": requestedCount, "size": "1024x1024"}, Mode: service.ImageStudioModeEdit,
		AssetIDs: []int64{assetIDs[1], assetIDs[0]},
	}
	if requestedCount > 1 {
		submission.Mode, submission.AssetIDs = service.ImageStudioModeGeneration, nil
	}
	normalized, err := service.NormalizeAsyncImageSubmission(context.Background(), db, 407, submission)
	require.NoError(t, err)
	quoteContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	quoteContext.Request = httptest.NewRequest(http.MethodPost, "/internal/image-quote", nil)
	quoteContext.Set("id", 407)
	require.NoError(t, prepareAsyncImageContext(quoteContext, normalized))
	require.NoError(t, replaceImageStudioRelayBody(quoteContext, normalized.RelayRequest))
	defer common.CleanupBodyStorage(quoteContext)
	require.NoError(t, selectAsyncImageChannel(quoteContext, normalized.Model))
	price, err := calculateImageStudioQuote(quoteContext)
	require.NoError(t, err)
	quote, err := service.NewImageStudioQuote(normalized, price.Quota, price.OtherRatios, price.ImagePricingSnapshot, price.MembershipPricing)
	require.NoError(t, err)
	expectedMaximum := 502_500 * requestedCount
	if memberQuota > 0 {
		// The authorization includes the fixed violation reserve, but successful
		// delivery still charges only its delivered images.
		expectedMaximum += 37_500
	}
	require.Equal(t, expectedMaximum, quote.Quota)
	submission.QuoteToken, normalized.QuoteToken = quote.QuoteToken, quote.QuoteToken
	generation, err := service.AcceptAsyncImageSubmission(context.Background(), db, submission, normalized, "async-edit-integration", "async-edit-request", "127.0.0.1")
	require.NoError(t, err)
	assert.Zero(t, providerCalls.Load(), "acceptance must not call the provider")
	var user model.User
	require.NoError(t, db.First(&user, 407).Error)
	memberFunded := account != nil && memberQuota >= int64(quote.Quota/2)
	reserved, charged := quote.Quota, 502_500
	walletAfterReserve, walletAfterDelivery := startingQuota-reserved, startingQuota-charged
	if memberFunded {
		reserved, charged = reserved/2, charged/2
		walletAfterReserve, walletAfterDelivery = startingQuota, startingQuota
	}
	assert.EqualValues(t, walletAfterReserve, user.Quota)
	assert.Equal(t, reserved, generation.ReservedQuota)
	if account != nil {
		// Recovery settles this original entitlement even if a later upgrade or
		// expiry has changed the current account revision.
		require.NoError(t, db.Model(account).Updates(map[string]any{"expires_at": time.Now().Add(-time.Minute).Unix(), "revision": account.Revision + 1}).Error)
	}

	// A queued request keeps the quote even when both live price inputs change.
	tier := pricing.Models[imageStudioEditTestModel].Tiers["1k"]
	tier.UnitPrice = 6.7
	pricing.Models[imageStudioEditTestModel].Tiers["1k"] = tier
	pricingJSON, err = common.Marshal(pricing)
	require.NoError(t, err)
	require.NoError(t, image_pricing_setting.UpdateByJSONString(string(pricingJSON)))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"图片工作室":3}`))
	leader := model.KKAIJobLease{LeaseName: "image-async-integration", Holder: "writer", Fence: 1, LeaseUntil: time.Now().Add(time.Minute).Unix()}
	require.NoError(t, db.Create(&leader).Error)
	ctx := service.WithImageWorkerLease(context.Background(), leader)
	processor := service.NewKKAIOutboxProcessor(db, "async-edit-integration")
	require.NoError(t, processor.Register(service.ImageGenerateTopic, runImageGenerationEvent))
	batch, err := processor.ProcessBatch(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 1, batch.Delivered, "%+v", batch)
	require.NoError(t, db.First(generation, generation.ID).Error)
	expectedStatus := model.ImageGenerationStatusSucceeded
	if requestedCount > 1 {
		expectedStatus = model.ImageGenerationStatusPartial
	}
	require.Equal(t, expectedStatus, generation.Status, "%+v", generation)
	assert.Equal(t, model.ImageGenerationBillingStateSettled, generation.BillingState)
	assert.Equal(t, charged, generation.FinalQuota)
	assert.Equal(t, 1, generation.SucceededCount)
	require.EqualValues(t, 1, providerCalls.Load())
	select {
	case call := <-observed:
		if requestedCount > 1 {
			assert.Equal(t, "/v1/images/generations", call.Path)
			assert.Empty(t, call.Images)
		} else {
			assert.Equal(t, "/v1/images/edits", call.Path)
			assert.Equal(t, [][]byte{referenceImages[1], referenceImages[0]}, call.Images)
		}
		assert.Equal(t, submission.Model, call.Model)
		assert.Equal(t, submission.Prompt, call.Prompt)
		assert.Equal(t, "1024x1024", call.Size)
		assert.Equal(t, strconv.Itoa(requestedCount), call.Count)
	default:
		t.Fatal("worker did not reach the local HTTP image adapter")
	}
	var execution model.KKAIImageExecution
	require.NoError(t, db.First(&execution, "generation_id = ?", generation.ID).Error)
	assert.Equal(t, model.ImageGenerationExecutionDone, execution.State)
	assert.Equal(t, channel.Id, execution.DispatchChannelID)
	var output model.KKAIImageAsset
	require.NoError(t, db.Where("generation_id = ? AND kind = ?", generation.ID, model.ImageAssetKindOutput).Take(&output).Error)
	assert.Equal(t, model.ImageAssetStateReady, output.State)
	assert.Equal(t, fmt.Sprintf("%x", sha256.Sum256(outputImage)), output.SHA256)
	objectsMu.Lock()
	assert.Equal(t, outputImage, objects[output.ObjectKey])
	objectsMu.Unlock()
	assert.EqualValues(t, 1, storagePuts.Load())
	var remainingReferences int64
	require.NoError(t, db.Model(&model.KKAIImageGenerationReference{}).Where("generation_id = ?", generation.ID).Count(&remainingReferences).Error)
	assert.Zero(t, remainingReferences)

	// Re-delivery after a lost acknowledgement cannot POST, archive or debit again.
	var event model.KKAIOutboxEvent
	require.NoError(t, db.First(&event, execution.EventID).Error)
	require.NoError(t, db.Model(&event).Updates(map[string]any{"status": model.KKAIOutboxStatusPending, "locked_by": "", "locked_at": 0, "available_at": time.Now().Unix()}).Error)
	batch, err = processor.ProcessBatch(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, batch.Delivered)
	assert.EqualValues(t, 1, providerCalls.Load())
	assert.EqualValues(t, 1, storagePuts.Load())
	require.NoError(t, db.First(&user, 407).Error)
	assert.EqualValues(t, walletAfterDelivery, user.Quota)
	var outputCount int64
	require.NoError(t, db.Model(&model.KKAIImageAsset{}).Where("generation_id = ? AND kind = ?", generation.ID, model.ImageAssetKindOutput).Count(&outputCount).Error)
	assert.EqualValues(t, 1, outputCount)
	if account != nil {
		record, err := model.GetMembershipBillingRecord(model.MembershipImageBillingKey(generation.ID))
		require.NoError(t, err)
		assert.Equal(t, "settled", record.State)
		assert.EqualValues(t, reserved, record.ReservedQuota)
		assert.EqualValues(t, charged, record.ChargedQuota)
		require.NoError(t, db.First(account, account.ID).Error)
		assert.Zero(t, account.AmountReserved)
		if memberFunded {
			assert.Equal(t, model.MembershipSource, record.Source)
			assert.EqualValues(t, charged, account.AmountUsed)
		} else {
			assert.Equal(t, model.MembershipWalletSource, record.Source)
			assert.Zero(t, account.AmountUsed)
		}
	}
}
