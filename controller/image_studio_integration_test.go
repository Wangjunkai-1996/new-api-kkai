package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestImageStudioSubmissionArchivesResultWithoutExposingProviderPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	db := setupImageStudioIntegrationState(t)

	const providerBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	imageBody, err := base64.StdEncoding.DecodeString(providerBase64)
	require.NoError(t, err)
	type providerRequest struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
		N      int    `json:"n"`
		Stream bool   `json:"stream"`
	}
	observed := make(chan providerRequest, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		var decoded providerRequest
		require.NoError(t, common.Unmarshal(body, &decoded))
		observed <- decoded
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(fmt.Sprintf(
			`{"created":1,"data":[{"b64_json":%q}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
			providerBase64,
		)))
	}))
	t.Cleanup(upstream.Close)

	var channel model.Channel
	require.NoError(t, db.First(&channel).Error)
	channel.BaseURL = &upstream.URL
	require.NoError(t, db.Save(&channel).Error)
	model.InitChannelCache()

	requestBody := imageStudioIntegrationRequestBody(t, db, "A precise lighthouse")

	store := &imageIntegrationAssetStore{objects: map[string][]byte{}}
	pipeline, err := service.NewImageAssetPipeline(
		db, store, service.NewHTTPImageArchiveFetcher(t.TempDir()), 1<<20, 100,
	)
	require.NoError(t, err)
	engine := imageStudioIntegrationEngine(pipeline)
	request := httptest.NewRequest(http.MethodPost, "/pg/images", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "image-integration-submit")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	assert.NotContains(t, response.Body.String(), providerBase64)
	assert.NotContains(t, response.Body.String(), upstream.URL)
	providerCall := <-observed
	assert.Equal(t, "gpt-image-1", providerCall.Model)
	assert.Equal(t, "A precise lighthouse", providerCall.Prompt)
	assert.Equal(t, 1, providerCall.N)
	assert.False(t, providerCall.Stream)

	var generation model.KKAIImageGeneration
	require.NoError(t, db.First(&generation).Error)
	assert.Equal(t, model.ImageGenerationStatusSucceeded, generation.Status)
	assert.Equal(t, 1, generation.SucceededCount)
	assert.Positive(t, generation.FinalQuota)
	var asset model.KKAIImageAsset
	require.NoError(t, db.First(&asset).Error)
	assert.Equal(t, model.ImageAssetStateReady, asset.State)
	assert.Equal(t, imageBody, store.objects[asset.ObjectKey])
	var reservation model.KKAIIdempotencyKey
	require.NoError(t, db.First(&reservation).Error)
	assert.Equal(t, model.ImageIdempotencyResourceGeneration, reservation.ResourceType)
	assert.Equal(t, fmt.Sprintf("%d", generation.ID), reservation.ResourceID)
	var managedToken model.Token
	require.NoError(t, db.First(&managedToken).Error)
	assert.Zero(t, managedToken.RemainQuota)
	assert.Zero(t, managedToken.UsedQuota)
	var accountingEvent model.KKAIOutboxEvent
	require.NoError(t, db.Where(
		"topic = ? AND aggregate_id = ?",
		model.KKAIOutboxTopicImageAccounting, fmt.Sprintf("%d", generation.ID),
	).First(&accountingEvent).Error)
	accountingHandler := service.ImageGenerationAccountingHandler{}
	require.NoError(t, accountingHandler.Handle(context.Background(), accountingEvent))
	require.NoError(t, accountingHandler.Handle(context.Background(), accountingEvent))
	var consumeLogs int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&consumeLogs).Error)
	assert.EqualValues(t, 1, consumeLogs)
}

func TestImageStudioSubmissionFailureRefundsAndReplaysWithoutCallingProviderAgain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	db := setupImageStudioIntegrationState(t)

	var providerCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		providerCalls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":{"message":"provider rejected request","type":"invalid_request_error"}}`))
	}))
	t.Cleanup(upstream.Close)
	var channel model.Channel
	require.NoError(t, db.First(&channel).Error)
	channel.BaseURL = &upstream.URL
	require.NoError(t, db.Save(&channel).Error)
	model.InitChannelCache()

	store := &imageIntegrationAssetStore{objects: map[string][]byte{}}
	pipeline, err := service.NewImageAssetPipeline(
		db, store, service.NewHTTPImageArchiveFetcher(t.TempDir()), 1<<20, 100,
	)
	require.NoError(t, err)
	engine := imageStudioIntegrationEngine(pipeline)
	requestBody := imageStudioIntegrationRequestBody(t, db, "A request the provider rejects")

	first := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/pg/images", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "image-integration-failure")
	engine.ServeHTTP(first, request)
	require.Equal(t, http.StatusBadRequest, first.Code, first.Body.String())
	require.EqualValues(t, 1, providerCalls.Load())

	var generation model.KKAIImageGeneration
	require.NoError(t, db.First(&generation).Error)
	require.Equal(t, model.ImageGenerationStatusFailed, generation.Status)
	require.Zero(t, generation.FinalQuota)
	var assetCount int64
	require.NoError(t, db.Model(&model.KKAIImageAsset{}).Count(&assetCount).Error)
	require.Zero(t, assetCount)
	require.Eventually(t, func() bool {
		var user model.User
		var managedToken model.Token
		if err := db.First(&user, 407).Error; err != nil {
			return false
		}
		if err := db.First(&managedToken).Error; err != nil {
			return false
		}
		return user.Quota == 100_000 && managedToken.RemainQuota == 0 && managedToken.UsedQuota == 0
	}, time.Second, 10*time.Millisecond)

	replay := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/pg/images", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "image-integration-failure")
	engine.ServeHTTP(replay, request)
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	require.Contains(t, replay.Body.String(), `"status":"failed"`)
	require.EqualValues(t, 1, providerCalls.Load())
	var generationCount int64
	require.NoError(t, db.Model(&model.KKAIImageGeneration{}).Count(&generationCount).Error)
	require.EqualValues(t, 1, generationCount)
	var managedToken model.Token
	require.NoError(t, db.First(&managedToken).Error)
	require.Zero(t, managedToken.RemainQuota)
	require.Zero(t, managedToken.UsedQuota)
}

