package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantResponsesWSRunnerPreservesHostAndRevalidatesEveryTurn(t *testing.T) {
	for _, change := range []string{"disable_tenant", "revoke_domain", "reassign_domain"} {
		t.Run(change, func(t *testing.T) {
			_, token := setupTenantTokenGroupRouter(t)
			db := model.DB
			require.NoError(t, db.AutoMigrate(&model.TenantDomain{}))
			domain := model.TenantDomain{TenantID: 42, Hostname: "tenant.example.test", Status: model.TenantDomainStatusActive}
			require.NoError(t, db.Create(&domain).Error)
			var runner relay.ResponsesWSRequestRunner
			engine := gin.New()
			engine.Use(middleware.TenantRouting(db, true, "main.example.test"), middleware.TenantFeatureGate())
			engine.GET("/v1/responses", middleware.TokenAuth(), func(c *gin.Context) { runner = newResponsesWSRequestRunner(c) })
			request := httptest.NewRequest(http.MethodGet, "https://tenant.example.test/v1/responses", nil)
			request.Header.Set("Authorization", "Bearer sk-"+token.Key)
			request.Header.Set("Connection", "Upgrade")
			request.Header.Set("Upgrade", "websocket")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.NotNil(t, runner)
			called := 0
			handle := func(c *gin.Context) *types.NewAPIError {
				called++
				assert.Equal(t, int64(42), common.GetContextKeyInt64(c, constant.ContextKeyTenantID))
				assert.Equal(t, "tenant.example.test", c.Request.Host)
				assert.Equal(t, "default", common.GetContextKeyString(c, constant.ContextKeyUsingGroup))
				return nil
			}
			// The transport's synthetic HTTP request cannot replace the Host that
			// authenticated the WebSocket connection.
			turn := httptest.NewRequest(http.MethodPost, "https://main.example.test/v1/responses", nil)
			require.Nil(t, runner(turn, "first-turn", handle))
			switch change {
			case "disable_tenant":
				require.NoError(t, db.Model(&model.Tenant{}).Where("id = ?", 42).Update("status", model.TenantStatusDisabled).Error)
			case "revoke_domain":
				require.NoError(t, db.Delete(&domain).Error)
			case "reassign_domain":
				require.NoError(t, db.Create(&model.Tenant{ID: 43, Status: model.TenantStatusActive}).Error)
				require.NoError(t, db.Model(&domain).Update("tenant_id", 43).Error)
			}
			apiErr := runner(turn, "revoked-turn", handle)
			require.NotNil(t, apiErr)
			assert.GreaterOrEqual(t, apiErr.StatusCode, http.StatusBadRequest)
			assert.Equal(t, 1, called, "revoked or cross-tenant credentials cannot reach relay or billing")
			assert.NotContains(t, apiErr.Error(), token.Key)
		})
	}
}

