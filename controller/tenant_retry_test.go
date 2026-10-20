package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaypkg "github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantRetryRevalidatesCatalogWithoutChangingQuotedGroup(t *testing.T) {
	for _, change := range []string{"disable_tenant", "revoke_model", "change_group"} {
		t.Run(change, func(t *testing.T) {
			db := setupModelListControllerTestDB(t)
			previousCache := common.MemoryCacheEnabled
			oldOpen, oldGroups, oldModels := setting.UserUsableGroups2JSONString(), ratio_setting.GroupRatio2JSONString(), ratio_setting.ModelRatio2JSONString()
			common.MemoryCacheEnabled = false
			require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","premium":"Premium"}`))
			require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"premium":1.5}`))
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"text":1}`))
			t.Cleanup(func() {
				common.MemoryCacheEnabled = previousCache
				require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldOpen))
				require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldModels))
				model.InvalidatePricingCache()
			})
			require.NoError(t, db.AutoMigrate(&model.Tenant{}))
			require.NoError(t, db.Create(&model.Tenant{ID: 42, Status: model.TenantStatusActive}).Error)
			require.NoError(t, db.Create(&model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "test-key", Group: "default,premium", Models: "text"}).Error)
			require.NoError(t, db.Create(&[]model.Ability{{ChannelId: 1, Group: "default", Model: "text", Enabled: true}, {ChannelId: 1, Group: "premium", Model: "text", Enabled: true}}).Error)
			model.InvalidatePricingCache()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
			info := &relaycommon.RelayInfo{TenantID: 42, TenantQuoteID: 17, TenantSupplySnapshot: `{"supply_group":"default","retail_ratio":"2"}`, TenantRetailMultiplier: 2, UsingGroup: "default", OriginModelName: "text", ChannelMeta: &relaycommon.ChannelMeta{}}
			retry := &service.RetryParam{Ctx: c, TokenGroup: "default", ModelName: "text"}
			channel, apiErr := getChannel(c, info, retry)
			require.Nil(t, apiErr)
			require.NotNil(t, channel)
			assert.Equal(t, 1, channel.Id)
			switch change {
			case "disable_tenant":
				require.NoError(t, db.Model(&model.Tenant{}).Where("id = ?", 42).Update("status", model.TenantStatusDisabled).Error)
			case "revoke_model":
				require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", 1).Update("enabled", false).Error)
			case "change_group":
				require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"premium":"Premium"}`))
				_, err := service.ResolveTenantRequestCatalog(c.Request.Context(), 42, "text", "premium")
				require.NoError(t, err, "another public group remains available, but the accepted request cannot switch to it")
			}
			retry.IncreaseRetry()
			channel, apiErr = getChannel(c, info, retry)
			assert.Nil(t, channel)
			require.NotNil(t, apiErr)
			assert.True(t, types.IsSkipRetryError(apiErr))
			assert.Equal(t, "default", retry.TokenGroup)
			assert.Equal(t, "default", info.UsingGroup)
			assert.EqualValues(t, 17, info.TenantQuoteID)
			assert.Equal(t, 2.0, info.PriceData.GroupRatioInfo.GroupRatio)
		})
	}
}

func TestTenantFreeUnknownOutcomeStopsRetryAndPreservesDurableRecord(t *testing.T) {
	for _, outcome := range []string{"known_rejection", "transport_unknown", "incomplete_stream"} {
		t.Run(outcome, func(t *testing.T) {
			db := setupModelListControllerTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.Token{}, &model.TenantConsumption{}))
			user := model.User{TenantID: 42, Username: "free-customer", Status: common.UserStatusEnabled, AffCode: "free-customer"}
			require.NoError(t, db.Create(&user).Error)
			token := model.Token{TenantID: 42, UserId: user.Id, Key: "free-unknown", Status: common.TokenStatusEnabled, ExpiredTime: -1}
			require.NoError(t, db.Create(&token).Error)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{TenantID: 42, UserId: user.Id, TokenId: token.Id, RequestId: "free-unknown-request", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 1}, TenantBudget: &hosttypes.MembershipPricing{}, PriceData: hosttypes.PriceData{FreeModel: true}}
			assert.False(t, info.HasDurableBilling(), "a prepared price without a persisted billing session is not durable")
			require.Nil(t, service.PreConsumeBilling(c, 0, info))
			assert.Zero(t, info.TenantQuoteID)
			require.True(t, info.HasDurableBilling())
			if outcome == "transport_unknown" {
				info.BillingOutcomeUnknown = true
			} else if outcome == "incomplete_stream" {
				info.IsStream, info.StreamStatus = true, relaycommon.NewStreamStatus()
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, context.DeadlineExceeded)
				require.ErrorContains(t, service.SettleBilling(c, info, 0), "retained pending final usage")
				assert.True(t, info.BillingOutcomeUnknown)
			}
			apiErr := types.NewErrorWithStatusCode(context.DeadlineExceeded, types.ErrorCodeChannelResponseTimeExceeded, http.StatusGatewayTimeout)
			decision := decideRelayRetry(c, info, apiErr, 2)
			if outcome == "known_rejection" {
				assert.Equal(t, "retry", decision.Action)
			} else {
				assert.Equal(t, "stop", decision.Action)
				assert.Equal(t, "billing_outcome_unknown", decision.Reason)
			}
			assert.Same(t, apiErr, relaypkg.RefundFailedRequestBilling(c, info, apiErr))
			var record model.TenantConsumption
			require.NoError(t, db.Where("request_id = ?", info.RequestId).First(&record).Error)
			if outcome == "known_rejection" {
				assert.Equal(t, "refunded", record.Status)
				assert.Empty(t, record.LastError)
			} else {
				assert.True(t, types.IsSkipRetryError(apiErr))
				assert.Equal(t, "reserved", record.Status, "an unknown free execution cannot be recorded as a confirmed refund")
				assert.Equal(t, "upstream_final_usage_unknown", record.LastError)
			}
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Zero(t, user.Quota)
			assert.Zero(t, user.UsedQuota)
			assert.Zero(t, user.RequestCount)
		})
	}
}
