package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantModelAllowedInheritsOnlyPublicSupportedSupply(t *testing.T) {
	db := newTenantMiddlewareTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Model{}, &model.Vendor{}))
	oldDB, oldPrice := model.DB, operation_setting.Price
	oldOpen, oldGroups, oldModels := setting.UserUsableGroups2JSONString(), ratio_setting.GroupRatio2JSONString(), ratio_setting.ModelRatio2JSONString()
	model.DB, operation_setting.Price = db, 1
	t.Cleanup(func() {
		model.DB, operation_setting.Price = oldDB, oldPrice
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldOpen))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldModels))
		model.InvalidatePricingCache()
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"private":1}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"public-text":1,"private-text":1,"public-image":1}`))
	for i, name := range []string{"public-text", "private-text", "public-image"} {
		group := "default"
		if name == "private-text" {
			group = "private"
		}
		require.NoError(t, db.Create(&model.Channel{Id: i + 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: name, Models: name, Group: group}).Error)
		require.NoError(t, db.Create(&model.Ability{ChannelId: i + 1, Model: name, Group: group, Enabled: true}).Error)
	}
	require.NoError(t, db.Create(&model.Model{ModelName: "public-image", Status: 1, Endpoints: `{"image-generation":"/v1/images/generations"}`}).Error)
	model.InvalidatePricingCache()
	for _, test := range []struct {
		name    string
		allowed bool
	}{{"public-text", true}, {"private-text", false}, {"public-image", false}, {"unknown", false}} {
		allowed, err := TenantModelAllowed(42, test.name)
		require.NoError(t, err)
		assert.Equal(t, test.allowed, allowed, test.name)
	}
	allowed, err := TenantModelAllowed(model.PlatformTenantID, "private-text")
	require.NoError(t, err)
	assert.True(t, allowed)
}

func TestTenantFeatureGateDefaultsToDenyUnsupportedAPIs(t *testing.T) {
	for _, test := range []struct {
		method, route, path string
		allowed             bool
	}{
		{http.MethodPost, "/v1/chat/completions", "/v1/chat/completions", true},
		{http.MethodPost, "/v1/responses", "/v1/responses", true},
		{http.MethodGet, "/v1/responses", "/v1/responses", false},
		{http.MethodPost, "/v1/images/generations", "/v1/images/generations", false},
		{http.MethodPost, "/pg/chat/completions", "/pg/chat/completions", false},
		{http.MethodPost, "/suno/submit/:action", "/suno/submit/music", false},
		{http.MethodGet, "/:mode/mj/task/:id/fetch", "/proxy/mj/task/1/fetch", false},
		{http.MethodGet, "/api/task/:task_id/artifacts", "/api/task/1/artifacts", false},
		{http.MethodPost, "/api/user/topup", "/api/user/topup", false},
		{http.MethodPost, "/api/user/checkin", "/api/user/checkin", false},
		{http.MethodPost, "/api/user/aff_transfer", "/api/user/aff_transfer", false},
		{http.MethodPost, "/api/user/stripe/pay", "/api/user/stripe/pay", false},
		{http.MethodPost, "/api/subscription/balance/pay", "/api/subscription/balance/pay", false},
		{http.MethodPost, "/api/user/passkey/login/finish", "/api/user/passkey/login/finish", false},
		{http.MethodGet, "/api/oauth/:provider", "/api/oauth/github", false},
		{http.MethodPost, "/api/user/pay", "/api/user/pay", true},
		{http.MethodGet, "/api/token/:id", "/api/token/10", true},
		{http.MethodPost, "/api/reseller/withdrawals", "/api/reseller/withdrawals", true},
		{http.MethodGet, "/api/admin/resellers/withdrawals", "/api/admin/resellers/withdrawals", false},
		{http.MethodPost, "/api/new-platform-feature", "/api/new-platform-feature", false},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			engine := gin.New()
			engine.Use(func(c *gin.Context) {
				common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
			}, TenantFeatureGate())
			engine.Handle(test.method, test.route, func(c *gin.Context) { c.Status(http.StatusNoContent) })
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest(test.method, "https://kimi.example.com"+test.path, nil))
			expected := http.StatusForbidden
			if test.allowed {
				expected = http.StatusNoContent
			}
			assert.Equal(t, expected, recorder.Code)
		})
	}
}

func TestTenantFeatureGatePreservesMainSiteAndRejectsWebSocketUpgrade(t *testing.T) {
	for _, tenantID := range []int64{0, 42} {
		engine := gin.New()
		engine.Use(func(c *gin.Context) { common.SetContextKey(c, constant.ContextKeyTenantID, tenantID) }, TenantFeatureGate())
		engine.POST("/v1/chat/completions", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		request := httptest.NewRequest(http.MethodPost, "https://kimi.example.com/v1/chat/completions", nil)
		request.Header.Set("Upgrade", "websocket")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		expected := http.StatusNoContent
		if tenantID > 0 {
			expected = http.StatusForbidden
		}
		assert.Equal(t, expected, response.Code)
	}
}

func TestTenantFeatureGateAllowsKnownSPAPagesAndRejectsUnknownFallbacks(t *testing.T) {
	for _, test := range []struct {
		path string
		want int
	}{
		{"/dashboard/overview", http.StatusNoContent},
		{"/reseller/earnings", http.StatusNoContent},
		{"/user-agreement", http.StatusNoContent},
		{"/privacy-policy", http.StatusNoContent},
		{"/assets/app.js", http.StatusNoContent},
		{"/api/new-feature", http.StatusForbidden},
		{"/image-studio", http.StatusForbidden},
		{"/admin/users", http.StatusForbidden},
		{"/unknown-plugin-route", http.StatusForbidden},
	} {
		engine := gin.New()
		engine.Use(func(c *gin.Context) { common.SetContextKey(c, constant.ContextKeyTenantID, int64(42)) }, TenantFeatureGate())
		engine.NoRoute(func(c *gin.Context) { c.Status(http.StatusNoContent) })
		request := httptest.NewRequest(http.MethodGet, "https://kimi.example.com"+test.path, nil)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		assert.Equal(t, test.want, response.Code, test.path)
	}
}

func TestTenantOriginRejectsOtherTenantsAndInsecureOrigins(t *testing.T) {
	for _, test := range []struct {
		origin string
		ok     bool
	}{
		{"", true}, {"https://kimi.example.com", true}, {"https://KIMI.example.com:443", true},
		{"https://other.example.com", false}, {"http://kimi.example.com", false},
		{"https://kimi.example.com:8443", false}, {"null", false}, {"https://kimi.example.com.evil.test", false},
	} {
		request := httptest.NewRequest(http.MethodPost, "https://kimi.example.com/api/user/pay", nil)
		if test.origin != "" {
			request.Header.Set("Origin", test.origin)
		}
		_, ok := tenantBrowserOrigin(request)
		assert.Equal(t, test.ok, ok, test.origin)
	}
	request := httptest.NewRequest(http.MethodPost, "https://kimi.example.com/api/user/pay", nil)
	request.Header.Add("Origin", "https://kimi.example.com")
	request.Header.Add("Origin", "https://other.example.com")
	_, ok := tenantBrowserOrigin(request)
	assert.False(t, ok)
}
