package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOrdinaryImageRelayOverdeliveryBilling(t *testing.T) {
	// The client requests one image in every case. Both the forwarded result
	// and the wallet must retain the contract of the selected pricing method.
	for _, tc := range []struct {
		name, expr      string
		firstN          int
		retry           bool
		images, charged int
	}{
		{"fixed price", "", 1, false, 2, 1000},
		{"configured quantity and short result", "", 3, false, 2, 1000},
		{"image count expression", `tier("image", fixed(0.001)) * image_count`, 1, false, 2, 1000},
		{"reported tokens", `tier("tokens", p * 2 + c * 4)`, 1, false, 2, 11},
		{"failed override then backup", "", 3, true, 2, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service.InitHttpClient()
			db := setupImageStudioIntegrationState(t)
			oldRetries, oldLogs := common.RetryTimes, common.LogConsumeEnabled
			common.RetryTimes, common.LogConsumeEnabled = 1, true
			t.Cleanup(func() { common.RetryTimes, common.LogConsumeEnabled = oldRetries, oldLogs })
			if tc.expr != "" {
				pricing := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
				previous := *pricing
				t.Cleanup(func() { *pricing = previous })
				pricing.BillingMode = map[string]string{"gpt-image-1": "tiered_expr"}
				pricing.BillingExpr = map[string]string{"gpt-image-1": tc.expr}
			}
			var firstCalls, backupCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					N int `json:"n"`
				}
				if !assert.NoError(t, common.DecodeJson(r.Body, &request)) {
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if r.Header.Get("Authorization") == "Bearer provider-key" {
					firstCalls.Add(1)
					assert.Equal(t, tc.firstN, request.N)
					if tc.retry {
						w.WriteHeader(http.StatusServiceUnavailable)
						fmt.Fprint(w, `{"error":{"code":"account_pool_exhausted","type":"api_error","message":"capacity unavailable"}}`)
						return
					}
				} else {
					assert.Equal(t, "Bearer backup-key", r.Header.Get("Authorization"))
					backupCalls.Add(1)
					assert.Equal(t, 1, request.N)
				}
				fmt.Fprint(w, `{"created":1,"data":[{"url":"https://cdn.example/a"},{"url":"https://cdn.example/b"}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}`)
			}))
			t.Cleanup(upstream.Close)
			var first model.Channel
			require.NoError(t, db.First(&first).Error)
			first.BaseURL, first.Priority = &upstream.URL, common.GetPointer(int64(30))
			if tc.firstN != 1 {
				first.ParamOverride = common.GetPointer(fmt.Sprintf(`{"n":%d}`, tc.firstN))
			}
			require.NoError(t, db.Save(&first).Error)
			require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", first.Id).Update("priority", 30).Error)
			expectedChannel := first.Id
			if tc.retry {
				backup := first
				backup.Id, backup.Key, backup.ParamOverride, backup.Priority = 0, "backup-key", nil, common.GetPointer(int64(10))
				require.NoError(t, db.Create(&backup).Error)
				require.NoError(t, db.Create(&model.Ability{Group: backup.Group, Model: backup.Models, ChannelId: backup.Id, Enabled: true, Priority: backup.Priority, Weight: 100}).Error)
				expectedChannel = backup.Id
			}
			require.NoError(t, model.SyncChannelCacheOnce())
			var token model.Token
			require.NoError(t, db.First(&token).Error)
			user, err := model.GetUserCache(token.UserId)
			require.NoError(t, err)
			var initial model.User
			require.NoError(t, db.First(&initial, token.UserId).Error)
			engine := gin.New()
			engine.POST("/v1/images/generations", func(c *gin.Context) {
				user.WriteContext(c)
				common.SetContextKey(c, constant.ContextKeyUsingGroup, token.Group)
				require.NoError(t, middleware.SetupContextForToken(c, &token))
			}, middleware.Distribute(), func(c *gin.Context) { Relay(c, types.RelayFormatOpenAIImage) })
			request := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"gpt-image-1","prompt":"a square","n":1}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			assert.Len(t, gjson.Get(response.Body.String(), "data").Array(), tc.images)
			assert.EqualValues(t, 1, firstCalls.Load())
			if tc.retry {
				assert.EqualValues(t, 1, backupCalls.Load())
			} else {
				assert.Zero(t, backupCalls.Load())
			}
			var stored model.User
			require.NoError(t, db.First(&stored, token.UserId).Error)
			assert.EqualValues(t, tc.charged, initial.Quota-stored.Quota)
			var entries []model.Log
			require.NoError(t, db.Where("type = ? AND user_id = ?", model.LogTypeConsume, token.UserId).Find(&entries).Error)
			require.Len(t, entries, 1)
			assert.Equal(t, tc.charged, entries[0].Quota)
			assert.Equal(t, expectedChannel, entries[0].ChannelId)
		})
	}
}
