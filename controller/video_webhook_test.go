package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVideoWebhookDashboardSecretAndOwnership(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	oldDB, oldLogDB, oldOrigin := model.DB, model.LOG_DB, system_setting.ServerAddress
	model.DB, model.LOG_DB, system_setting.ServerAddress = db, db, "https://api.example.test"
	t.Cleanup(func() { model.DB, model.LOG_DB, system_setting.ServerAddress = oldDB, oldLogDB, oldOrigin })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.AuditLog{}))
	require.NoError(t, kkaimigrate.ApplyVideoWebhookExpansion(t.Context(), db, kkaimigrate.Options{Offline: true}))
	t.Setenv("KKAI_VIDEO_WEBHOOK_ENABLED", "true")
	t.Setenv("KKAI_VIDEO_WEBHOOK_TOKENS", "*")
	t.Setenv("KKAI_VIDEO_WEBHOOK_KEYS", `{"test":"YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE="}`)
	t.Setenv("KKAI_VIDEO_WEBHOOK_KEYS_FILE", "")
	t.Setenv("KKAI_VIDEO_WEBHOOK_KEY_ID", "test")
	owner := model.User{Username: "hookowner", AffCode: "hookowner", Role: common.RoleCommonUser}
	require.NoError(t, db.Create(&owner).Error)
	token := model.Token{UserId: owner.Id, Key: "hookfixture"}
	require.NoError(t, db.Create(&token).Error)
	engine := gin.New()
	userID := owner.Id
	session := true
	hooks := engine.Group("/api/token/:id/video-webhook", middleware.DisableCache(), func(c *gin.Context) {
		c.Set("id", userID)
		c.Set("role", common.RoleCommonUser)
		if session {
			c.Set("session_id", "live-session")
			c.Set("auth_version", int64(1))
			c.Set("session_version", int64(1))
		}
	})
	hooks.PUT("", ManageVideoWebhook)
	hooks.GET("", ManageVideoWebhook)
	hooks.DELETE("/:version", ManageVideoWebhook)
	hooks.GET("/deliveries", VideoWebhookDeliveries)
	send := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, fmt.Sprintf("/api/token/%d/video-webhook%s", token.Id, path), bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(w, req)
		return w
	}
	put := send(http.MethodPut, "", `{"url":"https://8.8.8.8/video"}`)
	require.Equal(t, 200, put.Code, put.Body.String())
	assert.Contains(t, put.Header().Get("Cache-Control"), "no-store")
	var issued struct {
		Data struct {
			Secret   string
			Endpoint model.VideoWebhookEndpoint
		}
	}
	require.NoError(t, common.Unmarshal(put.Body.Bytes(), &issued))
	require.NotEmpty(t, issued.Data.Secret)
	assert.Equal(t, "pending", issued.Data.Endpoint.State)
	get := send(http.MethodGet, "", "")
	assert.Contains(t, get.Body.String(), issued.Data.Endpoint.ID)
	assert.NotContains(t, get.Body.String(), issued.Data.Secret)
	assert.NotContains(t, get.Body.String(), "key_version")
	userID++
	assert.Equal(t, 404, send(http.MethodGet, "/deliveries", "").Code)
	assert.Equal(t, 404, send(http.MethodDelete, "/"+issued.Data.Endpoint.ID, "").Code)
	userID = owner.Id
	session = false
	assert.Equal(t, 403, send(http.MethodGet, "", "").Code)
	session = true
	require.Equal(t, 200, send(http.MethodDelete, "/"+issued.Data.Endpoint.ID, "").Code)
	assert.NotContains(t, send(http.MethodGet, "", "").Body.String(), issued.Data.Endpoint.ID)
}
