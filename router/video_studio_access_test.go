package router

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type videoStudioAccessFixture struct {
	engine      *gin.Engine
	db          *gorm.DB
	accessToken string
	userID      int
}

func newVideoStudioAccessFixture(t *testing.T, accessMode string) videoStudioAccessFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dsn := fmt.Sprintf("file:video-access-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}))
	previousDB := model.DB
	previousDatabaseType := common.MainDatabaseType()
	previousRedisEnabled := common.RedisEnabled
	previousSessionSecret := common.SessionSecret
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.SessionSecret = "video-access-test-secret"
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetMainDatabaseType(previousDatabaseType)
		common.RedisEnabled = previousRedisEnabled
		common.SessionSecret = previousSessionSecret
	})

	rawSetting := config.GlobalConfig.Get("video_studio")
	originalSetting, err := config.ConfigToMap(rawSetting)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.UpdateConfigFromMap(rawSetting, originalSetting))
	})
	require.NoError(t, config.UpdateConfigFromMap(rawSetting, map[string]string{"access_mode": accessMode}))

	user := model.User{
		Id: 77, Username: "stale-video-admin", Password: "test-password", DisplayName: "Video Admin",
		Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)
	now := time.Now().Unix()
	session := model.UserSession{
		SID: "video-access-session", UserID: user.Id, Version: 1, UserAuthVersion: user.AuthVersion,
		Status: model.UserSessionStatusActive, RefreshHash: "video-access-refresh-hash", LoginMethod: "password",
		LastActiveAt: now, ExpiresAt: now + 3600,
	}
	require.NoError(t, model.CreateUserSession(&session))
	accessToken, _, err := service.IssueAccessToken(service.AuthIdentity{
		UserID: user.Id, SessionID: session.SID, UserAuthVersion: session.UserAuthVersion, SessionVersion: session.Version,
	})
	require.NoError(t, err)

	engine := gin.New()
	engine.GET("/video-access-probe", middleware.UserAuth(), middleware.VideoStudioAccess(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"role": c.GetInt("role"), "status": c.GetInt("status")})
	})
	registerVideoStudioAPIRoutes(engine.Group("/api"))
	SetRelayRouter(engine)

	return videoStudioAccessFixture{engine: engine, db: db, accessToken: accessToken, userID: user.Id}
}

func (fixture videoStudioAccessFixture) request(method string, path string, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Authorization", "Bearer "+fixture.accessToken)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	fixture.engine.ServeHTTP(recorder, request)
	return recorder
}

func TestVideoStudioRoutesRejectStaleAdminSessionAfterRoleDowngrade(t *testing.T) {
	fixture := newVideoStudioAccessFixture(t, video_studio_setting.AccessModeAdmin)
	require.NoError(t, fixture.db.Model(&model.User{}).Where("id = ?", fixture.userID).
		Update("role", common.RoleCommonUser).Error)

	requests := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/video-studio/token?model=video-model-a"},
		{method: http.MethodPost, path: "/api/video-studio/token", body: `{"model":"video-model-a"}`},
		{method: http.MethodPost, path: "/pg/videos/quote", body: `{}`},
		{method: http.MethodPost, path: "/pg/videos", body: `{}`},
	}
	for _, testRequest := range requests {
		t.Run(testRequest.method+" "+testRequest.path, func(t *testing.T) {
			recorder := fixture.request(testRequest.method, testRequest.path, testRequest.body)
			require.Equal(t, http.StatusForbidden, recorder.Code)
			require.Contains(t, recorder.Body.String(), "video_studio_access_denied")
		})
	}
}

func TestVideoStudioAccessRefreshesRoleAndStatusFromDatabase(t *testing.T) {
	fixture := newVideoStudioAccessFixture(t, video_studio_setting.AccessModeAll)
	require.NoError(t, fixture.db.Model(&model.User{}).Where("id = ?", fixture.userID).
		Update("role", common.RoleCommonUser).Error)

	recorder := fixture.request(http.MethodGet, "/video-access-probe", "")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), fmt.Sprintf(`"role":%d`, common.RoleCommonUser))
	require.Contains(t, recorder.Body.String(), fmt.Sprintf(`"status":%d`, common.UserStatusEnabled))
}

func TestVideoStudioAccessRejectsStaleEnabledSessionAfterUserDisabled(t *testing.T) {
	fixture := newVideoStudioAccessFixture(t, video_studio_setting.AccessModeAll)
	require.NoError(t, fixture.db.Model(&model.User{}).Where("id = ?", fixture.userID).
		Update("status", common.UserStatusDisabled).Error)

	recorder := fixture.request(http.MethodGet, "/video-access-probe", "")
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Contains(t, recorder.Body.String(), "AUTH_SESSION_REVOKED")
	require.NotContains(t, strings.ToLower(recorder.Body.String()), "database")
}
