package controller

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/sora"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVideoStudioQuotePayloadSubmitsThroughSpecialSoraContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, token := newVideoStudioRelayTestDB(t)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	minimum, maximum, step := 1.0, 10.0, 1.0
	const studioModel = "video-studio-special-v1"
	specification, err := common.Marshal(service.VideoModelSpec{
		Version: 1,
		Modes:   []string{service.VideoModeTextToVideo},
		Parameters: []service.VideoParameterSpec{
			{Key: "duration", Label: "Duration", Control: service.VideoControlNumber, Required: true, Min: &minimum, Max: &maximum, Step: &step},
			{Key: "ratio", Label: "Ratio", Control: service.VideoControlSelect, Required: true, Options: []service.VideoParameterOption{{Label: "16:9", Value: "16:9"}}},
			{Key: "generate_audio", Label: "Audio", Control: service.VideoControlSwitch, Required: true},
		},
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.KKAIVideoModelProfile{
		Model: studioModel, DisplayName: "Special Video", Description: "test",
		SpecificationVersion: 1, Specification: string(specification), DefaultParameters: `{}`, Enabled: true,
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}).Error)
	priority := int64(0)
	require.NoError(t, db.Create(&model.Ability{
		Group: service.VideoStudioTokenGroup, Model: studioModel, ChannelId: 2,
		Enabled: true, Priority: &priority,
	}).Error)

	creativeRequest := service.VideoStudioSubmissionRequest{
		TokenID: token.Id,
		Model:   studioModel,
		Mode:    service.VideoModeTextToVideo,
		Prompt:  "A precise camera movement",
		Parameters: map[string]any{
			"duration":       5,
			"ratio":          "16:9",
			"generate_audio": false,
		},
	}
	prepare := func(t *testing.T, path string, request service.VideoStudioSubmissionRequest, idempotencyKey string) *gin.Context {
		t.Helper()
		body, err := common.Marshal(request)
		require.NoError(t, err)
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		if idempotencyKey != "" {
			ctx.Request.Header.Set("Idempotency-Key", idempotencyKey)
		}
		ctx.Set("id", 7)
		ctx.Set("user_group", "default")
		ctx.Set(videoStudioAssetStoreContextKey, videoStudioRelayTestStore{})
		t.Cleanup(func() { common.CleanupBodyStorage(ctx) })
		PrepareVideoStudioTaskRequest(ctx)
		require.False(t, ctx.IsAborted())
		return ctx
	}

	quoteContext := prepare(t, "/pg/videos/quote", creativeRequest, "")
	quoteNormalized, ok := videoStudioNormalizedSubmission(quoteContext)
	require.True(t, ok)
	quote := service.NewVideoStudioQuote(quoteNormalized, 12345, nil)

	maxQuota := quote.Quota
	submitRequest := creativeRequest
	submitRequest.MaxQuota = &maxQuota
	submitRequest.QuoteHash = quote.RequestHash
	submitRequest.QuoteExpiresAt = quote.ExpiresAt
	submitContext := prepare(t, "/pg/videos", submitRequest, "special-submit-key")
	submitNormalized, ok := videoStudioNormalizedSubmission(submitContext)
	require.True(t, ok)
	assert.Equal(t, quoteNormalized.RequestHash, submitNormalized.RequestHash)
	require.NoError(t, service.ValidateVideoStudioQuote(submitNormalized, time.Now()))

	var genericPayload map[string]any
	require.NoError(t, common.Unmarshal(submitNormalized.TaskPayload, &genericPayload))
	assert.Contains(t, genericPayload, "group")
	assert.Contains(t, genericPayload, "mode")
	assert.Contains(t, genericPayload, "metadata")

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "sd_2.0_special_1080p"},
	}
	body, err := (&sora.TaskAdaptor{}).BuildRequestBody(submitContext, info)
	require.NoError(t, err)
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	var outbound map[string]any
	require.NoError(t, common.Unmarshal(raw, &outbound))
	assert.Equal(t, map[string]any{
		"model":          "sd_2.0_special_1080p",
		"prompt":         "A precise camera movement",
		"duration":       float64(5),
		"ratio":          "16:9",
		"generate_audio": false,
	}, outbound)
}
