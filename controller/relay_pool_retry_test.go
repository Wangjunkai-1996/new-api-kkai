package controller

import (
	"context"
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
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayExhaustedPoolIsNotReplayed(t *testing.T) {
	for _, tt := range []struct {
		name, code, errType                      string
		status, retries, firstCalls, finalStatus int
		backup                                   bool
		intermediateFailure, disableMemoryCache  bool
	}{
		{"single exhausted pool", "account_pool_exhausted", "api_error", 503, 2, 1, 503, false, false, false},
		{"single exhausted egress", "egress_capacity_exhausted", "rate_limit_error", 429, 2, 1, 429, false, false, false},
		{"single cooling pool", "account_pool_rate_limited", "rate_limit_error", 429, 2, 1, 429, false, false, false},
		{"healthy backup", "account_pool_exhausted", "api_error", 503, 2, 1, 200, true, false, false},
		{"retries disabled", "account_pool_exhausted", "api_error", 503, 0, 1, 503, true, false, false},
		{"ordinary key limit", "rate_limit_exceeded", "rate_limit_error", 429, 2, 3, 429, false, false, false},
		{"pool then ordinary failure cached", "account_pool_exhausted", "api_error", 503, 2, 1, 200, true, true, false},
		{"pool then ordinary failure database", "account_pool_exhausted", "api_error", 503, 2, 1, 200, true, true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			service.InitHttpClient()
			db := setupImageStudioIntegrationState(t)
			common.MemoryCacheEnabled = !tt.disableMemoryCache
			constant.ErrorLogEnabled = false
			previousRetries := common.RetryTimes
			common.RetryTimes = tt.retries
			t.Cleanup(func() { common.RetryTimes = previousRetries })
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"gpt-image-1":0}`))
			var token model.Token
			require.NoError(t, db.First(&token).Error)
			user, err := model.GetUserCache(token.UserId)
			require.NoError(t, err)
			var firstCalls, backupCalls, intermediateCalls atomic.Int32
			firstUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				firstCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "90")
				w.WriteHeader(tt.status)
				_, _ = fmt.Fprintf(w, `{"error":{"code":%q,"type":%q,"message":"capacity unavailable"}}`, tt.code, tt.errType)
			}))
			t.Cleanup(firstUpstream.Close)
			var first model.Channel
			require.NoError(t, db.First(&first).Error)
			require.NoError(t, db.Model(&first).Updates(map[string]any{"base_url": firstUpstream.URL, "priority": 30}).Error)
			require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", first.Id).Update("priority", 30).Error)
			if tt.intermediateFailure {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					intermediateCalls.Add(1)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":{"code":"internal_error","type":"api_error","message":"temporary failure"}}`))
				}))
				t.Cleanup(upstream.Close)
				priority := int64(20)
				intermediate := first
				intermediate.Id, intermediate.Key, intermediate.BaseURL, intermediate.Priority = 0, "intermediate-key", &upstream.URL, &priority
				require.NoError(t, db.Create(&intermediate).Error)
				require.NoError(t, db.Create(&model.Ability{Group: first.Group, Model: first.Models, ChannelId: intermediate.Id,
					Enabled: true, Priority: &priority, Weight: 100}).Error)
			}
			if tt.backup {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					backupCalls.Add(1)
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"created":1,"data":[{"url":"https://images.example/result.png"}]}`))
				}))
				t.Cleanup(upstream.Close)
				priority, weight, autoBan := int64(10), uint(100), 0
				backup := model.Channel{Type: constant.ChannelTypeOpenAI, Key: "backup-key", Status: common.ChannelStatusEnabled,
					Name: "backup", Models: first.Models, Group: first.Group, BaseURL: &upstream.URL, Priority: &priority, Weight: &weight, AutoBan: &autoBan}
				require.NoError(t, db.Create(&backup).Error)
				require.NoError(t, db.Create(&model.Ability{Group: first.Group, Model: first.Models, ChannelId: backup.Id,
					Enabled: true, Priority: &priority, Weight: weight}).Error)
			}
			require.NoError(t, model.SyncChannelCacheOnce())
			engine := gin.New()
			engine.POST("/v1/images/generations", func(c *gin.Context) {
				user.WriteContext(c)
				common.SetContextKey(c, constant.ContextKeyUsingGroup, token.Group)
				require.NoError(t, middleware.SetupContextForToken(c, &token))
			}, middleware.Distribute(), func(c *gin.Context) { Relay(c, types.RelayFormatOpenAIImage) })
			request := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"gpt-image-1","prompt":"a square","n":1}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer pooltesttoken")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			require.Equal(t, tt.finalStatus, response.Code, response.Body.String())
			assert.EqualValues(t, tt.firstCalls, firstCalls.Load())
			if tt.intermediateFailure {
				assert.EqualValues(t, 1, intermediateCalls.Load(), "ordinary errors still advance to a lower-priority backup")
			}
			if tt.finalStatus == http.StatusOK {
				assert.EqualValues(t, 1, backupCalls.Load())
				assert.Empty(t, response.Header().Get("Retry-After"), "a recovered request must not inherit failed-attempt headers")
			} else {
				assert.Zero(t, backupCalls.Load())
				if tt.code != "rate_limit_exceeded" {
					assert.Equal(t, "90", response.Header().Get("Retry-After"))
					assert.Contains(t, response.Body.String(), tt.code)
				}
			}
			require.NoError(t, db.First(&first, first.Id).Error)
			assert.Equal(t, common.ChannelStatusEnabled, first.Status, "request-local exhaustion must not disable a channel")
		})
	}
}

func TestShouldRetryStopsAfterOutputOrCancellation(t *testing.T) {
	apiErr := types.WithOpenAIError(types.OpenAIError{Code: "account_pool_exhausted", Type: "api_error"}, 503,
		types.ErrOptionWithOriginalStatusCode(503))
	for _, committed := range []bool{false, true} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		requestContext, cancel := context.WithCancel(context.Background())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestContext)
		if committed {
			ctx.Header("Content-Type", "text/event-stream")
			_, err := ctx.Writer.WriteString("data: output\n\n")
			require.NoError(t, err)
		} else {
			cancel()
		}
		assert.False(t, shouldRetry(ctx, nil, apiErr, 2))
		cancel()
	}
}

func TestRealtimeRetryAfterClientHandshake(t *testing.T) {
	for _, tt := range []struct {
		name         string
		format       types.RelayFormat
		upstreamOpen bool
		code         types.ErrorCode
		retries      int
		canceled     bool
		want         bool
	}{
		{"upstream dial failed", types.RelayFormatOpenAIRealtime, false, types.ErrorCodeDoRequestFailed, 2, false, true},
		{"response phase started", types.RelayFormatOpenAIRealtime, true, types.ErrorCodeDoRequestFailed, 2, false, false},
		{"non-dial failure", types.RelayFormatOpenAIRealtime, false, types.ErrorCodeBadResponseBody, 2, false, false},
		{"retry budget exhausted", types.RelayFormatOpenAIRealtime, false, types.ErrorCodeDoRequestFailed, 0, false, false},
		{"client canceled", types.RelayFormatOpenAIRealtime, false, types.ErrorCodeDoRequestFailed, 2, true, false},
		{"upgrade header on HTTP relay", types.RelayFormatOpenAI, false, types.ErrorCodeDoRequestFailed, 2, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := make(chan bool, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx, _ := gin.CreateTestContext(w)
				requestContext, cancel := context.WithCancel(r.Context())
				defer cancel()
				ctx.Request = r.WithContext(requestContext)
				conn, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
				if err != nil {
					t.Errorf("upgrade client websocket: %v", err)
					return
				}
				defer conn.Close()
				info := &relaycommon.RelayInfo{RelayFormat: tt.format, ClientWs: conn}
				if tt.upstreamOpen {
					info.TargetWs = &websocket.Conn{}
				}
				if tt.canceled {
					cancel()
				}
				apiErr := types.NewErrorWithStatusCode(fmt.Errorf("upstream unavailable"), tt.code, http.StatusBadGateway)
				result <- shouldRetry(ctx, info, apiErr, tt.retries)
			}))
			t.Cleanup(server.Close)
			client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })
			assert.Equal(t, tt.want, <-result)
		})
	}
}
