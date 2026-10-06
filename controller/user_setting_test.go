package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUpdateUserSettingPreservesOmittedFieldsAndValidatesNotificationTarget(t *testing.T) {
	previousDB, previousRedis := model.DB, common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB, common.RedisEnabled = db, false
	t.Cleanup(func() {
		model.DB, common.RedisEnabled = previousDB, previousRedis
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)

	initial := dto.UserSetting{
		NotifyType: dto.NotifyTypeGotify, QuotaWarningThreshold: 50,
		WebhookUrl: "https://hooks.example/notify", WebhookSecret: "webhook-secret",
		NotificationEmail: "notify@example.com", BarkUrl: "https://bark.example/device-key",
		GotifyUrl: "https://gotify.example", GotifyToken: "gotify-token", GotifyPriority: 7,
		UpstreamModelUpdateNotifyEnabled: true, AcceptUnsetRatioModel: true, RecordIpLog: true,
		SidebarModules: `{"chat":false}`, BillingPreference: "wallet_only", Language: "zh",
	}
	privacy := initial
	privacy.RecordIpLog = false
	pricing := initial
	pricing.AcceptUnsetRatioModel = false
	token := initial
	token.GotifyToken = "replacement-token"
	priority := initial
	priority.GotifyPriority = 0
	email := initial
	email.NotifyType = dto.NotifyTypeEmail
	email.NotificationEmail = ""
	adminNotify := initial
	adminNotify.UpstreamModelUpdateNotifyEnabled = false
	unconfigured := dto.UserSetting{}
	unconfiguredPrivacy := dto.UserSetting{RecordIpLog: true}

	tests := []struct {
		name    string
		before  dto.UserSetting
		body    string
		want    dto.UserSetting
		admin   bool
		success bool
	}{
		{"privacy only", initial, `{"record_ip_log":false}`, privacy, false, true},
		{"privacy without notifications", unconfigured, `{"record_ip_log":true}`, unconfiguredPrivacy, false, true},
		{"pricing only", initial, `{"accept_unset_model_ratio_model":false}`, pricing, false, true},
		{"same notify type preserves target and priority", initial, `{"notify_type":"gotify"}`, initial, false, true},
		{"token only", initial, `{"gotify_token":"replacement-token"}`, token, false, true},
		{"explicit zero priority", initial, `{"gotify_priority":0}`, priority, false, true},
		{"email reset uses account address", initial, `{"notify_type":"email","notification_email":""}`, email, false, true},
		{"admin preference", initial, `{"upstream_model_update_notify_enabled":false}`, adminNotify, true, true},
		{"common user cannot change admin preference", initial, `{"upstream_model_update_notify_enabled":false}`, initial, false, true},
		{"empty active token rejected", initial, `{"gotify_token":""}`, initial, false, false},
		{"non http webhook rejected", initial, `{"notify_type":"webhook","webhook_url":"ftp://hooks.example/notify"}`, initial, false, false},
		{"relative webhook rejected", initial, `{"notify_type":"webhook","webhook_url":"/notify"}`, initial, false, false},
		{"invalid email rejected", initial, `{"notify_type":"email","notification_email":"invalid@"}`, initial, false, false},
		{"target only validation", initial, `{"gotify_url":"https:///notify"}`, initial, false, false},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			user := model.User{Username: test.name, AffCode: strconv.Itoa(index), Role: common.RoleCommonUser}
			if test.admin {
				user.Role = common.RoleAdminUser
			}
			user.SetSetting(test.before)
			require.NoError(t, db.Create(&user).Error)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Set("id", user.Id)
			c.Request = httptest.NewRequest(http.MethodPut, "/api/user/setting", strings.NewReader(test.body))
			c.Request.Header.Set("Content-Type", "application/json")

			UpdateUserSetting(c)

			var response struct {
				Success bool `json:"success"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Equal(t, test.success, response.Success, recorder.Body.String())
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, test.want, user.GetSetting())
		})
	}
}
