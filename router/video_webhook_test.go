package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVideoWebhookPublicSubmissionRoutes(t *testing.T) {
	setupRelayRouterTestDB(t)
	require.NoError(t, i18n.Init())
	service.InitHttpClient()
	oldCache, oldOrigin, oldPrices := common.MemoryCacheEnabled, system_setting.ServerAddress, ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldCache
		system_setting.ServerAddress = oldOrigin
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
	})
	common.MemoryCacheEnabled = false
	system_setting.ServerAddress = "https://gateway.example.test"
	t.Setenv("KKAI_VIDEO_WEBHOOK_ENABLED", "true")
	t.Setenv("KKAI_VIDEO_WEBHOOK_TOKENS", "*")
	t.Setenv("KKAI_VIDEO_WEBHOOK_KEY_ID", "fixture")
	t.Setenv("KKAI_VIDEO_WEBHOOK_KEYS_FILE", "")
	t.Setenv("KKAI_VIDEO_WEBHOOK_KEYS", `{"fixture":"YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE="}`)
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}, &model.KKAIOutboxEvent{}, &model.Log{}, &model.KKAIVideoGeneration{}, &model.UserSubscription{}))
	require.NoError(t, kkaimigrate.ApplyVideoWebhookExpansion(t.Context(), db, kkaimigrate.Options{Offline: true}))
	user := model.User{Username: "webhook-route-owner", AffCode: "hookroute", Status: common.UserStatusEnabled, Group: "default", Quota: 1_000_000}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "webhookroutekey", UnlimitedQuota: true, Status: common.TokenStatusEnabled}
	require.NoError(t, db.Create(&token).Error)
	otherUser := model.User{Username: "other-route-owner", AffCode: "otherhookroute", Status: common.UserStatusEnabled, Group: "default", Quota: 1_000_000}
	require.NoError(t, db.Create(&otherUser).Error)
	otherToken := model.Token{UserId: otherUser.Id, Key: "otherwebhookroutekey", UnlimitedQuota: true, Status: common.TokenStatusEnabled}
	require.NoError(t, db.Create(&otherToken).Error)
	owner := model.VideoWebhookOwner{UserID: user.Id, TokenID: token.Id}

	type providerRequest struct {
		body               string
		webhook, ref, auth string
	}
	providerRequests := make(chan providerRequest, 10)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/videos/provider-private-id/content" {
			if r.Header.Get("Authorization") != "Bearer adapter-fixture" {
				w.WriteHeader(401)
				return
			}
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("video-fixture-bytes"))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		providerRequests <- providerRequest{string(raw), r.Header.Get(service.VideoWebhookHeader), r.Header.Get(service.VideoWebhookReferenceHeader), r.Header.Get("Authorization")}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"provider-private-id","status":"queued"}`))
	}))
	t.Cleanup(upstream.Close)
	// Exercise the registered built-in Sora adapter, including its model projection.
	modelNames := []string{"seedance-2.5", "sd_2.5_special_720p", "sd_2.5_special_1080p", "sd_2.5_special_720p_with_video_ref", "sd_2.5_special_1080p_with_video_ref"}
	prices := map[string]float64{}
	for _, name := range modelNames {
		prices[name] = 0.01
	}
	encodedPrices, err := common.Marshal(prices)
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(encodedPrices)))
	priority := int64(1)
	channel := model.Channel{Type: constant.ChannelTypeSora, Key: "adapter-fixture", Status: common.ChannelStatusEnabled, Name: "webhook test", Models: strings.Join(modelNames, ","), Group: "default", BaseURL: &upstream.URL, Priority: &priority}
	require.NoError(t, db.Create(&channel).Error)
	for _, name := range modelNames {
		require.NoError(t, db.Create(&model.Ability{Group: "default", Model: name, ChannelId: channel.Id, Enabled: true, Priority: &priority, Weight: 1}).Error)
	}

	engine := gin.New()
	engine.Use(middleware.BodyStorageCleanup())
	SetTaskPluginProtocolRouter(engine)
	SetVideoRouter(engine)
	SetApiRouter(engine)
	submit := func(path, name, mode, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"`+name+`","prompt":"harbor","duration":5,"ratio":"16:9","generate_audio":false}`))
		r.Header.Set("Authorization", "Bearer "+key)
		r.Header.Set("Content-Type", "application/json")
		if mode != "" {
			r.Header.Set(service.VideoWebhookHeader, mode)
			r.Header.Set(service.VideoWebhookReferenceHeader, "order-42")
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/v1/videos", "/v1/video/generations"} {
		resp := submit(path, "seedance-2.5", "terminal", token.Key)
		assert.Equal(t, 409, resp.Code, resp.Body.String())
		assert.Contains(t, resp.Body.String(), "webhook_endpoint_not_ready")
		assert.Empty(t, resp.Header().Get(service.VideoWebhookHeader))
	}
	require.Empty(t, providerRequests)
	ep := model.VideoWebhookEndpoint{ID: "wh_router", VideoWebhookOwner: owner, URL: "https://customer.example.test/video", State: "active", KeyVersion: "fixture"}
	require.NoError(t, db.Create(&ep).Error)
	for i, name := range modelNames {
		path := []string{"/v1/videos", "/v1/video/generations"}[i%2]
		resp := submit(path, name, "terminal", token.Key)
		require.Equal(t, 200, resp.Code, resp.Body.String())
		assert.Equal(t, "registered", resp.Header().Get(service.VideoWebhookHeader))
		var created struct {
			ID string `json:"id"`
		}
		require.NoError(t, common.Unmarshal(resp.Body.Bytes(), &created))
		require.NotEmpty(t, created.ID)
		assert.NotContains(t, resp.Body.String(), "provider-private-id")
		select {
		case r := <-providerRequests:
			assert.Empty(t, r.webhook)
			assert.Empty(t, r.ref)
			assert.NotContains(t, r.body, "order-42")
			assert.Equal(t, "Bearer adapter-fixture", r.auth)
		default:
			t.Fatal("the request did not reach the adapter")
		}
		var subscription model.VideoWebhookTask
		require.NoError(t, db.Where("task_id = ?", created.ID).First(&subscription).Error)
		assert.Equal(t, ep.ID, subscription.EndpointID)
		assert.Equal(t, "order-42", subscription.Reference)
		assert.Equal(t, name, subscription.Model)
		var stored model.Task
		require.NoError(t, db.First(&stored, subscription.TaskRowID).Error)
		assert.Equal(t, token.Id, stored.PrivateData.TokenId)
		assert.Equal(t, "provider-private-id", stored.PrivateData.UpstreamTaskID)
		if i == 0 {
			require.NoError(t, db.Model(&stored).Update("status", model.TaskStatusSuccess).Error)
			download := func(key string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodGet, "/v1/videos/"+created.ID+"/content", nil)
				r.Header.Set("Authorization", "Bearer "+key)
				w := httptest.NewRecorder()
				engine.ServeHTTP(w, r)
				return w
			}
			result := download(otherToken.Key)
			assert.Equal(t, 404, result.Code, result.Body.String())
			result = download(token.Key)
			require.Equal(t, 200, result.Code, result.Body.String())
			assert.Equal(t, "video-fixture-bytes", result.Body.String())
		}
	}
	resp := submit("/v1/videos", modelNames[0], "terminal", otherToken.Key)
	assert.Equal(t, 409, resp.Code, resp.Body.String())
	assert.Empty(t, providerRequests)
	resp = submit("/v1/videos", modelNames[0], "", token.Key)
	require.Equal(t, 200, resp.Code, resp.Body.String())
	assert.Empty(t, resp.Header().Get(service.VideoWebhookHeader))
	var count int64
	require.NoError(t, db.Model(&model.VideoWebhookTask{}).Count(&count).Error)
	assert.EqualValues(t, len(modelNames), count)
}
