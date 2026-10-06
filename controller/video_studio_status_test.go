package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetStatusExposesVideoStudioUploadLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)

	GetStatus(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data struct {
			VideoStudio struct {
				ProcessingAvailable bool                              `json:"processing_available"`
				UploadLimits        video_studio_setting.UploadLimits `json:"upload_limits"`
			} `json:"video_studio"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, video_studio_setting.Get().WorkerEnabled, response.Data.VideoStudio.ProcessingAvailable)
	require.Equal(t, video_studio_setting.Get().UploadLimits(), response.Data.VideoStudio.UploadLimits)
}

func TestGetStatusExposesFrontendMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("FRONTEND_MODE", "external")
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)

	GetStatus(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data struct {
			FrontendMode string `json:"frontend_mode"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "external", response.Data.FrontendMode)
}

func TestGetStatusExposesPasskeyAndTelegramConfiguration(t *testing.T) {
	passkey := system_setting.GetPasskeySettings()
	previousPasskey := *passkey
	telegram := system_setting.GetTelegramSettings()
	previousTelegram := *telegram
	previousTelegramEnabled, previousServerAddress := common.TelegramOAuthEnabled, system_setting.ServerAddress
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		*passkey, *telegram = previousPasskey, previousTelegram
		common.TelegramOAuthEnabled, system_setting.ServerAddress = previousTelegramEnabled, previousServerAddress
	})
	cases := []struct {
		name              string
		rpID              string
		legacyIDs         string
		wantRPID          string
		wantRPIDs         []string
		telegramEnabled   bool
		telegramSecret    string
		wantTelegramReady bool
	}{
		{"configured with legacy domains", "login.example.com:8443", "old.example.com,login.example.com,old.example.com", "login.example.com", []string{"login.example.com", "old.example.com"}, true, "test-oauth-secret", true},
		{"derived RP and incomplete Telegram", "", "", "api.example.com", []string{"api.example.com"}, true, "", false},
		{"disabled Telegram", "login.example.com", "", "login.example.com", []string{"login.example.com"}, false, "test-oauth-secret", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			common.OptionMapRWMutex.Lock()
			*passkey = system_setting.PasskeySettings{Enabled: true, RPID: tc.rpID, LegacyRPIDs: tc.legacyIDs}
			*telegram = system_setting.TelegramSettings{ClientID: "test-oauth-client", ClientSecret: tc.telegramSecret}
			common.TelegramOAuthEnabled = tc.telegramEnabled
			system_setting.ServerAddress = "https://api.example.com:8443"
			common.OptionMapRWMutex.Unlock()
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)
			GetStatus(ctx)
			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Success bool `json:"success"`
				Data    struct {
					PasskeyEnabled     bool     `json:"passkey_login"`
					RPID               string   `json:"passkey_rp_id"`
					RPIDs              []string `json:"passkey_rp_ids"`
					TelegramEnabled    bool     `json:"telegram_oauth"`
					TelegramConfigured *bool    `json:"telegram_oauth_configured"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.True(t, response.Success)
			assert.True(t, response.Data.PasskeyEnabled)
			assert.Equal(t, tc.wantRPID, response.Data.RPID)
			assert.Equal(t, tc.wantRPIDs, response.Data.RPIDs)
			assert.Equal(t, tc.telegramEnabled, response.Data.TelegramEnabled)
			require.NotNil(t, response.Data.TelegramConfigured)
			assert.Equal(t, tc.wantTelegramReady, *response.Data.TelegramConfigured)
			assert.NotContains(t, recorder.Body.String(), "test-oauth-secret")
		})
	}
}

func TestSendEmailVerificationRejectsAccountEmailPolicyViolations(t *testing.T) {
	previousDomainRestriction, previousAliasRestriction := common.EmailDomainRestrictionEnabled, common.EmailAliasRestrictionEnabled
	previousWhitelist := common.EmailDomainWhitelist
	common.EmailDomainRestrictionEnabled, common.EmailAliasRestrictionEnabled = true, true
	common.EmailDomainWhitelist = []string{"example.com"}
	t.Cleanup(func() {
		common.EmailDomainRestrictionEnabled, common.EmailAliasRestrictionEnabled = previousDomainRestriction, previousAliasRestriction
		common.EmailDomainWhitelist = previousWhitelist
	})
	for _, tc := range []struct {
		name, email string
		wantMessage string
	}{
		{"invalid address", "not-an-email", service.ErrAccountEmailInvalid.Error()},
		{"address length", strings.Repeat("a", 39) + "@example.com", service.ErrAccountEmailInvalid.Error()},
		{"domain policy", "person@unapproved.example", service.ErrAccountEmailRestricted.Error()},
		{"alias policy", "person+alias@example.com", service.ErrAccountEmailRestricted.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/verification?email="+url.QueryEscape(tc.email), nil)
			SendEmailVerification(ctx)
			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Success bool   `json:"success"`
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success)
			assert.Equal(t, "EMAIL_ADDRESS_REJECTED", response.Code)
			assert.Equal(t, tc.wantMessage, response.Message)
		})
	}
}
