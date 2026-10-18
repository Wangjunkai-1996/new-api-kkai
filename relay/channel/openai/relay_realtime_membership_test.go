package openai

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMembershipRealtimeRejectsUnfundedAutomaticFeatures(t *testing.T) {
	for _, raw := range []string{
		`{}`,
		`{"turn_detection":{"type":"server_vad"}}`,
		`{"turn_detection":null,"audio":{"input":{"turn_detection":{"create_response":true}}}}`,
		`{"turn_detection":{"create_response":true},"audio":{"input":{"turn_detection":null}}}`,
		`{"turn_detection":null,"input_audio_transcription":{"model":"whisper-1"}}`,
		`{"audio":{"input":{"turn_detection":null,"transcription":{"model":"whisper-1"}}}}`,
		`{"turn_detection":null,"tools":[{"type":"web_search"}]}`,
		`{"turn_detection":null,"model":"other-model"}`,
		`{"turn_detection":null,"type":"transcription"}`,
	} {
		var session map[string]any
		require.NoError(t, common.UnmarshalJsonStr(raw, &session))
		require.Error(t, validateMembershipRealtimeSession(session, "test-model"), raw)
	}
	for _, raw := range []string{`{"turn_detection":null}`, `{"audio":{"input":{"turn_detection":{"create_response":false}}}}`} {
		var session map[string]any
		require.NoError(t, common.UnmarshalJsonStr(raw, &session))
		require.NoError(t, validateMembershipRealtimeSession(session, "test-model"))
	}
	var response map[string]any
	require.NoError(t, common.UnmarshalJsonStr(`{"input":[{"content":[{"type":"input_image"}]}]}`, &response))
	require.Error(t, validateMembershipRealtimeResponse(response))
	_, err := membershipRealtimeOutputLimit(11.0, 10)
	require.Error(t, err)
	_, err = membershipRealtimeOutputLimit("inf", 10)
	require.Error(t, err)
	limit, err := membershipRealtimeOutputLimit(nil, 10)
	require.NoError(t, err)
	assert.Equal(t, 10, limit)
}

