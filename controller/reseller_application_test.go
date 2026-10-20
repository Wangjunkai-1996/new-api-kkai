package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestResellerApplicationDomainPreferenceAndLegacySchema(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("TENANT_ROOT_DOMAIN", "EXAMPLE.COM")
	t.Setenv("TENANT_PLATFORM_HOSTS", "console.example.com,main.other.test")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ResellerApplication{}, &model.TenantDomain{}, &model.Tenant{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})

	request := func(userID int, prefix string) *httptest.ResponseRecorder {
		t.Helper()
		body, err := common.Marshal(map[string]string{
			"business_name": " KimiAI ", "contact_email": " Owner@EXAMPLE.COM ", "desired_domain_prefix": prefix,
		})
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/reseller/applications", strings.NewReader(string(body)))
		ctx.Set("id", userID)
		CreateResellerApplication(ctx)
		return recorder
	}

	blocked := request(1, "kimi")
	assert.Equal(t, http.StatusServiceUnavailable, blocked.Code, blocked.Body.String())
	assert.Contains(t, blocked.Body.String(), "RESELLER_DOMAIN_PREFIX_UNAVAILABLE")
	legacy := request(1, "")
	require.Equal(t, http.StatusOK, legacy.Code, legacy.Body.String())
	var legacyRow model.ResellerApplication
	require.NoError(t, db.Where("applicant_user_id = ?", 1).First(&legacyRow).Error)
	assert.Empty(t, legacyRow.ContactName)
	assert.Equal(t, "KimiAI", legacyRow.BusinessName)
	assert.Equal(t, "owner@example.com", legacyRow.ContactEmail)

	require.NoError(t, db.Exec("ALTER TABLE reseller_applications ADD COLUMN desired_domain_prefix varchar(63)").Error)
	require.NoError(t, db.AutoMigrate(&kkaimigrate.AppliedMigration{}))
	require.NoError(t, db.Create(&kkaimigrate.AppliedMigration{Version: 15, Name: "application-prefix"}).Error)
	require.NoError(t, db.Create(&model.TenantDomain{TenantID: 90, Hostname: "used.example.com", Status: model.TenantDomainStatusActive}).Error)
	require.NoError(t, db.Create(&model.TenantDomain{TenantID: 91, Hostname: "disabled.example.com", Status: model.TenantDomainStatusDisabled}).Error)
	for index, test := range []struct {
		prefix string
		status int
		code   string
	}{
		{"admin", http.StatusBadRequest, "RESELLER_DOMAIN_PREFIX_INVALID"},
		{"kimi.example.com", http.StatusBadRequest, "RESELLER_DOMAIN_PREFIX_INVALID"},
		{"kimi-", http.StatusBadRequest, "RESELLER_DOMAIN_PREFIX_INVALID"},
		{"kimi:443", http.StatusBadRequest, "RESELLER_DOMAIN_PREFIX_INVALID"},
		{"console", http.StatusBadRequest, "RESELLER_DOMAIN_PREFIX_INVALID"},
		{"used", http.StatusConflict, "RESELLER_DOMAIN_PREFIX_TAKEN"},
		{"disabled", http.StatusConflict, "RESELLER_DOMAIN_PREFIX_TAKEN"},
	} {
		t.Run(test.prefix, func(t *testing.T) {
			response := request(index+10, test.prefix)
			assert.Equal(t, test.status, response.Code, response.Body.String())
			assert.Contains(t, response.Body.String(), test.code)
		})
	}

	created := request(42, " KiMi ")
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	var application model.ResellerApplication
	require.NoError(t, db.Where("applicant_user_id = ?", 42).First(&application).Error)
	assert.Equal(t, "kimi", application.DesiredDomainPrefix)
	assert.Zero(t, application.TenantID)
	assert.Empty(t, application.ContactName)
	assert.Equal(t, model.ResellerApplicationStatusPending, application.Status)
	var tenants, domains int64
	require.NoError(t, db.Model(&model.Tenant{}).Count(&tenants).Error)
	require.NoError(t, db.Model(&model.TenantDomain{}).Where("hostname = ?", "kimi.example.com").Count(&domains).Error)
	assert.Zero(t, tenants)
	assert.Zero(t, domains, "an application does not reserve or provision the preferred hostname")
	duplicate := request(42, "another")
	assert.Equal(t, http.StatusConflict, duplicate.Code, duplicate.Body.String())
	assert.Contains(t, duplicate.Body.String(), "RESELLER_APPLICATION_PENDING")
	otherApplicant := request(43, "kimi")
	assert.Equal(t, http.StatusOK, otherApplicant.Code, otherApplicant.Body.String())

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 42)
	ListResellerApplications(ctx)
	var own struct {
		Data []model.ResellerApplication `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &own))
	require.Len(t, own.Data, 1)
	assert.Equal(t, "kimi", own.Data[0].DesiredDomainPrefix)

	recorder = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(recorder)
	AdminListResellers(ctx)
	var admin struct {
		Data struct {
			RootDomain   string                      `json:"root_domain"`
			Applications []model.ResellerApplication `json:"applications"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &admin))
	assert.Equal(t, "example.com", admin.Data.RootDomain)
	require.Len(t, admin.Data.Applications, 3)
	assert.Equal(t, "kimi", admin.Data.Applications[0].DesiredDomainPrefix)
}

func TestAdminResellerDomainUsesSharedPrefixRules(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("TENANT_ROOT_DOMAIN", "example.com")
	t.Setenv("TENANT_PLATFORM_HOSTS", "console.example.com")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Tenant{}, &model.TenantDomain{}))
	require.NoError(t, db.Create(&model.Tenant{ID: 42, Status: model.TenantStatusProvisioning}).Error)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	for _, test := range []struct {
		host   string
		status int
	}{
		{"admin.example.com", http.StatusBadRequest},
		{"console.example.com", http.StatusBadRequest},
		{"nested.kimi.example.com", http.StatusBadRequest},
		{"kimi.other.com", http.StatusBadRequest},
		{"KIMI.EXAMPLE.COM", http.StatusOK},
	} {
		t.Run(test.host, func(t *testing.T) {
			body, err := common.Marshal(map[string]string{"hostname": test.host})
			require.NoError(t, err)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
			ctx.Params = gin.Params{{Key: "id", Value: "42"}}
			AdminProvisionResellerDomain(ctx)
			assert.Equal(t, test.status, recorder.Code, recorder.Body.String())
		})
	}
	var domain model.TenantDomain
	require.NoError(t, db.Where("tenant_id = ?", 42).First(&domain).Error)
	assert.Equal(t, "kimi.example.com", domain.Hostname)
}
