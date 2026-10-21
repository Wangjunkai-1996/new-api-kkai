package service

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
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
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTenantBudgetTest(t *testing.T) *model.User {
	t.Helper()
	// No membership tables or published catalog: ordinary tenant wallets do not
	// depend on the platform selling membership products.
	user := setupAuthSessionTestDB(t)
	previousUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 1_000_000
	t.Cleanup(func() { common.QuotaPerUnit = previousUnit })
	return user
}

func TestTenantBudgetUsesSharedEstimateWithoutMembershipConfiguration(t *testing.T) {
	user := setupTenantBudgetTest(t)
	info := &relaycommon.RelayInfo{TenantID: 7, UserId: user.Id, OriginModelName: "tenant-text", RelayMode: relayconstant.RelayModeChatCompletions, Request: &dto.GeneralOpenAIRequest{}}
	price := types.PriceData{ModelRatio: 2, CompletionRatio: 3, QuotaToPreConsume: 42, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 2}}
	require.NoError(t, PrepareTenantBudget(info, &price, 1, &relaytypes.TokenCountMeta{}))
	assert.Equal(t, 42, price.QuotaToPreConsume)
	assert.Equal(t, price, info.PriceData)
	assert.False(t, info.TenantBudget.RequiresOutputBound)
	assert.Nil(t, info.Request.(*dto.GeneralOpenAIRequest).MaxTokens)
	require.NoError(t, model.DB.AutoMigrate(&model.TenantConsumption{}))
	require.NoError(t, model.DB.Model(user).Updates(map[string]any{"tenant_id": 7, "quota": 41}).Error)
	_, err := model.ReserveTenantConsumption(context.Background(), model.DB, model.ResellerPricingQuoteInput{TenantID: 7, CustomerID: user.Id, RequestID: "underfunded-estimate", QuotedQuota: 42}, 1)
	assert.ErrorIs(t, err, model.ErrResellerInsufficientFunds)
	require.NoError(t, model.DB.First(user, user.Id).Error)
	assert.EqualValues(t, 41, user.Quota)
}

func TestTenantBudgetRejectsInvalidEstimates(t *testing.T) {
	setupTenantBudgetTest(t)
	for _, tc := range []struct {
		name                 string
		input, output, quota int
	}{
		{"negative input", -1, 0, 1},
		{"negative output", 1, -1, 1},
		{"negative reservation", 1, 0, -1},
		{"reservation overflow", 1, 0, math.MaxInt32 + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{TenantID: 7}
			price := types.PriceData{QuotaToPreConsume: tc.quota}
			assert.Error(t, PrepareTenantBudget(info, &price, tc.input, &relaytypes.TokenCountMeta{MaxTokens: tc.output}))
			assert.Nil(t, info.TenantBudget)
		})
	}
}

func TestTenantBudgetFixedPriceAndRetryStayFrozen(t *testing.T) {
	setupTenantBudgetTest(t)
	info := &relaycommon.RelayInfo{TenantID: 7, OriginModelName: "fixed-price", Request: &dto.GeneralOpenAIRequest{}}
	price := types.PriceData{UsePrice: true, ModelPrice: .0000006, QuotaToPreConsume: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 2}}
	require.NoError(t, PrepareTenantBudget(info, &price, 1, &relaytypes.TokenCountMeta{}))
	assert.Equal(t, 1, price.QuotaToPreConsume)
	common.QuotaPerUnit *= 100
	price.ModelPrice, price.GroupRatioInfo.GroupRatio, price.QuotaToPreConsume = 100, 20, 200
	require.NoError(t, PrepareTenantBudget(info, &price, 1, &relaytypes.TokenCountMeta{}))
	assert.Equal(t, 1, price.QuotaToPreConsume)
	assert.Equal(t, .0000006, price.ModelPrice)
	assert.Equal(t, float64(1_000_000), info.TenantBudget.QuotaPerUnit)
}