func TestTenantResponsesWSPaidTurnsSettleOnceWithIndependentPrices(t *testing.T) {
	_, token := setupTenantTokenGroupRouter(t)
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.TenantDomain{}, &model.TenantConsumption{}, &model.PricingQuote{}, &model.CustomerPaymentLot{}, &model.ResellerWallet{}, &model.ResellerLedgerEntry{}))
	require.False(t, db.Migrator().HasTable(&model.MembershipCatalogState{}), "tenant requests must work without a membership catalog")
	oldUnit, oldPrice, oldBatch, oldLogs, oldCount := common.QuotaPerUnit, operation_setting.Price, common.BatchUpdateEnabled, common.LogConsumeEnabled, constant.CountToken
	oldPrices := ratio_setting.ModelPrice2JSONString()
	common.QuotaPerUnit, operation_setting.Price, common.BatchUpdateEnabled, common.LogConsumeEnabled, constant.CountToken = 500000, 1, false, false, false
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"zz-default-model":0.01}`))
	t.Cleanup(func() {
		common.QuotaPerUnit, operation_setting.Price, common.BatchUpdateEnabled, common.LogConsumeEnabled, constant.CountToken = oldUnit, oldPrice, oldBatch, oldLogs, oldCount
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
	})
	const startingQuota = 500000
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Update("quota", startingQuota).Error)
	require.NoError(t, db.Model(token).Updates(map[string]any{"remain_quota": startingQuota, "unlimited_quota": false}).Error)
	require.NoError(t, db.Create(&model.TenantDomain{TenantID: 42, Hostname: "tenant.example.test", Status: model.TenantDomainStatusActive}).Error)
	require.NoError(t, db.Create(&model.ResellerWallet{TenantID: 42, ExactProfitCarry: "0", Version: 1}).Error)
	lot, err := model.CreateCustomerPaymentLotTx(db, model.ResellerPaymentLotInput{TenantID: 42, CustomerID: 1, TopUpID: 1, OperationID: "ws-customer-payment", PaidMinor: 100, CreditedQuota: startingQuota})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.TenantPriceVersion{TenantID: 42, Model: "", Group: "default", Version: 1, SalesMultiplier: "2"}).Error)
	firstReceived := make(chan struct{})
	releaseFirst := make(chan struct{})
	defer func() {
		select {
		case <-releaseFirst:
		default:
			close(releaseFirst)
		}
	}()
	upstreamDone := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if !assert.NoError(t, err) {
			return
		}
		defer ws.Close()
		defer close(upstreamDone)
		for index := 1; index <= 2; index++ {
			_, body, err := ws.ReadMessage()
			if !assert.NoError(t, err) {
				return
			}
			assert.Contains(t, string(body), `"type":"response.create"`)
			assert.NotContains(t, string(body), "max_output_tokens")
			if index == 1 {
				close(firstReceived)
				<-releaseFirst
			} else {
				// A repeated terminal from the first response cannot charge either
				// response twice on the persistent upstream connection.
				if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"paid-1","status":"completed","usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}`))) {
					return
				}
			}
			terminal := fmt.Sprintf(`{"type":"response.completed","response":{"id":"paid-%d","status":"completed","usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}`, index)
			if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(terminal))) {
				return
			}
		}
		_, _, _ = ws.ReadMessage()
	}))
	t.Cleanup(upstream.Close)
	var channel model.Channel
	require.NoError(t, db.First(&channel, 1).Error)
	channel.BaseURL = &upstream.URL
	channel.SetSetting(dto.ChannelSettings{ResponsesWebSocketEnabled: true})
	require.NoError(t, db.Save(&channel).Error)
	model.InvalidatePricingCache()
	engine := gin.New()
	engine.Use(middleware.TenantRouting(db, true), middleware.TenantFeatureGate())
	done := make(chan struct{})
	engine.GET("/v1/responses", middleware.TokenAuth(), func(c *gin.Context) {
		defer close(done)
		c.Set(common.RequestIdKey, "tenant-paid-ws")
		ResponsesWebSocket(c)
	})
	gateway := httptest.NewServer(engine)
	t.Cleanup(gateway.Close)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/v1/responses", http.Header{"Host": {"tenant.example.test"}, "Authorization": {"Bearer sk-" + token.Key}})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = client.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("tenant websocket handler did not close")
		}
		select {
		case <-upstreamDone:
		case <-time.After(3 * time.Second):
			t.Error("tenant upstream websocket did not close")
		}
	})
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"zz-default-model","input":"first"}`)))
	select {
	case <-firstReceived:
	case <-time.After(3 * time.Second):
		t.Fatal("paid request did not reach upstream")
	}
	// Change both prices after the first reservation and before its terminal.
	// That request keeps its quote, while the next turn accepts fresh prices.
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"zz-default-model":0.02}`))
	require.NoError(t, db.Create(&model.TenantPriceVersion{TenantID: 42, Model: "", Group: "default", Version: 2, SalesMultiplier: "3"}).Error)
	model.InvalidatePricingCache()
	close(releaseFirst)
	first := readResponsesWSTestEvent(t, client)
	require.Equal(t, "response.completed", first["type"])
	require.Equal(t, "paid-1", first["response"].(map[string]any)["id"])
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"zz-default-model","input":"second","previous_response_id":"paid-1"}`)))
	second := readResponsesWSTestEvent(t, client)
	require.Equal(t, "response.completed", second["type"])
	require.Equal(t, "paid-2", second["response"].(map[string]any)["id"])
	var records []model.TenantConsumption
	require.NoError(t, db.Order("id").Find(&records).Error)
	require.Len(t, records, 2)
	for index, expected := range []int64{10000, 30000} {
		assert.Equal(t, "settled", records[index].Status)
		assert.Equal(t, expected, records[index].ActualQuota)
		assert.Equal(t, expected, records[index].ReservedQuota)
		var quote model.PricingQuote
		require.NoError(t, db.First(&quote, records[index].QuoteID).Error)
		assert.Equal(t, []string{"2", "3"}[index], quote.SalesMultiplier)
		require.NoError(t, model.SettleTenantConsumption(context.Background(), db, records[index].RequestID))
	}
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.EqualValues(t, startingQuota-40000, user.Quota)
	assert.EqualValues(t, 40000, user.UsedQuota)
	assert.Equal(t, 2, user.RequestCount)
	require.NoError(t, db.First(token, token.Id).Error)
	assert.Equal(t, startingQuota-40000, token.RemainQuota)
	assert.Equal(t, 40000, token.UsedQuota)
	require.NoError(t, db.First(&channel, channel.Id).Error)
	assert.EqualValues(t, 40000, channel.UsedQuota)
	require.NoError(t, db.First(lot, lot.ID).Error)
	assert.EqualValues(t, 40000, lot.ConsumedQuota)
	assert.EqualValues(t, startingQuota-40000, lot.RemainingQuota)
	var ledger []model.ResellerLedgerEntry
	require.NoError(t, db.Order("id").Find(&ledger).Error)
	require.Len(t, ledger, 2)
	assert.EqualValues(t, 2, ledger[0].SaleMinor)
	assert.EqualValues(t, 6, ledger[1].SaleMinor)
	assert.Equal(t, "1", ledger[0].SupplierMinorExact)
	assert.Equal(t, "2", ledger[1].SupplierMinorExact)
	var wallet model.ResellerWallet
	require.NoError(t, db.Where("tenant_id = ?", 42).First(&wallet).Error)
	assert.EqualValues(t, 5, wallet.PendingMinor)
}
