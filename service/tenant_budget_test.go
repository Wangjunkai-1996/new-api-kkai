package service

import (
	"context"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTenantBudgetTest(t *testing.T) *model.User {
	t.Helper()
	user := setupAuthSessionTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(model.MembershipModels()...))
	_, err := model.PublishMembershipCatalog(model.MembershipCatalog{ModelLimits: map[string]model.MembershipModelLimit{
		"tenant-text": {MaxInputTokens: 100, MaxOutputTokens: 40, Evidence: "provider guarantees aggregate per-request bounds"},
	}}, 0, 1)
	require.NoError(t, err)
	previousUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 1_000_000
	t.Cleanup(func() { common.QuotaPerUnit = previousUnit })
	return user
}

func TestTenantBudgetReservesCompleteUsageBeforeLowBalanceDispatch(t *testing.T) {
	user := setupTenantBudgetTest(t)
	copies := 2
	info := &relaycommon.RelayInfo{TenantID: 7, UserId: user.Id, OriginModelName: "tenant-text", RelayMode: relayconstant.RelayModeChatCompletions, Request: &dto.GeneralOpenAIRequest{N: &copies}}
	price := types.PriceData{ModelRatio: 2, CompletionRatio: 3, CacheRatio: .1, CacheCreationRatio: 1, CacheCreation5mRatio: 2, CacheCreation1hRatio: 4, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 2}}
	require.NoError(t, PrepareTenantBudget(info, &price, 1, &relaytypes.TokenCountMeta{MaxTokens: 20}))
	// One locally estimated input token must still cover the verified input
	// bound, both outputs, and independently reported cache-write categories.
	assert.GreaterOrEqual(t, price.QuotaToPreConsume, 4200)
	assert.Equal(t, price.QuotaToPreConsume, info.PriceData.QuotaToPreConsume)
	require.NoError(t, model.DB.AutoMigrate(&model.TenantConsumption{}))
	require.NoError(t, model.DB.Model(user).Updates(map[string]any{"tenant_id": 7, "quota": price.QuotaToPreConsume - 1}).Error)
	_, err := model.ReserveTenantConsumption(context.Background(), model.DB, model.ResellerPricingQuoteInput{TenantID: 7, CustomerID: user.Id, RequestID: "underfunded-large-output", QuotedQuota: int64(price.QuotaToPreConsume)}, 1)
	assert.ErrorIs(t, err, model.ErrResellerInsufficientFunds)
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.EqualValues(t, price.QuotaToPreConsume-1, user.Quota)
}

func TestTenantBudgetRejectsUnverifiableBounds(t *testing.T) {
	setupTenantBudgetTest(t)
	for _, tc := range []struct {
		name, model   string
		input, output int
		want          string
	}{
		{"missing output", "tenant-text", 1, 0, "max_tokens"},
		{"unknown provider bound", "unverified", 1, 20, "verified"},
		{"input exceeds bound", "tenant-text", 101, 20, "exceeds"},
		{"output exceeds bound", "tenant-text", 1, 41, "exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{TenantID: 7, OriginModelName: tc.model, RelayMode: relayconstant.RelayModeChatCompletions, Request: &dto.GeneralOpenAIRequest{}}
			price := types.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}
			assert.ErrorContains(t, PrepareTenantBudget(info, &price, tc.input, &relaytypes.TokenCountMeta{MaxTokens: tc.output}), tc.want)
			assert.Nil(t, info.TenantBudget)
		})
	}
}

func TestTenantBudgetFixedPriceAndRetryStayFrozen(t *testing.T) {
	setupTenantBudgetTest(t)
	info := &relaycommon.RelayInfo{TenantID: 7, OriginModelName: "fixed-price", RelayMode: relayconstant.RelayModeChatCompletions, Request: &dto.GeneralOpenAIRequest{}}
	price := types.PriceData{UsePrice: true, ModelPrice: .0000006, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 2}}
	require.NoError(t, PrepareTenantBudget(info, &price, 1, &relaytypes.TokenCountMeta{}))
	assert.Equal(t, 2, price.QuotaToPreConsume, "the complete fixed charge rounds up before dispatch")
	assert.False(t, info.TenantBudget.RequiresOutputBound)
	common.QuotaPerUnit *= 100
	price.ModelPrice = 100
	price.GroupRatioInfo.GroupRatio = 20
	require.NoError(t, PrepareTenantBudget(info, &price, 1, &relaytypes.TokenCountMeta{}))
	assert.Equal(t, 2, price.QuotaToPreConsume)
	assert.Equal(t, .0000006, price.ModelPrice)
	assert.Equal(t, float64(1_000_000), info.TenantBudget.QuotaPerUnit)
}

