package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAdminUserListsIncludePrimarySiteOwnership(t *testing.T) {
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Tenant{}, &model.TenantProfile{}, &model.TenantDomain{}, &model.TenantMembership{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		require.NoError(t, sqlDB.Close())
	})
	gin.SetMode(gin.TestMode)

	for _, tenant := range []model.Tenant{
		{ID: 42, Status: model.TenantStatusActive, DisplayName: "Original name"},
		{ID: 43, Status: model.TenantStatusDisabled, DisplayName: "Closed site"},
		{ID: 44, Status: model.TenantStatusActive, DisplayName: "Fallback name"},
	} {
		require.NoError(t, db.Create(&tenant).Error)
	}
	require.NoError(t, db.Create(&model.TenantProfile{TenantID: 42, SiteName: "Current brand"}).Error)
	require.NoError(t, db.Create(&model.TenantProfile{TenantID: 43}).Error)
	for _, domain := range []model.TenantDomain{
		{TenantID: 42, Hostname: "old.example.test", Status: model.TenantDomainStatusDisabled},
		{TenantID: 42, Hostname: "active.example.test", Status: model.TenantDomainStatusActive},
		{TenantID: 43, Hostname: "closed.example.test", Status: model.TenantDomainStatusDisabled},
	} {
		require.NoError(t, db.Create(&domain).Error)
	}
	for i, tenantID := range []int64{0, 42, 43, 44, 99} {
		username := "customer-" + string(rune('a'+i))
		if tenantID == 0 {
			username = "owner"
		}
		user := model.User{Id: i + 1, Username: username, TenantID: tenantID, Password: "private-password-hash", AffCode: username}
		user.SetAccessToken("private-access-token-" + username)
		require.NoError(t, db.Create(&user).Error)
	}
	require.NoError(t, db.Create(&model.TenantMembership{UserID: 1, TenantID: 42, Role: model.TenantMembershipRoleOwner, Status: model.TenantMembershipStatusActive}).Error)
	require.NoError(t, db.Delete(&model.User{}, 3).Error)

	wants := map[int]struct {
		tenantID int64
		name     string
		domain   string
	}{
		1: {0, "", ""},
		2: {42, "Current brand", "active.example.test"},
		3: {43, "Closed site", "closed.example.test"},
		4: {44, "Fallback name", ""},
		5: {99, "", ""},
	}
	for _, tc := range []struct {
		name    string
		path    string
		handler gin.HandlerFunc
		ids     []int
		total   int
	}{
		{"listing", "/api/user/?sort_by=id&sort_order=asc", GetAllUsers, []int{1, 2, 3, 4, 5}, 5},
		{"search", "/api/user/search?keyword=customer&sort_by=id&sort_order=asc", SearchUsers, []int{2, 3, 4, 5}, 4},
		{"listing page", "/api/user/?p=2&page_size=2&sort_by=id&sort_order=asc", GetAllUsers, []int{3, 4}, 5},
		{"search page", "/api/user/search?keyword=customer&p=2&page_size=2&sort_by=id&sort_order=asc", SearchUsers, []int{4, 5}, 4},
		{"empty search", "/api/user/search?keyword=not-found", SearchUsers, []int{}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, tc.path, nil)
			tc.handler(c)
			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Success bool `json:"success"`
				Data    struct {
					Items []map[string]any `json:"items"`
					Total int              `json:"total"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.True(t, response.Success, recorder.Body.String())
			assert.Equal(t, tc.total, response.Data.Total)
			require.Len(t, response.Data.Items, len(tc.ids))
			for i, id := range tc.ids {
				row, want := response.Data.Items[i], wants[id]
				assert.Equal(t, float64(id), row["id"])
				assert.Equal(t, float64(want.tenantID), row["tenant_id"])
				assert.Equal(t, want.name, row["tenant_name"])
				assert.Equal(t, want.domain, row["tenant_domain"])
				assert.Empty(t, row["password"])
				assert.NotContains(t, row, "access_token")
			}
			assert.NotContains(t, recorder.Body.String(), "private-password-hash")
			assert.NotContains(t, recorder.Body.String(), "private-access-token")
		})
	}

	ordinaryUser, err := common.Marshal(&model.User{TenantID: 42})
	require.NoError(t, err)
	assert.NotContains(t, string(ordinaryUser), "tenant_id")
	assert.NotContains(t, string(ordinaryUser), "tenant_name")
	assert.NotContains(t, string(ordinaryUser), "tenant_domain")
}
