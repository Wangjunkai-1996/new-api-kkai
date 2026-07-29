package controller

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
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

func setupVideoStudioTokenControllerTest(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:video-token-controller-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.KKAIVideoModelProfile{}, &model.Ability{}))

	previousDB := model.DB
	previousRedisEnabled := common.RedisEnabled
	previousUsableGroups := setting.UserUsableGroups2JSONString()
	previousGroupRatios := ratio_setting.GroupRatio2JSONString()
	previousSpecialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.MarshalJSONString()
	model.DB = db
	common.RedisEnabled = false
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","Seedance 视频":"Seedance 视频"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"Seedance 视频":1}`))
	require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(`{}`)))
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedisEnabled
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios))
		require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(previousSpecialGroups)))
	})

	require.NoError(t, db.Create(&model.User{
		Id: 42, Username: "video-controller-user", Password: "password", DisplayName: "Video User",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
	}).Error)
	require.NoError(t, db.Create(&model.KKAIVideoModelProfile{
		Model: "video-model-a", DisplayName: "Video Model", Description: "test",
		SpecificationVersion: 1, Specification: `{}`, DefaultParameters: `{}`, Enabled: true,
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}).Error)
	priority := int64(0)
	require.NoError(t, db.Create(&model.Ability{
		Group: service.VideoStudioTokenGroup, Model: "video-model-a", ChannelId: 1,
		Enabled: true, Priority: &priority,
	}).Error)
	return db
}

func newVideoStudioTokenControllerContext(method string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, "/api/video-studio/token?model=video-model-a", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", 42)
	ctx.Set("user_group", "default")
	return ctx, recorder
}