func TestImageStudioInvalidSuccessPayloadIsRejectedBeforeSettlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	db := setupImageStudioIntegrationState(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"created":1,"data":[],"usage":{"input_tokens":1,"output_tokens":999,"total_tokens":1000}}`))
	}))
	t.Cleanup(upstream.Close)
	var channel model.Channel
	require.NoError(t, db.First(&channel).Error)
	channel.BaseURL = &upstream.URL
	require.NoError(t, db.Save(&channel).Error)
	model.InitChannelCache()

	store := &imageIntegrationAssetStore{objects: map[string][]byte{}}
	pipeline, err := service.NewImageAssetPipeline(
		db, store, service.NewHTTPImageArchiveFetcher(t.TempDir()), 1<<20, 100,
	)
	require.NoError(t, err)
	request := httptest.NewRequest(
		http.MethodPost, "/pg/images",
		bytes.NewReader(imageStudioIntegrationRequestBody(t, db, "An invalid empty response")),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "image-integration-invalid-success")
	response := httptest.NewRecorder()
	imageStudioIntegrationEngine(pipeline).ServeHTTP(response, request)

	require.Equal(t, http.StatusBadGateway, response.Code, response.Body.String())
	var generation model.KKAIImageGeneration
	require.NoError(t, db.First(&generation).Error)
	require.Equal(t, model.ImageGenerationStatusFailed, generation.Status)
	require.Equal(t, model.ImageGenerationBillingStateRefunded, generation.BillingState)
	require.Zero(t, generation.ReservedQuota)
	require.Zero(t, generation.FinalQuota)
	var user model.User
	require.NoError(t, db.First(&user, 407).Error)
	require.EqualValues(t, 100_000, user.Quota)
	var consumeLogs int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&consumeLogs).Error)
	require.Zero(t, consumeLogs)
}

func TestImageStudioUndeliverableSuccessPayloadIsRefunded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	db := setupImageStudioIntegrationState(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"created":1,"data":[{"b64_json":"not-an-image"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(upstream.Close)
	var channel model.Channel
	require.NoError(t, db.First(&channel).Error)
	channel.BaseURL = &upstream.URL
	require.NoError(t, db.Save(&channel).Error)
	model.InitChannelCache()

	store := &imageIntegrationAssetStore{objects: map[string][]byte{}}
	pipeline, err := service.NewImageAssetPipeline(
		db, store, service.NewHTTPImageArchiveFetcher(t.TempDir()), 1<<20, 100,
	)
	require.NoError(t, err)
	request := httptest.NewRequest(
		http.MethodPost, "/pg/images",
		bytes.NewReader(imageStudioIntegrationRequestBody(t, db, "An undeliverable image")),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "image-integration-undeliverable")
	response := httptest.NewRecorder()
	imageStudioIntegrationEngine(pipeline).ServeHTTP(response, request)

	require.Equal(t, http.StatusBadGateway, response.Code, response.Body.String())
	var generation model.KKAIImageGeneration
	require.NoError(t, db.First(&generation).Error)
	require.Equal(t, model.ImageGenerationStatusArchiveFailed, generation.Status)
	require.Equal(t, model.ImageGenerationBillingStateRefunded, generation.BillingState)
	require.Zero(t, generation.FinalQuota)
	var user model.User
	require.NoError(t, db.First(&user, 407).Error)
	require.EqualValues(t, 100_000, user.Quota)
	var consumeLogs int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&consumeLogs).Error)
	require.Zero(t, consumeLogs)
}

func TestImageStudioPartialArchiveIsDiscardedAndFullyRefunded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	db := setupImageStudioIntegrationState(t)
	const validImage = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(fmt.Sprintf(
			`{"created":1,"data":[{"b64_json":%q},{"b64_json":"not-an-image"}],`+
				`"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`,
			validImage,
		)))
	}))
	t.Cleanup(upstream.Close)
	var channel model.Channel
	require.NoError(t, db.First(&channel).Error)
	channel.BaseURL = &upstream.URL
	require.NoError(t, db.Save(&channel).Error)
	model.InitChannelCache()

	store := &imageIntegrationAssetStore{objects: map[string][]byte{}}
	pipeline, err := service.NewImageAssetPipeline(
		db, store, service.NewHTTPImageArchiveFetcher(t.TempDir()), 1<<20, 100,
	)
	require.NoError(t, err)
	request := httptest.NewRequest(
		http.MethodPost, "/pg/images",
		bytes.NewReader(imageStudioIntegrationRequestBodyWithCount(t, db, "A partial response", 2)),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "image-integration-partial")
	response := httptest.NewRecorder()
	imageStudioIntegrationEngine(pipeline).ServeHTTP(response, request)

	require.Equal(t, http.StatusBadGateway, response.Code, response.Body.String())
	var generation model.KKAIImageGeneration
	require.NoError(t, db.First(&generation).Error)
	require.Equal(t, model.ImageGenerationStatusArchiveFailed, generation.Status)
	require.Equal(t, model.ImageGenerationBillingStateRefunded, generation.BillingState)
	require.Zero(t, generation.FinalQuota)
	var activeAssets int64
	require.NoError(t, db.Model(&model.KKAIImageAsset{}).Where("deleted_at = 0").Count(&activeAssets).Error)
	require.Zero(t, activeAssets)
	var user model.User
	require.NoError(t, db.First(&user, 407).Error)
	require.EqualValues(t, 100_000, user.Quota)
	var consumeLogs int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&consumeLogs).Error)
	require.Zero(t, consumeLogs)
}

