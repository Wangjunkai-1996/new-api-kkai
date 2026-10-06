package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDistributorSessionModes(t *testing.T) {
	for _, mode := range []string{"off", "prefer", "strict"} {
		for _, unavailable := range []bool{false, true} {
			name := mode + "/available"
			if unavailable {
				name = mode + "/unavailable"
			}
			t.Run(name, func(t *testing.T) {
				channels := setupImageStudioDistributorTest(t)
				affinityKey := t.Name()
				configureImageStudioAffinityTest(t, affinityKey, channels.openAI.Id)
				settings := operation_setting.GetChannelAffinitySetting()
				settings.SessionMode = mode
				settings.Rules[0].SessionMode = "inherit"
				if unavailable {
					require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", channels.openAI.Id).Update("status", common.ChannelStatusManuallyDisabled).Error)
					require.NoError(t, model.DB.Model(&model.Ability{}).Where("channel_id = ?", channels.openAI.Id).Update("enabled", false).Error)
					require.NoError(t, model.SyncChannelCacheOnce())
				}
				selected := 0
				response := runImageStudioDistributorTestRequest(t, func(c *gin.Context) {
					service.SetImageStudioReferenceCount(c, 1)
					c.Request.Header.Set("X-Image-Affinity", affinityKey)
				}, func(c *gin.Context) {
					selected = common.GetContextKeyInt(c, constant.ContextKeyChannelId)
					c.Status(http.StatusNoContent)
				})
				if mode == "strict" && unavailable {
					assert.Equal(t, http.StatusServiceUnavailable, response.Code)
					assert.Zero(t, selected, "strict sessions must never fall through to another channel")
					assert.Contains(t, response.Body.String(), "strict_session_binding_unavailable")
					return
				}
				require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
				if mode == "off" || unavailable {
					assert.Equal(t, channels.replicate.Id, selected)
				} else {
					assert.Equal(t, channels.openAI.Id, selected)
				}
			})
		}
	}
}

func TestRequestPolicyAccessTokenScopes(t *testing.T) {
	require.NoError(t, i18n.Init())
	for _, test := range []struct {
		method string
		scopes string
		status int
	}{
		{http.MethodGet, `["option:read"]`, http.StatusNoContent},
		{http.MethodPatch, `["option:read"]`, http.StatusForbidden},
		{http.MethodPatch, `["option:write"]`, http.StatusNoContent},
		{http.MethodGet, `["profile:read"]`, http.StatusForbidden},
	} {
		t.Run(test.method+test.scopes, func(t *testing.T) {
			engine := gin.New()
			engine.Handle(test.method, "/api/option/request_policy", func(c *gin.Context) {
				lookup := &accessTokenLookup{token: &model.UserAccessToken{Scopes: test.scopes}}
				if enforceAccessTokenRoute(c, lookup, true) {
					c.Status(http.StatusNoContent)
				}
			})
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(test.method, "/api/option/request_policy", nil))
			assert.Equal(t, test.status, response.Code)
		})
	}
}