func startMembershipRealtimeTest(t *testing.T) (*websocket.Conn, *websocket.Conn, <-chan *types.NewAPIError) {
	t.Helper()
	if !model.MembershipEnabled() {
		t.Skip("run with KKAI_MEMBERSHIP_ENABLED=true")
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "realtime.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(append(model.MembershipModels(), &model.User{}, &model.Token{}, &model.Channel{}, &model.UserSubscription{})...))
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	oldDB, oldRedis, oldLog, oldTimeout := model.DB, common.RedisEnabled, common.LogConsumeEnabled, constant.StreamingTimeout
	oldGroups, oldModels, oldCompletion := ratio_setting.GroupRatio2JSONString(), ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString()
	oldFee := model_setting.GetGrokSettings().ViolationDeductionEnabled
	model.DB, common.RedisEnabled, common.LogConsumeEnabled, constant.StreamingTimeout = db, false, false, 2
	model_setting.GetGrokSettings().ViolationDeductionEnabled = false
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"member-test-group":1}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"member-realtime-test":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"member-realtime-test":1}`))
	t.Cleanup(func() {
		model.DB, common.RedisEnabled, common.LogConsumeEnabled, constant.StreamingTimeout = oldDB, oldRedis, oldLog, oldTimeout
		model_setting.GetGrokSettings().ViolationDeductionEnabled = oldFee
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldModels))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(oldCompletion))
		require.NoError(t, connection.Close())
	})
	require.NoError(t, db.Create(&model.User{Id: 777, Username: "realtime-member", Group: "default", Quota: 10000}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 77, Name: "realtime-fixture"}).Error)
	catalog, err := model.PublishMembershipCatalog(model.MembershipCatalog{SalesEnabled: true, Tiers: []model.MembershipTier{{ID: "gold", Name: "Gold", Style: "gold", Rank: 1, Currency: "USD", PriceMinor: 100, Quota: 100, Enabled: true, Discounts: map[string]float64{"member-test-group": 0.5}}}, Groups: []model.MembershipGroup{{Key: "member-test-group", Published: true, Enabled: true}}, ModelLimits: map[string]model.MembershipModelLimit{"member-realtime-test": {MaxInputTokens: 100, MaxOutputTokens: 10, Evidence: "verified total response bounds"}}}, 0, 1)
	require.NoError(t, err)
	order, err := model.CreateMembershipOrder(model.MembershipOrderInput{UserID: 777, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: model.PaymentProviderEpay, PaymentMethod: "alipay", TradeNo: "realtime-order", ExpiresAt: common.GetTimestamp() + 300})
	require.NoError(t, err)
	_, err = model.CompleteMembershipPayment(model.MembershipPaymentInput{TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: "realtime-payment", AmountMinor: order.AmountMinor, Currency: order.Currency, OneTime: true, Paid: true})
	require.NoError(t, err)
	downstreamServer, downstreamPeer := newRealtimeWebSocketPair(t)
	upstreamServer, upstreamPeer := newRealtimeWebSocketPair(t)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{UserId: 777, UserGroup: "default", UsingGroup: "member-test-group", IsPlayground: true, OriginModelName: "member-realtime-test", MembershipRealtime: true, ClientWs: downstreamServer, TargetWs: upstreamServer, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 77, UpstreamModelName: "member-realtime-test"}}
	result := make(chan *types.NewAPIError, 1)
	go func() {
		apiErr, _ := OpenaiRealtimeHandler(ctx, info)
		_ = downstreamServer.Close() // The controller owns the downstream lifetime.
		result <- apiErr
	}()
	return downstreamPeer, upstreamPeer, result
}

func membershipRealtimeExchange(t *testing.T, writer, reader *websocket.Conn, payload string) []byte {
	t.Helper()
	require.NoError(t, writer.WriteMessage(websocket.TextMessage, []byte(payload)))
	require.NoError(t, reader.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, message, err := reader.ReadMessage()
	require.NoError(t, err)
	return message
}

func TestMembershipRealtimeAuthorizesEveryTurnAndFallsBackAtWalletPrice(t *testing.T) {
	downstream, upstream, result := startMembershipRealtimeTest(t)
	membershipRealtimeExchange(t, downstream, upstream, `{"type":"session.update","session":{"turn_detection":null}}`)
	membershipRealtimeExchange(t, upstream, downstream, `{"type":"session.updated","session":{"turn_detection":null}}`)
	for index, usage := range []string{
		`{"total_tokens":100,"input_tokens":90,"output_tokens":10,"input_token_details":{"text_tokens":90},"output_token_details":{"text_tokens":10}}`,
		`{"total_tokens":10,"input_tokens":4,"output_tokens":6,"input_token_details":{"text_tokens":4},"output_token_details":{"text_tokens":6}}`,
	} {
		forwarded := membershipRealtimeExchange(t, downstream, upstream, `{"type":"response.create"}`)
		assert.JSONEq(t, `{"type":"response.create","response":{"max_output_tokens":10}}`, string(forwarded))
		var reserved model.MembershipBillingRecord
		require.NoError(t, model.DB.Where("state = ?", "reserved").First(&reserved).Error)
		assert.Positive(t, reserved.DispatchedAt)
		if index == 0 {
			assert.Equal(t, model.MembershipSource, reserved.Source)
			assert.EqualValues(t, 55, reserved.ReservedQuota)
			require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"member-test-group":2}`))
		} else {
			assert.Equal(t, model.MembershipWalletSource, reserved.Source)
			assert.EqualValues(t, 220, reserved.ReservedQuota)
		}
		membershipRealtimeExchange(t, upstream, downstream, `{"type":"response.created","response":{"id":"r"}}`)
		membershipRealtimeExchange(t, upstream, downstream, `{"type":"response.done","response":{"id":"r","usage":`+usage+`}}`)
	}
	require.NoError(t, upstream.Close())
	require.Nil(t, <-result)
	var records []model.MembershipBillingRecord
	require.NoError(t, model.DB.Order("request_id").Find(&records).Error)
	require.Len(t, records, 2)
	assert.NotEqual(t, records[0].RequestID, records[1].RequestID)
	assert.EqualValues(t, 50, records[0].ChargedQuota)
	assert.EqualValues(t, 20, records[1].ChargedQuota)
	var user model.User
	require.NoError(t, model.DB.First(&user, 777).Error)
	assert.EqualValues(t, 9980, user.Quota)
	assert.Equal(t, 2, user.RequestCount)
}

