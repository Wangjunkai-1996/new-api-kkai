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

func TestMembershipBudgetEstimatesOmittedOutputWithoutChangingRequest(t *testing.T) {
	ctx, user := setupMembershipServiceTest(t)
	request := &dto.GeneralOpenAIRequest{}
	info := &relaycommon.RelayInfo{UserId: user.Id, RelayMode: relayconstant.RelayModeChatCompletions, Request: request, OriginModelName: "test-model"}
	price := types.PriceData{ModelRatio: 1, CompletionRatio: 1}
	require.NoError(t, PrepareMembershipBudget(ctx, info, &price, 100, &relaytypes.TokenCountMeta{}))
	require.NotNil(t, info.MembershipPricing)
	assert.Equal(t, 1124.0, info.MembershipPricing.MaximumBeforeGroup-info.MembershipPricing.ViolationFeeBeforeGroup)
	assert.False(t, info.MembershipPricing.RequiresOutputBound)
	assert.Nil(t, request.MaxTokens)
	assert.Nil(t, request.MaxCompletionTokens)
	info.MembershipBillingRequestID = "estimated"
	outbound, err := http.NewRequest(http.MethodPost, "https://example.invalid/v1/chat/completions", strings.NewReader(`{"model":"test-model"}`))
	require.NoError(t, err)
	require.NoError(t, ValidateMembershipOutbound(outbound, info))
	oversized, err := http.NewRequest(http.MethodPost, "https://example.invalid/v1/chat/completions", strings.NewReader(`{"max_tokens":1073741824}`))
	require.NoError(t, err)
	require.ErrorContains(t, ValidateMembershipOutbound(oversized, info), "authorized output limit")

	zero := uint(0)
	request.MaxTokens = &zero
	require.ErrorContains(t, PrepareMembershipBudget(ctx, &relaycommon.RelayInfo{UserId: user.Id, RelayMode: relayconstant.RelayModeChatCompletions, Request: request, OriginModelName: "test-model"}, &price, 100, &relaytypes.TokenCountMeta{}), "must be positive")
}

func TestMembershipBudgetEstimatesOneSearchWithEmptyModelLimits(t *testing.T) {
	ctx, user := setupMembershipServiceTest(t)
	catalog, err := model.GetMembershipCatalog()
	require.NoError(t, err)
	catalog.ModelLimits = nil
	_, err = model.PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
	require.NoError(t, err)
	price := types.PriceData{ModelRatio: 1, CompletionRatio: 1}
	for _, tc := range []struct {
		name, model, tool string
		request           dto.Request
	}{
		{"search preview", "test-search-preview", dto.BuildInToolWebSearchPreview, &dto.GeneralOpenAIRequest{}},
		{"Gemini search", "gemini-test", dto.BuildInToolGoogleSearch, &dto.GeminiChatRequest{Tools: []byte(`[{"google_search":{}}]`)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{UserId: user.Id, RelayMode: relayconstant.RelayModeChatCompletions, Request: tc.request, OriginModelName: tc.model}
			require.NoError(t, PrepareMembershipBudget(ctx, info, &price, 1, &relaytypes.TokenCountMeta{MaxTokens: 10}))
			require.NotNil(t, info.MembershipPricing)
			toolPrice := info.MembershipPricing.ToolPrices[tc.tool]
			assert.Positive(t, toolPrice)
			assert.Equal(t, 11+toolPrice/1000*common.QuotaPerUnit, info.MembershipPricing.MaximumBeforeGroup-info.MembershipPricing.ViolationFeeBeforeGroup)
		})
	}
}