func TestTenantBudgetFreezesTieredRequestPricing(t *testing.T) {
	setupTenantBudgetTest(t)
	expression := `p * 2 + c * 3 + cc * 4 + (param("service_tier") == "priority" ? 10 : 0)`
	info := &relaycommon.RelayInfo{TenantID: 7, OriginModelName: "tenant-text", RelayMode: relayconstant.RelayModeResponses, Request: &dto.OpenAIResponsesRequest{}, BillingRequestInput: &billingexpr.RequestInput{Body: []byte(`{"service_tier":"priority"}`)}, TieredBillingSnapshot: &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ExprString: expression, QuotaPerUnit: common.QuotaPerUnit, GroupRatio: 2, ExprVersion: 1}}
	price := types.PriceData{QuotaToPreConsume: 7, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 2}}
	require.NoError(t, PrepareTenantBudget(info, &price, 1, &relaytypes.TokenCountMeta{}))
	assert.Equal(t, 7, price.QuotaToPreConsume, "freeze, rather than replace, the shared estimate")
	assert.NotContains(t, info.TieredBillingSnapshot.ExprString, "param(")
	info.BillingRequestInput.Body = []byte(`{"service_tier":"standard"}`)
	_, quota, _ := TryTieredSettle(info, billingexpr.TokenParams{P: 100, C: 40, CC: 100})
	assert.Equal(t, 1460, quota)
}

func TestTenantOutboundRequiresPricingAndIntegratedTransport(t *testing.T) {
	setupTenantBudgetTest(t)
	request, err := http.NewRequest(http.MethodPost, "https://upstream.invalid/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"hello"}]}`))
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{TenantID: 7, OriginModelName: "tenant-text", ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, ApiType: constant.APITypeOpenAI}}
	assert.Error(t, ValidateTenantOutbound(request, info))
	price := types.PriceData{QuotaToPreConsume: 1}
	require.NoError(t, PrepareTenantBudget(info, &price, 1, &relaytypes.TokenCountMeta{}))
	assert.NoError(t, ValidateTenantOutbound(request, info), "optional provider output caps remain optional")
	info.ChannelMeta.ChannelType = constant.ChannelTypeAws
	assert.Error(t, ValidateTenantOutbound(request, info), "an unintegrated SDK transport cannot bypass tenant accounting")
}

func TestTenantBudgetFreeGroupIsDurableAndCannotBecomePositiveCharge(t *testing.T) {
	user := setupTenantBudgetTest(t)
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.TenantConsumption{}, &model.ResellerWallet{}, &model.ResellerLedgerEntry{}))
	require.NoError(t, db.Model(user).Updates(map[string]any{"tenant_id": 7, "quota": 0}).Error)
	token := model.Token{UserId: user.Id, TenantID: 7, Key: "free-tenant", Status: common.TokenStatusEnabled, ExpiredTime: -1}
	require.NoError(t, db.Create(&token).Error)
	info := &relaycommon.RelayInfo{TenantID: 7, UserId: user.Id, TokenId: token.Id, RequestId: "free-tenant-request", ChannelMeta: &relaycommon.ChannelMeta{}, OriginModelName: "tenant-text", RelayMode: relayconstant.RelayModeChatCompletions, Request: &dto.GeneralOpenAIRequest{}}
	price := types.PriceData{FreeModel: true, ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0}}
	require.NoError(t, PrepareTenantBudget(info, &price, 1, &relaytypes.TokenCountMeta{MaxTokens: 20}))
	assert.Zero(t, price.QuotaToPreConsume)
	assert.True(t, price.FreeModel)
	assert.NotNil(t, info.TenantBudget, "free requests retain their frozen pricing")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	require.Nil(t, preConsumeTenantBilling(c, info, 0))
	assert.ErrorIs(t, info.Billing.Reserve(1), model.ErrResellerInvalidPrice)
	assert.ErrorIs(t, info.Billing.Settle(1), model.ErrResellerInvalidPrice)
	assert.ErrorIs(t, model.RecordTenantConsumptionResult(db, info.RequestId, 1, 0, false), model.ErrResellerFinanceInvalidInput)
	assert.ErrorIs(t, model.IncreaseTenantConsumptionReservation(db, info.RequestId, 1), model.ErrResellerFinanceInvalidInput)
	assert.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := model.ReconcileTenantConsumptionTx(tx, model.TenantConsumptionReconciliationInput{TenantID: 7, RequestID: info.RequestId, ActualQuota: 1, IdempotencyKey: "free-review", ConfirmedExecutionStopped: true, ActorID: 99, Reason: "reviewed", Evidence: "receipt"})
		return err
	}), model.ErrResellerFinanceInvalidInput)
	require.NoError(t, info.Billing.Settle(0))
	require.NoError(t, info.Billing.Settle(0))
	info.Billing.Refund(c)
	require.NoError(t, db.First(user, user.Id).Error)
	assert.Zero(t, user.Quota)
	assert.Zero(t, user.UsedQuota)
	assert.EqualValues(t, 1, user.RequestCount)
	var record model.TenantConsumption
	require.NoError(t, db.Where("request_id = ?", info.RequestId).First(&record).Error)
	assert.Equal(t, "settled", record.Status)
	assert.Zero(t, record.ActualQuota)
	_, err := model.ReserveTenantFreeConsumption(context.Background(), db, 7, user.Id, token.Id, info.RequestId)
	assert.ErrorIs(t, err, model.ErrTenantConsumptionConflict)
}

