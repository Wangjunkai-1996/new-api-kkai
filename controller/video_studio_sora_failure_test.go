package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestVideoStudioSoraHTTPRejectionRefundsWalletAndFailsGeneration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()

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

	dsn := fmt.Sprintf("file:video-studio-sora-failure-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Token{},
		&model.Channel{},
		&model.Ability{},
		&model.Task{},
		&model.KKAIIdempotencyKey{},
		&model.KKAIVideoModelProfile{},
		&model.KKAIVideoGeneration{},
		&model.KKAIVideoAsset{},
		&model.KKAIVideoTaskAsset{},
		&model.KKAIOutboxEvent{},
		&model.Log{},
	))
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	common.MemoryCacheEnabled = true
	constant.ErrorLogEnabled = true

	usableGroups, err := common.Marshal(map[string]string{
		"default":                     "Default",
		service.VideoStudioTokenGroup: service.VideoStudioTokenGroup,
	})
	require.NoError(t, err)
	groupRatios, err := common.Marshal(map[string]float64{
		"default":                     1,
		service.VideoStudioTokenGroup: 1,
	})
	require.NoError(t, err)
	const studioModel = "video-studio-special-rejection"
	modelPrices, err := common.Marshal(map[string]float64{studioModel: 0.001})
	require.NoError(t, err)
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(string(usableGroups)))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(string(groupRatios)))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(modelPrices)))

	const userID = 407
	const initialQuota int64 = 100_000
	user := model.User{
		Id: userID, Username: "sora-refund-user", Password: "password", DisplayName: "Sora Refund",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", Quota: initialQuota,
	}
	user.SetSetting(dto.UserSetting{BillingPreference: "wallet_only"})
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{
		UserId: userID, Key: "sora-refund-token", Status: common.TokenStatusEnabled,
		Name: "video studio token", CreatedTime: time.Now().Unix(), AccessedTime: time.Now().Unix(),
		ExpiredTime: -1, UnlimitedQuota: true, Group: service.VideoStudioTokenGroup,
	}
	require.NoError(t, db.Create(&token).Error)

	minimum, maximum, step := 1.0, 10.0, 1.0
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
	profile := model.KKAIVideoModelProfile{
		Model: studioModel, DisplayName: "Special Video", Description: "failure regression",
		SpecificationVersion: 1, Specification: string(specification), DefaultParameters: `{}`, Enabled: true,
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(&profile).Error)

	type upstreamObservation struct {
		method        string
		path          string
		authorization string
		payload       map[string]any
		decodeErr     error
	}
	observed := make(chan upstreamObservation, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, readErr := io.ReadAll(request.Body)
		observation := upstreamObservation{
			method: request.Method, path: request.URL.Path,
			authorization: request.Header.Get("Authorization"), decodeErr: readErr,
		}
		if readErr == nil {
			observation.decodeErr = common.Unmarshal(raw, &observation.payload)
		}
		observed <- observation
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":{"message":"strict adapter rejection","type":"invalid_request_error","code":"invalid_request"}}`))
	}))
	t.Cleanup(upstream.Close)

	priority := int64(0)
	weight := uint(100)
	autoBan := 0
	mapping := `{"video-studio-special-rejection":"sd_2.0_special_1080p"}`
	channel := model.Channel{
		Type: constant.ChannelTypeSora, Key: "strict-adapter-key", Status: common.ChannelStatusEnabled,
		Name: "strict special adapter", Models: studioModel, Group: service.VideoStudioTokenGroup,
		BaseURL: &upstream.URL, ModelMapping: &mapping, Priority: &priority, Weight: &weight, AutoBan: &autoBan,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: service.VideoStudioTokenGroup, Model: studioModel, ChannelId: channel.Id,
		Enabled: true, Priority: &priority, Weight: weight,
	}).Error)
	model.InitChannelCache()

	creativeRequest := service.VideoStudioSubmissionRequest{
		TokenID: token.Id, Model: studioModel, Group: service.VideoStudioTokenGroup,
		Mode: service.VideoModeTextToVideo, Prompt: "A precise camera movement",
		Parameters: map[string]any{"duration": 5, "ratio": "16:9", "generate_audio": false},
	}
	normalized, err := service.NormalizeVideoStudioSubmission(
		context.Background(), db, videoStudioRelayTestStore{}, userID, creativeRequest,
	)
	require.NoError(t, err)
	quote := service.NewVideoStudioQuote(normalized, 100_000, nil)
	creativeRequest.MaxQuota = &quote.Quota
	creativeRequest.QuoteHash = quote.RequestHash
	creativeRequest.QuoteExpiresAt = quote.ExpiresAt
	body, err := common.Marshal(creativeRequest)
	require.NoError(t, err)

	engine := gin.New()
	engine.POST(
		"/pg/videos",
		func(c *gin.Context) {
			c.Set("id", userID)
			c.Set("user_group", "default")
			c.Set(videoStudioAssetStoreContextKey, videoStudioRelayTestStore{})
		},
		PrepareVideoStudioTaskRequest,
		middleware.Distribute(),
		SubmitVideoStudioTask,
	)
	request := httptest.NewRequest(http.MethodPost, "/pg/videos", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "sora-http-400-refund")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Contains(t, response.Body.String(), "fail_to_fetch_task")
	observation := <-observed
	require.NoError(t, observation.decodeErr)
	assert.Equal(t, http.MethodPost, observation.method)
	assert.Equal(t, "/v1/videos", observation.path)
	assert.Equal(t, "Bearer strict-adapter-key", observation.authorization)
	assert.Equal(t, map[string]any{
		"model":          "sd_2.0_special_1080p",
		"prompt":         "A precise camera movement",
		"duration":       float64(5),
		"ratio":          "16:9",
		"generate_audio": false,
	}, observation.payload)

	var task model.Task
	require.NoError(t, db.First(&task).Error)
	assert.EqualValues(t, model.TaskStatusFailure, task.Status)
	assert.Equal(t, model.TaskBillingStateRefunded, task.PrivateData.BillingState)
	assert.Zero(t, task.Quota)
	assert.Empty(t, task.PrivateData.UpstreamTaskID)

	var generation model.KKAIVideoGeneration
	require.NoError(t, db.First(&generation).Error)
	generationView, err := service.GetVideoGeneration(context.Background(), db, userID, generation.ID)
	require.NoError(t, err)
	assert.Equal(t, "failed", generationView.Status)
	assert.Zero(t, generationView.Quota)

	var reloadedUser model.User
	require.NoError(t, db.First(&reloadedUser, userID).Error)
	assert.Equal(t, initialQuota, reloadedUser.Quota)

	var reservation model.KKAIIdempotencyKey
	require.NoError(t, db.First(&reservation).Error)
	assert.Equal(t, model.VideoIdempotencyResourceTask, reservation.ResourceType)
	assert.Equal(t, task.TaskID, reservation.ResourceID)

	var outboxCount int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Count(&outboxCount).Error)
	assert.Positive(t, outboxCount)
	var errorLogCount int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ? AND quota = 0", model.LogTypeError).Count(&errorLogCount).Error)
	assert.EqualValues(t, 1, errorLogCount)
}