func TestMembershipBudgetEstimatesOmittedPaidToolCounts(t *testing.T) {
	ctx, user := setupMembershipServiceTest(t)
	price := types.PriceData{ModelRatio: 1, CompletionRatio: 1}
	for _, tc := range []struct {
		name, tool string
		request    dto.Request
		mode       int
	}{
		{"responses search", dto.BuildInToolWebSearch, &dto.OpenAIResponsesRequest{Tools: []byte(`[{"type":"web_search"}]`)}, relayconstant.RelayModeResponses},
		{"claude search", dto.BuildInToolWebSearch, &dto.ClaudeRequest{Tools: []any{dto.ClaudeWebSearchTool{Type: "web_search_20250305"}}}, relayconstant.RelayModeChatCompletions},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{UserId: user.Id, RelayMode: tc.mode, Request: tc.request, OriginModelName: "test-model"}
			require.NoError(t, PrepareMembershipBudget(ctx, info, &price, 1, &relaytypes.TokenCountMeta{MaxTokens: 10}))
			toolPrice := info.MembershipPricing.ToolPrices[tc.tool]
			assert.Positive(t, toolPrice)
			assert.Equal(t, 11+toolPrice/1000*common.QuotaPerUnit, info.MembershipPricing.MaximumBeforeGroup-info.MembershipPricing.ViolationFeeBeforeGroup)
			assert.Empty(t, info.MembershipPricing.ToolCallLimits)
		})
	}
}

