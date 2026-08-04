package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPrepareImageStudioRequestRewritesOnlyValidatedRelayFields(t *testing.T) {
	db, token := newImageStudioRelayTestDB(t)
	body, err := common.Marshal(service.ImageStudioSubmissionRequest{
		TokenID: token.Id, Model: "gpt-image-1", Prompt: "draw a lighthouse",
		Parameters: map[string]any{"count": 2},
	})
	require.NoError(t, err)
	ctx, recorder := newImageStudioRelayContext(http.MethodPost, "/pg/images/quote", body)

	PrepareImageStudioRequest(ctx)

	require.False(t, ctx.IsAborted())
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "/v1/images/generations", ctx.Request.URL.Path)
	assert.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyIsPlayground))
	assert.Equal(t, service.ImageStudioTokenGroup, common.GetContextKeyString(ctx, constant.ContextKeyUsingGroup))
	assert.Equal(t, token.Id, ctx.GetInt("token_id"))
	rewritten, err := io.ReadAll(ctx.Request.Body)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(rewritten, &payload))
	assert.Equal(t, "gpt-image-1", payload["model"])
	assert.Equal(t, "draw a lighthouse", payload["prompt"])
	assert.Equal(t, float64(2), payload["n"])
	assert.Equal(t, false, payload["stream"])
	assert.NotContains(t, payload, "token_id")
	assert.NotContains(t, payload, "parameters")
	assert.NotContains(t, payload, "extra_fields")
	assert.NotContains(t, payload, "response_format")

	var reservations int64
	require.NoError(t, db.Model(&model.KKAIIdempotencyKey{}).Count(&reservations).Error)
	assert.Zero(t, reservations)
}

func TestPrepareImageStudioSubmitRequiresIdempotencyBeforeRelay(t *testing.T) {
	_, token := newImageStudioRelayTestDB(t)
	body, err := common.Marshal(service.ImageStudioSubmissionRequest{
		TokenID: token.Id, Model: "gpt-image-1", Prompt: "draw a lighthouse",
		QuoteToken: "quote-token",
	})
	require.NoError(t, err)
	ctx, recorder := newImageStudioRelayContext(http.MethodPost, "/pg/images", body)

	PrepareImageStudioRequest(ctx)

	assert.True(t, ctx.IsAborted())
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "idempotency_key_required")
}

func TestPrepareImageStudioRequestReleasesUnboundReservationAfterClientCancellation(t *testing.T) {
	db, token := newImageStudioRelayTestDB(t)
	body, err := common.Marshal(service.ImageStudioSubmissionRequest{
		TokenID: token.Id, Model: "gpt-image-1", Prompt: "draw a lighthouse",
		QuoteToken: "quote-token",
	})
	require.NoError(t, err)
	requestContext, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodPost, "/pg/images", bytes.NewReader(body)).WithContext(requestContext)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "disconnect-before-binding")
	recorder := httptest.NewRecorder()
	engine := gin.New()
	engine.POST("/pg/images", func(c *gin.Context) {
		c.Set("id", 7)
		c.Set("user_group", "default")
	}, PrepareImageStudioRequest, func(c *gin.Context) {
		cancel()
		c.Status(http.StatusNoContent)
	})

	engine.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusNoContent, recorder.Code)
	var reservations int64
	require.NoError(t, db.Model(&model.KKAIIdempotencyKey{}).Count(&reservations).Error)
	require.Zero(t, reservations)
}

func TestImageStudioErrorStatusPreservesClientAndConcurrencyFailures(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{service.ErrImageModelProfileConflict, http.StatusConflict, "image_studio_conflict"},
		{imagepricing.ErrUnsupportedSize, http.StatusBadRequest, "invalid_image_size"},
		{service.ErrImageArchiveTooLarge, http.StatusRequestEntityTooLarge, "image_asset_too_large"},
		{service.ErrImageArchiveMIMERejected, http.StatusBadRequest, "invalid_image_asset"},
		{service.ErrImageTemporaryStorageUnavailable, http.StatusServiceUnavailable, "image_temporary_storage_unavailable"},
	}
	for _, test := range tests {
		status, code := imageStudioErrorStatus(test.err)
		assert.Equal(t, test.status, status)
		assert.Equal(t, test.code, code)
	}
}

func TestImageStudioSubmissionCapacityEnforcesGlobalAndPerUserBounds(t *testing.T) {
	resetImageStudioSubmissionCapacity(t)
	require.True(t, imageStudioCapacity.acquire(7, 2, 1))
	require.False(t, imageStudioCapacity.acquire(7, 2, 1))
	require.True(t, imageStudioCapacity.acquire(8, 2, 1))
	require.False(t, imageStudioCapacity.acquire(9, 2, 1))

	imageStudioCapacity.release(7)
	require.True(t, imageStudioCapacity.acquire(9, 2, 1))
	imageStudioCapacity.release(8)
	imageStudioCapacity.release(9)
	imageStudioCapacity.mu.Lock()
	require.Zero(t, imageStudioCapacity.total)
	require.Empty(t, imageStudioCapacity.byUser)
	imageStudioCapacity.mu.Unlock()

	status, code := imageStudioErrorStatus(service.ErrImageStudioCapacityExceeded)
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Equal(t, "image_studio_busy", code)
}

