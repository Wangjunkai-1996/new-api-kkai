package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func resellerPricesFixture(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	previousDB := model.DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousOpen, previousGroups, previousModels, previousPrice := setting.UserUsableGroups2JSONString(), ratio_setting.GroupRatio2JSONString(), ratio_setting.ModelRatio2JSONString(), operation_setting.Price
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetDatabaseTypes(previousMain, previousLog)
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousOpen))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroups))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousModels))
		operation_setting.Price = previousPrice
		model.InvalidatePricingCache()
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Model{}, &model.Vendor{}, &model.Tenant{}, &model.TenantMembership{}, &model.TenantCatalogEntry{}, &model.TenantPriceVersion{}))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","premium":"Premium"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"premium":2,"private":0.5}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"shared-text":1}`))
	operation_setting.Price = 1
	require.NoError(t, db.Create(&[]model.Tenant{{ID: 42, Status: model.TenantStatusActive}, {ID: 43, Status: model.TenantStatusActive}}).Error)
	for _, membership := range []model.TenantMembership{
		{TenantID: 42, UserID: 7, Role: model.TenantMembershipRoleOwner, Status: model.TenantMembershipStatusActive},
		{TenantID: 43, UserID: 8, Role: model.TenantMembershipRoleOwner, Status: model.TenantMembershipStatusActive},
		{TenantID: 42, UserID: 9, Role: model.TenantMembershipRoleCustomer, Status: model.TenantMembershipStatusActive},
	} {
		require.NoError(t, db.Create(&membership).Error)
	}
	require.NoError(t, db.Create(&model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "public-supply", Models: "shared-text", Group: "default,premium,private"}).Error)
	for _, group := range []string{"default", "premium", "private"} {
		require.NoError(t, db.Create(&model.Ability{ChannelId: 1, Model: "shared-text", Group: group, Enabled: true}).Error)
	}
	model.InvalidatePricingCache()
	return db
}

func resellerPricesHTTP(t *testing.T, tenantID int64, userID int, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyTenantContext, model.TenantContext{TenantID: tenantID})
		c.Set("id", userID)
	}, middleware.TenantAdminAuth())
	router.GET("/catalog", GetResellerCatalog)
	router.PATCH("/catalog", UpdateResellerCatalog)
	router.GET("/prices", GetResellerPrices)
	router.PATCH("/prices", UpdateResellerPrices)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
	return response
}