func TestMembershipSpeechReservesOutputFromVisibleInput(t *testing.T) {
	ctx, user := setupMembershipServiceTest(t)
	info := &relaycommon.RelayInfo{UserId: user.Id, RelayMode: relayconstant.RelayModeAudioSpeech, Request: &dto.AudioRequest{Input: "Hello"}, OriginModelName: "test-model"}
	price := types.PriceData{ModelRatio: 1, CompletionRatio: 2}
	require.NoError(t, PrepareMembershipBudget(ctx, info, &price, 5, &relaytypes.TokenCountMeta{}))
	assert.Equal(t, 5+1024*2.0, info.MembershipPricing.MaximumBeforeGroup-info.MembershipPricing.ViolationFeeBeforeGroup)
	assert.False(t, info.MembershipPricing.RequiresOutputBound)
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
	catalog, err := model.GetMembershipCatalog()
	require.NoError(t, err)
	catalog.Tiers[0].Discounts["gpt"] = 0.8
	catalog, err = model.PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"gpt":9}`))
	info.PriceData.ModelRatio = 99
	require.NoError(t, ApplyMembershipReservation(info, record))
	assert.Equal(t, input.ExpectedReleaseID, info.MembershipPricing.ReleaseID)
	assert.NotEqual(t, catalog.ReleaseID, info.MembershipPricing.ReleaseID, "an accepted request keeps its pricing release after publication")
	assert.Equal(t, 0.5, info.MembershipPricing.Discount)
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

func TestMembershipResponsesRejectsInsufficientAPIKeyQuotaWithoutChargingMember(t *testing.T) {
	ctx, user := setupMembershipServiceTest(t)
	token := model.Token{UserId: user.Id, Key: "limited-member-key", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 3}
	require.NoError(t, model.DB.Create(&token).Error)
	outputCap := uint(10)
	price := types.PriceData{ModelRatio: 2, CompletionRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.2}}
	info := &relaycommon.RelayInfo{
		UserId: user.Id, TokenId: token.Id, TokenKey: token.Key, RequestId: "responses-limited-key",
		RelayMode: relayconstant.RelayModeResponses, Request: &dto.OpenAIResponsesRequest{MaxOutputTokens: &outputCap},
		OriginModelName: "test-model", UsingGroup: "gpt", PriceData: price,
	}
	require.NoError(t, PrepareMembershipBudget(ctx, info, &price, 10, &relaytypes.TokenCountMeta{MaxTokens: 10}))
	apiErr := PreConsumeBilling(ctx, 4, info)
	require.NotNil(t, apiErr)
	require.ErrorIs(t, apiErr, model.ErrTaskBillingInsufficientToken)
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	assert.Equal(t, relaytypes.ErrorCodePreConsumeTokenQuotaFailed, apiErr.GetErrorCode())
	assert.Contains(t, apiErr.Error(), "API key cannot authorize this request")
	assert.NotContains(t, apiErr.Error(), token.Key)

	self, err := model.GetMembershipSelf(user.Id)
	require.NoError(t, err)
	require.NotNil(t, self.Account)
	assert.EqualValues(t, 100, self.Account.Available())
	require.NoError(t, model.DB.First(&token, token.Id).Error)
	assert.Equal(t, 3, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)
	var wallet model.User
	require.NoError(t, model.DB.First(&wallet, user.Id).Error)
	assert.EqualValues(t, 10000, wallet.Quota)
	var billingCount int64
	require.NoError(t, model.DB.Model(&model.MembershipBillingRecord{}).Where("request_id = ?", info.RequestId).Count(&billingCount).Error)
	assert.Zero(t, billingCount)
}

func TestMembershipReservationUsesLatestPublishedDiscountForExistingMember(t *testing.T) {
	for _, tc := range []struct {
		name        string
		coefficient float64
		quota       int64
	}{
		{name: "lower price", coefficient: 0.25, quota: 5},
		{name: "higher price", coefficient: 0.625, quota: 13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, user := setupMembershipServiceTest(t)
			info := &relaycommon.RelayInfo{UserId: user.Id, RequestId: "latest-discount", UsingGroup: "gpt", IsPlayground: true, OriginModelName: "test-model", PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.2}}}
			stale, err := PrepareMembershipReservation(info, 100)
			require.NoError(t, err)
			catalog, err := model.GetMembershipCatalog()
			require.NoError(t, err)
			catalog.Tiers[0].Discounts["gpt"] = tc.coefficient
			catalog, err = model.PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
			require.NoError(t, err)

			_, err = model.ReserveMembershipBilling(stale)
			require.ErrorIs(t, err, model.ErrMembershipQuoteChanged, "a new request cannot authorize a quote prepared before publication")
			input, err := PrepareMembershipReservation(info, 100)
			require.NoError(t, err)
			assert.Equal(t, catalog.ReleaseID, input.ExpectedReleaseID)
			assert.NotEqual(t, stale.ExpectedReleaseID, input.ExpectedReleaseID)
			assert.Equal(t, tc.quota, input.SubscriptionQuota)
			assert.EqualValues(t, 20, input.WalletQuota, "membership publication does not change the wallet quote")
			record, err := model.ReserveMembershipBilling(input)
			require.NoError(t, err)
			require.Equal(t, model.MembershipSource, record.Source)
			require.NoError(t, ApplyMembershipReservation(info, record))
			assert.Equal(t, catalog.ReleaseID, info.MembershipPricing.ReleaseID)
			assert.Equal(t, tc.coefficient, info.MembershipPricing.Discount)
			assert.InDelta(t, 0.2*tc.coefficient, info.PriceData.GroupRatioInfo.GroupRatio, 1e-9)
		})
	}
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

func TestMembershipBudgetUsesRequestEstimateWithoutModelLimits(t *testing.T) {
	ctx, user := setupMembershipServiceTest(t)
	price := types.PriceData{ModelRatio: 1, CompletionRatio: 2}
	outputCap := uint(10)
	info := &relaycommon.RelayInfo{UserId: user.Id, RelayMode: relayconstant.RelayModeResponses, Request: &dto.OpenAIResponsesRequest{MaxOutputTokens: &outputCap}, OriginModelName: "test-model"}
	require.NoError(t, PrepareMembershipBudget(ctx, info, &price, 1, &relaytypes.TokenCountMeta{MaxTokens: 10}))
	require.NotNil(t, info.MembershipPricing)
	assert.Equal(t, 21.0, info.MembershipPricing.MaximumBeforeGroup-info.MembershipPricing.ViolationFeeBeforeGroup)
	assert.Equal(t, 1, info.MembershipPricing.MaxInputTokens)
	unknown := &relaycommon.RelayInfo{UserId: user.Id, RelayMode: relayconstant.RelayModeResponses, Request: &dto.OpenAIResponsesRequest{MaxOutputTokens: &outputCap}, OriginModelName: "unknown-model"}
	require.NoError(t, PrepareMembershipBudget(ctx, unknown, &price, 1, &relaytypes.TokenCountMeta{MaxTokens: 10}))
	assert.Equal(t, 21.0, unknown.MembershipPricing.MaximumBeforeGroup-unknown.MembershipPricing.ViolationFeeBeforeGroup)
	require.NoError(t, PrepareMembershipBudget(ctx, info, &price, 101, &relaytypes.TokenCountMeta{MaxTokens: 10}))
	assert.Equal(t, 121.0, info.MembershipPricing.MaximumBeforeGroup-info.MembershipPricing.ViolationFeeBeforeGroup)
}
