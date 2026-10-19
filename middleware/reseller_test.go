package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newResellerMiddlewareTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:reseller-middleware-test-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Tenant{}, &model.TenantDomain{}, &model.TenantMembership{}))
	return db
}

func TestTenantAdminAuthRequiresHostTenantOwner(t *testing.T) {
	db := newResellerMiddlewareTestDB(t)
	require.NoError(t, db.Create(&model.Tenant{ID: 42, Status: model.TenantStatusActive}).Error)
	require.NoError(t, db.Create(&model.TenantDomain{TenantID: 42, Hostname: "kimi.example.com", Status: model.TenantDomainStatusActive}).Error)
	require.NoError(t, db.Create(&model.TenantMembership{UserID: 7, TenantID: 42, Role: model.TenantMembershipRoleOwner, Status: model.TenantMembershipStatusActive}).Error)
	require.NoError(t, db.Create(&model.TenantMembership{UserID: 8, TenantID: 42, Role: model.TenantMembershipRoleCustomer, Status: model.TenantMembershipStatusActive}).Error)

	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous })
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(TenantContext(db))
	engine.GET("/owner", func(c *gin.Context) {
		c.Set("id", 7)
		TenantAdminAuth()(c)
	})
	engine.GET("/customer", func(c *gin.Context) {
		c.Set("id", 8)
		TenantAdminAuth()(c)
	})

	ownerRequest := httptest.NewRequest(http.MethodGet, "http://kimi.example.com/owner", nil)
	ownerResponse := httptest.NewRecorder()
	engine.ServeHTTP(ownerResponse, ownerRequest)
	assert.Equal(t, http.StatusOK, ownerResponse.Code)

	customerRequest := httptest.NewRequest(http.MethodGet, "http://kimi.example.com/customer", nil)
	customerResponse := httptest.NewRecorder()
	engine.ServeHTTP(customerResponse, customerRequest)
	assert.Equal(t, http.StatusForbidden, customerResponse.Code)
}
