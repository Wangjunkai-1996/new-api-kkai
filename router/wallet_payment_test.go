package router

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWalletPaymentRoutesRequireAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerWalletPaymentRoutes(engine.Group("/api"))
	for _, endpoint := range []struct{ method, path string }{
		{"GET", "/api/user/topup/current"},
		{"POST", "/api/user/topup/current/restore"},
		{"POST", "/api/user/topup/quote"},
		{"POST", "/api/user/topup/orders"},
		{"GET", "/api/user/topup/orders/private"},
		{"POST", "/api/user/topup/orders/private/resume"},
		{"POST", "/api/user/topup/orders/private/cancel"},
		{"POST", "/api/user/topup/orders/private/end"},
		{"POST", "/api/user/topup/orders/private/reconcile"},
		{"POST", "/api/user/topup/current/acknowledge"},
		{"GET", "/api/user/topup/orders/private/admin"},
		{"POST", "/api/user/topup/orders/private/admin-reconcile"},
		{"POST", "/api/user/topup/reconcile-runs/report"},
		{"POST", "/api/user/topup/reconcile-runs"},
		{"GET", "/api/user/topup/reconcile-runs/private"},
		{"POST", "/api/user/topup/reconcile-runs/private/pause"},
		{"POST", "/api/user/topup/reconcile-runs/private/resume"},
	} {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(endpoint.method, endpoint.path, nil))
		assert.Equal(t, http.StatusUnauthorized, response.Code, endpoint.method+" "+endpoint.path)
	}
}

func TestWalletPaymentIncidentSequenceDoesNotExhaustAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "wallet-routes.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}))
	oldDB, oldRedis, oldSecure := model.DB, common.RedisEnabled, common.SessionCookieSecure
	oldEnabled, oldLimit, oldDuration := common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration
	model.DB, common.RedisEnabled, common.SessionCookieSecure = db, false, false
	common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration = true, 20, 1200
	t.Cleanup(func() {
		model.DB, common.RedisEnabled, common.SessionCookieSecure = oldDB, oldRedis, oldSecure
		common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration = oldEnabled, oldLimit, oldDuration
	})
	user := model.User{Id: 871391, Username: "wallet-route-user", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, db.Create(&user).Error)
	bundle, err := service.CreateLoginSession(user.Id, "test", "192.0.2.193", "wallet-regression")
	require.NoError(t, err)
	engine := gin.New()
	api := engine.Group("/api")
	registerWalletPaymentRoutes(api)
	api.POST("/user/auth/refresh", middleware.CriticalRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		req.RemoteAddr = "192.0.2.193:1234"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bundle.AccessToken)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		return response
	}
	// The observed twenty requests previously shared the CT bucket. Controllers
	// intentionally fail closed on this fixture's absent wallet schema: a 503
	// proves admission reached the handler without creating financial data.
	for _, step := range []struct {
		path  string
		count int
	}{
		{"/api/user/auth/refresh", 5},
		{"/api/user/topup/current/restore", 3},
		{"/api/user/topup/quote", 6},
		{"/api/user/topup/orders", 2},
		{"/api/user/topup/orders/WT-incident/resume", 2},
		{"/api/user/topup/current/acknowledge", 2},
	} {
		for count := 0; count < step.count; count++ {
			want := http.StatusServiceUnavailable
			if step.path == "/api/user/auth/refresh" {
				want = http.StatusNoContent
			}
			require.Equal(t, want, request(step.path).Code, step.path)
		}
	}
	assert.Equal(t, http.StatusServiceUnavailable, request("/api/user/topup/quote").Code, "the incident's twenty-first request still reaches the quote handler")
	for remaining := 0; remaining < 15; remaining++ {
		require.Equal(t, http.StatusNoContent, request("/api/user/auth/refresh").Code)
	}
	assert.Equal(t, http.StatusTooManyRequests, request("/api/user/auth/refresh").Code, "authentication retains its original independent protection")
	assert.Equal(t, http.StatusServiceUnavailable, request("/api/user/topup/quote").Code, "an exhausted authentication bucket cannot block wallet quotes")
}
