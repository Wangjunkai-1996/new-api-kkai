package relay

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	appdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTenantResponsesWSContext(t *testing.T) (*gin.Context, *responsesWSSession) {
	t.Helper()
	db := setupRelayChannelDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Tenant{}, &model.TenantPriceVersion{}, &model.Ability{}, &model.Model{}, &model.Vendor{}, &model.TenantConsumption{}))
	oldRedis, oldBatch, oldLogs, oldCount := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, constant.CountToken
	oldPrice := operation_setting.Price
	oldOpen, oldGroups, oldModels := setting.UserUsableGroups2JSONString(), ratio_setting.GroupRatio2JSONString(), ratio_setting.ModelRatio2JSONString()
	common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, constant.CountToken = false, false, false, false
	operation_setting.Price = 1
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"ws-tenant":"WS Tenant"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"ws-tenant":0}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"ws-tenant-model":1}`))
	t.Cleanup(func() {
		common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, constant.CountToken = oldRedis, oldBatch, oldLogs, oldCount
		operation_setting.Price = oldPrice
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldOpen))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldModels))
		model.InvalidatePricingCache()
	})
	require.NoError(t, db.Create(&model.Tenant{ID: 42, Status: model.TenantStatusActive}).Error)
	user := model.User{TenantID: 42, Username: "ws-tenant-user", Status: common.UserStatusEnabled, Group: "ws-tenant"}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{TenantID: 42, UserId: user.Id, Key: "ws-tenant-token", Status: common.TokenStatusEnabled, ExpiredTime: -1}
	require.NoError(t, db.Create(&token).Error)
	channel := model.Channel{Name: "ws-tenant-channel", Key: "upstream-key", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "ws-tenant-model", Group: "ws-tenant"}
	channel.SetSetting(dto.ChannelSettings{ResponsesWebSocketEnabled: true})
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: channel.Id, Model: "ws-tenant-model", Group: "ws-tenant", Enabled: true}).Error)
	model.InvalidatePricingCache()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(common.RequestIdKey, "ws-tenant-request")
	common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
	common.SetContextKey(c, constant.ContextKeyUserId, user.Id)
	common.SetContextKey(c, constant.ContextKeyTokenId, token.Id)
	common.SetContextKey(c, constant.ContextKeyTokenKey, token.Key)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "ws-tenant")
	common.SetContextKey(c, constant.ContextKeyUserGroup, "ws-tenant")
	common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{QuotaWarningThreshold: -1})
	service.GetChannelConstraints(c).AddPin(appdto.ChannelPin{ChannelId: channel.Id, Source: appdto.PinSourceToken, Rank: appdto.PinRankToken, RetryMode: appdto.PinRetrySingleAttempt})
	session := &responsesWSSession{lockedModel: "ws-tenant-model", lockedChannelID: channel.Id, lockedGroup: "ws-tenant", lockedKey: channel.Key,
		lockedContext: map[constant.ContextKey]any{
			constant.ContextKeyChannelId: channel.Id, constant.ContextKeyChannelType: channel.Type,
			constant.ContextKeyChannelKey: channel.Key, constant.ContextKeyChannelBaseUrl: channel.GetBaseURL(),
			constant.ContextKeyChannelHeaderOverride: channel.GetHeaderOverride(),
		}}
	return c, session
}

func TestTenantResponsesWSPinnedConnectionRechecksRevocations(t *testing.T) {
	for _, change := range []string{"tenant_disabled", "ability_revoked", "public_group_removed", "token_group_changed"} {
		t.Run(change, func(t *testing.T) {
			c, session := setupTenantResponsesWSContext(t)
			require.Nil(t, session.restoreConnectionContext(c, "ws-tenant-model"))
			switch change {
			case "tenant_disabled":
				require.NoError(t, model.DB.Model(&model.Tenant{}).Where("id = ?", 42).Update("status", model.TenantStatusDisabled).Error)
			case "ability_revoked":
				require.NoError(t, model.DB.Model(&model.Ability{}).Where("channel_id = ?", session.lockedChannelID).Update("enabled", false).Error)
			case "public_group_removed":
				require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{}`))
			case "token_group_changed":
				common.SetContextKey(c, constant.ContextKeyUsingGroup, "other")
			}
			apiErr := session.restoreConnectionContext(c, "ws-tenant-model")
			require.NotNil(t, apiErr)
			assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
			assert.True(t, types.IsSkipRetryError(apiErr))
		})
	}
}

