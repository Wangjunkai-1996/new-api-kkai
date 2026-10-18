package relay

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	appdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesWSWalletClientCancellationSettlesMeasuredUsage(t *testing.T) {
	if model.MembershipEnabled() {
		t.Skip("run with KKAI_MEMBERSHIP_ENABLED=false to exercise the legacy wallet")
	}
	database := setupRelayChannelDB(t)
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.Token{}))
	previousRedis, previousBatch, previousLogs := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	previousCount, previousUnit := constant.CountToken, common.QuotaPerUnit
	previousTrust := operation_setting.GetQuotaSetting().TrustQuotaUSD
	previousPrices, previousGroups := ratio_setting.ModelPrice2JSONString(), ratio_setting.GroupRatio2JSONString()
	common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, false
	constant.CountToken, common.QuotaPerUnit = false, 500000
	operation_setting.GetQuotaSetting().TrustQuotaUSD = 0
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"ws-wallet-cancel":0.001}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"ws-wallet":1}`))
	t.Cleanup(func() {
		common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = previousRedis, previousBatch, previousLogs
		constant.CountToken, common.QuotaPerUnit = previousCount, previousUnit
		operation_setting.GetQuotaSetting().TrustQuotaUSD = previousTrust
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroups))
	})

	const startingQuota, requestCharge = 5000, 500
	user := model.User{Username: "ws-wallet-cancel", Quota: startingQuota, Status: common.UserStatusEnabled}
	require.NoError(t, database.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "ws-wallet-cancel", RemainQuota: startingQuota, Status: common.TokenStatusEnabled}
	require.NoError(t, database.Create(&token).Error)
	channel := model.Channel{Name: "ws-wallet-cancel", Key: "upstream-key", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled}
	channel.SetSetting(dto.ChannelSettings{ResponsesWebSocketEnabled: true})
	require.NoError(t, database.Create(&channel).Error)

	client, downstream, cleanupClient := newTestWebSocketPair(t)
	t.Cleanup(cleanupClient)
	target, upstream, cleanupTarget := newTestWebSocketPair(t)
	t.Cleanup(cleanupTarget)
	ctx, cancel := context.WithCancel(context.Background())
	state := &responsesWSCallState{inbox: make(chan responsesWSMessage), controls: make(chan responsesWSControl, 1), done: make(chan struct{})}
	session := &responsesWSSession{ctx: ctx, cancel: cancel, client: downstream, target: target, current: state,
		lockedModel: "ws-wallet-cancel", lockedChannelID: channel.Id, lockedGroup: "ws-wallet", lockedKey: channel.Key,
		lockedContext: map[constant.ContextKey]any{
			constant.ContextKeyChannelId: channel.Id, constant.ContextKeyChannelType: channel.Type,
			constant.ContextKeyChannelKey: channel.Key, constant.ContextKeyChannelBaseUrl: channel.GetBaseURL(),
			constant.ContextKeyChannelHeaderOverride: channel.GetHeaderOverride(),
		}}
	session.startTargetReader(target)
	t.Cleanup(func() { session.shutdown(); session.workers.Wait() })

	create, _, err := normalizeResponsesWSTestMessage([]byte(`{"type":"response.create","model":"ws-wallet-cancel","input":"hello"}`))
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(create.Body)).WithContext(ctx)
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyUserId, user.Id)
	common.SetContextKey(c, constant.ContextKeyTokenId, token.Id)
	common.SetContextKey(c, constant.ContextKeyTokenKey, token.Key)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "ws-wallet")
	common.SetContextKey(c, constant.ContextKeyUserGroup, "ws-wallet")
	common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{BillingPreference: "wallet_only", QuotaWarningThreshold: -1})
	service.GetChannelConstraints(c).AddPin(appdto.ChannelPin{ChannelId: channel.Id, Source: appdto.PinSourceToken, Rank: appdto.PinRankToken, RetryMode: appdto.PinRetrySingleAttempt})
	finished := make(chan *types.NewAPIError, 1)
	go func() { finished <- session.runCall(c, state, create) }()

	require.NoError(t, upstream.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, request, err := upstream.ReadMessage()
	require.NoError(t, err)
	assert.Contains(t, string(request), `"type":"response.create"`)
	var reserved model.User
	require.NoError(t, database.First(&reserved, user.Id).Error)
	require.EqualValues(t, startingQuota-requestCharge, reserved.Quota, "the request must reserve real wallet funds before dispatch")

	const delta = `{"type":"response.output_text.delta","delta":"billable output"}`
	require.NoError(t, upstream.WriteMessage(websocket.TextMessage, []byte(delta)))
	require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, output, err := client.ReadMessage()
	require.NoError(t, err)
	require.JSONEq(t, delta, string(output))
	// Receiving the forwarded delta proves that accounting observed output before
	// the client cancellation. No terminal usage event is supplied by the provider.
	cancel()
	select {
	case apiErr := <-finished:
		require.Nil(t, apiErr, "client cancellation with measured usage must settle instead of taking the membership timeout/refund path")
	case <-time.After(5 * time.Second):
		t.Fatal("websocket call did not finish after client cancellation")
	}

	var settled model.User
	require.NoError(t, database.First(&settled, user.Id).Error)
	assert.EqualValues(t, startingQuota-requestCharge, settled.Quota)
	assert.EqualValues(t, requestCharge, settled.UsedQuota)
	assert.Equal(t, 1, settled.RequestCount)
	var chargedToken model.Token
	require.NoError(t, database.First(&chargedToken, token.Id).Error)
	assert.Equal(t, startingQuota-requestCharge, chargedToken.RemainQuota)
	assert.Equal(t, requestCharge, chargedToken.UsedQuota)
}
