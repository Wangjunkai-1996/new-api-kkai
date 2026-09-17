package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAutoGroupAccessRouter(t *testing.T) *gin.Engine {
	t.Helper()
	originalDB, originalLogDB := model.DB, model.LOG_DB
	originalRedis, originalMemoryCache := common.RedisEnabled, common.MemoryCacheEnabled
	originalMainType, originalLogType := common.MainDatabaseType(), common.LogDatabaseType()
	originalAutoGroups := setting.AutoGroups2JsonString()
	originalProfiles := setting.AutoGroupProfiles2JsonString()
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	affinity := operation_setting.GetChannelAffinitySetting()
	originalAffinityEnabled := affinity.Enabled
	t.Cleanup(func() {
		model.DB, model.LOG_DB = originalDB, originalLogDB
		common.RedisEnabled, common.MemoryCacheEnabled = originalRedis, originalMemoryCache
		common.SetDatabaseTypes(originalMainType, originalLogType)
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(originalProfiles))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
		affinity.Enabled = originalAffinityEnabled
		model.InvalidatePricingCache()
	})

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}))
	withSelfUseModeEnabled(t)
	require.NoError(t, i18n.Init())
	common.MemoryCacheEnabled = false
	affinity.Enabled = false
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default"]`))
	require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(`{"auto2":["restricted","vip"],"auto3":["vip"]}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP","auto":"Auto","auto2":"Auto 2","auto4":"Deleted"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":2,"restricted":3}`))
	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "auto-access-user", Password: "unused", Group: "default",
		Status: common.UserStatusEnabled,
	}).Error)
	for id, seed := range []struct {
		group  string
		models []string
	}{
		{group: "default", models: []string{"zz-default-model", "zz-shared-model"}},
		{group: "vip", models: []string{"zz-vip-model", "zz-shared-model", "gpt-4-gizmo-demo"}},
		{group: "restricted", models: []string{"zz-restricted-model"}},
	} {
		channel := model.Channel{
			Id: id + 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
			Name: seed.group, Key: "upstream-key", Group: seed.group, Models: strings.Join(seed.models, ","),
		}
		require.NoError(t, db.Create(&channel).Error)
		for _, modelName := range seed.models {
			require.NoError(t, db.Create(&model.Ability{
				Group: seed.group, Model: modelName, ChannelId: channel.Id, Enabled: true,
			}).Error)
		}
	}
	const limits = "zz-default-model,zz-vip-model,gpt-4-gizmo-*,zz-restricted-model"
	for id, token := range []model.Token{
		{Key: "auto", Group: "auto"},
		{Key: "auto2", Group: "auto2"},
		{Key: "limitedauto", Group: "auto", ModelLimitsEnabled: true, ModelLimits: limits},
		{Key: "limitedauto2", Group: "auto2", ModelLimitsEnabled: true, ModelLimits: limits},
		{Key: "denied", Group: "auto3"},
		{Key: "deleted", Group: "auto4"},
	} {
		token.Id, token.UserId = id+1, 1
		token.Status, token.ExpiredTime, token.UnlimitedQuota = common.TokenStatusEnabled, -1, true
		require.NoError(t, db.Create(&token).Error)
	}
	model.InvalidatePricingCache()

	router := gin.New()
	router.GET("/v1/models", middleware.TokenAuth(), func(c *gin.Context) {
		ListModels(c, constant.ChannelTypeOpenAI)
	})
	router.GET("/api/token/:id/models", func(c *gin.Context) {
		c.Set("id", 1)
		GetTokenModels(c)
	})
	router.POST("/v1/chat/completions", middleware.TokenAuth(), middleware.Distribute(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"token_group": common.GetContextKeyString(c, constant.ContextKeyTokenGroup),
			"auto_group":  common.GetContextKeyString(c, constant.ContextKeyAutoGroup),
			"channel_id":  common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		})
	})
	return router
}

func TestAutoGroupModelListsRespectProfileAndTokenLimits(t *testing.T) {
	router := setupAutoGroupAccessRouter(t)
	for _, tc := range []struct {
		key      string
		tokenID  int
		expected []string
	}{
		{key: "auto", tokenID: 1, expected: []string{"zz-default-model", "zz-shared-model"}},
		{key: "auto2", tokenID: 2, expected: []string{"zz-vip-model", "zz-shared-model", "gpt-4-gizmo-demo"}},
		{key: "limitedauto", tokenID: 3, expected: []string{"zz-default-model"}},
		{key: "limitedauto2", tokenID: 4, expected: []string{"zz-vip-model", "gpt-4-gizmo-demo"}},
	} {
		t.Run(tc.key, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			request.Header.Set("Authorization", "Bearer sk-"+tc.key)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			payload := decodeListModelsPayload(t, response)
			ids := make([]string, 0, len(payload.Data))
			for _, item := range payload.Data {
				ids = append(ids, item.Id)
			}
			assert.ElementsMatch(t, tc.expected, ids)

			response = httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/token/"+strconv.Itoa(tc.tokenID)+"/models", nil))
			assert.ElementsMatch(t, tc.expected, decodeUserModelsResponse(t, response))
		})
	}
	for _, id := range []int{5, 6} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/token/"+strconv.Itoa(id)+"/models", nil))
		assert.Empty(t, decodeUserModelsResponse(t, response), "unauthorized and deleted profiles expose no models")
	}
}

func TestAutoGroupAuthAndDistributionRespectAccess(t *testing.T) {
	router := setupAutoGroupAccessRouter(t)
	for _, tc := range []struct {
		name         string
		key          string
		model        string
		status       int
		group        string
		tokenGroup   string
		channelID    int
		errorMessage string
	}{
		{name: "legacy auto", key: "auto", model: "zz-shared-model", status: http.StatusOK, group: "default", tokenGroup: "auto", channelID: 1},
		{name: "independent auto2", key: "auto2", model: "zz-shared-model", status: http.StatusOK, group: "vip", tokenGroup: "auto2", channelID: 2},
		{name: "limited allowed model", key: "limitedauto2", model: "gpt-4-gizmo-demo", status: http.StatusOK, group: "vip", tokenGroup: "auto2", channelID: 2},
		{name: "profile permission denied", key: "denied", model: "zz-vip-model", status: http.StatusForbidden, errorMessage: "auto3"},
		{name: "deleted profile denied", key: "deleted", model: "zz-vip-model", status: http.StatusForbidden, errorMessage: "auto4"},
		{name: "model limit denied", key: "limitedauto2", model: "zz-shared-model", status: http.StatusForbidden, errorMessage: "zz-shared-model"},
		{name: "unauthorized candidate denied", key: "auto2", model: "zz-restricted-model", status: http.StatusServiceUnavailable, errorMessage: "auto2"},
		{name: "other profile model denied", key: "auto2", model: "zz-default-model", status: http.StatusServiceUnavailable, errorMessage: "auto2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"`+tc.model+`"}`))
			request.Header.Set("Authorization", "Bearer sk-"+tc.key)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			if tc.status != http.StatusOK {
				assert.Contains(t, response.Body.String(), tc.errorMessage)
				return
			}
			var payload struct {
				TokenGroup string `json:"token_group"`
				AutoGroup  string `json:"auto_group"`
				ChannelID  int    `json:"channel_id"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
			assert.Equal(t, tc.tokenGroup, payload.TokenGroup)
			assert.Equal(t, tc.group, payload.AutoGroup)
			assert.Equal(t, tc.channelID, payload.ChannelID)
		})
	}
}
