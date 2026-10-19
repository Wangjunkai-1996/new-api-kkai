package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantPricingAndModelsRespectCatalogRetailAndTokenLimits(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	oldRatios := ratio_setting.ModelRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"tenant-text-a":1,"tenant-text-b":2,"platform-only":3}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
		model.InvalidatePricingCache()
	})
	require.NoError(t, db.AutoMigrate(&model.TenantCatalogEntry{}, &model.TenantPriceVersion{}))
	require.NoError(t, db.Create(&model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "tenant-test", Group: "default", Models: "tenant-text-a,tenant-text-b,platform-only"}).Error)
	for _, name := range []string{"tenant-text-a", "tenant-text-b", "platform-only"} {
		require.NoError(t, db.Create(&model.Ability{Group: "default", Model: name, ChannelId: 1, Enabled: true}).Error)
	}
	require.NoError(t, db.Create(&[]model.TenantCatalogEntry{
		{TenantID: 42, Model: "tenant-text-a", SalesGroup: "default", Status: model.TenantCatalogStatusEnabled, IsDefault: true},
		{TenantID: 42, Model: "tenant-text-b", SalesGroup: "default", Status: model.TenantCatalogStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]model.TenantPriceVersion{
		{TenantID: 42, Model: "tenant-text-a", Group: "default", SalesMultiplier: "2", Version: 1},
		{TenantID: 42, Model: "tenant-text-a", Group: "default", SalesMultiplier: "3", Version: 2},
		{TenantID: 43, Model: "tenant-text-b", Group: "default", SalesMultiplier: "9", Version: 1},
	}).Error)
	model.InvalidatePricingCache()
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
	require.Len(t, pricing.Data, 1)
	assert.Equal(t, "tenant-text-a", pricing.Data[0].ModelName)
	assert.Equal(t, 3.0, pricing.Data[0].GroupRatio["default"])
	assert.Equal(t, 1.0, pricing.GroupRatio["default"])
	assert.Equal(t, 1.0, pricing.Data[0].ModelRatio)
	assert.NotContains(t, response.Body.String(), "supplier")
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))

	for _, allowed := range []bool{true, false} {
		response = httptest.NewRecorder()
		c, _ = gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
		common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
		common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"tenant-text-a": allowed, "platform-only": true})
		ListModels(c, constant.ChannelTypeOpenAI)
		names := decodeListModelsResponse(t, response)
		if allowed {
			assert.Contains(t, names, "tenant-text-a")
		} else {
			assert.Empty(t, names)
		}
		assert.NotContains(t, names, "platform-only")
	}

	require.NoError(t, db.AutoMigrate(&model.Token{}))
	user := model.User{TenantID: 42, Username: "tenant-token-user", Status: common.UserStatusEnabled, AffCode: "tenant-token-user"}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, TenantID: 42, Key: "tenant-test-key", Group: "default", ModelLimitsEnabled: true, ModelLimits: "tenant-text-a,platform-only"}
	require.NoError(t, db.Create(&token).Error)
	response = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(response)
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
	assert.Equal(t, map[int][]string{constant.ChannelTypeOpenAI: {"tenant-text-a"}}, dashboard.Data)
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
	require.Len(t, groups.Data, 1)
	assert.Equal(t, 1.0, groups.Data["default"].Ratio)
	response = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/token/auto-groups", nil)
	common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
	GetTokenAutoGroups(c)
	assert.Contains(t, response.Body.String(), `"groups":[]`)
	assert.Contains(t, response.Body.String(), `"max_count":0`)
	// Retiring the tenant model takes effect without invalidating platform caches.
	require.NoError(t, db.Model(&model.TenantCatalogEntry{}).Where("tenant_id = ?", 42).Update("status", model.TenantCatalogStatusDisabled).Error)
	response = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
	ListModels(c, constant.ChannelTypeAnthropic)
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"data":[]`)
}
