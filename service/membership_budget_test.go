package service

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupMembershipServiceTest(t *testing.T) (*gin.Context, model.User) {
	t.Helper()
	if !model.MembershipEnabled() {
		t.Skip("run with KKAI_MEMBERSHIP_ENABLED=true to exercise membership funding")
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "membership.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(append(model.MembershipModels(), &model.User{}, &model.Token{}, &model.Channel{}, &model.UserSubscription{})...))
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	previousDB, previousRedis := model.DB, common.RedisEnabled
	previousGroups := ratio_setting.GroupRatio2JSONString()
	model.DB, common.RedisEnabled = db, false
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"gpt":0.2}`))
	t.Cleanup(func() {
		model.DB, common.RedisEnabled = previousDB, previousRedis
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroups))
		require.NoError(t, connection.Close())
	})
	user := model.User{Id: 99, Username: "member-test", Group: "default", Quota: 10000}
	require.NoError(t, db.Create(&user).Error)
	catalog, err := model.PublishMembershipCatalog(model.MembershipCatalog{ModelLimits: map[string]model.MembershipModelLimit{"test-model": {MaxInputTokens: 100, MaxOutputTokens: 100, Evidence: "verified fixture cumulative request limits"}}, SalesEnabled: true, Tiers: []model.MembershipTier{{ID: "gold", Name: "Gold", Rank: 1, Style: "gold", Currency: "USD", PriceMinor: 1000, Quota: 100, Enabled: true, Discounts: map[string]float64{"gpt": 0.5}}}, Groups: []model.MembershipGroup{{Key: "gpt", Enabled: true, Published: true}}}, 0, 1)
	require.NoError(t, err)
	order, err := model.CreateMembershipOrder(model.MembershipOrderInput{UserID: user.Id, TierID: "gold", ReleaseID: catalog.ReleaseID, Provider: model.PaymentProviderEpay, PaymentMethod: "alipay", TradeNo: "service-membership", ExpiresAt: common.GetTimestamp() + 600})
	require.NoError(t, err)
	order, err = model.CompleteMembershipPayment(model.MembershipPaymentInput{TradeNo: order.TradeNo, Provider: order.Provider, ReceiptID: "service-payment", AmountMinor: order.AmountMinor, Currency: order.Currency, OneTime: true, Paid: true})
	require.NoError(t, err)
	require.Equal(t, model.MembershipOrderSuccess, order.Status)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	return ctx, user
}

func TestMembershipBudgetBoundsEveryCacheCategory(t *testing.T) {
	ctx, user := setupMembershipServiceTest(t)
	info := &relaycommon.RelayInfo{UserId: user.Id, RelayMode: relayconstant.RelayModeChatCompletions, Request: &dto.GeneralOpenAIRequest{}, OriginModelName: "test-model"}
	price := types.PriceData{ModelRatio: 1, CompletionRatio: 1, CacheCreationRatio: 1, CacheCreation5mRatio: 20, CacheCreation1hRatio: 2, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}
	require.NoError(t, PrepareMembershipBudget(ctx, info, &price, 100, &relaytypes.TokenCountMeta{MaxTokens: 10}))
	info.PriceData = price
	actual := calculateTextQuotaSummary(ctx, info, &dto.Usage{PromptTokens: 0, CompletionTokens: 10, ClaudeCacheCreation5mTokens: 100, UsageSemantic: "anthropic"})
	assert.GreaterOrEqual(t, info.MembershipPricing.MaximumBeforeGroup-info.MembershipPricing.ViolationFeeBeforeGroup, float64(actual.Quota), "five-minute cache creation is independently configurable and must fit the full reservation")
}

func TestMembershipBudgetRequiresOutputBound(t *testing.T) {
	ctx, user := setupMembershipServiceTest(t)
	info := &relaycommon.RelayInfo{UserId: user.Id, RelayMode: relayconstant.RelayModeChatCompletions, Request: &dto.GeneralOpenAIRequest{}, OriginModelName: "test-model"}
	price := types.PriceData{ModelRatio: 1, CompletionRatio: 1}
	require.ErrorContains(t, PrepareMembershipBudget(ctx, info, &price, 100, &relaytypes.TokenCountMeta{}), "max_tokens")
	require.Nil(t, info.MembershipPricing)
}

func TestMembershipReservationRestoresFrozenPricesAndCapsSettlement(t *testing.T) {
	_, user := setupMembershipServiceTest(t)
	info := &relaycommon.RelayInfo{UserId: user.Id, RequestId: "freeze-and-settle", ChannelMeta: &relaycommon.ChannelMeta{}, UsingGroup: "gpt", IsPlayground: true, OriginModelName: "test-model", PriceData: types.PriceData{ModelRatio: 2, CompletionRatio: 3, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.2}}}
	info.PriceData.AddOtherRatio("quality", 2)
	input, err := PrepareMembershipReservation(info, 800)
	require.NoError(t, err)
	record, err := model.ReserveMembershipBilling(input)
	require.NoError(t, err)
	require.Equal(t, model.MembershipSource, record.Source)
	require.EqualValues(t, 80, record.ReservedQuota)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"gpt":9}`))
	info.PriceData.ModelRatio = 99
	require.NoError(t, ApplyMembershipReservation(info, record))
	assert.Equal(t, 0.1, info.PriceData.GroupRatioInfo.GroupRatio)
	assert.Equal(t, 2.0, info.PriceData.ModelRatio)
	assert.Equal(t, 2.0, info.PriceData.OtherRatioMultiplier())
	info.Billing = &membershipBillingSession{info: info, requestID: record.RequestID, reserved: 80, state: "reserved"}
	info.MembershipHasBillableUsage = true
	assert.Equal(t, 80, MembershipCharge(info, 120))
	require.NoError(t, SettleBilling(nil, info, 80))
	require.NoError(t, SettleBilling(nil, info, 80))
	stored, err := model.GetMembershipBillingRecord(info.RequestId)
	require.NoError(t, err)
	assert.EqualValues(t, 120, stored.ActualQuota)
	assert.EqualValues(t, 80, stored.ChargedQuota)
	assert.EqualValues(t, 40, stored.ExcessQuota)
	self, err := model.GetMembershipSelf(user.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 80, self.Account.AmountUsed)
	assert.Zero(t, self.Account.AmountReserved)
	var updated model.User
	require.NoError(t, model.DB.First(&updated, user.Id).Error)
	assert.EqualValues(t, 10000, updated.Quota)
	assert.Equal(t, 1, updated.RequestCount)
	assert.EqualValues(t, 80, updated.UsedQuota)
}

