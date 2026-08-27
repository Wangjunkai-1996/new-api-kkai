package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	settingconfig "github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/image_pricing_setting"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"
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

func TestImageStudioRatioBatchQuoteAndSettlementMultiplyCountOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	db := setupImageStudioIntegrationState(t)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-image-1":1}`))

	const providerBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	var providerCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var providerRequest dto.ImageRequest
		require.NoError(t, common.DecodeJson(request.Body, &providerRequest))
		require.NotNil(t, providerRequest.N)
		providerCalls.Add(1)
		data := make([]map[string]string, int(*providerRequest.N))
		for index := range data {
			data[index] = map[string]string{"b64_json": providerBase64}
		}
		body, err := common.Marshal(map[string]any{
			"created": 1,
			"data":    data,
			"usage": map[string]int{
				"input_tokens": 1, "output_tokens": 1, "total_tokens": 2,
			},
		})
		require.NoError(t, err)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(body)
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

	var token model.Token
	require.NoError(t, db.First(&token).Error)
	quotes := make(map[int]service.ImageStudioQuote, 2)
	finalQuotas := make(map[int]int, 2)
	for _, count := range []int{1, 4} {
		quoteRequest := service.ImageStudioSubmissionRequest{
			TokenID: token.Id, Model: "gpt-image-1", Prompt: "A priced batch",
			Parameters: map[string]any{"count": count},
		}
		body, err := common.Marshal(quoteRequest)
		require.NoError(t, err)
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/pg/images/quote", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var envelope struct {
			Success bool                     `json:"success"`
			Data    service.ImageStudioQuote `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope))
		require.True(t, envelope.Success)
		quotes[count] = envelope.Data

		quoteRequest.QuoteToken = envelope.Data.QuoteToken
		body, err = common.Marshal(quoteRequest)
		require.NoError(t, err)
		response = httptest.NewRecorder()
		request = httptest.NewRequest(http.MethodPost, "/pg/images", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", fmt.Sprintf("ratio-batch-%d", count))
		engine.ServeHTTP(response, request)
		require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
		var generation model.KKAIImageGeneration
		require.NoError(t, db.Where("requested_count = ?", count).First(&generation).Error)
		require.Equal(t, model.ImageGenerationStatusSucceeded, generation.Status)
		require.Equal(t, count, generation.SucceededCount)
		finalQuotas[count] = generation.FinalQuota
	}

	require.Positive(t, quotes[1].Quota)
	assert.Equal(t, quotes[1].Quota*4, quotes[4].Quota)
	assert.Equal(t, float64(1), quotes[1].OtherRatios["n"])
	assert.Equal(t, float64(4), quotes[4].OtherRatios["n"])
	require.Positive(t, finalQuotas[1])
	assert.Equal(t, finalQuotas[1], finalQuotas[4])
	assert.EqualValues(t, 2, providerCalls.Load())
	assert.Len(t, store.objects, 5)
}

func TestImageStudioBatchAllFailedRefundsEntireReservation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	db := setupImageStudioIntegrationState(t)
	previousRetryTimes := common.RetryTimes
	common.RetryTimes = 0
	t.Cleanup(func() { common.RetryTimes = previousRetryTimes })

	var providerCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var payload map[string]any
		if err := common.DecodeJson(request.Body, &payload); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		assert.Equal(t, float64(4), payload["n"])
		providerCalls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte(`{"error":{"message":"candidate failed","type":"upstream_error"}}`))
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
	requestBody := imageStudioIntegrationRequestBodyWithCount(t, db, "No usable candidates", 4)
	request := httptest.NewRequest(http.MethodPost, "/pg/images", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "image-integration-all-failed-batch")
	response := httptest.NewRecorder()
	engine := imageStudioIntegrationEngine(pipeline)
	engine.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadGateway, response.Code, response.Body.String())
	assert.EqualValues(t, 1, providerCalls.Load())
	assertImageStudioIntegrationBatchRefunded(t, db)
	var generation model.KKAIImageGeneration
	require.NoError(t, db.First(&generation).Error)
	assert.Equal(t, model.ImageGenerationStatusFailed, generation.Status)
	assert.Zero(t, generation.SucceededCount)
	assert.Empty(t, store.objects)
	var accountingEvents int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where(
		"topic = ?", model.KKAIOutboxTopicImageAccounting,
	).Count(&accountingEvents).Error)
	assert.Zero(t, accountingEvents)

	replay := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/pg/images", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "image-integration-all-failed-batch")
	engine.ServeHTTP(replay, request)
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	assert.Contains(t, replay.Body.String(), `"status":"failed"`)
	assert.EqualValues(t, 1, providerCalls.Load())
}

func TestImageStudioSubmissionArchivesCompletedSSEAfterClientCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	db := setupImageStudioIntegrationState(t)

	const providerBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	imageBody, err := base64.StdEncoding.DecodeString(providerBase64)
	require.NoError(t, err)
	partialSent := make(chan struct{})
	releaseUpstream := make(chan struct{})
	var releaseOnce sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			t.Errorf("read image request: %v", readErr)
			return
		}
		var providerRequest struct {
			Stream bool `json:"stream"`
		}
		if decodeErr := common.Unmarshal(body, &providerRequest); decodeErr != nil {
			t.Errorf("decode image request: %v", decodeErr)
			return
		}
		if !providerRequest.Stream {
			t.Error("image request did not enable upstream streaming")
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		if _, writeErr := writer.Write([]byte("data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"partial\"}\n\n")); writeErr != nil {
			t.Errorf("write partial image event: %v", writeErr)
			return
		}
		writer.(http.Flusher).Flush()
		close(partialSent)
		<-releaseUpstream
		if _, writeErr := writer.Write([]byte(fmt.Sprintf(
			"data: {\"type\":\"image_generation.completed\",\"created_at\":1,\"b64_json\":%q,\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}\n\ndata: [DONE]\n\n",
			providerBase64,
		))); writeErr != nil {
			t.Errorf("write completed image event: %v", writeErr)
		}
		writer.(http.Flusher).Flush()
	}))
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseUpstream) })
		upstream.Close()
	})

	var channel model.Channel
	require.NoError(t, db.First(&channel).Error)
	paramOverride := `{"operations":[{"path":"stream","mode":"set","value":true}]}`
	channel.BaseURL = &upstream.URL
	channel.ParamOverride = &paramOverride
	require.NoError(t, db.Save(&channel).Error)
	model.InitChannelCache()

	store := &imageIntegrationAssetStore{objects: map[string][]byte{}}
	pipeline, err := service.NewImageAssetPipeline(
		db, store, service.NewHTTPImageArchiveFetcher(t.TempDir()), 1<<20, 100,
	)
	require.NoError(t, err)
	engine := imageStudioIntegrationEngine(pipeline)
	requestContext, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	request := httptest.NewRequest(
		http.MethodPost, "/pg/images",
		bytes.NewReader(imageStudioIntegrationRequestBody(t, db, "A completed image after disconnect")),
	).WithContext(requestContext)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "image-integration-canceled-sse")
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		engine.ServeHTTP(response, request)
		close(done)
	}()

	select {
	case <-partialSent:
	case <-time.After(time.Second):
		t.Fatal("upstream did not send the partial image event")
	}
	cancel()
	releaseOnce.Do(func() { close(releaseUpstream) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("image studio did not finish after the completed upstream event")
	}

	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var generation model.KKAIImageGeneration
	require.NoError(t, db.First(&generation).Error)
	assert.Equal(t, model.ImageGenerationStatusSucceeded, generation.Status)
	assert.Equal(t, model.ImageGenerationBillingStateSettled, generation.BillingState)
	assert.Positive(t, generation.FinalQuota)
	var asset model.KKAIImageAsset
	require.NoError(t, db.First(&asset).Error)
	assert.Equal(t, model.ImageAssetStateReady, asset.State)
	assert.Equal(t, imageBody, store.objects[asset.ObjectKey])
}

func TestImageStudioEditQuoteAndSubmitUseValidatedMultipartAndResolutionPricing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	db := setupImageStudioIntegrationState(t)
	enableImageStudioEditIntegrationModel(t, db)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"图片工作室":1.5}`))
	previousPolicy := image_pricing_setting.JSON()
	t.Cleanup(func() { require.NoError(t, image_pricing_setting.UpdateByJSONString(previousPolicy)) })
	pricingConfig := image_pricing_setting.DefaultConfig()
	pricingConfig.Enabled = true
	pricingJSON, err := common.Marshal(pricingConfig)
	require.NoError(t, err)
	require.NoError(t, image_pricing_setting.UpdateByJSONString(string(pricingJSON)))
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 407).Update("quota", 3_000_000).Error)

	const providerBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	imageBytes, err := base64.StdEncoding.DecodeString(providerBase64)
	require.NoError(t, err)
	type providerRequest struct {
		Path         string
		Model        string
		Prompt       string
		Size         string
		N            string
		Stream       string
		Images       [][]byte
		ContentTypes []string
	}
	observed := make(chan providerRequest, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		require.NoError(t, request.ParseMultipartForm(1<<20))
		fileHeaders := request.MultipartForm.File["image[]"]
		require.Len(t, fileHeaders, 2)
		images := make([][]byte, 0, len(fileHeaders))
		contentTypes := make([]string, 0, len(fileHeaders))
		for _, fileHeader := range fileHeaders {
			file, err := fileHeader.Open()
			require.NoError(t, err)
			body, readErr := io.ReadAll(file)
			closeErr := file.Close()
			require.NoError(t, errors.Join(readErr, closeErr))
			images = append(images, body)
			contentTypes = append(contentTypes, fileHeader.Header.Get("Content-Type"))
		}
		observed <- providerRequest{
			Path: request.URL.Path, Model: request.PostForm.Get("model"),
			Prompt: request.PostForm.Get("prompt"), Size: request.PostForm.Get("size"),
			N: request.PostForm.Get("n"), Stream: request.PostForm.Get("stream"),
			Images: images, ContentTypes: contentTypes,
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(fmt.Sprintf(
			`{"created":1,"data":[{"b64_json":%q}],`+
				`"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
			providerBase64,
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
	engine := imageStudioIntegrationEngine(pipeline)
	var token model.Token
	require.NoError(t, db.First(&token).Error)
	referenceImages := [][]byte{
		imageBytes,
		imageStudioEditTestPNG(t, color.RGBA{R: 0x20, G: 0x70, B: 0xd0, A: 0xff}),
	}
	references := imageStudioEditTestReferences(referenceImages)
	quoteRequest, err := common.Marshal(service.ImageStudioSubmissionRequest{
		TokenID: token.Id, Model: service.ImageStudioEditModel, Prompt: "Use this reference",
		Parameters: map[string]any{"count": 1, "size": "1024x1024"},
		References: references,
	})
	require.NoError(t, err)
	quoteResponse := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/pg/images/edits/quote", bytes.NewReader(quoteRequest))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(quoteResponse, request)
	require.Equal(t, http.StatusOK, quoteResponse.Code, quoteResponse.Body.String())
	var quoteEnvelope struct {
		Success bool                     `json:"success"`
		Data    service.ImageStudioQuote `json:"data"`
	}
	require.NoError(t, common.Unmarshal(quoteResponse.Body.Bytes(), &quoteEnvelope))
	require.True(t, quoteEnvelope.Success)
	require.Equal(t, 502_500, quoteEnvelope.Data.Quota)
	require.Equal(t, float64(1), quoteEnvelope.Data.OtherRatios["n"])
	require.NotEmpty(t, quoteEnvelope.Data.QuoteToken)

	submitJSON, err := common.Marshal(service.ImageStudioSubmissionRequest{
		TokenID: token.Id, Model: service.ImageStudioEditModel, Prompt: "Use this reference",
		Parameters: map[string]any{"count": 1, "size": "1024x1024"},
		QuoteToken: quoteEnvelope.Data.QuoteToken, References: references,
	})
	require.NoError(t, err)
	submitBody, submitContentType := imageStudioEditMultipartBody(t, submitJSON, referenceImages, false)
	submitResponse := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/pg/images/edits", bytes.NewReader(submitBody))
	request.Header.Set("Content-Type", submitContentType)
	request.Header.Set("Idempotency-Key", "image-edit-integration-submit")
	engine.ServeHTTP(submitResponse, request)
	require.Equal(t, http.StatusCreated, submitResponse.Code, submitResponse.Body.String())

	providerCall := <-observed
	assert.Equal(t, "/v1/images/edits", providerCall.Path)
	assert.Equal(t, service.ImageStudioEditModel, providerCall.Model)
	assert.Equal(t, "Use this reference", providerCall.Prompt)
	assert.Equal(t, "1024x1024", providerCall.Size)
	assert.Equal(t, "1", providerCall.N)
	assert.Equal(t, "false", providerCall.Stream)
	assert.Equal(t, []string{"image/png", "image/png"}, providerCall.ContentTypes)
	assert.Equal(t, referenceImages, providerCall.Images)

	var generation model.KKAIImageGeneration
	require.NoError(t, db.First(&generation).Error)
	assert.Equal(t, service.ImageStudioEditModel, generation.Model)
	assert.Equal(t, model.ImageGenerationStatusSucceeded, generation.Status)
	assert.Equal(t, 1, generation.RequestedCount)
	assert.Equal(t, 1, generation.SucceededCount)
	assert.Equal(t, quoteEnvelope.Data.Quota, generation.FinalQuota)
	assert.Len(t, store.objects, 1)
	var assets []model.KKAIImageAsset
	require.NoError(t, db.Order("position ASC, id ASC").Find(&assets).Error)
	require.Len(t, assets, 1)
	for position, asset := range assets {
		assert.Equal(t, position, asset.Position)
	}
	var accountingEvent model.KKAIOutboxEvent
	require.NoError(t, db.Where(
		"topic = ? AND aggregate_id = ?",
		model.KKAIOutboxTopicImageAccounting, fmt.Sprintf("%d", generation.ID),
	).First(&accountingEvent).Error)
	require.NoError(t, (service.ImageGenerationAccountingHandler{}).Handle(context.Background(), accountingEvent))
	var consumeLog model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&consumeLog).Error)
	assert.Equal(t, quoteEnvelope.Data.Quota, consumeLog.Quota)
	assert.Equal(t, service.ImageStudioEditModel, consumeLog.ModelName)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(consumeLog.Other, &other))
	adminInfo, ok := other["admin_info"].(map[string]any)
	require.True(t, ok)
	pricingSnapshot, ok := adminInfo["image_pricing"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, service.ImageStudioEditModel, pricingSnapshot["model"])
	assert.Equal(t, "1024x1024", pricingSnapshot["size"])
	assert.Equal(t, 0.67, pricingSnapshot["unit_price"])
	assert.Equal(t, 1.5, pricingSnapshot["group_ratio"])
	assert.Equal(t, float64(1), pricingSnapshot["requested_count"])
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

func TestImageStudioPartialArchiveChargesOnlyReadyAsset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	db := setupImageStudioIntegrationState(t)
	const validImage = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	var providerCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var payload map[string]any
		if err := common.DecodeJson(request.Body, &payload); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		assert.Equal(t, float64(2), payload["n"])
		providerCalls.Add(1)
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

	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	assert.EqualValues(t, 1, providerCalls.Load())
	var generation model.KKAIImageGeneration
	require.NoError(t, db.First(&generation).Error)
	require.Equal(t, model.ImageGenerationStatusPartial, generation.Status)
	require.Equal(t, model.ImageGenerationBillingStateSettled, generation.BillingState)
	require.Equal(t, 1, generation.SucceededCount)
	unitQuota, err := common.QuotaFromFloatStrict(0.001 * common.QuotaPerUnit)
	require.NoError(t, err)
	require.Equal(t, unitQuota, generation.FinalQuota)
	var readyAssets int64
	require.NoError(t, db.Model(&model.KKAIImageAsset{}).Where(
		"generation_id = ? AND state = ? AND deleted_at = 0",
		generation.ID, model.ImageAssetStateReady,
	).Count(&readyAssets).Error)
	require.EqualValues(t, 1, readyAssets)
	var activeAssets int64
	require.NoError(t, db.Model(&model.KKAIImageAsset{}).Where(
		"generation_id = ? AND deleted_at = 0", generation.ID,
	).Count(&activeAssets).Error)
	require.EqualValues(t, 1, activeAssets)
	require.Len(t, store.objects, 1)
	var user model.User
	require.NoError(t, db.First(&user, 407).Error)
	require.EqualValues(t, 100_000-unitQuota, user.Quota)
}

func TestImageStudioArchiveHardFailurePreservesReadyAssetAndChargesIt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	db := setupImageStudioIntegrationState(t)
	const validImage = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	var providerCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var payload map[string]any
		if err := common.DecodeJson(request.Body, &payload); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		assert.Equal(t, float64(2), payload["n"])
		providerCalls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(fmt.Sprintf(
			`{"created":1,"data":[{"b64_json":%q},{"b64_json":%q}],`+
				`"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`,
			validImage, validImage,
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
	callbackName := "test:image_studio_archive_hard_failure"
	var objectCompensationCreates atomic.Int32
	var injected atomic.Bool
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != (model.KKAIOutboxEvent{}).TableName() {
			return
		}
		event, ok := tx.Statement.Dest.(*model.KKAIOutboxEvent)
		if !ok || event.Topic != service.ImageAssetDeleteTopic {
			return
		}
		if objectCompensationCreates.Add(1) == 2 {
			injected.Store(true)
			tx.AddError(errors.New("forced second image archive failure"))
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callbackName) })

	request := httptest.NewRequest(
		http.MethodPost, "/pg/images",
		bytes.NewReader(imageStudioIntegrationRequestBodyWithCount(t, db, "A recoverable archive failure", 2)),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "image-integration-archive-hard-failure")
	response := httptest.NewRecorder()
	imageStudioIntegrationEngine(pipeline).ServeHTTP(response, request)

	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	require.True(t, injected.Load())
	assert.EqualValues(t, 1, providerCalls.Load())
	var generation model.KKAIImageGeneration
	require.NoError(t, db.First(&generation).Error)
	assert.Equal(t, model.ImageGenerationStatusPartial, generation.Status)
	assert.Equal(t, model.ImageGenerationBillingStateSettled, generation.BillingState)
	assert.Equal(t, 2, generation.RequestedCount)
	assert.Equal(t, 1, generation.SucceededCount)
	assert.Equal(t, "archive", generation.FailureStage)
	assert.Equal(t, "partial_archive", generation.ErrorCode)
	unitQuota, err := common.QuotaFromFloatStrict(0.001 * common.QuotaPerUnit)
	require.NoError(t, err)
	assert.Equal(t, unitQuota, generation.FinalQuota)

	var assets []model.KKAIImageAsset
	require.NoError(t, db.Unscoped().Where("generation_id = ?", generation.ID).
		Order("position ASC, id ASC").Find(&assets).Error)
	require.Len(t, assets, 2)
	assert.Equal(t, model.ImageAssetStateReady, assets[0].State)
	assert.Zero(t, assets[0].DeletedAt)
	assert.Contains(t, store.objects, assets[0].ObjectKey)
	assert.Equal(t, model.ImageAssetStateDeleted, assets[1].State)
	assert.NotZero(t, assets[1].DeletedAt)
	assert.NotContains(t, store.objects, assets[1].ObjectKey)
	assert.Len(t, store.objects, 1)
	var deletionEvents int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Where(
		"topic = ? AND aggregate_id = ?", service.ImageAssetDeleteTopic, fmt.Sprintf("%d", assets[1].ID),
	).Count(&deletionEvents).Error)
	assert.EqualValues(t, 1, deletionEvents)
	var user model.User
	require.NoError(t, db.First(&user, 407).Error)
	assert.EqualValues(t, 100_000-unitQuota, user.Quota)
}

func TestImageStudioShortBatchResponseDeliversAndChargesReadyAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	db := setupImageStudioIntegrationState(t)
	const validImage = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	var providerCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var payload map[string]any
		if err := common.DecodeJson(request.Body, &payload); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		assert.Equal(t, float64(4), payload["n"])
		providerCalls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(fmt.Sprintf(
			`{"created":1,"data":[{"b64_json":%q},{"b64_json":%q},{"b64_json":%q}],`+
				`"usage":{"input_tokens":1,"output_tokens":3,"total_tokens":4}}`,
			validImage, validImage, validImage,
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
		bytes.NewReader(imageStudioIntegrationRequestBodyWithCount(t, db, "A short batch", 4)),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "image-integration-short-batch")
	response := httptest.NewRecorder()
	imageStudioIntegrationEngine(pipeline).ServeHTTP(response, request)

	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	assert.EqualValues(t, 1, providerCalls.Load())
	var generation model.KKAIImageGeneration
	require.NoError(t, db.First(&generation).Error)
	assert.Equal(t, model.ImageGenerationStatusPartial, generation.Status)
	assert.Equal(t, model.ImageGenerationBillingStateSettled, generation.BillingState)
	assert.Equal(t, 4, generation.RequestedCount)
	assert.Equal(t, 3, generation.SucceededCount)
	unitQuota, err := common.QuotaFromFloatStrict(0.001 * common.QuotaPerUnit)
	require.NoError(t, err)
	assert.Equal(t, 3*unitQuota, generation.FinalQuota)
	var readyAssets int64
	require.NoError(t, db.Model(&model.KKAIImageAsset{}).Where(
		"generation_id = ? AND state = ? AND deleted_at = 0",
		generation.ID, model.ImageAssetStateReady,
	).Count(&readyAssets).Error)
	assert.EqualValues(t, 3, readyAssets)
	assert.Len(t, store.objects, 3)
	var user model.User
	require.NoError(t, db.First(&user, 407).Error)
	assert.EqualValues(t, 100_000-3*unitQuota, user.Quota)
}

func TestImageStudioOversizedBatchResponseIsFullyRefunded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	settings := image_studio_setting.Get()
	require.NoError(t, settingconfig.GlobalConfig.LoadFromDB(map[string]string{
		"image_studio.max_output_bytes":   "256",
		"image_studio.max_response_bytes": "256",
	}))
	t.Cleanup(func() {
		require.NoError(t, settingconfig.GlobalConfig.LoadFromDB(map[string]string{
			"image_studio.max_output_bytes":   fmt.Sprint(settings.MaxOutputBytes),
			"image_studio.max_response_bytes": fmt.Sprint(settings.MaxResponseBytes),
		}))
	})
	db := setupImageStudioIntegrationState(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(fmt.Sprintf(
			`{"created":1,"data":[{"b64_json":%q}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
			strings.Repeat("A", 512),
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
		bytes.NewReader(imageStudioIntegrationRequestBodyWithCount(t, db, "An oversized batch", 4)),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "image-integration-oversized-batch")
	response := httptest.NewRecorder()
	imageStudioIntegrationEngine(pipeline).ServeHTTP(response, request)

	require.Equal(t, http.StatusBadGateway, response.Code, response.Body.String())
	assertImageStudioIntegrationBatchRefunded(t, db)
	assert.Empty(t, store.objects)
}

func assertImageStudioIntegrationBatchRefunded(t *testing.T, db *gorm.DB) {
	t.Helper()
	var generation model.KKAIImageGeneration
	require.NoError(t, db.First(&generation).Error)
	assert.NotEqual(t, model.ImageGenerationStatusSucceeded, generation.Status)
	assert.Equal(t, model.ImageGenerationBillingStateRefunded, generation.BillingState)
	assert.Zero(t, generation.FinalQuota)
	var activeAssets int64
	require.NoError(t, db.Model(&model.KKAIImageAsset{}).Where("deleted_at = 0").Count(&activeAssets).Error)
	assert.Zero(t, activeAssets)
	var user model.User
	require.NoError(t, db.First(&user, 407).Error)
	assert.EqualValues(t, 100_000, user.Quota)
	var consumeLogs int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&consumeLogs).Error)
	assert.Zero(t, consumeLogs)
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
	quote, err := service.NewImageStudioQuote(
		normalized, 100_000, map[string]float64{"n": float64(normalized.RequestedCount)}, nil,
	)
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
	setup := func(c *gin.Context) {
		c.Set("id", 407)
		c.Set("user_group", "default")
		if pipeline != nil {
			c.Set(imageStudioAssetPipelineContextKey, pipeline)
		}
	}
	engine.POST("/pg/images/quote", setup, PrepareImageStudioRequest, middleware.Distribute(), QuoteImageStudioGeneration)
	engine.POST("/pg/images", setup, PrepareImageStudioRequest, middleware.Distribute(), SubmitImageStudioGeneration)
	engine.POST("/pg/images/edits/quote", setup, PrepareImageStudioRequest, middleware.Distribute(), QuoteImageStudioGeneration)
	engine.POST("/pg/images/edits", setup, PrepareImageStudioRequest, middleware.Distribute(), SubmitImageStudioGeneration)
	return engine
}

func enableImageStudioEditIntegrationModel(t *testing.T, db *gorm.DB) {
	t.Helper()
	minimum := 1
	maximum := 4
	specification, err := common.Marshal(service.ImageModelSpec{Version: 1, MaxReferenceImages: 2, Parameters: []service.ImageParameterSpec{
		{Key: "count", Label: "Count", Control: service.ImageControlInteger, RequestKey: "n", Min: &minimum, Max: &maximum},
		{Key: "size", Label: "Size", Control: service.ImageControlSelect, RequestKey: "size", Options: []service.ImageParameterOption{
			{Label: "Square", Value: "1024x1024"},
		}},
	}})
	require.NoError(t, err)
	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.KKAIImageModelProfile{
		Model: service.ImageStudioEditModel, DisplayName: "Image Edit", SpecificationVersion: 1,
		Specification: string(specification), DefaultParameters: `{"count":1,"size":"1024x1024"}`,
		Enabled: true, CreatedAt: now, UpdatedAt: now,
	}).Error)
	var channel model.Channel
	require.NoError(t, db.First(&channel).Error)
	require.NoError(t, db.Model(&channel).Update("models", "gpt-image-1,gpt-image-2").Error)
	priority := int64(0)
	weight := uint(100)
	require.NoError(t, db.Create(&model.Ability{
		Group: service.ImageStudioTokenGroup, Model: service.ImageStudioEditModel, ChannelId: channel.Id,
		Enabled: true, Priority: &priority, Weight: weight,
	}).Error)
	model.InitChannelCache()
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
	previousModelRatios := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		constant.ErrorLogEnabled = previousErrorLogEnabled
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousModelPrices))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousModelRatios))
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
