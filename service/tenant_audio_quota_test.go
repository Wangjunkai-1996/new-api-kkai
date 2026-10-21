package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tenantAudioQuotaFixture(t *testing.T, fixed bool) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	user := setupAuthSessionTestDB(t)
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.TenantConsumption{}, &model.PricingQuote{}, &model.CustomerPaymentLot{}, &model.ResellerWallet{}, &model.ResellerLedgerEntry{}, &model.Channel{}))
	require.NoError(t, db.Model(user).Updates(map[string]any{"tenant_id": 42, "quota": 10000}).Error)
	token := model.Token{UserId: user.Id, TenantID: 42, Key: "tenant-audio-quota", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 10000}
	require.NoError(t, db.Create(&token).Error)
	channel := model.Channel{Name: "tenant-audio", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.ResellerWallet{TenantID: 42, Version: 1, ExactProfitCarry: "0"}).Error)
	_, err := model.CreateCustomerPaymentLotTx(db, model.ResellerPaymentLotInput{TenantID: 42, CustomerID: user.Id, TopUpID: 1, OperationID: "tenant-audio-payment", PaidMinor: 100, CreditedQuota: 10000})
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	price := types.PriceData{ModelRatio: 2, CompletionRatio: 4, AudioRatio: 5, AudioCompletionRatio: 6, UsePrice: fixed, ModelPrice: .5, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 3}}
	info := &relaycommon.RelayInfo{TenantID: 42, UserId: user.Id, TokenId: token.Id, TokenKey: token.Key, RequestId: "tenant-audio-request", OriginModelName: "tenant-audio", UsingGroup: "default", StartTime: time.Now(), ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channel.Id}, PriceData: price, TenantBudget: &types.MembershipPricing{Price: price, QuotaPerUnit: 100}, TenantSalesMultiplier: "1", TenantSaleMinorPerQuota: "0.01", TenantSupplierMinorPerQuota: "0.005", TenantSupplySnapshot: "frozen-test-tariff"}
	require.Nil(t, preConsumeTenantBilling(c, info, 2000))
	originalUnit, originalLogs := common.QuotaPerUnit, common.LogConsumeEnabled
	originalCompletion := ratio_setting.CompletionRatio2JSONString()
	originalAudio := ratio_setting.AudioRatio2JSONString()
	originalAudioCompletion := ratio_setting.AudioCompletionRatio2JSONString()
	t.Cleanup(func() {
		common.QuotaPerUnit, common.LogConsumeEnabled = originalUnit, originalLogs
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(originalCompletion))
		require.NoError(t, ratio_setting.UpdateAudioRatioByJSONString(originalAudio))
		require.NoError(t, ratio_setting.UpdateAudioCompletionRatioByJSONString(originalAudioCompletion))
	})
	common.QuotaPerUnit, common.LogConsumeEnabled = 1_000_000, false
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"tenant-audio":99}`))
	require.NoError(t, ratio_setting.UpdateAudioRatioByJSONString(`{"tenant-audio":99}`))
	require.NoError(t, ratio_setting.UpdateAudioCompletionRatioByJSONString(`{"tenant-audio":99}`))
	info.PriceData.ModelPrice, info.PriceData.ModelRatio, info.PriceData.GroupRatioInfo.GroupRatio = 99, 99, 99
	return c, info
}

func TestTenantAudioSettlementUsesFrozenPricesAndCountsOnce(t *testing.T) {
	for _, realtime := range []bool{false, true} {
		for _, fixed := range []bool{false, true} {
			name := "audio"
			if realtime {
				name = "realtime"
			}
			if fixed {
				name += " fixed"
			} else {
				name += " tokens"
			}
			t.Run(name, func(t *testing.T) {
				c, info := tenantAudioQuotaFixture(t, fixed)
				input := dto.InputTokenDetails{TextTokens: 2, AudioTokens: 5}
				output := dto.OutputTokenDetails{TextTokens: 3, AudioTokens: 7}
				for range 2 {
					if realtime {
						PostWssConsumeQuota(c, info, "tenant-audio", &dto.RealtimeUsage{InputTokens: 7, OutputTokens: 10, TotalTokens: 17, InputTokenDetails: input, OutputTokenDetails: output}, "")
					} else {
						PostAudioConsumeQuota(c, info, &dto.Usage{PromptTokens: 7, CompletionTokens: 10, TotalTokens: 17, PromptTokensDetails: input, CompletionTokenDetails: output}, "")
					}
				}
				want := 1494 // (2 + 3*4 + 5*5 + 7*5*6) * 2 * 3
				if fixed {
					want = 150 // .5 * frozen 100 * frozen 3
				}
				var user model.User
				require.NoError(t, model.DB.First(&user, info.UserId).Error)
				assert.EqualValues(t, 10000-want, user.Quota)
				assert.EqualValues(t, want, user.UsedQuota)
				assert.EqualValues(t, 1, user.RequestCount)
				var token model.Token
				require.NoError(t, model.DB.First(&token, info.TokenId).Error)
				assert.Equal(t, 10000-want, token.RemainQuota)
				assert.Equal(t, want, token.UsedQuota)
				var channel model.Channel
				require.NoError(t, model.DB.First(&channel, info.ChannelId).Error)
				assert.EqualValues(t, want, channel.UsedQuota)
				var ledgerCount int64
				require.NoError(t, model.DB.Model(&model.ResellerLedgerEntry{}).Count(&ledgerCount).Error)
				assert.EqualValues(t, 1, ledgerCount)
			})
		}
	}
}

func TestTenantAudioMissingUsageRetainsReservation(t *testing.T) {
	for _, realtime := range []bool{false, true} {
		t.Run(map[bool]string{false: "audio", true: "realtime"}[realtime], func(t *testing.T) {
			c, info := tenantAudioQuotaFixture(t, false)
			if realtime {
				PostWssConsumeQuota(c, info, "tenant-audio", nil, "")
			} else {
				PostAudioConsumeQuota(c, info, nil, "")
			}
			assert.True(t, info.BillingOutcomeUnknown)
			info.Billing.Refund(c)
			var consumption model.TenantConsumption
			require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&consumption).Error)
			assert.Equal(t, "reserved", consumption.Status)
			assert.Equal(t, "upstream_final_usage_unknown", consumption.LastError)
			assert.Zero(t, consumption.ActualQuota)
			var user model.User
			require.NoError(t, model.DB.First(&user, info.UserId).Error)
			assert.EqualValues(t, 8000, user.Quota)
			assert.Zero(t, user.UsedQuota)
			assert.Zero(t, user.RequestCount)
		})
	}
}