func TestMembershipInsufficientReservationUsesWalletPrice(t *testing.T) {
	_, user := setupMembershipServiceTest(t)
	info := &relaycommon.RelayInfo{UserId: user.Id, RequestId: "wallet-fallback", UsingGroup: "gpt", IsPlayground: true, OriginModelName: "test-model", PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.2}}}
	input, err := PrepareMembershipReservation(info, 1100)
	require.NoError(t, err)
	record, err := model.ReserveMembershipBilling(input)
	require.NoError(t, err)
	require.Equal(t, model.MembershipWalletSource, record.Source)
	require.NoError(t, ApplyMembershipReservation(info, record))
	assert.Equal(t, 0.2, info.PriceData.GroupRatioInfo.GroupRatio)
	assert.EqualValues(t, 220, record.ReservedQuota)
	self, err := model.GetMembershipSelf(user.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 100, self.Account.Available())
	assert.Equal(t, model.MembershipActive, self.Account.Status)
	_, err = model.ReleaseMembershipBilling(info.RequestId)
	require.NoError(t, err)
}

func TestMembershipOutboundKeepsAuthorizedBounds(t *testing.T) {
	cases := []struct {
		name, body string
		valid      bool
	}{
		{"bounded", `{"max_tokens":100,"n":2,"max_tool_calls":3}`, true},
		{"lower limits", `{"max_tokens":40,"n":1,"max_tool_calls":1}`, true},
		{"removed limit", `{"n":2,"max_tool_calls":3}`, false},
		{"increased output", `{"max_tokens":101,"max_tool_calls":3}`, false},
		{"increased copies", `{"max_tokens":100,"n":3,"max_tool_calls":3}`, false},
		{"alternate provider field", `{"generationConfig":{"maxOutputTokens":101},"max_tool_calls":3}`, false},
		{"increased tool calls", `{"max_tokens":100,"max_tool_calls":4}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodPost, "https://example.invalid/v1/chat/completions", strings.NewReader(tc.body))
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{MembershipBillingRequestID: "authorized", MembershipPricing: &types.MembershipPricing{RequiresOutputBound: true, MaxOutputTokens: 100, OutputCopies: 2, ToolCallLimits: map[string]int{"max_tool_calls": 3}}}
			err = ValidateMembershipOutbound(request, info)
			if tc.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestMembershipGroupQuotesSkipVirtualAndRemovedGroups(t *testing.T) {
	_, user := setupMembershipServiceTest(t)
	old := setting.UserUsableGroups2JSONString()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"gpt":"GPT","auto":"Auto","removed":"Removed"}`))
	t.Cleanup(func() { require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(old)) })
	catalog, err := model.GetMembershipCatalog()
	require.NoError(t, err)
	quotes, err := GetMembershipGroupQuotes(user.Id, catalog)
	require.NoError(t, err)
	require.Len(t, quotes, 1)
	assert.Equal(t, "gpt", quotes[0].GroupKey)
	assert.Equal(t, 0.1, quotes[0].MembershipRatio)
	catalog.Groups[0].Enabled = false
	quotes, err = GetMembershipGroupQuotes(user.Id, catalog)
	require.NoError(t, err)
	assert.Empty(t, quotes)
}

func TestMembershipBudgetUsesVerifiedHiddenContextBound(t *testing.T) {
	ctx, user := setupMembershipServiceTest(t)
	price := types.PriceData{ModelRatio: 1, CompletionRatio: 2}
	info := &relaycommon.RelayInfo{UserId: user.Id, RelayMode: relayconstant.RelayModeResponses, Request: &dto.OpenAIResponsesRequest{}, OriginModelName: "test-model"}
	require.NoError(t, PrepareMembershipBudget(ctx, info, &price, 1, &relaytypes.TokenCountMeta{MaxTokens: 10}))
	require.NotNil(t, info.MembershipPricing)
	assert.Equal(t, 300.0, info.MembershipPricing.MaximumBeforeGroup-info.MembershipPricing.ViolationFeeBeforeGroup, "reserve verified aggregate input and output rather than the one visible token")
	unknown := &relaycommon.RelayInfo{UserId: user.Id, RelayMode: relayconstant.RelayModeResponses, Request: &dto.OpenAIResponsesRequest{}, OriginModelName: "unknown-model"}
	require.ErrorContains(t, PrepareMembershipBudget(ctx, unknown, &price, 1, &relaytypes.TokenCountMeta{MaxTokens: 10}), "verified")
	assert.Nil(t, unknown.MembershipPricing)
	require.ErrorContains(t, PrepareMembershipBudget(ctx, info, &price, 101, &relaytypes.TokenCountMeta{MaxTokens: 10}), "exceeds")
}
