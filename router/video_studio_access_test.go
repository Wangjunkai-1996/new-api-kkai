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
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type videoStudioAccessFixture struct {
	engine  *gin.Engine
	db      *gorm.DB
	cookies []*http.Cookie
	userID  int
}

func newVideoStudioAccessFixture(t *testing.T, accessMode string) videoStudioAccessFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dsn := fmt.Sprintf("file:video-access-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	rawSetting := config.GlobalConfig.Get("video_studio")
	originalSetting, err := config.ConfigToMap(rawSetting)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.UpdateConfigFromMap(rawSetting, originalSetting))
	})
	require.NoError(t, config.UpdateConfigFromMap(rawSetting, map[string]string{"access_mode": accessMode}))

	user := model.User{
		Id: 77, Username: "stale-video-admin", Password: "test-password", DisplayName: "Video Admin",
		Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "default",
	}
	require.NoError(t, db.Create(&user).Error)

	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("video-access-test"))))
	engine.GET("/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("username", user.Username)
		session.Set("role", common.RoleAdminUser)
		session.Set("id", user.Id)
		session.Set("status", common.UserStatusEnabled)
		session.Set("group", user.Group)
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	engine.GET("/video-access-probe", middleware.UserAuth(), middleware.VideoStudioAccess(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"role": c.GetInt("role"), "status": c.GetInt("status")})
	})
	registerVideoStudioAPIRoutes(engine.Group("/api"))
	SetRelayRouter(engine)

	loginRecorder := httptest.NewRecorder()
	engine.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodGet, "/login", nil))
	require.Equal(t, http.StatusNoContent, loginRecorder.Code)
	return videoStudioAccessFixture{engine: engine, db: db, cookies: loginRecorder.Result().Cookies(), userID: user.Id}
}

func (fixture videoStudioAccessFixture) request(method string, path string, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("New-Api-User", fmt.Sprintf("%d", fixture.userID))
	request.Header.Set("Content-Type", "application/json")
	for _, sessionCookie := range fixture.cookies {
		request.AddCookie(sessionCookie)
	}
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
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":false`)
	require.NotContains(t, strings.ToLower(recorder.Body.String()), "database")
}
