package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/image_pricing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

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
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 407).Update("quota", 2_000_000).Error)

	const providerBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	imageBytes, err := base64.StdEncoding.DecodeString(providerBase64)
	require.NoError(t, err)
	type providerRequest struct {
		Path        string
		Model       string
		Prompt      string
		Size        string
		N           string
		Stream      string
		Image       []byte
		ContentType string
	}
	observed := make(chan providerRequest, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		require.NoError(t, request.ParseMultipartForm(1<<20))
		require.Len(t, request.MultipartForm.File["image"], 1)
		fileHeader := request.MultipartForm.File["image"][0]
		file, err := fileHeader.Open()
		require.NoError(t, err)
		body, err := io.ReadAll(file)
		require.NoError(t, file.Close())
		require.NoError(t, err)
		observed <- providerRequest{
			Path: request.URL.Path, Model: request.PostForm.Get("model"),
			Prompt: request.PostForm.Get("prompt"), Size: request.PostForm.Get("size"),
			N: request.PostForm.Get("n"), Stream: request.PostForm.Get("stream"), Image: body,
			ContentType: fileHeader.Header.Get("Content-Type"),
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(fmt.Sprintf(
			`{"created":1,"data":[{"b64_json":%q},{"b64_json":%q}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
			providerBase64, providerBase64,
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
	digest := sha256.Sum256(imageBytes)
	reference := &service.ImageStudioReferenceMetadata{
		SHA256: hex.EncodeToString(digest[:]), SizeBytes: int64(len(imageBytes)),
	}
	quoteRequest, err := common.Marshal(service.ImageStudioSubmissionRequest{
		TokenID: token.Id, Model: service.ImageStudioEditModel, Prompt: "Use this reference",
		Parameters: map[string]any{"count": 2, "size": "1024x1024"}, Reference: reference,
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
	require.Equal(t, 1_005_000, quoteEnvelope.Data.Quota)
	require.Equal(t, float64(2), quoteEnvelope.Data.OtherRatios["n"])
	require.NotEmpty(t, quoteEnvelope.Data.QuoteToken)

	submitJSON, err := common.Marshal(service.ImageStudioSubmissionRequest{
		TokenID: token.Id, Model: service.ImageStudioEditModel, Prompt: "Use this reference",
		Parameters: map[string]any{"count": 2, "size": "1024x1024"},
		QuoteToken: quoteEnvelope.Data.QuoteToken,
	})
	require.NoError(t, err)
	submitBody, submitContentType := imageStudioEditMultipartBody(t, submitJSON, imageBytes, false)
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
	assert.Equal(t, "2", providerCall.N)
	assert.Equal(t, "false", providerCall.Stream)
	assert.Equal(t, "image/png", providerCall.ContentType)
	assert.Equal(t, imageBytes, providerCall.Image)

	var generation model.KKAIImageGeneration
	require.NoError(t, db.First(&generation).Error)
	assert.Equal(t, service.ImageStudioEditModel, generation.Model)
	assert.Equal(t, model.ImageGenerationStatusSucceeded, generation.Status)
	assert.Equal(t, 2, generation.SucceededCount)
	assert.Equal(t, quoteEnvelope.Data.Quota, generation.FinalQuota)
	assert.Len(t, store.objects, 2)
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
	assert.Equal(t, float64(2), pricingSnapshot["requested_count"])
}

func enableImageStudioEditIntegrationModel(t *testing.T, db *gorm.DB) {
	t.Helper()
	minimum := 1
	maximum := 4
	specification, err := common.Marshal(service.ImageModelSpec{Version: 1, Parameters: []service.ImageParameterSpec{
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
