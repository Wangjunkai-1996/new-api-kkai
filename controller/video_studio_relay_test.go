package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newVideoStudioRelayTestDB(t *testing.T) (*gorm.DB, model.Token) {
	t.Helper()
	previousRedisEnabled := common.RedisEnabled
	previousUsableGroups := setting.UserUsableGroups2JSONString()
	previousGroupRatios := ratio_setting.GroupRatio2JSONString()
	common.RedisEnabled = false
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","Seedance 视频":"Seedance 视频"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"Seedance 视频":1}`))
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios))
	})
	dsn := fmt.Sprintf("file:video-relay-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Task{}, &model.KKAIIdempotencyKey{}, &model.Token{}, &model.KKAIVideoModelProfile{},
		&model.KKAIVideoSample{}, &model.Ability{},
	))
	require.NoError(t, db.Create(&model.User{
		Id: 7, Username: "video-relay-user", Password: "password", DisplayName: "Video User",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
	}).Error)
	token := model.Token{
		UserId: 7, Key: fmt.Sprintf("relay-video-key-%d", time.Now().UnixNano()), Status: common.TokenStatusEnabled,
		Name: "video token", CreatedTime: time.Now().Unix(), AccessedTime: time.Now().Unix(),
		ExpiredTime: -1, UnlimitedQuota: true, Group: service.VideoStudioTokenGroup,
	}
	require.NoError(t, db.Create(&token).Error)
	priority := int64(0)
	require.NoError(t, db.Create(&model.Ability{
		Group: service.VideoStudioTokenGroup, Model: "video-model-v1", ChannelId: 1,
		Enabled: true, Priority: &priority,
	}).Error)
	return db, token
}

type videoStudioRelayTestStore struct{}

func (videoStudioRelayTestStore) PresignUpload(context.Context, string, string, int64, time.Duration) (service.VideoAssetSignedRequest, error) {
	return service.VideoAssetSignedRequest{}, nil
}

func (videoStudioRelayTestStore) PresignDownload(context.Context, string, string, bool, time.Duration) (string, error) {
	return "", nil
}

func (videoStudioRelayTestStore) Head(context.Context, string) (service.VideoAssetObjectMetadata, error) {
	return service.VideoAssetObjectMetadata{}, nil
}

func (videoStudioRelayTestStore) Get(context.Context, string) (service.VideoAssetObject, error) {
	return service.VideoAssetObject{}, nil
}

func (videoStudioRelayTestStore) Put(context.Context, string, string, io.Reader, int64) error {
	return nil
}

func (videoStudioRelayTestStore) Delete(context.Context, []string) error { return nil }

func TestPrepareVideoStudioTaskRequestForcesValidatedTokenGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, token := newVideoStudioRelayTestDB(t)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	specification, err := common.Marshal(service.VideoModelSpec{
		Version: 1, Modes: []string{service.VideoModeTextToVideo},
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.KKAIVideoModelProfile{
		Model: "video-model-v1", DisplayName: "Video Model", Description: "test",
		SpecificationVersion: 1, Specification: string(specification), DefaultParameters: `{}`, Enabled: true,
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}).Error)
	body, err := common.Marshal(map[string]any{
		"token_id": token.Id, "model": "video-model-v1", "group": "default",
		"mode": "text_to_video", "prompt": "test",
	})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/pg/videos/quote", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	ctx.Set("id", 7)
	ctx.Set("user_group", "default")
	ctx.Set(videoStudioAssetStoreContextKey, videoStudioRelayTestStore{})

	PrepareVideoStudioTaskRequest(ctx)

	require.False(t, ctx.IsAborted())
	require.Equal(t, token.Id, ctx.GetInt("token_id"))
	require.Equal(t, token.Key, ctx.GetString("token_key"))
	require.Equal(t, service.VideoStudioTokenGroup, ctx.GetString("group"))
	require.Equal(t, service.VideoStudioTokenGroup, ctx.GetString("token_group"))
	require.Equal(t, service.VideoStudioTokenGroup, common.GetContextKeyString(ctx, constant.ContextKeyUsingGroup))
	require.Nil(t, PreparePlaygroundTaskContext(ctx))
	require.Equal(t, token.Id, ctx.GetInt("token_id"))
	require.Equal(t, token.Key, ctx.GetString("token_key"))
	require.Equal(t, service.VideoStudioTokenGroup, ctx.GetString("token_group"))
	require.Equal(t, service.VideoStudioTokenGroup, common.GetContextKeyString(ctx, constant.ContextKeyUsingGroup))
	rewritten, err := io.ReadAll(ctx.Request.Body)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(rewritten, &payload))
	require.Equal(t, service.VideoStudioTokenGroup, payload["group"])
}