func imageStudioIntegrationRequestBody(t *testing.T, db *gorm.DB, prompt string) []byte {
	return imageStudioIntegrationRequestBodyWithCount(t, db, prompt, 1)
}

func imageStudioIntegrationRequestBodyWithCount(
	t *testing.T, db *gorm.DB, prompt string, count int,
) []byte {
	t.Helper()
	var token model.Token
	require.NoError(t, db.First(&token).Error)
	normalized, err := service.NormalizeImageStudioSubmission(context.Background(), db, 407, service.ImageStudioSubmissionRequest{
		TokenID: token.Id, Model: "gpt-image-1", Prompt: prompt,
		Parameters: map[string]any{"count": count},
	})
	require.NoError(t, err)
	quote, err := service.NewImageStudioQuote(normalized, 100_000, nil, nil)
	require.NoError(t, err)
	requestBody, err := common.Marshal(service.ImageStudioSubmissionRequest{
		TokenID: token.Id, Model: normalized.Model, Prompt: normalized.Prompt,
		Parameters: normalized.Parameters, QuoteToken: quote.QuoteToken,
	})
	require.NoError(t, err)
	return requestBody
}

func imageStudioIntegrationEngine(pipeline *service.ImageAssetPipeline) *gin.Engine {
	engine := gin.New()
	engine.POST(
		"/pg/images",
		func(c *gin.Context) {
			c.Set("id", 407)
			c.Set("user_group", "default")
			c.Set(imageStudioAssetPipelineContextKey, pipeline)
		},
		PrepareImageStudioRequest,
		middleware.Distribute(),
		SubmitImageStudioGeneration,
	)
	return engine
}

