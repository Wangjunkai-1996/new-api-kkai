package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantAuthenticatedCatalogPreservesAllPublicGroups(t *testing.T) {
	setupTenantTokenGroupRouter(t)
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.TenantMembership{}, &model.UserSession{}))
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Updates(map[string]any{"role": common.RoleCommonUser, "auth_version": 1}).Error)
	require.NoError(t, db.Create(&model.TenantMembership{TenantID: 42, UserID: 1, Role: model.TenantMembershipRoleCustomer, Status: model.TenantMembershipStatusActive}).Error)
	session, err := service.CreateLoginSession(1, "password", "127.0.0.1", "tenant-catalog-test", 42)
	require.NoError(t, err)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
		common.SetContextKey(c, constant.ContextKeyTenantContext, model.TenantContext{TenantID: 42, Hostname: "tenant.example.test"})
	})
	router.GET("/api/pricing", middleware.TryUserAuth(), func(c *gin.Context) {
		c.Header("X-Test-Authenticated-User", strconv.Itoa(c.GetInt("id")))
		GetPricing(c)
	})
	router.GET("/api/models", middleware.UserAuth(), DashboardListModels)
	router.GET("/api/user/models", middleware.UserAuth(), GetUserModels)

	var anonymousPrices []tenantCustomerPricing
	var anonymousGroups map[string]float64
	for _, authenticated := range []bool{false, true} {
		request := httptest.NewRequest(http.MethodGet, "https://tenant.example.test/api/pricing", nil)
		if authenticated {
			request.Header.Set("Authorization", "Bearer "+session.AccessToken)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var pricing struct {
			Success    bool                    `json:"success"`
			Data       []tenantCustomerPricing `json:"data"`
			GroupRatio map[string]float64      `json:"group_ratio"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &pricing))
		require.True(t, pricing.Success, response.Body.String())
		names := make([]string, 0, len(pricing.Data))
		for _, row := range pricing.Data {
			names = append(names, row.ModelName)
		}
		assert.ElementsMatch(t, []string{"zz-default-model", "zz-shared-model", "zz-vip-model", "gpt-4-gizmo-demo"}, names)
		assert.Equal(t, map[string]float64{"default": 1, "vip": 2}, pricing.GroupRatio)
		if !authenticated {
			assert.Equal(t, "0", response.Header().Get("X-Test-Authenticated-User"))
			anonymousPrices, anonymousGroups = pricing.Data, pricing.GroupRatio
			continue
		}
		require.Equal(t, "1", response.Header().Get("X-Test-Authenticated-User"), "optional authentication must not silently fall back to anonymous")
		assert.ElementsMatch(t, anonymousPrices, pricing.Data, "signing in must preserve every public model and its available groups")
		assert.Equal(t, anonymousGroups, pricing.GroupRatio)
	}

	for _, path := range []string{"/api/models", "/api/user/models"} {
		request := httptest.NewRequest(http.MethodGet, "https://tenant.example.test"+path, nil)
		request.Header.Set("Authorization", "Bearer "+session.AccessToken)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var names []string
		if path == "/api/models" {
			var listing struct {
				Success bool             `json:"success"`
				Data    map[int][]string `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &listing))
			require.True(t, listing.Success, response.Body.String())
			names = listing.Data[constant.ChannelTypeOpenAI]
		} else {
			names = decodeUserModelsResponse(t, response)
		}
		assert.ElementsMatch(t, []string{"zz-default-model", "zz-shared-model", "zz-vip-model", "gpt-4-gizmo-demo"}, names, path)
	}
}