func TestTenantResponsesWSPayloadRequiresAuthorizedPricingSnapshot(t *testing.T) {
	c, session := setupTenantResponsesWSContext(t)
	require.Nil(t, session.restoreConnectionContext(c, "ws-tenant-model"))
	create, _, err := normalizeResponsesWSTestMessage([]byte(`{"type":"response.create","model":"ws-tenant-model","input":"hello"}`))
	require.NoError(t, err)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "ws-tenant-model")
	info := relaycommon.GenRelayInfoResponses(c, &create.Request)
	info.TenantID = 42
	_, apiErr := buildResponsesWSCreatePayload(c, info, create.Request, create.Generate, create.StreamID)
	require.NotNil(t, apiErr)
	assert.ErrorContains(t, apiErr, "authorized billing budget")
	info.TenantBudget = &hosttypes.MembershipPricing{}
	payload, apiErr := buildResponsesWSCreatePayload(c, info, create.Request, create.Generate, create.StreamID)
	require.Nil(t, apiErr)
	assert.Contains(t, string(payload), `"type":"response.create"`)
	assert.NotContains(t, string(payload), "max_output_tokens", "optional provider output parameters stay omitted")
}

func TestTenantResponsesWSDurableOutcomes(t *testing.T) {
	for _, outcome := range []string{"completed", "confirmed_rejection", "server_error", "client_cancel", "cancel_without_final_usage"} {
		t.Run(outcome, func(t *testing.T) {
			c, session := setupTenantResponsesWSContext(t)
			client, downstream, cleanupClient := newTestWebSocketPair(t)
			t.Cleanup(cleanupClient)
			target, upstream, cleanupTarget := newTestWebSocketPair(t)
			t.Cleanup(cleanupTarget)
			ctx, cancel := context.WithCancel(context.Background())
			state := &responsesWSCallState{inbox: make(chan responsesWSMessage), controls: make(chan responsesWSControl, 1), done: make(chan struct{})}
			session.ctx, session.cancel, session.client, session.target, session.current = ctx, cancel, downstream, target, state
			session.startTargetReader(target)
			t.Cleanup(func() { session.shutdown(); session.workers.Wait() })
			create, _, err := normalizeResponsesWSTestMessage([]byte(`{"type":"response.create","model":"ws-tenant-model","input":"hello"}`))
			require.NoError(t, err)
			c.Request = c.Request.WithContext(ctx)
			c.Request.Body = io.NopCloser(bytes.NewReader(create.Body))
			c.Request.ContentLength = int64(len(create.Body))
			finished := make(chan *types.NewAPIError, 1)
			go func() { finished <- session.runCall(c, state, create) }()
			require.NoError(t, upstream.SetReadDeadline(time.Now().Add(5*time.Second)))
			_, request, err := upstream.ReadMessage()
			if err != nil {
				select {
				case apiErr := <-finished:
					t.Fatalf("request stopped before dispatch: %v", apiErr)
				default:
				}
			}
			require.NoError(t, err, "authorized requests must reach the existing upstream connection")
			assert.Contains(t, string(request), `"type":"response.create"`)
			var reserved model.TenantConsumption
			require.NoError(t, model.DB.Where("request_id = ?", "ws-tenant-request").First(&reserved).Error)
			require.Equal(t, "reserved", reserved.Status)
			switch outcome {
			case "completed":
				require.NoError(t, upstream.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"tenant-response","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)))
			case "confirmed_rejection":
				require.NoError(t, upstream.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","status":400,"error":{"type":"invalid_request_error","message":"invalid request"}}`)))
			case "server_error":
				require.NoError(t, upstream.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","status":500,"error":{"type":"server_error","message":"unknown execution"}}`)))
			case "client_cancel":
				require.NoError(t, upstream.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.output_text.delta","delta":"partial generated text"}`)))
				require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))
				_, _, err = client.ReadMessage()
				require.NoError(t, err)
				cancel()
			case "cancel_without_final_usage":
				require.NoError(t, upstream.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"tenant-response","status":"in_progress"}}`)))
				require.NoError(t, upstream.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.output_text.delta","delta":"partial generated text"}`)))
				require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))
				for range 2 {
					_, _, err = client.ReadMessage()
					require.NoError(t, err)
				}
				state.controls <- responsesWSControl{body: []byte(`{"type":"response.cancel","response_id":"tenant-response"}`)}
				_, control, err := upstream.ReadMessage()
				require.NoError(t, err)
				assert.Contains(t, string(control), "response.cancel")
				require.NoError(t, upstream.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.cancelled","response":{"id":"tenant-response","status":"cancelled"}}`)))
			}
			select {
			case apiErr := <-finished:
				if outcome == "completed" {
					require.Nil(t, apiErr)
				} else {
					require.NotNil(t, apiErr)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("tenant websocket call did not complete")
			}
			var record model.TenantConsumption
			require.NoError(t, model.DB.First(&record, reserved.ID).Error)
			switch outcome {
			case "completed":
				assert.Equal(t, "settled", record.Status)
			case "confirmed_rejection":
				assert.Equal(t, "refunded", record.Status)
			default:
				assert.Equal(t, "reserved", record.Status)
				assert.Equal(t, "upstream_final_usage_unknown", record.LastError)
			}
			var user model.User
			require.NoError(t, model.DB.First(&user, reserved.CustomerID).Error)
			assert.Zero(t, user.Quota)
			assert.Zero(t, user.UsedQuota)
			if outcome == "completed" {
				assert.Equal(t, 1, user.RequestCount)
			} else {
				assert.Zero(t, user.RequestCount)
			}
		})
	}
}
