package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRequestTicketRoutesRequireAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerRequestTicketRoutes(engine.Group("/api"))
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodGet, "/api/requests"},
		{http.MethodPost, "/api/requests"},
		{http.MethodGet, "/api/requests/1"},
		{http.MethodGet, "/api/requests/notifications"},
		{http.MethodPost, "/api/requests/notifications/1/read"},
		{http.MethodPost, "/api/requests/notifications/read-all"},
		{http.MethodGet, "/api/requests/admin"},
		{http.MethodGet, "/api/requests/admin/1"},
		{http.MethodPatch, "/api/requests/admin/1/status"},
	} {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(endpoint.method, endpoint.path, nil))
		assert.Equal(t, http.StatusUnauthorized, response.Code, endpoint.method+" "+endpoint.path)
	}
}

func TestRequestTicketAdminRoutesRejectOrdinaryUserSessions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	previousDB, previousRedis := model.DB, common.RedisEnabled
	model.DB, common.RedisEnabled = db, false
	t.Cleanup(func() {
		model.DB, common.RedisEnabled = previousDB, previousRedis
		require.NoError(t, sqlDB.Close())
	})
	user := model.User{Id: 77, Username: "ticket-user", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	session, err := service.CreateLoginSession(user.Id, "test", "127.0.0.1", "router-test")
	require.NoError(t, err)
	engine := gin.New()
	registerRequestTicketRoutes(engine.Group("/api"))
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodGet, "/api/requests/admin"},
		{http.MethodGet, "/api/requests/admin/1"},
		{http.MethodPatch, "/api/requests/admin/1/status"},
	} {
		request := httptest.NewRequest(endpoint.method, endpoint.path, nil)
		request.Header.Set("Authorization", "Bearer "+session.AccessToken)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		assert.Equal(t, http.StatusForbidden, response.Code, endpoint.path+response.Body.String())
		assert.Contains(t, response.Body.String(), "AUTH_INSUFFICIENT_PRIVILEGE")
	}
}