func TestTenantPricingPublishesActualExpressionWithoutChangingPlatformDisplay(t *testing.T) {
	const name = "tenant-cache-price"
	const actual = `tier("base", p * 10 + cr * 2)`
	const display = `tier("base", p * 10 + cr * 1)`
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.TenantPriceVersion{}))
	withTieredBillingConfig(t, map[string]string{name: "tiered_expr"}, map[string]string{name: actual})
	displays, err := common.Marshal(map[string]string{name: display})
	require.NoError(t, err)
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{"display_billing_expr": string(displays)}))
	oldGroups, oldOpen := ratio_setting.GroupRatio2JSONString(), setting.UserUsableGroups2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldOpen))
		model.InvalidatePricingCache()
	})
	require.NoError(t, db.Create(&model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: name, Group: "default"}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: name, ChannelId: 1, Enabled: true}).Error)
	require.NoError(t, db.Create(&model.TenantPriceVersion{TenantID: 42, Group: "default", SalesMultiplier: "3", Version: 1}).Error)
	model.InvalidatePricingCache()

	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/pricing", nil)
	common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
	GetPricing(c)
	var pricing struct {
		Success bool                    `json:"success"`
		Data    []tenantCustomerPricing `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &pricing))
	require.True(t, pricing.Success, response.Body.String())
	require.Len(t, pricing.Data, 1)
	assert.Equal(t, actual, pricing.Data[0].BillingExpr)
	assert.Equal(t, map[string]float64{"default": 3}, pricing.Data[0].GroupRatio)
	platform := model.GetPricing()
	require.Len(t, platform, 1)
	assert.Equal(t, display, platform[0].BillingExpr, "tenant projection must not mutate the shared pricing cache")
}

func TestTenantPricingAndModelsInheritPublicGroupsAndRespectTokenLimits(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	oldModels, oldGroups, oldOpen := ratio_setting.ModelRatio2JSONString(), ratio_setting.GroupRatio2JSONString(), setting.UserUsableGroups2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"tenant-text-a":1,"tenant-text-b":2,"private-only":3}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"premium":2,"private":0.5,"auto":1}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","premium":"Premium","auto":"Automatic"}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldModels))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldOpen))
		model.InvalidatePricingCache()
	})
	require.NoError(t, db.AutoMigrate(&model.TenantPriceVersion{}))
	require.NoError(t, db.Create(&model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "public-supply", Group: "default,premium,private,auto", Models: "tenant-text-a,tenant-text-b,private-only"}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "tenant-text-a", ChannelId: 1, Enabled: true},
		{Group: "premium", Model: "tenant-text-a", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "tenant-text-b", ChannelId: 1, Enabled: true},
		{Group: "private", Model: "private-only", ChannelId: 1, Enabled: true},
		{Group: "auto", Model: "private-only", ChannelId: 1, Enabled: true},
	}).Error)
	// Legacy model-specific rows and another tenant's overrides do not replace
	// inherited public-group pricing. No tenant catalog rows are required.
	require.NoError(t, db.Create(&[]model.TenantPriceVersion{
		{TenantID: 42, Model: "tenant-text-a", Group: "default", SalesMultiplier: "9", Version: 1},
		{TenantID: 43, Group: "premium", SalesMultiplier: "9", Version: 1},
	}).Error)
	model.InvalidatePricingCache()
	for _, overridden := range []bool{false, true} {
		if overridden {
			require.NoError(t, db.Create(&model.TenantPriceVersion{TenantID: 42, Group: "default", SalesMultiplier: "3", Version: 1}).Error)
		}
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/pricing", nil)
		common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
		GetPricing(c)
		var pricing struct {
			Success    bool                    `json:"success"`
			Data       []tenantCustomerPricing `json:"data"`
			GroupRatio map[string]float64      `json:"group_ratio"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &pricing))
		require.True(t, pricing.Success, response.Body.String())
		require.Len(t, pricing.Data, 2, "one model must have one row even when available in several groups")
		byName := make(map[string]tenantCustomerPricing)
		for _, entry := range pricing.Data {
			byName[entry.ModelName] = entry
		}
		defaultRatio := 1.0
		if overridden {
			defaultRatio = 3
		}
		assert.Equal(t, map[string]float64{"default": defaultRatio, "premium": 2}, byName["tenant-text-a"].GroupRatio)
		assert.Equal(t, map[string]float64{"default": defaultRatio}, byName["tenant-text-b"].GroupRatio)
		assert.ElementsMatch(t, []string{"default", "premium"}, byName["tenant-text-a"].EnableGroup)
		assert.Equal(t, map[string]float64{"default": defaultRatio, "premium": 2}, pricing.GroupRatio)
		assert.Equal(t, 1.0, byName["tenant-text-a"].ModelRatio)
		assert.NotContains(t, response.Body.String(), "supplier")
		assert.NotContains(t, response.Body.String(), "private-only")
		assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	}

	for _, allowed := range []bool{true, false} {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
		common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
		common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"tenant-text-a": allowed, "private-only": true})
		ListModels(c, constant.ChannelTypeOpenAI)
		names := decodeListModelsResponse(t, response)
		if allowed {
			assert.Contains(t, names, "tenant-text-a")
			var listing listModelsResponse
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &listing))
			require.Len(t, listing.Data, 1, "multi-group supply must not duplicate model IDs")
			assert.Equal(t, "tenant-text-a", listing.Data[0].Id)
		} else {
			assert.Empty(t, names)
		}
		assert.NotContains(t, names, "private-only")
	}

	require.NoError(t, db.AutoMigrate(&model.Token{}))
	user := model.User{TenantID: 42, Username: "tenant-token-user", Status: common.UserStatusEnabled, AffCode: "tenant-token-user"}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, TenantID: 42, Key: "tenant-test-key", Group: "default", ModelLimitsEnabled: true, ModelLimits: "tenant-text-a,private-only"}
	require.NoError(t, db.Create(&token).Error)
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/token/1/models", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.Itoa(token.Id)}}
	c.Set("id", user.Id)
	common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
	GetTokenModels(c)
	assert.Equal(t, []string{"tenant-text-a"}, decodeUserModelsResponse(t, response))

	response = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/models", nil)
	common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
	DashboardListModels(c)
	var dashboard struct {
		Success bool             `json:"success"`
		Data    map[int][]string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &dashboard))
	require.True(t, dashboard.Success)
	assert.Equal(t, map[int][]string{constant.ChannelTypeOpenAI: {"tenant-text-a", "tenant-text-b"}}, dashboard.Data)
	response = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/groups", nil)
	common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
	GetUserGroups(c)
	var groups struct {
		Data map[string]struct {
			Ratio float64 `json:"ratio"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &groups))
	require.Len(t, groups.Data, 2)
	assert.Equal(t, 3.0, groups.Data["default"].Ratio)
	assert.Equal(t, 2.0, groups.Data["premium"].Ratio)
	response = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/token/auto-groups", nil)
	common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
	GetTokenAutoGroups(c)
	assert.Contains(t, response.Body.String(), `"groups":[]`)
	assert.Contains(t, response.Body.String(), `"max_count":0`)
	// Revoking platform supply takes effect even while pricing metadata is cached.
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", 1).Update("enabled", false).Error)
	response = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
	ListModels(c, constant.ChannelTypeAnthropic)
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"data":[]`)
}
