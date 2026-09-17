package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoGroupRelayBillsFinalCandidate(t *testing.T) {
	service.InitHttpClient()
	for _, tc := range []struct {
		name         string
		failPrimary  bool
		finalGroup   string
		finalChannel int
		quota        int
		usedChannels []string
	}{
		{name: "primary succeeds", finalGroup: "vip", finalChannel: 2, quota: 200, usedChannels: []string{"2"}},
		{name: "primary fails over", failPrimary: true, finalGroup: "default", finalChannel: 1, quota: 100, usedChannels: []string{"2", "1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := setupAutoGroupAccessRouter(t)
			db := model.DB
			require.NoError(t, db.AutoMigrate(&model.Log{}))
			originalPrice := ratio_setting.ModelPrice2JSONString()
			originalQuotaPerUnit := common.QuotaPerUnit
			originalRetries, originalCountToken := common.RetryTimes, constant.CountToken
			originalBatch, originalExport := common.BatchUpdateEnabled, common.DataExportEnabled
			originalConsumeLog, originalErrorLog := common.LogConsumeEnabled, constant.ErrorLogEnabled
			t.Cleanup(func() {
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalPrice))
				common.QuotaPerUnit = originalQuotaPerUnit
				common.RetryTimes, constant.CountToken = originalRetries, originalCountToken
				common.BatchUpdateEnabled, common.DataExportEnabled = originalBatch, originalExport
				common.LogConsumeEnabled, constant.ErrorLogEnabled = originalConsumeLog, originalErrorLog
			})
			common.QuotaPerUnit = 1000
			common.RetryTimes, constant.CountToken = 2, false
			common.BatchUpdateEnabled, common.DataExportEnabled = false, false
			common.LogConsumeEnabled, constant.ErrorLogEnabled = true, false
			require.NoError(t, setting.UpdateAutoGroupProfilesByJsonString(`{"auto2":["vip","default"]}`))
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"zz-shared-model":0.1}`))
			require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Updates(map[string]any{
				"quota": 10000, "setting": `{"billing_preference":"wallet_only"}`,
			}).Error)
			require.NoError(t, db.Model(&model.Token{}).Where("id = ?", 2).Updates(map[string]any{
				"unlimited_quota": false, "remain_quota": 10000, "cross_group_retry": true,
			}).Error)

			var primaryCalls, backupCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/responses", r.URL.Path)
				assert.Equal(t, http.MethodPost, r.Method)
				var request dto.OpenAIResponsesRequest
				assert.NoError(t, common.DecodeJson(r.Body, &request))
				assert.Equal(t, "zz-shared-model", request.Model)
				primary := r.Header.Get("Authorization") == "Bearer vip-key"
				if primary {
					primaryCalls.Add(1)
					var reservedUser model.User
					var reservedToken model.Token
					assert.NoError(t, db.First(&reservedUser, 1).Error)
					assert.NoError(t, db.First(&reservedToken, 2).Error)
					assert.EqualValues(t, 9800, reservedUser.Quota, "preconsume uses the selected vip group's ratio")
					assert.Equal(t, 9800, reservedToken.RemainQuota)
				} else {
					assert.Equal(t, "Bearer default-key", r.Header.Get("Authorization"))
					backupCalls.Add(1)
				}
				w.Header().Set("Content-Type", "application/json")
				if primary && tc.failPrimary {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte(`{"error":{"code":"account_pool_exhausted","type":"api_error","message":"primary pool unavailable"}}`))
					return
				}
				_, _ = w.Write([]byte(`{"id":"resp-auto2","object":"response","status":"completed","model":"zz-shared-model","output":[],"usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14}}`))
			}))
			t.Cleanup(upstream.Close)
			for id, key := range map[int]string{1: "default-key", 2: "vip-key"} {
				require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", id).Updates(map[string]any{
					"base_url": upstream.URL, "key": key, "auto_ban": 0,
				}).Error)
			}

			var finalTokenGroup, finalAutoGroup string
			router.POST("/v1/responses", middleware.TokenAuth(), middleware.Distribute(), func(c *gin.Context) {
				c.Set(common.RequestIdKey, "auto2-relay-billing")
				Relay(c, types.RelayFormatOpenAIResponses)
				finalTokenGroup = common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
				finalAutoGroup = common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
			})
			request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"zz-shared-model","input":"hello","max_output_tokens":8}`))
			request.Header.Set("Authorization", "Bearer sk-auto2")
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			assert.Contains(t, response.Body.String(), `"id":"resp-auto2"`)
			assert.EqualValues(t, 1, primaryCalls.Load())
			if tc.failPrimary {
				assert.EqualValues(t, 1, backupCalls.Load())
			} else {
				assert.Zero(t, backupCalls.Load())
			}
			assert.Equal(t, "auto2", finalTokenGroup)
			assert.Equal(t, tc.finalGroup, finalAutoGroup)
			var user model.User
			var token model.Token
			require.NoError(t, db.First(&user, 1).Error)
			require.NoError(t, db.First(&token, 2).Error)
			assert.EqualValues(t, 10000-tc.quota, user.Quota)
			assert.EqualValues(t, tc.quota, user.UsedQuota)
			assert.Equal(t, 1, user.RequestCount)
			assert.Equal(t, 10000-tc.quota, token.RemainQuota)
			assert.Equal(t, tc.quota, token.UsedQuota)
			assert.Equal(t, "auto2", token.Group)
			var logs []model.Log
			require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
			require.Len(t, logs, 1)
			assert.Equal(t, tc.finalGroup, logs[0].Group)
			assert.Equal(t, tc.finalChannel, logs[0].ChannelId)
			assert.Equal(t, tc.quota, logs[0].Quota)
			assert.Equal(t, 10, logs[0].PromptTokens)
			assert.Equal(t, 4, logs[0].CompletionTokens)
			var other struct {
				GroupRatio float64 `json:"group_ratio"`
				AdminInfo  struct {
					UsedChannels []string `json:"use_channel"`
				} `json:"admin_info"`
			}
			require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
			assert.Equal(t, float64(tc.quota)/100, other.GroupRatio)
			assert.Equal(t, tc.usedChannels, other.AdminInfo.UsedChannels)
		})
	}
}