func TestResellerGroupPricesInheritCatalogAndKeepModelReadCompatibility(t *testing.T) {
	db := resellerPricesFixture(t)
	require.NoError(t, db.Create(&[]model.TenantPriceVersion{
		{TenantID: 42, Model: "legacy-hidden", Group: "default", Version: 1, SalesMultiplier: "9"},
		{TenantID: 43, Group: "default", Version: 1, SalesMultiplier: "8"},
	}).Error)
	catalog := resellerPricesHTTP(t, 42, 7, http.MethodGet, "/catalog", "")
	var catalogResult struct {
		Success bool                       `json:"success"`
		Data    []model.TenantCatalogEntry `json:"data"`
	}
	require.Equal(t, http.StatusOK, catalog.Code)
	require.NoError(t, common.Unmarshal(catalog.Body.Bytes(), &catalogResult))
	require.True(t, catalogResult.Success, catalog.Body.String())
	require.Len(t, catalogResult.Data, 2)
	for _, entry := range catalogResult.Data {
		assert.Equal(t, "shared-text", entry.Model)
		assert.EqualValues(t, 42, entry.TenantID)
		assert.Contains(t, []string{"default", "premium"}, entry.SalesGroup)
	}
	groupResponse := resellerPricesHTTP(t, 42, 7, http.MethodGet, "/prices?scope=group", "")
	var groups struct {
		Success bool                       `json:"success"`
		Data    []service.TenantGroupPrice `json:"data"`
	}
	require.Equal(t, http.StatusOK, groupResponse.Code)
	require.NoError(t, common.Unmarshal(groupResponse.Body.Bytes(), &groups))
	require.True(t, groups.Success, groupResponse.Body.String())
	require.Len(t, groups.Data, 2)
	assert.Equal(t, service.TenantGroupPrice{Group: "default", SalesMultiplier: "1", PlatformMultiplier: "1", MinimumMultiplier: "1", MarkupPercent: "0", PricingMode: "markup", Inherited: true}, groups.Data[0])
	assert.Equal(t, service.TenantGroupPrice{Group: "premium", SalesMultiplier: "2", PlatformMultiplier: "2", MinimumMultiplier: "2", MarkupPercent: "0", PricingMode: "markup", Inherited: true}, groups.Data[1])
	legacyResponse := resellerPricesHTTP(t, 42, 7, http.MethodGet, "/prices", "")
	var prices struct {
		Success bool                       `json:"success"`
		Data    []model.TenantPriceVersion `json:"data"`
	}
	require.NoError(t, common.Unmarshal(legacyResponse.Body.Bytes(), &prices))
	require.True(t, prices.Success, legacyResponse.Body.String())
	require.Len(t, prices.Data, 2)
	for i, price := range prices.Data {
		assert.Equal(t, "shared-text", price.Model, "never expose internal empty-model group rows")
		assert.EqualValues(t, 42, price.TenantID)
		assert.Equal(t, groups.Data[i].SalesMultiplier, price.SalesMultiplier)
		assert.Positive(t, price.SupplierPriceMinor)
	}
	var count int64
	require.NoError(t, db.Model(&model.TenantPriceVersion{}).Count(&count).Error)
	assert.EqualValues(t, 2, count, "inherited reads must not create overrides")
	require.NoError(t, db.Model(&model.TenantCatalogEntry{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestResellerGroupPricesSaveConflictAndReset(t *testing.T) {
	db := resellerPricesFixture(t)
	saved := resellerPricesHTTP(t, 42, 7, http.MethodPatch, "/prices", `{"prices":[{"sales_group":"default","sale_multiplier":"1.2","expected_version":0}]}`)
	require.Equal(t, http.StatusOK, saved.Code)
	var result struct {
		Success bool                       `json:"success"`
		Code    string                     `json:"code"`
		Data    []service.TenantGroupPrice `json:"data"`
	}
	require.NoError(t, common.Unmarshal(saved.Body.Bytes(), &result))
	require.True(t, result.Success, saved.Body.String())
	require.Len(t, result.Data, 2)
	assert.Equal(t, "1.2", result.Data[0].SalesMultiplier)
	assert.EqualValues(t, 1, result.Data[0].Version)
	assert.False(t, result.Data[0].Inherited)
	assert.True(t, result.Data[1].Inherited)
	read := resellerPricesHTTP(t, 42, 7, http.MethodGet, "/prices", "")
	var projection struct {
		Data []model.TenantPriceVersion `json:"data"`
	}
	require.NoError(t, common.Unmarshal(read.Body.Bytes(), &projection))
	require.Len(t, projection.Data, 2)
	assert.Equal(t, "shared-text", projection.Data[0].Model)
	assert.Equal(t, "1.2", projection.Data[0].SalesMultiplier)
	assert.Equal(t, result.Data[0].ID, projection.Data[0].ID)
	assert.EqualValues(t, 1, projection.Data[0].Version)
	conflict := resellerPricesHTTP(t, 42, 7, http.MethodPatch, "/prices", `{"prices":[{"sales_group":"default","sale_multiplier":"1.3","expected_version":0}]}`)
	require.Equal(t, http.StatusConflict, conflict.Code)
	require.NoError(t, common.Unmarshal(conflict.Body.Bytes(), &result))
	assert.False(t, result.Success)
	assert.Equal(t, "TENANT_PRICE_VERSION_CONFLICT", result.Code)
	reset := resellerPricesHTTP(t, 42, 7, http.MethodPatch, "/prices", `{"prices":[{"sales_group":"default","sale_multiplier":"","expected_version":1}]}`)
	require.Equal(t, http.StatusOK, reset.Code)
	require.NoError(t, common.Unmarshal(reset.Body.Bytes(), &result))
	require.True(t, result.Success, reset.Body.String())
	assert.True(t, result.Data[0].Inherited)
	assert.EqualValues(t, 2, result.Data[0].Version)
	assert.Equal(t, "1", result.Data[0].SalesMultiplier)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1.25,"premium":2}`))
	read = resellerPricesHTTP(t, 42, 7, http.MethodGet, "/prices?scope=group", "")
	require.NoError(t, common.Unmarshal(read.Body.Bytes(), &result))
	assert.Equal(t, "1.25", result.Data[0].SalesMultiplier)
	var versions []model.TenantPriceVersion
	require.NoError(t, db.Order("version").Find(&versions).Error)
	require.Len(t, versions, 2)
	assert.Empty(t, versions[0].Model)
	assert.Equal(t, "1.2", versions[0].SalesMultiplier)
	assert.Empty(t, versions[1].SalesMultiplier, "reset appends an immutable inheritance event")
}

func TestResellerPricesRejectLegacyEmptyAndInvalidUpdates(t *testing.T) {
	db := resellerPricesFixture(t)
	for _, test := range []struct {
		name, path, body, code string
	}{
		{"legacy model", "/prices", `{"prices":[{"sales_group":"default","model":"shared-text","sale_multiplier":"2"}]}`, "TENANT_GROUP_PRICING_REQUIRED"},
		{"mixed payload", "/prices", `{"prices":[{"sales_group":"default","sale_multiplier":"2"},{"sales_group":"premium","model":"shared-text","sale_multiplier":"3"}]}`, "TENANT_GROUP_PRICING_REQUIRED"},
		{"legacy catalog", "/catalog", `{"entries":[{"sales_group":"default","model":"shared-text","is_default":true}]}`, "TENANT_GROUP_PRICING_REQUIRED"},
		{"empty updates", "/prices", `{"prices":[]}`, ""},
		{"missing updates", "/prices", `{}`, ""},
		{"below supply", "/prices", `{"prices":[{"sales_group":"premium","sale_multiplier":"1.9"}]}`, ""},
		{"invalid multiplier", "/prices", `{"prices":[{"sales_group":"default","sale_multiplier":"bad"}]}`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := resellerPricesHTTP(t, 42, 7, http.MethodPatch, test.path, test.body)
			require.Equal(t, http.StatusBadRequest, response.Code)
			var result struct {
				Success bool   `json:"success"`
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			assert.False(t, result.Success)
			assert.Equal(t, test.code, result.Code)
			assert.NotContains(t, result.Message, "1.6%")
			if test.code != "" {
				assert.Equal(t, "Set prices by group instead of model.", result.Message)
			}
		})
	}
	var count int64
	require.NoError(t, db.Model(&model.TenantPriceVersion{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, db.Model(&model.TenantCatalogEntry{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestResellerPricesRequireOwnerAndIgnoreClientTenant(t *testing.T) {
	db := resellerPricesFixture(t)
	require.NoError(t, db.Create(&model.TenantPriceVersion{TenantID: 43, Group: "default", Version: 1, SalesMultiplier: "8"}).Error)
	for _, identity := range []struct {
		tenantID int64
		userID   int
	}{{42, 9}, {42, 8}, {43, 7}, {0, 7}} {
		for _, method := range []string{http.MethodGet, http.MethodPatch} {
			response := resellerPricesHTTP(t, identity.tenantID, identity.userID, method, "/prices?scope=group", `{"prices":[{"sales_group":"default","sale_multiplier":"2"}]}`)
			assert.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
		}
	}
	saved := resellerPricesHTTP(t, 42, 7, http.MethodPatch, "/prices?tenant_id=43", `{"tenant_id":43,"prices":[{"sales_group":"default","sale_multiplier":"1.3"}]}`)
	require.Equal(t, http.StatusOK, saved.Code)
	var result struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(saved.Body.Bytes(), &result))
	require.True(t, result.Success, saved.Body.String())
	var versions []model.TenantPriceVersion
	require.NoError(t, db.Order("tenant_id").Find(&versions).Error)
	require.Len(t, versions, 2)
	assert.EqualValues(t, 42, versions[0].TenantID)
	assert.Equal(t, "1.3", versions[0].SalesMultiplier)
	assert.EqualValues(t, 43, versions[1].TenantID)
	assert.Equal(t, "8", versions[1].SalesMultiplier)
	read := resellerPricesHTTP(t, 42, 7, http.MethodGet, "/prices?scope=group&tenant_id=43", "")
	assert.NotContains(t, read.Body.String(), `"sales_multiplier":"8"`)
}

func TestResellerPricesReportDatabaseFailures(t *testing.T) {
	db := resellerPricesFixture(t)
	require.NoError(t, db.Migrator().DropTable(&model.TenantPriceVersion{}))
	for _, method := range []string{http.MethodGet, http.MethodPatch} {
		response := resellerPricesHTTP(t, 42, 7, method, "/prices?scope=group", `{"prices":[{"sales_group":"default","sale_multiplier":"2"}]}`)
		require.Equal(t, http.StatusOK, response.Code, "database errors use the existing common.ApiError contract")
		var result struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		assert.False(t, result.Success)
		assert.Contains(t, result.Message, "tenant_price_versions")
	}
}
