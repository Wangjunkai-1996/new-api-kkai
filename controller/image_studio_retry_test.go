package controller

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldRetryKeepsImageStudioChannelFailoverEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamErr := types.NewErrorWithStatusCode(
		errors.New("upstream unavailable"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadGateway,
	)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.NoError(t, service.SetImageStudioGenerationID(ctx, 1))
	assert.True(t, shouldRetry(ctx, upstreamErr, 1))
}

func TestImageStudioBatchKeepsExistingChannelFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	db := setupImageStudioIntegrationState(t)

	previousRetryTimes := common.RetryTimes
	common.RetryTimes = 1
	t.Cleanup(func() { common.RetryTimes = previousRetryTimes })

	var firstCalls atomic.Int32
	firstUpstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var payload map[string]any
		if err := common.DecodeJson(request.Body, &payload); err != nil {
			t.Errorf("decode first provider request: %v", err)
		}
		assert.Equal(t, float64(2), payload["n"])
		firstCalls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte(`{"error":{"message":"first upstream failed","type":"upstream_error"}}`))
	}))
	t.Cleanup(firstUpstream.Close)

	var fallbackCalls atomic.Int32
	fallbackUpstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var payload map[string]any
		if err := common.DecodeJson(request.Body, &payload); err != nil {
			t.Errorf("decode fallback provider request: %v", err)
		}
		assert.Equal(t, float64(2), payload["n"])
		fallbackCalls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(
			`{"created":1,"data":[` +
				`{"b64_json":"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="},` +
				`{"b64_json":"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="}` +
				`],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`,
		))
	}))
	t.Cleanup(fallbackUpstream.Close)

	highPriority := int64(10)
	var firstChannel model.Channel
	require.NoError(t, db.First(&firstChannel).Error)
	require.NoError(t, db.Model(&firstChannel).Updates(map[string]any{
		"base_url": firstUpstream.URL,
		"priority": highPriority,
	}).Error)
	require.NoError(t, db.Model(&model.Ability{}).
		Where("channel_id = ?", firstChannel.Id).
		Update("priority", highPriority).Error)

	lowPriority := int64(0)
	weight := uint(100)
	autoBan := 0
	fallbackBaseURL := fallbackUpstream.URL
	fallbackChannel := model.Channel{
		Type: constant.ChannelTypeOpenAI, Key: "fallback-provider-key", Status: common.ChannelStatusEnabled,
		Name: "image fallback provider", Models: "gpt-image-1", Group: service.ImageStudioTokenGroup,
		BaseURL: &fallbackBaseURL, Priority: &lowPriority, Weight: &weight, AutoBan: &autoBan,
	}
	require.NoError(t, db.Create(&fallbackChannel).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: service.ImageStudioTokenGroup, Model: "gpt-image-1", ChannelId: fallbackChannel.Id,
		Enabled: true, Priority: &lowPriority, Weight: weight,
	}).Error)
	model.InitChannelCache()

	store := &imageIntegrationAssetStore{objects: map[string][]byte{}}
	pipeline, err := service.NewImageAssetPipeline(
		db, store, service.NewHTTPImageArchiveFetcher(t.TempDir()), 1<<20, 100,
	)
	require.NoError(t, err)
	request := httptest.NewRequest(
		http.MethodPost,
		"/pg/images",
		bytes.NewReader(imageStudioIntegrationRequestBodyWithCount(t, db, "A batch with normal failover", 2)),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "image-integration-batch-failover")
	response := httptest.NewRecorder()
	imageStudioIntegrationEngine(pipeline).ServeHTTP(response, request)

	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	assert.EqualValues(t, 1, firstCalls.Load())
	assert.EqualValues(t, 1, fallbackCalls.Load())
	var generation model.KKAIImageGeneration
	require.NoError(t, db.First(&generation).Error)
	assert.Equal(t, model.ImageGenerationStatusSucceeded, generation.Status)
	assert.Equal(t, 2, generation.SucceededCount)
	assert.Len(t, store.objects, 2)
}