func TestMembershipRealtimeRetainsUnknownUsageAndDrainsLatePolicy(t *testing.T) {
	for _, mode := range []string{"missing usage", "client failure followed by policy"} {
		t.Run(mode, func(t *testing.T) {
			downstream, upstream, result := startMembershipRealtimeTest(t)
			membershipRealtimeExchange(t, downstream, upstream, `{"type":"session.update","session":{"turn_detection":null}}`)
			membershipRealtimeExchange(t, upstream, downstream, `{"type":"session.updated","session":{"turn_detection":null}}`)
			membershipRealtimeExchange(t, downstream, upstream, `{"type":"response.create"}`)
			membershipRealtimeExchange(t, upstream, downstream, `{"type":"response.created","response":{"id":"r"}}`)
			if mode == "missing usage" {
				require.NoError(t, upstream.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.done","response":{"id":"r"}}`)))
			} else {
				require.NoError(t, downstream.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create"}`)))
				require.NoError(t, upstream.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","error":{"code":"cyber_policy","message":"rejected upstream"}}`)))
			}
			apiErr := <-result
			require.NotNil(t, apiErr)
			if mode != "missing usage" {
				assert.Equal(t, service.KKAIPolicyCausalityClientToken, service.ClassifyKKAIUpstreamPolicyError(apiErr).Causality)
			}
			var record model.MembershipBillingRecord
			require.NoError(t, model.DB.First(&record).Error)
			assert.Equal(t, "reserved", record.State)
			assert.EqualValues(t, 55, record.ReservedQuota)
			assert.Zero(t, record.ChargedQuota)
		})
	}
}

func TestMembershipRealtimeUsageRejectsNegativeAndIncompleteTotals(t *testing.T) {
	assert.False(t, validMembershipRealtimeUsage(nil))
	assert.False(t, validMembershipRealtimeUsage(&dto.RealtimeUsage{TotalTokens: -1}))
	assert.False(t, validMembershipRealtimeUsage(&dto.RealtimeUsage{TotalTokens: 1, InputTokens: 1}))
	assert.True(t, validMembershipRealtimeUsage(&dto.RealtimeUsage{}))
}

func TestMembershipRealtimeInsufficientFullBudgetNeverDispatches(t *testing.T) {
	downstream, upstream, result := startMembershipRealtimeTest(t)
	membershipRealtimeExchange(t, downstream, upstream, `{"type":"session.update","session":{"turn_detection":null}}`)
	membershipRealtimeExchange(t, upstream, downstream, `{"type":"session.updated","session":{"turn_detection":null}}`)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"member-realtime-test":10000}`))
	require.NoError(t, downstream.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create"}`)))
	apiErr := <-result
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	_, message, err := upstream.ReadMessage()
	require.Error(t, err)
	assert.Empty(t, message)
	var count int64
	require.NoError(t, model.DB.Model(&model.MembershipBillingRecord{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestMembershipRealtimeRejectsOversizedFramesBeforeForwarding(t *testing.T) {
	previous := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() { constant.MaxRequestBodyMB = previous })
	downstream, upstream, result := startMembershipRealtimeTest(t)
	payload := `{"type":"session.update","session":{"turn_detection":null,"instructions":"` + strings.Repeat("x", 1<<20) + `"}}`
	_ = downstream.WriteMessage(websocket.TextMessage, []byte(payload))
	require.NoError(t, upstream.SetReadDeadline(time.Now().Add(4*time.Second)))
	_, message, err := upstream.ReadMessage()
	require.Error(t, err)
	assert.Empty(t, message)
	require.Nil(t, <-result)
	var count int64
	require.NoError(t, model.DB.Model(&model.MembershipBillingRecord{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestMembershipRealtimeLeavesDownstreamWritableForPublicError(t *testing.T) {
	downstreamServer, downstream := newRealtimeWebSocketPair(t)
	upstreamServer, _ := newRealtimeWebSocketPair(t)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{MembershipRealtime: true, ClientWs: downstreamServer, TargetWs: upstreamServer, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "test"}}
	written := make(chan error, 1)
	go func() {
		apiErr, _ := OpenaiRealtimeHandler(ctx, info)
		if apiErr == nil {
			written <- nil
			return
		}
		encoded, err := common.Marshal(map[string]any{"type": "error", "error": apiErr.ToOpenAIError()})
		if err == nil {
			err = downstreamServer.WriteMessage(websocket.TextMessage, encoded)
		}
		written <- err
	}()
	require.NoError(t, downstream.WriteMessage(websocket.TextMessage, []byte(`{"type":"session.update","session":{"turn_detection":{"create_response":true}}}`)))
	require.NoError(t, downstream.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, message, err := downstream.ReadMessage()
	require.NoError(t, err)
	assert.Contains(t, string(message), "automatic realtime responses must be disabled")
	require.NoError(t, <-written)
}
