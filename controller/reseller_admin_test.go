package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func resellerAdminFixture(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("TENANT_ROOT_DOMAIN", "example.com")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Tenant{}, &model.TenantDomain{}, &model.ResellerApplication{}))
	require.NoError(t, db.Create(&model.Tenant{ID: 42, DisplayName: "KimiAI", Status: model.TenantStatusProvisioning}).Error)
	previous := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func TestAdminResellerActivationReadinessMatchesStatusGate(t *testing.T) {
	for _, step := range []struct {
		name     string
		statuses []string
		ready    bool
	}{
		{"unconfigured", nil, false},
		{"disabled domain", []string{model.TenantDomainStatusDisabled}, false},
		{"one active domain without catalog or prices", []string{model.TenantDomainStatusActive}, true},
		{"multiple active domains", []string{model.TenantDomainStatusActive, model.TenantDomainStatusActive}, false},
	} {
		t.Run(step.name, func(t *testing.T) {
			db := resellerAdminFixture(t)
			want := model.ResellerActivation{DomainReady: step.ready, Ready: step.ready}
			for index, status := range step.statuses {
				host := []string{"first.example.com", "second.example.com"}[index]
				require.NoError(t, db.Create(&model.TenantDomain{TenantID: 42, Hostname: host, Status: status}).Error)
			}
			listed := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(listed)
			AdminListResellers(ctx)
			var list struct {
				Success bool `json:"success"`
				Data    struct {
					Tenants []struct {
						Domains    []model.TenantDomain     `json:"domains"`
						Activation model.ResellerActivation `json:"activation"`
					} `json:"tenants"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(listed.Body.Bytes(), &list))
			require.True(t, list.Success, listed.Body.String())
			require.Len(t, list.Data.Tenants, 1)
			assert.Equal(t, want, list.Data.Tenants[0].Activation)
			assert.NotNil(t, list.Data.Tenants[0].Domains)
			response := httptest.NewRecorder()
			ctx, _ = gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest(http.MethodPatch, "/api/admin/resellers/42/status", strings.NewReader(`{"status":"active"}`))
			ctx.Params = gin.Params{{Key: "id", Value: "42"}}
			AdminUpdateResellerStatus(ctx)
			var result struct {
				Success    bool                     `json:"success"`
				Code       string                   `json:"code"`
				Activation model.ResellerActivation `json:"activation"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			var tenant model.Tenant
			require.NoError(t, db.First(&tenant, 42).Error)
			if step.ready {
				assert.Equal(t, http.StatusOK, response.Code)
				assert.True(t, result.Success)
				assert.Equal(t, model.TenantStatusActive, tenant.Status)
			} else {
				assert.Equal(t, http.StatusBadRequest, response.Code)
				assert.False(t, result.Success)
				assert.Equal(t, "RESELLER_ACTIVATION_INCOMPLETE", result.Code)
				assert.Equal(t, want, result.Activation)
				assert.Equal(t, model.TenantStatusProvisioning, tenant.Status)
			}
		})
	}
}

func TestAdminResellerActivationDatabaseErrorsAreNotMissingConfiguration(t *testing.T) {
	db := resellerAdminFixture(t)
	require.NoError(t, db.Migrator().DropTable(&model.TenantDomain{}))
	for _, handler := range []gin.HandlerFunc{AdminListResellers, AdminUpdateResellerStatus} {
		response := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(response)
		ctx.Request = httptest.NewRequest(http.MethodPatch, "/api/admin/resellers/42/status", strings.NewReader(`{"status":"active"}`))
		ctx.Params = gin.Params{{Key: "id", Value: "42"}}
		handler(ctx)
		var result struct {
			Success    bool                      `json:"success"`
			Code       string                    `json:"code"`
			Activation *model.ResellerActivation `json:"activation"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		assert.False(t, result.Success)
		assert.Empty(t, result.Code)
		assert.Nil(t, result.Activation)
		assert.NotContains(t, response.Body.String(), "active tenants require")
	}
	var tenant model.Tenant
	require.NoError(t, db.First(&tenant, 42).Error)
	assert.Equal(t, model.TenantStatusProvisioning, tenant.Status)
}

func TestAdminResellerDomainReportsExistingAssignment(t *testing.T) {
	db := resellerAdminFixture(t)
	require.NoError(t, db.Create(&model.TenantDomain{TenantID: 42, Hostname: "kimi.example.com", Status: model.TenantDomainStatusActive}).Error)
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/admin/resellers/42/domain", strings.NewReader(`{"hostname":"other.example.com"}`))
	ctx.Params = gin.Params{{Key: "id", Value: "42"}}
	AdminProvisionResellerDomain(ctx)
	assert.Equal(t, http.StatusConflict, response.Code)
	var result struct {
		Code string `json:"code"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.Equal(t, "RESELLER_DOMAIN_ALREADY_ASSIGNED", result.Code)
	var domains []model.TenantDomain
	require.NoError(t, db.Where("tenant_id = ?", 42).Find(&domains).Error)
	require.Len(t, domains, 1)
	assert.Equal(t, "kimi.example.com", domains[0].Hostname)
}