func TestPrepareVideoStudioTaskRequestRejectsSampleDerivedModelWithoutGroupAbility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, token := newVideoStudioRelayTestDB(t)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	specification, err := common.Marshal(service.VideoModelSpec{
		Version: 1, Modes: []string{service.VideoModeTextToVideo},
	})
	require.NoError(t, err)
	profile := model.KKAIVideoModelProfile{
		Model: "sample-only-model", DisplayName: "Sample-only Model", Description: "test",
		SpecificationVersion: 1, Specification: string(specification), DefaultParameters: `{}`, Enabled: true,
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&profile).Error)
	sample := model.KKAIVideoSample{
		ModelProfileID: profile.ID, Title: "Unavailable sample", Prompt: "test", Mode: service.VideoModeTextToVideo,
		ModelVersion: 1, Parameters: `{}`, ReferenceAssetIDs: `[]`, Status: model.VideoSampleStatusPublished,
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&sample).Error)

	body, err := common.Marshal(map[string]any{
		"token_id": token.Id, "sample_id": sample.ID,
	})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/pg/videos/quote", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	ctx.Set("id", 7)
	ctx.Set("user_group", "default")
	ctx.Set(videoStudioAssetStoreContextKey, videoStudioRelayTestStore{})

	PrepareVideoStudioTaskRequest(ctx)

	require.True(t, ctx.IsAborted())
	require.Equal(t, http.StatusForbidden, response.Code)
	require.Contains(t, response.Body.String(), "video_token_model_forbidden")
}

