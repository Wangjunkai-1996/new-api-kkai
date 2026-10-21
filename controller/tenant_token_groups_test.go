package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTenantTokenGroupRouter(t *testing.T) (*gin.Engine, *model.Token) {
	t.Helper()
	setupAutoGroupAccessRouter(t)
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.Tenant{}, &model.TenantPriceVersion{}))
	require.NoError(t, db.Create(&model.Tenant{ID: 42, Status: model.TenantStatusActive}).Error)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Update("tenant_id", 42).Error)
	oldModels := ratio_setting.ModelRatio2JSONString()
	special := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	oldSpecial := special.MarshalJSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"zz-default-model":1,"zz-shared-model":1,"zz-vip-model":1,"zz-restricted-model":1}`))
	// These platform identity overrides must not change the tenant's public supply.
	require.NoError(t, special.UnmarshalJSON([]byte(`{"default":{"-:vip":"platform only exclusion","+:restricted":"platform only grant"}}`)))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldModels))
		require.NoError(t, special.UnmarshalJSON([]byte(oldSpecial)))
		model.InvalidatePricingCache()
	})
	model.InvalidatePricingCache()
	token := seedToken(t, db, 1, "tenant-group-token", "tenantpolicykey")
	assert.Equal(t, int64(42), token.TenantID)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", 1)
		common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
		common.SetContextKey(c, constant.ContextKeyTenantContext, model.TenantContext{TenantID: 42, Hostname: "tenant.example.test"})
	})
	router.GET("/api/user/groups", GetUserGroups)
	router.POST("/api/token/", AddToken)
	router.PUT("/api/token/", UpdateToken)
	router.PATCH("/api/token/:id/group", UpdateTokenGroup)
	router.GET("/api/token/:id/models", GetTokenModels)
	router.GET("/v1/models", middleware.TokenAuth(), func(c *gin.Context) { ListModels(c, constant.ChannelTypeOpenAI) })
	router.POST("/v1/chat/completions", middleware.TokenAuth(), middleware.Distribute(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"group": common.GetContextKeyString(c, constant.ContextKeyUsingGroup)})
	})
	return router, token
}

func TestTenantTokenGroupWritesMatchPublicSupply(t *testing.T) {
	for _, operation := range []string{"create", "update", "patch"} {
		t.Run(operation, func(t *testing.T) {
			router, token := setupTenantTokenGroupRouter(t)
			for _, tc := range []struct {
				group   string
				allowed bool
			}{{"default", true}, {"vip", true}, {"", true}, {"auto", false}, {"auto2", false}, {"restricted", false}, {"missing", false}} {
				t.Run("group="+tc.group, func(t *testing.T) {
					require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", token.Id).Update("group", "default").Error)
					body := map[string]any{"id": token.Id, "group": tc.group, "name": "write-" + tc.group, "unlimited_quota": true, "expired_time": -1}
					method, path := http.MethodPost, "/api/token/"
					if operation == "update" {
						method = http.MethodPut
					} else if operation == "patch" {
						method, path = http.MethodPatch, "/api/token/"+strconv.Itoa(token.Id)+"/group"
					}
					var before int64
					require.NoError(t, model.DB.Model(&model.Token{}).Count(&before).Error)
					payload, err := common.Marshal(body)
					require.NoError(t, err)
					request := httptest.NewRequest(method, path, bytes.NewReader(payload))
					request.Header.Set("Content-Type", "application/json")
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					var result tokenAPIResponse
					require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
					assert.Equal(t, tc.allowed, result.Success, response.Body.String())
					var stored model.Token
					if operation == "create" && tc.allowed {
						require.NoError(t, model.DB.Order("id DESC").First(&stored).Error)
						assert.Equal(t, int64(42), stored.TenantID)
					} else {
						require.NoError(t, model.DB.First(&stored, token.Id).Error)
					}
					if tc.allowed {
						assert.Equal(t, tc.group, stored.Group)
					} else {
						assert.Equal(t, "default", stored.Group, "rejected writes must preserve the usable key")
						var after int64
						require.NoError(t, model.DB.Model(&model.Token{}).Count(&after).Error)
						assert.Equal(t, before, after, "rejected creates must not persist unusable keys")
					}
				})
			}
		})
	}
}

func TestTenantTokenGroupListingAndRelayUseSameSupply(t *testing.T) {
	router, token := setupTenantTokenGroupRouter(t)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/user/groups", nil))
	var groups struct {
		Success bool                      `json:"success"`
		Data    map[string]map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &groups))
	require.True(t, groups.Success, response.Body.String())
	assert.Len(t, groups.Data, 2)
	assert.Contains(t, groups.Data, "default")
	assert.Contains(t, groups.Data, "vip", "platform account-specific exclusions do not remove public tenant supply")
	assert.NotContains(t, groups.Data, "restricted", "platform account-specific grants do not expose private tenant supply")

	for _, tc := range []struct {
		group     string
		allowed   bool
		modelName string
	}{{"default", true, "zz-default-model"}, {"vip", true, "zz-vip-model"}, {"", true, "zz-default-model"}, {"auto2", false, "zz-vip-model"}, {"restricted", false, "zz-restricted-model"}} {
		t.Run("group="+tc.group, func(t *testing.T) {
			require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", token.Id).Update("group", tc.group).Error)
			payload, err := common.Marshal(map[string]string{"model": tc.modelName})
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(payload))
			request.Header.Set("Authorization", "Bearer sk-"+token.Key)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if !tc.allowed {
				assert.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
				return
			}
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			for _, path := range []string{"/v1/models", "/api/token/" + strconv.Itoa(token.Id) + "/models"} {
				request := httptest.NewRequest(http.MethodGet, path, nil)
				request.Header.Set("Authorization", "Bearer sk-"+token.Key)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				if path == "/v1/models" {
					assert.Contains(t, decodeListModelsResponse(t, response), tc.modelName)
				} else {
					assert.Contains(t, decodeUserModelsResponse(t, response), tc.modelName)
				}
				other := "zz-vip-model"
				if tc.group == "vip" {
					other = "zz-default-model"
				}
				assert.NotContains(t, response.Body.String(), other, "a key model list must respect its effective group")
			}
		})
	}

	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", token.Id).Update("group", "vip").Error)
	require.NoError(t, model.DB.Model(&model.Ability{}).Where(map[string]any{"group": "vip"}).Update("enabled", false).Error)
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("Authorization", "Bearer sk-"+token.Key)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.Equal(t, http.StatusForbidden, response.Code, "revoking public supply must invalidate an already saved key group")
}

func TestTenantTokenStatusCannotEnableUnavailableGroup(t *testing.T) {
	router, token := setupTenantTokenGroupRouter(t)
	for _, tc := range []struct {
		group   string
		allowed bool
	}{{"vip", true}, {"", true}, {"auto", false}, {"auto2", false}, {"restricted", false}} {
		t.Run("group="+tc.group, func(t *testing.T) {
			require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", token.Id).Updates(map[string]any{"group": tc.group, "status": common.TokenStatusDisabled}).Error)
			payload, err := common.Marshal(map[string]any{"id": token.Id, "status": common.TokenStatusEnabled})
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodPut, "/api/token/?status_only=true", bytes.NewReader(payload))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			var result tokenAPIResponse
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			assert.Equal(t, tc.allowed, result.Success, response.Body.String())
			var stored model.Token
			require.NoError(t, model.DB.First(&stored, token.Id).Error)
			status := common.TokenStatusDisabled
			if tc.allowed {
				status = common.TokenStatusEnabled
			}
			assert.Equal(t, status, stored.Status)
		})
	}
}
