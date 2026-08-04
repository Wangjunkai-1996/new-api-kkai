package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	gsessions "github.com/gorilla/sessions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupAuthSessionControllerTest(t *testing.T, username string) (*gorm.DB, *model.User) {
	t.Helper()
	previousDB := model.DB
	previousType := common.MainDatabaseType()
	previousRedis := common.RedisEnabled
	previousSecret := common.SessionSecret
	previousActiveLimit := common.UserSessionActiveLimit
	previousIssuanceLimit := common.UserSessionIssuanceLimit
	previousIssuanceWindow := common.UserSessionIssuanceWindowSeconds
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.AuthFlow{}))
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.SessionSecret = "auth-session-controller-test-secret"
	common.UserSessionActiveLimit = common.DefaultUserSessionActiveLimit
	common.UserSessionIssuanceLimit = common.DefaultUserSessionIssuanceLimit
	common.UserSessionIssuanceWindowSeconds = int64(common.DefaultUserSessionIssuanceWindowSeconds)
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetMainDatabaseType(previousType)
		common.RedisEnabled = previousRedis
		common.SessionSecret = previousSecret
		common.UserSessionActiveLimit = previousActiveLimit
		common.UserSessionIssuanceLimit = previousIssuanceLimit
		common.UserSessionIssuanceWindowSeconds = previousIssuanceWindow
		_ = sqlDB.Close()
	})

	user := &model.User{
		Username: username, Password: "unused", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1,
	}
	require.NoError(t, db.Create(user).Error)
	return db, user
}

func controllerLegacySessionCookie(t *testing.T, userID int) *http.Cookie {
	t.Helper()
	store := gsessions.NewCookieStore([]byte(common.SessionSecret))
	store.MaxAge(30 * 24 * 60 * 60)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()
	session, err := store.New(request, service.LegacySessionCookieName)
	require.NoError(t, err)
	session.Values["id"] = userID
	require.NoError(t, store.Save(request, recorder, session))
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1)
	return cookies[0]
}

func requireResponseCookie(t *testing.T, recorder *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	require.FailNow(t, "response cookie not found", name)
	return nil
}

func TestAuthLogoutRejectsRefreshCookieSessionMismatch(t *testing.T) {
	_, user := setupAuthSessionControllerTest(t, "logout-mismatch-user")
	sessionA, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "agent-a")
	require.NoError(t, err)
	sessionB, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "agent-b")
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/auth/logout", nil)
	c.Request.Header.Set("Authorization", "Bearer "+sessionA.AccessToken)
	c.Request.Header.Set("X-Auth-Session", sessionA.Session.SID)
	c.Request.AddCookie(&http.Cookie{Name: service.RefreshCookieName, Value: sessionB.RefreshToken})

	AuthLogout(c)

	assert.Equal(t, http.StatusConflict, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, "AUTH_SESSION_MISMATCH", response.Code)
	for _, sid := range []string{sessionA.Session.SID, sessionB.Session.SID} {
		stored, err := model.GetUserSessionBySID(sid)
		require.NoError(t, err)
		assert.Equal(t, model.UserSessionStatusActive, stored.Status)
	}
}