func TestGetVideoStudioTokenStatusReturnsCapabilityWithoutKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupVideoStudioTokenControllerTest(t)
	rawKey := "controller-video-secret"
	token := model.Token{
		UserId: 42, Key: rawKey, Status: common.TokenStatusEnabled, Name: "existing video token",
		CreatedTime: time.Now().Unix(), AccessedTime: time.Now().Unix(), ExpiredTime: -1,
		UnlimitedQuota: true, ModelLimitsEnabled: true, ModelLimits: "video-model-a",
		Group: service.VideoStudioTokenGroup,
	}
	require.NoError(t, db.Create(&token).Error)

	ctx, recorder := newVideoStudioTokenControllerContext(http.MethodGet, nil)
	GetVideoStudioTokenStatus(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), rawKey)
	assert.NotContains(t, recorder.Body.String(), `"key"`)
	var response struct {
		Success bool                               `json:"success"`
		Data    service.VideoStudioTokenCapability `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, service.VideoStudioTokenGroup, response.Data.RequiredGroup)
	assert.True(t, response.Data.HasUsableToken)
	assert.Equal(t, service.VideoStudioTokenStatusReady, response.Data.Status)
	require.NotNil(t, response.Data.Token)
	assert.Equal(t, token.Id, response.Data.Token.ID)
}

func TestEnsureVideoStudioTokenReturnsCreatedThenReusesWithoutKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupVideoStudioTokenControllerTest(t)
	body := []byte(`{"model":"video-model-a"}`)

	firstContext, firstRecorder := newVideoStudioTokenControllerContext(http.MethodPost, body)
	EnsureVideoStudioToken(firstContext)
	require.Equal(t, http.StatusOK, firstRecorder.Code)
	var first struct {
		Data service.VideoStudioTokenEnsureResult `json:"data"`
	}
	require.NoError(t, common.Unmarshal(firstRecorder.Body.Bytes(), &first))
	assert.True(t, first.Data.Created)
	require.NotNil(t, first.Data.Token)
	assert.NotContains(t, firstRecorder.Body.String(), `"key"`)

	secondContext, secondRecorder := newVideoStudioTokenControllerContext(http.MethodPost, body)
	EnsureVideoStudioToken(secondContext)
	require.Equal(t, http.StatusOK, secondRecorder.Code)
	var second struct {
		Data service.VideoStudioTokenEnsureResult `json:"data"`
	}
	require.NoError(t, common.Unmarshal(secondRecorder.Body.Bytes(), &second))
	assert.False(t, second.Data.Created)
	require.NotNil(t, second.Data.Token)
	assert.Equal(t, first.Data.Token.ID, second.Data.Token.ID)
	assert.NotContains(t, secondRecorder.Body.String(), `"key"`)

	var count int64
	require.NoError(t, db.Model(&model.Token{}).Where("user_id = ?", 42).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	var stored model.Token
	require.NoError(t, db.First(&stored, first.Data.Token.ID).Error)
	assert.NotEmpty(t, stored.Key)
}

func TestVideoStudioTokenErrorsHaveStableHTTPContract(t *testing.T) {
	tests := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{service.ErrVideoStudioTokenRequired, http.StatusBadRequest, "video_token_required"},
		{service.ErrVideoStudioTokenInvalid, http.StatusForbidden, "video_token_invalid"},
		{service.ErrVideoStudioTokenGroupInvalid, http.StatusForbidden, "video_token_group_invalid"},
		{service.ErrVideoStudioTokenModelForbidden, http.StatusForbidden, "video_token_model_forbidden"},
		{service.ErrVideoStudioTokenIPForbidden, http.StatusForbidden, "video_token_ip_forbidden"},
		{service.ErrVideoStudioTokenGroupUnavailable, http.StatusForbidden, "video_token_group_unavailable"},
		{service.ErrVideoStudioTokenLimitReached, http.StatusConflict, "video_token_limit_reached"},
		{service.ErrVideoStudioTokenModelsUnavailable, http.StatusServiceUnavailable, "video_token_models_unavailable"},
	}
	for _, test := range tests {
		status, code := videoStudioErrorStatus(errors.Join(errors.New("wrapped"), test.err))
		assert.Equal(t, test.wantStatus, status)
		assert.Equal(t, test.wantCode, code)
	}
}

func TestVideoStudioTokenControllersIgnoreStaleSessionAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupVideoStudioTokenControllerTest(t)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 42).Update("status", common.UserStatusDisabled).Error)

	getContext, getRecorder := newVideoStudioTokenControllerContext(http.MethodGet, nil)
	getContext.Set("user_group", "default")
	GetVideoStudioTokenStatus(getContext)

	require.Equal(t, http.StatusOK, getRecorder.Code)
	var response struct {
		Data service.VideoStudioTokenCapability `json:"data"`
	}
	require.NoError(t, common.Unmarshal(getRecorder.Body.Bytes(), &response))
	assert.Equal(t, service.VideoStudioTokenStatusGroupUnavailable, response.Data.Status)
	assert.False(t, response.Data.CanCreate)

	postContext, postRecorder := newVideoStudioTokenControllerContext(http.MethodPost, []byte(`{"model":"video-model-a"}`))
	postContext.Set("user_group", "default")
	EnsureVideoStudioToken(postContext)
	require.Equal(t, http.StatusForbidden, postRecorder.Code)
	assert.Contains(t, postRecorder.Body.String(), "video_token_group_unavailable")
}

func TestGetVideoStudioTokenStatusHidesDatabaseFailureDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupVideoStudioTokenControllerTest(t)
	require.NoError(t, db.Migrator().DropTable(&model.Token{}))

	ctx, recorder := newVideoStudioTokenControllerContext(http.MethodGet, nil)
	GetVideoStudioTokenStatus(ctx)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Code    string `json:"code"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, "video_studio_internal_error", response.Code)
	assert.Equal(t, "video studio request failed", response.Message)
	body := strings.ToLower(recorder.Body.String())
	assert.NotContains(t, body, "no such table")
	assert.NotContains(t, body, "tokens")
	assert.NotContains(t, body, "select ")
}