func TestTenantBudgetClonesRequestMultipliers(t *testing.T) {
	setupTenantBudgetTest(t)
	info := &relaycommon.RelayInfo{TenantID: 7, OriginModelName: "tenant-text", Request: &dto.GeneralOpenAIRequest{}}
	price := types.PriceData{ModelRatio: 1, QuotaToPreConsume: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}
	price.AddOtherRatio("quality", 2)
	require.NoError(t, PrepareTenantBudget(info, &price, 1, &relaytypes.TokenCountMeta{}))
	price.AddOtherRatio("quality", 3)
	info.PriceData.AddOtherRatio("quality", 4)
	assert.Equal(t, float64(2), info.TenantBudget.Price.OtherRatios()["quality"])
	assert.Equal(t, float64(2), info.TenantBudget.OtherRatios["quality"])
	require.NoError(t, PrepareTenantBudget(info, &price, 1, &relaytypes.TokenCountMeta{}))
	assert.Equal(t, float64(2), price.OtherRatios()["quality"])
	price.AddOtherRatio("quality", 5)
	assert.Equal(t, float64(2), info.TenantBudget.Price.OtherRatios()["quality"], "retry restoration cannot reconnect mutable and frozen price maps")
}

func TestTenantFreeReservationRejectsPaidFeaturesAddedAtDispatch(t *testing.T) {
	setupTenantBudgetTest(t)
	for _, tc := range []struct {
		name, body string
		group      float64
		wantError  bool
	}{
		{name: "plain free request", body: `{"input":"hello"}`, group: 1},
		{name: "hosted tool added by later conversion", body: `{"input":"hello","tools":[{"type":"web_search"}]}`, group: 1, wantError: true},
		{name: "function schema remains data", body: `{"input":"hello","tools":[{"type":"function","name":"free_lookup","parameters":{"properties":{"tools":{"default":[{"type":"web_search"}]}}}}]}`, group: 1},
		{name: "zero group remains free", body: `{"input":"hello","tools":[{"type":"web_search"}]}`, group: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{TenantID: 7, OriginModelName: "free-text", Request: &dto.OpenAIResponsesRequest{}, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, ApiType: constant.APITypeOpenAI}}
			price := types.PriceData{ModelRatio: 0, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: tc.group}}
			require.NoError(t, PrepareTenantBudget(info, &price, 0, &relaytypes.TokenCountMeta{}))
			require.True(t, price.FreeModel)
			outbound, err := http.NewRequest(http.MethodPost, "https://provider.invalid", strings.NewReader(tc.body))
			require.NoError(t, err)
			err = ValidateTenantOutbound(outbound, info)
			if tc.wantError {
				assert.ErrorContains(t, err, "adds paid features")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