func TestVideoStudioDistributionUsesValidatedTokenGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, token := newVideoStudioRelayTestDB(t)
	previousDB := model.DB
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	model.DB = db
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
	})
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))

	const modelName = "video-model-v1"
	specification, err := common.Marshal(service.VideoModelSpec{
		Version: 1, Modes: []string{service.VideoModeTextToVideo},
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.KKAIVideoModelProfile{
		Model: modelName, DisplayName: "Video Model", Description: "test",
		SpecificationVersion: 1, Specification: string(specification), DefaultParameters: `{}`, Enabled: true,
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}).Error)

	priority := int64(0)
	defaultChannel := model.Channel{
		Type: constant.ChannelTypeOpenAI, Key: "default-channel-key", Status: common.ChannelStatusEnabled,
		Name: "ordinary-primary", Models: modelName, Group: "default", Priority: &priority,
	}
	videoChannel := model.Channel{
		Type: constant.ChannelTypeOpenAI, Key: "video-channel-key", Status: common.ChannelStatusEnabled,
		Name: "motion-primary", Models: modelName, Group: service.VideoStudioTokenGroup, Priority: &priority,
	}
	require.NoError(t, db.Create(&defaultChannel).Error)
	require.NoError(t, db.Create(&videoChannel).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "default", Model: modelName, ChannelId: defaultChannel.Id,
		Enabled: true, Priority: &priority, Weight: 100,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: service.VideoStudioTokenGroup, Model: modelName, ChannelId: videoChannel.Id,
		Enabled: true, Priority: &priority, Weight: 100,
	}).Error)
	model.InitChannelCache()

	for _, clientGroup := range []string{"default", "forged-client-group"} {
		t.Run(clientGroup, func(t *testing.T) {
			body, err := common.Marshal(map[string]any{
				"token_id": token.Id, "model": modelName, "group": clientGroup,
				"mode": "text_to_video", "prompt": "test",
			})
			require.NoError(t, err)

			var selectedChannelID int
			var tokenContext struct {
				ID         int
				Key        string
				TokenGroup string
				UsingGroup string
			}
			engine := gin.New()
			engine.POST(
				"/pg/videos/quote",
				func(c *gin.Context) {
					c.Set("id", 7)
					c.Set("user_group", "default")
					c.Set(videoStudioAssetStoreContextKey, videoStudioRelayTestStore{})
					c.Next()
				},
				PrepareVideoStudioTaskRequest,
				middleware.Distribute(),
				func(c *gin.Context) {
					selectedChannelID = c.GetInt("channel_id")
					require.Nil(t, PreparePlaygroundTaskContext(c))
					tokenContext.ID = c.GetInt("token_id")
					tokenContext.Key = c.GetString("token_key")
					tokenContext.TokenGroup = common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
					tokenContext.UsingGroup = common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
					c.Status(http.StatusNoContent)
				},
			)

			request := httptest.NewRequest(http.MethodPost, "/pg/videos/quote", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)

			require.Equal(t, http.StatusNoContent, response.Code)
			require.Equal(t, videoChannel.Id, selectedChannelID)
			require.NotEqual(t, defaultChannel.Id, selectedChannelID)
			require.Equal(t, token.Id, tokenContext.ID)
			require.Equal(t, token.Key, tokenContext.Key)
			require.Equal(t, service.VideoStudioTokenGroup, tokenContext.TokenGroup)
			require.Equal(t, service.VideoStudioTokenGroup, tokenContext.UsingGroup)
		})
	}
}

func TestPrepareVideoStudioTaskRequestRejectsDisabledCurrentUserDespiteStaleSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, token := newVideoStudioRelayTestDB(t)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 7).Update("status", common.UserStatusDisabled).Error)

	body, err := common.Marshal(map[string]any{
		"token_id": token.Id, "model": "video-model-v1", "mode": "text_to_video", "prompt": "test",
	})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/pg/videos/quote", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	ctx.Set("id", 7)
	ctx.Set("user_group", "default")

	PrepareVideoStudioTaskRequest(ctx)

	require.True(t, ctx.IsAborted())
	require.Equal(t, http.StatusForbidden, response.Code)
	require.Contains(t, response.Body.String(), "video_token_group_unavailable")
}

func TestPrepareVideoStudioTaskRequestRejectsTokenOutsideAllowedIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, token := newVideoStudioRelayTestDB(t)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	allowIPs := "10.0.0.0/8"
	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", token.Id).Update("allow_ips", allowIPs).Error)

	body, err := common.Marshal(map[string]any{
		"token_id": token.Id, "model": "video-model-v1", "mode": "text_to_video", "prompt": "test",
	})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/pg/videos/quote", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "203.0.113.8:1234"
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	ctx.Set("id", 7)

	PrepareVideoStudioTaskRequest(ctx)

	require.True(t, ctx.IsAborted())
	require.Equal(t, http.StatusForbidden, response.Code)
	require.Contains(t, response.Body.String(), "video_token_ip_forbidden")
}