func TestTenantBudgetFreezesTieredRequestPricing(t *testing.T) {
	setupTenantBudgetTest(t)
	expression := `p * 2 + c * 3 + cc * 4 + (param("service_tier") == "priority" ? 10 : 0)`
	info := &relaycommon.RelayInfo{TenantID: 7, OriginModelName: "tenant-text", RelayMode: relayconstant.RelayModeResponses, Request: &dto.OpenAIResponsesRequest{}, BillingRequestInput: &billingexpr.RequestInput{Body: []byte(`{"service_tier":"priority"}`)}, TieredBillingSnapshot: &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ExprString: expression, QuotaPerUnit: common.QuotaPerUnit, ExprVersion: 1}}
	price := types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 2}}
	require.NoError(t, PrepareTenantBudget(info, &price, 1, &relaytypes.TokenCountMeta{MaxTokens: 20}))
	assert.Equal(t, 1460, price.QuotaToPreConsume)
	assert.NotContains(t, info.TieredBillingSnapshot.ExprString, "param(")
	info.BillingRequestInput.Body = []byte(`{"service_tier":"standard"}`)
	_, quota, _ := TryTieredSettle(info, billingexpr.TokenParams{P: 100, C: 40, CC: 100})
	assert.Equal(t, price.QuotaToPreConsume, quota)
}

func TestTenantBudgetRejectsUnrepresentableReservation(t *testing.T) {
	setupTenantBudgetTest(t)
	info := &relaycommon.RelayInfo{TenantID: 7, OriginModelName: "fixed-price", Request: &dto.GeneralOpenAIRequest{}}
	price := types.PriceData{UsePrice: true, ModelPrice: math.MaxInt32, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}
	assert.Error(t, PrepareTenantBudget(info, &price, 1, &relaytypes.TokenCountMeta{}))
	assert.Nil(t, info.TenantBudget)
}

func TestTenantOutboundCannotRemoveOrIncreaseAuthorization(t *testing.T) {
	setupTenantBudgetTest(t)
	info := &relaycommon.RelayInfo{TenantID: 7, OriginModelName: "tenant-text", RelayMode: relayconstant.RelayModeChatCompletions, Request: &dto.GeneralOpenAIRequest{}, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, ApiType: constant.APITypeOpenAI}}
	price := types.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}
	require.NoError(t, PrepareTenantBudget(info, &price, 1, &relaytypes.TokenCountMeta{MaxTokens: 20}))
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"bounded", `{"max_tokens":20}`, true},
		{"converted lower limit", `{"max_output_tokens":10}`, true},
		{"removed limit", `{}`, false},
		{"increased limit", `{"max_tokens":21}`, false},
		{"conflicting alternate limit", `{"max_tokens":20,"max_completion_tokens":21}`, false},
		{"duplicate output limit", `{"max_tokens":20,"max_tokens":1000}`, false},
		{"duplicate nested output limit", `{"generationConfig":{"maxOutputTokens":20,"maxOutputTokens":1000}}`, false},
		{"increased copies", `{"max_tokens":20,"n":2}`, false},
		{"fractional limit", `{"max_tokens":1.5}`, false},
		{"override adds hosted tool", `{"max_tokens":20,"tools":[{"type":"image_generation"}]}`, false},
		{"override adds media", `{"max_tokens":20,"messages":[{"content":[{"type":"image_url","image_url":{"url":"https://example.invalid/image.png"}}]}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodPost, "https://upstream.invalid/v1/chat/completions", strings.NewReader(tc.body))
			require.NoError(t, err)
			err = ValidateTenantOutbound(request, info)
			if tc.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
	assert.Error(t, ValidateTenantOutbound(nil, &relaycommon.RelayInfo{TenantID: 7}))
}
