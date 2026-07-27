package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
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

type videoStudioMediaAuthFixture struct {
	engine         *gin.Engine
	cookies        []*http.Cookie
	ownedAssetID   int64
	foreignAssetID int64
	accessToken    string
}

func newVideoStudioMediaAuthFixture(t *testing.T) videoStudioMediaAuthFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.KKAIVideoAsset{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	rawSetting := config.GlobalConfig.Get("video_studio")
	originalSetting, err := config.ConfigToMap(rawSetting)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.UpdateConfigFromMap(rawSetting, originalSetting))
	})
	require.NoError(t, config.UpdateConfigFromMap(rawSetting, map[string]string{
		"access_mode": video_studio_setting.AccessModeAll,
	}))

	t.Setenv("VIDEO_STUDIO_R2_ENDPOINT", "https://r2.example.test")
	t.Setenv("VIDEO_STUDIO_R2_REGION", "auto")
	t.Setenv("VIDEO_STUDIO_R2_BUCKET", "video-test")
	t.Setenv("VIDEO_STUDIO_R2_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("VIDEO_STUDIO_R2_SECRET_ACCESS_KEY", "test-secret-key")

	accessToken := "video-media-access-token"
	user := model.User{
		Id: 1, Username: "media-user", Password: "test-password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AffCode: "media-user-aff",
	}
	user.SetAccessToken(accessToken)
	require.NoError(t, db.Create(&user).Error)

	now := time.Now().Unix()
	ownedAsset := model.KKAIVideoAsset{
		OwnerUserID: user.Id, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference,
		State: model.VideoAssetStateReady, ObjectKey: "users/1/owned/source.mp4",
		OriginalFilename: "owned.mp4", MIMEType: "video/mp4", CreatedAt: now, UpdatedAt: now,
	}
	foreignAsset := model.KKAIVideoAsset{
		OwnerUserID: 2, Scope: model.VideoAssetScopeUser, Kind: model.VideoAssetKindReference,
		State: model.VideoAssetStateReady, ObjectKey: "users/2/foreign/source.mp4",
		OriginalFilename: "foreign.mp4", MIMEType: "video/mp4", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&ownedAsset).Error)
	require.NoError(t, db.Create(&foreignAsset).Error)

	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("video-media-auth-test"))))
	engine.GET("/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("username", user.Username)
		session.Set("role", user.Role)
		session.Set("id", user.Id)
		session.Set("status", user.Status)
		session.Set("group", user.Group)
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	registerKKAIRoutes(engine.Group("/api"), func(c *gin.Context) { c.Next() })

	loginRecorder := httptest.NewRecorder()
	engine.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodGet, "/login", nil))
	require.Equal(t, http.StatusNoContent, loginRecorder.Code)

	return videoStudioMediaAuthFixture{
		engine: engine, cookies: loginRecorder.Result().Cookies(), ownedAssetID: ownedAsset.ID,
		foreignAssetID: foreignAsset.ID, accessToken: accessToken,
	}
}

func (fixture videoStudioMediaAuthFixture) request(
	t *testing.T,
	path string,
	withSession bool,
	headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if withSession {
		for _, sessionCookie := range fixture.cookies {
			request.AddCookie(sessionCookie)
		}
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	fixture.engine.ServeHTTP(recorder, request)
	return recorder
}

func TestVideoStudioMediaRoutesAllowSessionWithoutUserHeader(t *testing.T) {
	fixture := newVideoStudioMediaAuthFixture(t)

	for _, suffix := range []string{"content", "download"} {
		t.Run(suffix, func(t *testing.T) {
			path := fmt.Sprintf("/api/video-studio/assets/%d/%s", fixture.ownedAssetID, suffix)
			recorder := fixture.request(t, path, true, nil)

			require.Equal(t, http.StatusFound, recorder.Code)
			require.Contains(t, recorder.Header().Get("Location"), "r2.example.test/video-test/users/1/owned/source.mp4")
		})
	}
}

func TestVideoStudioJSONRoutesStillRequireUserHeaderForSession(t *testing.T) {
	fixture := newVideoStudioMediaAuthFixture(t)
	path := fmt.Sprintf("/api/video-studio/assets/%d", fixture.ownedAssetID)

	recorder := fixture.request(t, path, true, nil)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestVideoStudioMediaRoutesStillRequireUserHeaderForAccessToken(t *testing.T) {
	fixture := newVideoStudioMediaAuthFixture(t)
	path := fmt.Sprintf("/api/video-studio/assets/%d/content", fixture.ownedAssetID)

	recorder := fixture.request(t, path, false, map[string]string{
		"Authorization": "Bearer " + fixture.accessToken,
	})

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestVideoStudioMediaRoutesRejectMismatchedExplicitUserHeader(t *testing.T) {
	fixture := newVideoStudioMediaAuthFixture(t)
	path := fmt.Sprintf("/api/video-studio/assets/%d/content", fixture.ownedAssetID)

	recorder := fixture.request(t, path, true, map[string]string{
		"New-Api-User": "2",
	})

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestVideoStudioMediaRoutesKeepAccessTokenWithUserHeader(t *testing.T) {
	fixture := newVideoStudioMediaAuthFixture(t)
	path := fmt.Sprintf("/api/video-studio/assets/%d/content", fixture.ownedAssetID)

	recorder := fixture.request(t, path, false, map[string]string{
		"Authorization": "Bearer " + fixture.accessToken,
		"New-Api-User":  "1",
	})

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Contains(t, recorder.Header().Get("Location"), "r2.example.test/video-test/users/1/owned/source.mp4")
}

func TestVideoStudioMediaRoutesKeepAssetOwnershipBoundary(t *testing.T) {
	fixture := newVideoStudioMediaAuthFixture(t)
	path := fmt.Sprintf("/api/video-studio/assets/%d/content", fixture.foreignAssetID)

	recorder := fixture.request(t, path, true, nil)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "video_asset_access_denied")
}

func TestVideoStudioMediaRoutesKeepFeatureAccessBoundary(t *testing.T) {
	fixture := newVideoStudioMediaAuthFixture(t)
	rawSetting := config.GlobalConfig.Get("video_studio")
	require.NoError(t, config.UpdateConfigFromMap(rawSetting, map[string]string{
		"access_mode": video_studio_setting.AccessModeOff,
	}))
	path := fmt.Sprintf("/api/video-studio/assets/%d/content", fixture.ownedAssetID)

	recorder := fixture.request(t, path, true, nil)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "video_studio_access_denied")
}