func TestPrepareVideoStudioTaskRequestReplaysBeforeStorageAndDistribution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, token := newVideoStudioRelayTestDB(t)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	now := time.Now().Unix()
	task := model.Task{
		TaskID: "task_existing", UserId: 7, Status: model.TaskStatusQueued, Progress: "20%",
		Properties: model.Properties{OriginModelName: "video-model-v1"}, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&task).Error)
	quoteHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	quoteExpiresAt := now - 60
	fingerprint, err := service.VideoStudioIdempotencyFingerprint(service.VideoStudioSubmissionRequest{
		TokenID: token.Id, Model: "disabled-or-missing-model", Group: service.VideoStudioTokenGroup,
		Mode: service.VideoModeTextToVideo, Prompt: "test",
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.KKAIIdempotencyKey{
		UserID: 7, Operation: model.VideoIdempotencyOperationSubmit, Key: "retry-key",
		RequestHash: fingerprint, ResourceType: model.VideoIdempotencyResourceTask,
		ResourceID: task.TaskID, CreatedAt: now, ExpiresAt: now + 3600,
	}).Error)

	body, err := common.Marshal(map[string]any{
		"token_id": token.Id, "model": "disabled-or-missing-model", "mode": "text_to_video", "prompt": "test",
		"max_quota": 1, "quote_hash": quoteHash, "quote_expires_at": quoteExpiresAt,
	})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/pg/videos", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "retry-key")
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	ctx.Set("id", 7)
	ctx.Set("user_group", "default")

	PrepareVideoStudioTaskRequest(ctx)

	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"task_existing"`)
	require.Contains(t, response.Body.String(), `"queued"`)
}

func TestPrepareVideoStudioTaskRequestRejectsCopiedQuoteForDifferentCreativeRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, token := newVideoStudioRelayTestDB(t)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	quoteHash := strings.Repeat("a", 64)
	original := service.VideoStudioSubmissionRequest{
		TokenID: token.Id, Model: "video-model-v1", Group: service.VideoStudioTokenGroup,
		Mode: service.VideoModeTextToVideo, Prompt: "original prompt",
	}
	fingerprint, err := service.VideoStudioIdempotencyFingerprint(original)
	require.NoError(t, err)
	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.KKAIIdempotencyKey{
		UserID: 7, Operation: model.VideoIdempotencyOperationSubmit, Key: "copied-quote-key",
		RequestHash: fingerprint, ResourceType: model.VideoIdempotencyResourceTask,
		ResourceID: "task_original", CreatedAt: now, ExpiresAt: now + 3600,
	}).Error)

	body, err := common.Marshal(map[string]any{
		"token_id": token.Id, "model": "video-model-v1", "mode": "text_to_video", "prompt": "different prompt",
		"max_quota": 1, "quote_hash": quoteHash, "quote_expires_at": now + 300,
	})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/pg/videos", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "copied-quote-key")
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	ctx.Set("id", 7)
	ctx.Set("user_group", "default")

	PrepareVideoStudioTaskRequest(ctx)

	require.Equal(t, http.StatusConflict, response.Code)
	require.Contains(t, response.Body.String(), "video_studio_conflict")
}

func TestPrepareVideoStudioTaskRequestRejectsExpiredQuoteBeforeStorageAndReleasesReservation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, token := newVideoStudioRelayTestDB(t)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	now := time.Now().Unix()
	body, err := common.Marshal(map[string]any{
		"token_id": token.Id, "model": "missing-model", "mode": "text_to_video", "prompt": "test",
		"max_quota": 1, "quote_hash": strings.Repeat("a", 64), "quote_expires_at": now - 1,
	})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/pg/videos", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "expired-quote-key")
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	ctx.Set("id", 7)
	ctx.Set("user_group", "default")

	PrepareVideoStudioTaskRequest(ctx)

	require.Equal(t, http.StatusConflict, response.Code)
	require.Contains(t, response.Body.String(), "quote_stale")
	var reservations int64
	require.NoError(t, db.Model(&model.KKAIIdempotencyKey{}).Count(&reservations).Error)
	require.Zero(t, reservations)
}

func TestPrepareVideoStudioTaskRequestRequiresSubmissionGuardsBeforeStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, token := newVideoStudioRelayTestDB(t)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	body, err := common.Marshal(map[string]any{
		"token_id": token.Id, "model": "video-model-v1", "mode": "text_to_video", "prompt": "test", "max_quota": 1,
	})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/pg/videos", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	ctx.Set("id", 7)
	ctx.Set("user_group", "default")

	PrepareVideoStudioTaskRequest(ctx)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), "idempotency_key_required")
}
