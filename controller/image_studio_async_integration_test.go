package controller

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
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
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/image_pricing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageAsyncWorkerEditExecutesAndArchivesExactlyOnce(t *testing.T) {
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
	const startingQuota = 3_000_000
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 407).Update("quota", startingQuota).Error)

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
		Parameters: map[string]any{"count": 1, "size": "1024x1024"}, Mode: service.ImageStudioModeEdit,
		AssetIDs: []int64{assetIDs[1], assetIDs[0]},
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
	quote, err := service.NewImageStudioQuote(normalized, price.Quota, price.OtherRatios, price.ImagePricingSnapshot)
	require.NoError(t, err)
	require.Equal(t, 502_500, quote.Quota)
	submission.QuoteToken, normalized.QuoteToken = quote.QuoteToken, quote.QuoteToken
	generation, err := service.AcceptAsyncImageSubmission(context.Background(), db, submission, normalized, "async-edit-integration", "async-edit-request", "127.0.0.1")
	require.NoError(t, err)
	assert.Zero(t, providerCalls.Load(), "acceptance must not call the provider")
	var user model.User
	require.NoError(t, db.First(&user, 407).Error)
	assert.EqualValues(t, startingQuota-quote.Quota, user.Quota)

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
	require.Equal(t, model.ImageGenerationStatusSucceeded, generation.Status, "%+v", generation)
	assert.Equal(t, model.ImageGenerationBillingStateSettled, generation.BillingState)
	assert.Equal(t, quote.Quota, generation.FinalQuota)
	assert.Equal(t, 1, generation.SucceededCount)
	require.EqualValues(t, 1, providerCalls.Load())
	select {
	case call := <-observed:
		assert.Equal(t, "/v1/images/edits", call.Path)
		assert.Equal(t, submission.Model, call.Model)
		assert.Equal(t, submission.Prompt, call.Prompt)
		assert.Equal(t, "1024x1024", call.Size)
		assert.Equal(t, "1", call.Count)
		assert.Equal(t, [][]byte{referenceImages[1], referenceImages[0]}, call.Images)
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
	assert.EqualValues(t, startingQuota-quote.Quota, user.Quota)
	var outputCount int64
	require.NoError(t, db.Model(&model.KKAIImageAsset{}).Where("generation_id = ? AND kind = ?", generation.ID, model.ImageAssetKindOutput).Count(&outputCount).Error)
	assert.EqualValues(t, 1, outputCount)
}