type imageIntegrationAssetStore struct {
	objects map[string][]byte
}

func (store *imageIntegrationAssetStore) PresignDownload(context.Context, string, string, bool, time.Duration) (string, error) {
	return "https://signed.example/image", nil
}

func (store *imageIntegrationAssetStore) Get(context.Context, string) (service.ImageAssetObject, error) {
	return service.ImageAssetObject{}, errors.New("not implemented")
}

func (store *imageIntegrationAssetStore) Put(_ context.Context, key string, _ string, reader io.Reader, size int64) error {
	body, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if int64(len(body)) != size {
		return fmt.Errorf("image size mismatch")
	}
	store.objects[key] = body
	return nil
}

func (store *imageIntegrationAssetStore) Delete(_ context.Context, keys []string) error {
	for _, key := range keys {
		delete(store.objects, key)
	}
	return nil
}

func setupImageStudioIntegrationState(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousErrorLogEnabled := constant.ErrorLogEnabled
	previousUsableGroups := setting.UserUsableGroups2JSONString()
	previousGroupRatios := ratio_setting.GroupRatio2JSONString()
	previousModelPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		constant.ErrorLogEnabled = previousErrorLogEnabled
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousModelPrices))
	})

	dsn := fmt.Sprintf("file:image-integration-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.UserSubscription{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.Log{},
		&model.KKAIIdempotencyKey{}, &model.KKAIOutboxEvent{},
		&model.KKAIImageModelProfile{}, &model.KKAIImageSample{},
		&model.KKAIImageGeneration{}, &model.KKAIImageAsset{},
	))
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	common.MemoryCacheEnabled = true
	constant.ErrorLogEnabled = true
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","图片工作室":"图片工作室"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"图片工作室":1}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"gpt-image-1":0.001}`))

	user := model.User{
		Id: 407, Username: "image-integration-user", Password: "password", DisplayName: "Image User",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", Quota: 100_000,
	}
	user.SetSetting(dto.UserSetting{BillingPreference: "wallet_only"})
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{
		UserId: user.Id, Key: "image-integration-token", Status: common.TokenStatusEnabled,
		Name: service.ImageStudioTokenGroup, CreatedTime: time.Now().Unix(), AccessedTime: time.Now().Unix(),
		ExpiredTime: -1, UnlimitedQuota: true, Group: service.ImageStudioTokenGroup,
	}
	require.NoError(t, db.Create(&token).Error)
	minimum := 1
	maximum := 4
	specification, err := common.Marshal(service.ImageModelSpec{Version: 1, Parameters: []service.ImageParameterSpec{
		{Key: "count", Label: "Count", Control: service.ImageControlInteger, RequestKey: "n", Min: &minimum, Max: &maximum},
	}})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.KKAIImageModelProfile{
		Model: "gpt-image-1", DisplayName: "Image", SpecificationVersion: 1,
		Specification: string(specification), DefaultParameters: `{"count":1}`,
		Enabled: true, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}).Error)
	priority := int64(0)
	weight := uint(100)
	autoBan := 0
	channel := model.Channel{
		Type: constant.ChannelTypeOpenAI, Key: "provider-key", Status: common.ChannelStatusEnabled,
		Name: "image provider", Models: "gpt-image-1", Group: service.ImageStudioTokenGroup,
		Priority: &priority, Weight: &weight, AutoBan: &autoBan,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: service.ImageStudioTokenGroup, Model: "gpt-image-1", ChannelId: channel.Id,
		Enabled: true, Priority: &priority, Weight: weight,
	}).Error)
	return db
}
