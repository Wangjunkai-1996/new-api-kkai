package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newTenantMiddlewareTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:tenant-middleware-test-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Tenant{}, &model.TenantDomain{}))
	return db
}

func TestTenantContextMiddlewareSeparatesPlatformAndTenantHosts(t *testing.T) {
	db := newTenantMiddlewareTestDB(t)
	require.NoError(t, db.Create(&model.Tenant{ID: 42, Status: model.TenantStatusActive}).Error)
	require.NoError(t, db.Create(&model.TenantDomain{TenantID: 42, Hostname: "kimi.example.com", Status: model.TenantDomainStatusActive}).Error)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(TenantContext(db, "main.example.com"))
	engine.GET("/scope", func(c *gin.Context) {
		resolved, ok := GetTenantContext(c)
		require.True(t, ok)
		assert.Equal(t, resolved.TenantID, common.GetContextKeyInt64(c, constant.ContextKeyTenantID))
		c.JSON(http.StatusOK, gin.H{"tenant_id": resolved.TenantID, "host": resolved.Hostname})
	})

	platformRequest := httptest.NewRequest(http.MethodGet, "http://main.example.com/scope", nil)
	platformResponse := httptest.NewRecorder()
	engine.ServeHTTP(platformResponse, platformRequest)
	require.Equal(t, http.StatusOK, platformResponse.Code)
	assert.Contains(t, platformResponse.Body.String(), `"tenant_id":0`)

	tenantRequest := httptest.NewRequest(http.MethodGet, "http://kimi.example.com/scope", nil)
	tenantResponse := httptest.NewRecorder()
	engine.ServeHTTP(tenantResponse, tenantRequest)
	require.Equal(t, http.StatusOK, tenantResponse.Code)
	assert.Contains(t, tenantResponse.Body.String(), `"tenant_id":42`)
}

func TestTenantContextMiddlewareRejectsUnknownHostWithoutPlatformFallback(t *testing.T) {
	db := newTenantMiddlewareTestDB(t)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(TenantContext(db, "main.example.com"))
	called := false
	engine.GET("/scope", func(c *gin.Context) { called = true })

	request := httptest.NewRequest(http.MethodGet, "http://unprovisioned.example.com/scope", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	assert.Equal(t, http.StatusNotFound, response.Code)
	assert.False(t, called)
}

func TestTenantRoutingDisabledStillRejectsTenantAndUnknownHosts(t *testing.T) {
	db := newTenantMiddlewareTestDB(t)
	require.NoError(t, db.Create(&model.Tenant{ID: 42, Status: model.TenantStatusActive}).Error)
	require.NoError(t, db.Create(&model.TenantDomain{TenantID: 42, Hostname: "kimi.example.com", Status: model.TenantDomainStatusActive}).Error)
	engine := gin.New()
	engine.Use(TenantRouting(db, false, "main.example.com"))
	engine.GET("/scope", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for _, test := range []struct {
		host string
		code int
	}{
		{"MAIN.example.com:443", http.StatusNoContent},
		{"kimi.example.com", http.StatusNotFound},
		{"unknown.example.com", http.StatusNotFound},
		{"127.0.0.1", http.StatusNotFound},
	} {
		request := httptest.NewRequest(http.MethodGet, "https://"+test.host+"/scope", nil)
		request.Header.Set("X-Forwarded-Host", "main.example.com")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		assert.Equal(t, test.code, response.Code, test.host)
	}
}