func TestRefreshAuthUpgradesLegacyCookie(t *testing.T) {
	_, user := setupAuthSessionControllerTest(t, "legacy-refresh-user")
	legacyCookie := controllerLegacySessionCookie(t, user.Id)
	gim := gin.New()
	gim.POST("/api/user/auth/refresh", RefreshAuth)
	request := httptest.NewRequest(http.MethodPost, "/api/user/auth/refresh", nil)
	request.AddCookie(legacyCookie)
	response := httptest.NewRecorder()

	gim.ServeHTTP(response, request)

	assert.Equal(t, http.StatusOK, response.Code)
	refreshCookie := requireResponseCookie(t, response, service.RefreshCookieName)
	assert.NotEmpty(t, refreshCookie.Value)
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			AccessToken string                   `json:"access_token"`
			Session     service.LoginSessionView `json:"session"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	assert.True(t, body.Success)
	assert.NotEmpty(t, body.Data.AccessToken)
	assert.NotEmpty(t, body.Data.Session.SID)
	stored, err := model.GetUserSessionBySID(body.Data.Session.SID)
	require.NoError(t, err)
	assert.Equal(t, model.UserSessionStatusActive, stored.Status)
}

func TestLegacyGetLogoutPreventsCookieReplay(t *testing.T) {
	_, user := setupAuthSessionControllerTest(t, "legacy-get-logout-user")
	legacyCookie := controllerLegacySessionCookie(t, user.Id)
	gim := gin.New()
	gim.GET("/api/user/logout", AuthLogout)
	gim.POST("/api/user/auth/refresh", RefreshAuth)

	logoutRequest := httptest.NewRequest(http.MethodGet, "/api/user/logout", nil)
	logoutRequest.AddCookie(legacyCookie)
	logoutResponse := httptest.NewRecorder()
	gim.ServeHTTP(logoutResponse, logoutRequest)

	assert.Equal(t, http.StatusOK, logoutResponse.Code)
	assert.Less(t, requireResponseCookie(t, logoutResponse, service.LegacySessionCookieName).MaxAge, 0)
	var session model.UserSession
	require.NoError(t, model.DB.Take(&session).Error)
	assert.Equal(t, model.UserSessionStatusRevoked, session.Status)

	replayRequest := httptest.NewRequest(http.MethodPost, "/api/user/auth/refresh", nil)
	replayRequest.AddCookie(legacyCookie)
	replayResponse := httptest.NewRecorder()
	gim.ServeHTTP(replayResponse, replayRequest)

	assert.Equal(t, http.StatusUnauthorized, replayResponse.Code)
	var replayBody struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
	}
	require.NoError(t, common.Unmarshal(replayResponse.Body.Bytes(), &replayBody))
	assert.False(t, replayBody.Success)
	assert.Equal(t, "AUTH_SESSION_REVOKED", replayBody.Code)
	var sessionCount int64
	require.NoError(t, model.DB.Model(&model.UserSession{}).Count(&sessionCount).Error)
	assert.EqualValues(t, 1, sessionCount)
}

func TestAccessTokenLogoutAlsoRevokesLegacyCookieSession(t *testing.T) {
	_, user := setupAuthSessionControllerTest(t, "access-plus-legacy-logout-user")
	accessSession, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "current-browser")
	require.NoError(t, err)
	legacyCookie := controllerLegacySessionCookie(t, user.Id)
	legacyBundle, _, err := service.UpgradeLegacyLoginSession(
		requestWithCookie(http.MethodPost, "/api/user/auth/refresh", legacyCookie),
		"127.0.0.1",
		"legacy-browser",
	)
	require.NoError(t, err)
	require.NotEqual(t, accessSession.Session.SID, legacyBundle.Session.SID)

	request := requestWithCookie(http.MethodPost, "/api/user/auth/logout", legacyCookie)
	request.Header.Set("Authorization", "Bearer "+accessSession.AccessToken)
	request.Header.Set("X-Auth-Session", accessSession.Session.SID)
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = request

	AuthLogout(c)

	assert.Equal(t, http.StatusOK, response.Code)
	for _, sid := range []string{accessSession.Session.SID, legacyBundle.Session.SID} {
		stored, err := model.GetUserSessionBySID(sid)
		require.NoError(t, err)
		assert.Equal(t, model.UserSessionStatusRevoked, stored.Status)
	}
	_, _, err = service.UpgradeLegacyLoginSession(
		requestWithCookie(http.MethodPost, "/api/user/auth/refresh", legacyCookie),
		"127.0.0.1",
		"legacy-browser",
	)
	assert.ErrorIs(t, err, service.ErrLoginSessionRevoked)
}

func requestWithCookie(method, target string, cookie *http.Cookie) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	request.AddCookie(cookie)
	return request
}

func TestWriteAuthSessionErrorMapsSessionGrowthLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name           string
		err            error
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "active session limit",
			err:            model.ErrUserSessionLimit,
			expectedStatus: http.StatusConflict,
			expectedCode:   "AUTH_SESSION_LIMIT",
		},
		{
			name:           "issuance limit",
			err:            model.ErrUserSessionIssuanceLimit,
			expectedStatus: http.StatusTooManyRequests,
			expectedCode:   "AUTH_SESSION_ISSUANCE_LIMIT",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			writeAuthSessionError(c, test.err)

			assert.Equal(t, test.expectedStatus, recorder.Code)
			var response struct {
				Success bool   `json:"success"`
				Code    string `json:"code"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success)
			assert.Equal(t, test.expectedCode, response.Code)
		})
	}
}

func TestSessionLimitDoesNotRecordRejectedLoginAsSuccessful(t *testing.T) {
	previousDB := model.DB
	previousRedis := common.RedisEnabled
	previousActiveLimit := common.UserSessionActiveLimit
	previousIssuanceLimit := common.UserSessionIssuanceLimit
	previousIssuanceWindow := common.UserSessionIssuanceWindowSeconds
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}))
	model.DB = db
	common.RedisEnabled = false
	common.UserSessionActiveLimit = 1
	common.UserSessionIssuanceLimit = 100
	common.UserSessionIssuanceWindowSeconds = int64(common.DefaultUserSessionIssuanceWindowSeconds)
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedis
		common.UserSessionActiveLimit = previousActiveLimit
		common.UserSessionIssuanceLimit = previousIssuanceLimit
		common.UserSessionIssuanceWindowSeconds = previousIssuanceWindow
	})

	const previousLastLoginAt = int64(123)
	user := &model.User{
		Username: "rejected-login-audit-user", Password: "unused", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, LastLoginAt: previousLastLoginAt,
	}
	require.NoError(t, db.Create(user).Error)
	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.UserSession{
		SID: "existing-active-session", UserID: user.Id, Version: 1, UserAuthVersion: user.AuthVersion,
		Status: model.UserSessionStatusActive, RefreshHash: "hash", LoginMethod: "password",
		CreatedAt: now, LastActiveAt: now, ExpiresAt: now + 3600,
	}).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/login", nil)
	setupLogin(user, c)

	assert.Equal(t, http.StatusConflict, recorder.Code)
	var stored model.User
	require.NoError(t, db.First(&stored, user.Id).Error)
	assert.Equal(t, previousLastLoginAt, stored.LastLoginAt)
}