func TestImageStudioIdempotentReplayStatusContract(t *testing.T) {
	db, token := newImageStudioRelayTestDB(t)
	now := time.Now().Unix()
	tests := []struct {
		status       string
		responseCode int
		retryAfter   string
		viewStatus   string
	}{
		{model.ImageGenerationStatusSubmitting, http.StatusAccepted, "2", model.ImageGenerationStatusSubmitting},
		{model.ImageGenerationStatusRecovering, http.StatusAccepted, "2", model.ImageGenerationStatusSubmitting},
		{model.ImageGenerationStatusSucceeded, http.StatusOK, "", model.ImageGenerationStatusSucceeded},
	}
	for index, test := range tests {
		billingState := model.ImageGenerationBillingStatePending
		if test.status == model.ImageGenerationStatusSucceeded {
			billingState = model.ImageGenerationBillingStateSettled
		}
		generation := model.KKAIImageGeneration{
			UserID: 7, TokenID: token.Id, ModelProfileID: 1, SpecificationVersion: 1,
			Model: "gpt-image-1", Prompt: "idempotent replay", Parameters: `{}`,
			RequestHash: fmt.Sprintf("%064d", index+1), RequestID: fmt.Sprintf("replay-%d", index),
			Status: test.status, RequestedCount: 1, BillingState: billingState,
			HeartbeatAt: now, StartedAt: now, CreatedAt: now, UpdatedAt: now,
		}
		require.NoError(t, db.Create(&generation).Error)
		ctx, recorder := newImageStudioRelayContext(http.MethodPost, "/pg/images", nil)

		respondImageStudioIdempotentReplay(ctx, strconv.FormatInt(generation.ID, 10))

		require.Equal(t, test.responseCode, recorder.Code)
		assert.Equal(t, test.retryAfter, recorder.Header().Get("Retry-After"))
		assert.Contains(t, recorder.Body.String(), `"status":"`+test.viewStatus+`"`)
	}
}

func resetImageStudioSubmissionCapacity(t *testing.T) {
	t.Helper()
	reset := func() {
		imageStudioCapacity.mu.Lock()
		imageStudioCapacity.total = 0
		clear(imageStudioCapacity.byUser)
		imageStudioCapacity.mu.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

func newImageStudioRelayTestDB(t *testing.T) (*gorm.DB, model.Token) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previousDB := model.DB
	previousRedisEnabled := common.RedisEnabled
	previousUsableGroups := setting.UserUsableGroups2JSONString()
	previousGroupRatios := ratio_setting.GroupRatio2JSONString()
	previousSpecialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.MarshalJSONString()
	common.RedisEnabled = false
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","图片工作室":"图片工作室"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"图片工作室":1}`))
	require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(`{}`)))

	dsn := fmt.Sprintf("file:image-relay-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{},
		&model.KKAIImageModelProfile{}, &model.KKAIImageSample{}, &model.KKAIImageGeneration{},
		&model.KKAIImageAsset{}, &model.KKAIIdempotencyKey{},
	))
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedisEnabled
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios))
		require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(previousSpecialGroups)))
	})
	require.NoError(t, db.Create(&model.User{
		Id: 7, Username: "image-relay-user", Password: "password", DisplayName: "Image User",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
	}).Error)
	token := model.Token{
		UserId: 7, Key: fmt.Sprintf("image-relay-key-%d", time.Now().UnixNano()),
		Status: common.TokenStatusEnabled, Name: service.ImageStudioTokenGroup,
		CreatedTime: time.Now().Unix(), AccessedTime: time.Now().Unix(), ExpiredTime: -1,
		UnlimitedQuota: true, Group: service.ImageStudioTokenGroup,
	}
	require.NoError(t, db.Create(&token).Error)
	channel := model.Channel{
		Type: constant.ChannelTypeOpenAI, Key: "test-key", Status: common.ChannelStatusEnabled,
		Name: "image channel", Models: "gpt-image-1", Group: service.ImageStudioTokenGroup,
	}
	require.NoError(t, db.Create(&channel).Error)
	priority := int64(0)
	require.NoError(t, db.Create(&model.Ability{
		Group: service.ImageStudioTokenGroup, Model: "gpt-image-1", ChannelId: channel.Id,
		Enabled: true, Priority: &priority,
	}).Error)
	minimum := 1
	maximum := 4
	specification, err := common.Marshal(service.ImageModelSpec{Version: 1, Parameters: []service.ImageParameterSpec{
		{Key: "count", Label: "Count", Control: service.ImageControlInteger, RequestKey: "n", Min: &minimum, Max: &maximum},
	}})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.KKAIImageModelProfile{
		Model: "gpt-image-1", DisplayName: "Image Model", Description: "test",
		SpecificationVersion: 1, Specification: string(specification), DefaultParameters: `{"count":1}`,
		Enabled: true, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}).Error)
	return db, token
}

func newImageStudioRelayContext(method string, path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, path, bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", 7)
	ctx.Set("user_group", "default")
	return ctx, recorder
}
