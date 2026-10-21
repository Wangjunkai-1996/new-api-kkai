package relay

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTenantRequestBilling(t *testing.T) (model.User, model.Token) {
	t.Helper()
	db := setupRelayChannelDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Tenant{}, &model.TenantCatalogEntry{}, &model.TenantPriceVersion{}, &model.TenantConsumption{}, &model.PricingQuote{}, &model.CustomerPaymentLot{}, &model.ResellerWallet{}, &model.ResellerLedgerEntry{}, &model.Ability{}, &model.Model{}, &model.Vendor{}))
	previousOpen, previousGroups, previousModels := setting.UserUsableGroups2JSONString(), ratio_setting.GroupRatio2JSONString(), ratio_setting.ModelRatio2JSONString()
	previousPrice, previousUnit, previousCount := operation_setting.Price, common.QuotaPerUnit, constant.CountToken
	previousRedis := common.RedisEnabled
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousOpen))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroups))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousModels))
		operation_setting.Price, common.QuotaPerUnit, constant.CountToken = previousPrice, previousUnit, previousCount
		common.RedisEnabled = previousRedis
		model.InvalidatePricingCache()
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"tenant-text":1}`))
	operation_setting.Price, common.QuotaPerUnit, constant.CountToken = 1, 100_000, false
	common.RedisEnabled = false
	require.NoError(t, db.Create(&model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "tenant-supply", Models: "tenant-text", Group: "default"}).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: 1, Model: "tenant-text", Group: "default", Enabled: true}).Error)
	model.InvalidatePricingCache()
	user := model.User{Username: "request-billing-customer", TenantID: 42, Status: common.UserStatusEnabled, Quota: 100_000}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, TenantID: 42, Key: "request-billing-key", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100_000}
	require.NoError(t, db.Create(&token).Error)
	require.NoError(t, db.Create(&model.Tenant{ID: 42, Status: model.TenantStatusActive}).Error)
	require.NoError(t, db.Create(&model.ResellerWallet{TenantID: 42, Version: 1, ExactProfitCarry: "0"}).Error)
	_, err := model.CreateCustomerPaymentLotTx(db, model.ResellerPaymentLotInput{TenantID: 42, CustomerID: user.Id, TopUpID: 1, OperationID: "request-billing-payment", PaidMinor: 100, PaymentFeeMinor: 2, CreditedQuota: 100_000})
	require.NoError(t, err)
	return user, token
}

func TestPrepareTenantRequestBillingUsesPlatformProtocolWithoutMembershipCatalog(t *testing.T) {
	user, token := setupTenantRequestBilling(t)
	for index, tc := range []struct {
		name, body string
		format     types.RelayFormat
		mode       int
		channel    int
		media      bool
	}{
		{name: "chat omitted output limit", body: `{"model":"tenant-text","messages":[{"role":"user","content":"hello"}]}`, format: types.RelayFormatOpenAI, mode: relayconstant.RelayModeChatCompletions, channel: constant.ChannelTypeOpenAI},
		{name: "chat explicit zero", body: `{"model":"tenant-text","messages":[{"role":"user","content":"hello"}],"max_tokens":0}`, format: types.RelayFormatOpenAI, mode: relayconstant.RelayModeChatCompletions, channel: constant.ChannelTypeOpenAI},
		{name: "chat screenshot and function", body: `{"model":"tenant-text","messages":[{"role":"user","content":[{"type":"text","text":"Explain"},{"type":"image_url","image_url":{"url":"https://example.invalid/screenshot.png"}}]}],"tools":[{"type":"function","function":{"name":"web_search","parameters":{"type":"object"}}}]}`, format: types.RelayFormatOpenAI, mode: relayconstant.RelayModeChatCompletions, channel: constant.ChannelTypeOpenAI, media: true},
		{name: "responses screenshot", body: `{"model":"tenant-text","input":[{"role":"user","content":[{"type":"input_text","text":"Explain"},{"type":"input_image","image_url":"https://example.invalid/screenshot.png"}]}]}`, format: types.RelayFormatOpenAIResponses, mode: relayconstant.RelayModeResponses, channel: constant.ChannelTypeOpenAI, media: true},
		{name: "Codex additional tools", body: `{"model":"tenant-text","input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"local_lookup","parameters":{"type":"object","properties":{"image":{"type":"string"}}}},{"type":"custom","name":"apply_patch"}]}]},{"role":"user","content":"hello"}]}`, format: types.RelayFormatOpenAIResponses, mode: relayconstant.RelayModeResponses, channel: constant.ChannelTypeCodex},
		{name: "responses hosted search without extra cap", body: `{"model":"tenant-text","input":"hello","tools":[{"type":"web_search"}]}`, format: types.RelayFormatOpenAIResponses, mode: relayconstant.RelayModeResponses, channel: constant.ChannelTypeOpenAI},
		{name: "responses refusal and opaque prior history", body: `{"model":"tenant-text","input":[{"type":"compaction","encrypted_content":"opaque-existing-history"},{"role":"assistant","content":[{"type":"refusal","refusal":"I cannot help with that."}]},{"role":"user","content":"Continue"}]}`, format: types.RelayFormatOpenAIResponses, mode: relayconstant.RelayModeResponses, channel: constant.ChannelTypeOpenAI},
		{name: "Gemini media and search", body: `{"contents":[{"parts":[{"text":"Explain"},{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}]}],"tools":[{"googleSearch":{}}]}`, format: types.RelayFormatGemini, mode: relayconstant.RelayModeGemini, channel: constant.ChannelTypeGemini, media: true},
		{name: "Claude hosted search", body: `{"model":"tenant-text","max_tokens":512,"messages":[{"role":"user","content":"hello"}],"tools":[{"type":"web_search_20250305","name":"web_search"}]}`, format: types.RelayFormatClaude, mode: relayconstant.RelayModeChatCompletions, channel: constant.ChannelTypeAnthropic},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var request dto.Request
			switch tc.format {
			case types.RelayFormatOpenAIResponses:
				request = &dto.OpenAIResponsesRequest{}
			case types.RelayFormatGemini:
				request = &dto.GeminiChatRequest{}
			case types.RelayFormatClaude:
				request = &dto.ClaudeRequest{}
			default:
				request = &dto.GeneralOpenAIRequest{}
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			require.NoError(t, common.UnmarshalBodyReusable(c, request))
			original, err := common.Marshal(request)
			require.NoError(t, err)
			meta := request.GetTokenCountMeta()
			if tc.media {
				require.NotEmpty(t, meta.Files, "fixture exercises the previous media rejection")
			}
			apiType, _ := common.ChannelType2APIType(tc.channel)
			info := &relaycommon.RelayInfo{UserId: user.Id, UserGroup: "default", TokenId: token.Id, RequestId: fmt.Sprintf("tenant-protocol-%d", index), Request: request, OriginModelName: "tenant-text", RelayFormat: tc.format, RelayMode: tc.mode, UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelType: tc.channel, ApiType: apiType}}
			platformInfo := *info
			platformPrice, err := helper.ModelPriceHelper(c, &platformInfo, 0, meta)
			require.NoError(t, err)
			common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
			prepared := false
			require.Nil(t, PrepareRequestBilling(c, info, func() error { prepared = true; return nil }))
			assert.True(t, prepared)
			require.NotNil(t, info.Billing)
			assert.True(t, info.HasDurableBilling())
			assert.Equal(t, platformPrice.ModelRatio, info.PriceData.ModelRatio)
			assert.Equal(t, platformPrice.GroupRatioInfo, info.PriceData.GroupRatioInfo)
			assert.Equal(t, max(platformPrice.QuotaToPreConsume, 1), info.Billing.GetPreConsumedQuota(), "tenant uses platform estimate, not a configured aggregate cap")
			after, err := common.Marshal(request)
			require.NoError(t, err)
			assert.JSONEq(t, string(original), string(after), "billing cannot add output or hosted-tool limits")
			storage, err := common.GetBodyStorage(c)
			require.NoError(t, err)
			raw, err := storage.Bytes()
			require.NoError(t, err)
			assert.Equal(t, tc.body, string(raw))
			outbound := httptest.NewRequest(http.MethodPost, "https://provider.invalid/v1/responses", strings.NewReader(tc.body))
			require.NoError(t, service.ValidateTenantOutbound(outbound, info))
			var reservation model.TenantConsumption
			require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&reservation).Error)
			assert.Equal(t, "reserved", reservation.Status)
			assert.EqualValues(t, info.Billing.GetPreConsumedQuota(), reservation.ReservedQuota)
			info.Billing.Refund(c)
			require.NoError(t, model.DB.First(&reservation, reservation.ID).Error)
			assert.Equal(t, "refunded", reservation.Status)
		})
	}
	assert.False(t, model.DB.Migrator().HasTable(&model.MembershipPricingRelease{}), "ordinary tenant requests need no membership release")
}

func TestPrepareTenantRequestBillingRejectsUnaffordableResponsesHistory(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		arguments   bool
	}{
		{name: "assistant output text", input: `[{"role":"assistant","content":[{"type":"output_text","text":%s}]}]`},
		{name: "function arguments", input: `[{"type":"function_call","name":"lookup","call_id":"call_1","arguments":%s}]`, arguments: true},
		{name: "function output string", input: `[{"type":"function_call_output","call_id":"call_1","output":%s}]`},
		{name: "function output array", input: `[{"type":"function_call_output","call_id":"call_1","output":[{"type":"input_text","text":%s}]}]`},
		{name: "custom tool input", input: `[{"type":"custom_tool_call","name":"shell","call_id":"call_1","input":%s}]`},
		{name: "custom tool output string", input: `[{"type":"custom_tool_call_output","call_id":"call_1","output":%s}]`},
		{name: "custom tool output array", input: `[{"type":"custom_tool_call_output","call_id":"call_1","output":[{"type":"input_text","text":%s}]}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user, token := setupTenantRequestBilling(t)
			constant.CountToken = true
			previousMultiplier := operation_setting.GetQuotaSetting().PreConsumeMultiplier
			operation_setting.GetQuotaSetting().PreConsumeMultiplier = 1
			t.Cleanup(func() { operation_setting.GetQuotaSetting().PreConsumeMultiplier = previousMultiplier })
			require.NoError(t, model.DB.First(&user, user.Id).Error)
			require.NoError(t, model.DB.First(&token, token.Id).Error)
			var originalLot model.CustomerPaymentLot
			require.NoError(t, model.DB.Where("customer_id = ?", user.Id).First(&originalLot).Error)

			for _, size := range []struct {
				name, text   string
				unaffordable bool
			}{
				{name: "unaffordable", text: strings.Repeat("文", 200_000), unaffordable: true},
				{name: "affordable", text: "hello"},
			} {
				t.Run(size.name, func(t *testing.T) {
					encoded, err := common.Marshal(size.text)
					require.NoError(t, err)
					if tc.arguments {
						encoded, err = common.Marshal(`{"query":` + string(encoded) + `}`)
						require.NoError(t, err)
					}
					body := fmt.Sprintf(`{"model":"tenant-text","input":`+tc.input+`}`, encoded)
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
					c.Request.Header.Set("Content-Type", "application/json")
					t.Cleanup(func() { common.CleanupBodyStorage(c) })
					common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
					common.SetContextKey(c, constant.ContextKeyOriginalModel, "tenant-text")
					request := &dto.OpenAIResponsesRequest{}
					require.NoError(t, common.UnmarshalBodyReusable(c, request))
					original, err := common.Marshal(request)
					require.NoError(t, err)
					info := &relaycommon.RelayInfo{UserId: user.Id, UserGroup: "default", TokenId: token.Id, RequestId: "history-" + size.name, Request: request, OriginModelName: "tenant-text", RelayFormat: types.RelayFormatOpenAIResponses, RelayMode: relayconstant.RelayModeResponses, UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, ApiType: constant.APITypeOpenAI}}
					apiErr := PrepareRequestBilling(c, info, nil)
					assert.Positive(t, info.GetEstimatePromptTokens())
					if size.unaffordable {
						require.NotNil(t, apiErr)
						assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
						assert.Greater(t, int64(info.PriceData.QuotaToPreConsume), user.Quota)
						assert.Nil(t, info.Billing)
						assert.Zero(t, info.TenantQuoteID)
						var consumptions, quotes int64
						require.NoError(t, model.DB.Model(&model.TenantConsumption{}).Count(&consumptions).Error)
						require.NoError(t, model.DB.Model(&model.PricingQuote{}).Count(&quotes).Error)
						assert.Zero(t, consumptions)
						assert.Zero(t, quotes)
						var afterUser model.User
						var afterToken model.Token
						require.NoError(t, model.DB.First(&afterUser, user.Id).Error)
						require.NoError(t, model.DB.First(&afterToken, token.Id).Error)
						assert.Equal(t, user, afterUser)
						assert.Equal(t, token, afterToken)
					} else {
						require.Nil(t, apiErr)
						require.NotNil(t, info.Billing)
						assert.True(t, info.HasDurableBilling())
						reserved := info.Billing.GetPreConsumedQuota()
						assert.Positive(t, reserved)
						assert.Less(t, int64(reserved), user.Quota)
						var record model.TenantConsumption
						require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&record).Error)
						assert.Equal(t, "reserved", record.Status)
						assert.EqualValues(t, reserved, record.ReservedQuota)
						var quote model.PricingQuote
						require.NoError(t, model.DB.First(&quote, record.QuoteID).Error)
						assert.Equal(t, info.TenantQuoteID, quote.ID)
						assert.EqualValues(t, reserved, quote.QuotedQuota)
						var afterUser model.User
						var afterToken model.Token
						require.NoError(t, model.DB.First(&afterUser, user.Id).Error)
						require.NoError(t, model.DB.First(&afterToken, token.Id).Error)
						assert.Equal(t, user.Quota-int64(reserved), afterUser.Quota)
						assert.Equal(t, token.RemainQuota-reserved, afterToken.RemainQuota)
						info.Billing.Refund(c)
						require.NoError(t, model.DB.First(&record, record.ID).Error)
						assert.Equal(t, "refunded", record.Status)
						require.NoError(t, model.DB.First(&afterUser, user.Id).Error)
						require.NoError(t, model.DB.First(&afterToken, token.Id).Error)
						assert.Equal(t, user.Quota, afterUser.Quota)
						assert.Equal(t, user.UsedQuota, afterUser.UsedQuota)
						assert.Equal(t, user.RequestCount, afterUser.RequestCount)
						assert.Equal(t, token.RemainQuota, afterToken.RemainQuota)
						assert.Equal(t, token.UsedQuota, afterToken.UsedQuota)
					}
					var afterLot model.CustomerPaymentLot
					require.NoError(t, model.DB.First(&afterLot, originalLot.ID).Error)
					assert.Equal(t, originalLot, afterLot)
					after, err := common.Marshal(request)
					require.NoError(t, err)
					assert.True(t, string(original) == string(after), "billing must preserve request serialization")
					storage, err := common.GetBodyStorage(c)
					require.NoError(t, err)
					raw, err := storage.Bytes()
					require.NoError(t, err)
					assert.True(t, body == string(raw), "billing must preserve the raw request")
				})
			}
		})
	}
}

func TestTenantZeroBasePriceFundingIncludesAuxiliaryCharges(t *testing.T) {
	for _, preConsumeFree := range []bool{false, true} {
		for _, tc := range []struct {
			name, body, model string
			format            types.RelayFormat
			mode, channel     int
			paid              bool
			override          map[string]any
		}{
			{name: "plain text", body: `{"model":"tenant-text","input":"hello"}`, format: types.RelayFormatOpenAIResponses, mode: relayconstant.RelayModeResponses, channel: constant.ChannelTypeOpenAI},
			{name: "schema properties are not tool declarations", body: `{"model":"tenant-text","tools":[{"type":"function","name":"free_lookup","parameters":{"type":"object","properties":{"tools":{"type":"array","default":[{"type":"web_search"}]},"name":{"const":"paid_lookup"}}}}],"input":"hello"}`, format: types.RelayFormatOpenAIResponses, mode: relayconstant.RelayModeResponses, channel: constant.ChannelTypeOpenAI},
			{name: "caller reserved name stays free", body: `{"model":"tenant-text","tools":[{"type":"function","name":"web_search"}],"input":"hello"}`, format: types.RelayFormatOpenAIResponses, mode: relayconstant.RelayModeResponses, channel: constant.ChannelTypeOpenAI},
			{name: "nested caller function is paid", body: `{"model":"tenant-text","input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"functions","tools":[{"type":"namespace","name":"nested","tools":[{"type":"function","name":"paid_lookup"}]}]}]},{"role":"user","content":"hello"}]}`, format: types.RelayFormatOpenAIResponses, mode: relayconstant.RelayModeResponses, channel: constant.ChannelTypeCodex, paid: true},
			{name: "hosted search is paid", body: `{"model":"tenant-text","tools":[{"type":"web_search"}],"input":"hello"}`, format: types.RelayFormatOpenAIResponses, mode: relayconstant.RelayModeResponses, channel: constant.ChannelTypeOpenAI, paid: true},
			{name: "implicit search is paid", body: `{"model":"tenant-search-preview","messages":[{"role":"user","content":"hello"}]}`, model: "tenant-search-preview", format: types.RelayFormatOpenAI, mode: relayconstant.RelayModeChatCompletions, channel: constant.ChannelTypeOpenAI, paid: true},
			{name: "Gemini audio has independent price", body: `{"contents":[{"parts":[{"inlineData":{"mimeType":"audio/wav","data":"aGVsbG8="}}]}]}`, model: "gemini-2.5-flash", format: types.RelayFormatGemini, mode: relayconstant.RelayModeGemini, channel: constant.ChannelTypeGemini, paid: true},
			{name: "Gemini text stays free", body: `{"contents":[{"parts":[{"text":"hello"}]}]}`, model: "gemini-2.5-flash", format: types.RelayFormatGemini, mode: relayconstant.RelayModeGemini, channel: constant.ChannelTypeGemini},
			{name: "opaque prior response needs paid funding", body: `{"model":"tenant-text","input":"hello","previous_response_id":"resp_existing"}`, format: types.RelayFormatOpenAIResponses, mode: relayconstant.RelayModeResponses, channel: constant.ChannelTypeOpenAI, paid: true},
			{name: "opaque Gemini cache needs paid funding", body: `{"contents":[{"parts":[{"text":"hello"}]}],"cachedContent":"cachedContents/existing"}`, model: "gemini-2.5-flash", format: types.RelayFormatGemini, mode: relayconstant.RelayModeGemini, channel: constant.ChannelTypeGemini, paid: true},
			{name: "channel override can add paid tools", body: `{"model":"tenant-text","input":"hello"}`, format: types.RelayFormatOpenAIResponses, mode: relayconstant.RelayModeResponses, channel: constant.ChannelTypeOpenAI, override: map[string]any{"tools": []any{map[string]any{"type": "web_search"}}}, paid: true},
		} {
			t.Run(fmt.Sprintf("preconsume_free_%t/%s", preConsumeFree, tc.name), func(t *testing.T) {
				user, token := setupTenantRequestBilling(t)
				previousFree := operation_setting.GetQuotaSetting().EnableFreeModelPreConsume
				operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = preConsumeFree
				t.Cleanup(func() { operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = previousFree })
				operation_setting.SetToolPriceForTest("paid_lookup", 5)
				t.Cleanup(func() { operation_setting.DeleteToolPriceForTest("paid_lookup") })
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"tenant-text":0,"tenant-search-preview":0,"gemini-2.5-flash":0}`))
				modelName := tc.model
				if modelName == "" {
					modelName = "tenant-text"
				}
				require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 1).Updates(map[string]any{"type": tc.channel, "models": modelName}).Error)
				require.NoError(t, model.DB.Model(&model.Ability{}).Where("channel_id = ?", 1).Update("model", modelName).Error)
				model.InvalidatePricingCache()
				if !tc.paid {
					require.NoError(t, model.DB.Model(&user).Update("quota", 0).Error)
					require.NoError(t, model.DB.Model(&token).Update("remain_quota", 0).Error)
				}
				var request dto.Request = &dto.OpenAIResponsesRequest{}
				if tc.format == types.RelayFormatGemini {
					request = &dto.GeminiChatRequest{}
				} else if tc.format == types.RelayFormatOpenAI {
					request = &dto.GeneralOpenAIRequest{}
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(tc.body))
				c.Request.Header.Set("Content-Type", "application/json")
				t.Cleanup(func() { common.CleanupBodyStorage(c) })
				require.NoError(t, common.UnmarshalBodyReusable(c, request))
				common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
				apiType, _ := common.ChannelType2APIType(tc.channel)
				info := &relaycommon.RelayInfo{UserId: user.Id, TokenId: token.Id, RequestId: "zero-base-funding", OriginModelName: modelName, Request: request, RelayFormat: tc.format, RelayMode: tc.mode, UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelType: tc.channel, ApiType: apiType, ParamOverride: tc.override}}
				require.Nil(t, PrepareRequestBilling(c, info, nil))
				assert.Equal(t, !tc.paid, info.PriceData.FreeModel)
				require.NotNil(t, info.Billing)
				if tc.paid {
					assert.Equal(t, 1, info.Billing.GetPreConsumedQuota())
					require.NoError(t, info.Billing.Settle(500), "a zero base model price does not erase positive tool/audio usage")
				} else {
					assert.Zero(t, info.Billing.GetPreConsumedQuota())
					require.NoError(t, info.Billing.Settle(0), "truly free requests work with no customer balance")
				}
				var record model.TenantConsumption
				require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&record).Error)
				assert.Equal(t, "settled", record.Status)
			})
		}
	}
}

func TestTenantLargeResponsesBodyPreservesThirtyMiBPayload(t *testing.T) {
	user, token := setupTenantRequestBilling(t)
	body := `{"model":"tenant-text","input":"` + strings.Repeat("x", 30<<20) + `"}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.UnmarshalBodyReusable(c, request))
	info := &relaycommon.RelayInfo{UserId: user.Id, TokenId: token.Id, RequestId: "thirty-mib", OriginModelName: "tenant-text", Request: request, RelayFormat: types.RelayFormatOpenAIResponses, RelayMode: relayconstant.RelayModeResponses, UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, ApiType: constant.APITypeOpenAI}}
	require.Nil(t, PrepareRequestBilling(c, info, nil))
	storage, err := common.GetBodyStorage(c)
	require.NoError(t, err)
	raw, err := storage.Bytes()
	require.NoError(t, err)
	assert.Equal(t, body, string(raw))
	assert.Len(t, request.Input, (30<<20)+2)
	info.Billing.Refund(c)
}

func TestTenantCallerToolActualChargeUsesReservedPrice(t *testing.T) {
	user, token := setupTenantRequestBilling(t)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"tenant-text":0}`))
	const tool = "reserved_price_lookup"
	operation_setting.SetToolPriceForTest(tool, 5)
	t.Cleanup(func() { operation_setting.DeleteToolPriceForTest(tool) })
	body := `{"model":"tenant-text","input":"hello","tools":[{"type":"function","name":"reserved_price_lookup"}]}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	common.SetContextKey(c, constant.ContextKeyTenantID, int64(42))
	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.UnmarshalBodyReusable(c, request))
	info := &relaycommon.RelayInfo{UserId: user.Id, TokenId: token.Id, RequestId: "frozen-tool-actual", OriginModelName: "tenant-text", Request: request, RelayFormat: types.RelayFormatOpenAIResponses, RelayMode: relayconstant.RelayModeResponses, UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, ApiType: constant.APITypeOpenAI}}
	require.Nil(t, PrepareRequestBilling(c, info, nil))
	operation_setting.SetToolPriceForTest(tool, 0)
	common.QuotaPerUnit *= 10 // Both price and quota conversion retain the quote.
	info.CountBillableToolCall(dto.BuildInCallFunctionCall, tool)
	previousLog := model.LOG_DB
	model.LOG_DB = model.DB
	t.Cleanup(func() { model.LOG_DB = previousLog })
	require.NoError(t, model.DB.AutoMigrate(&model.Log{}))
	service.PostTextConsumeQuota(c, info, &dto.Usage{PromptTokens: 1, TotalTokens: 1}, nil)
	var record model.TenantConsumption
	require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&record).Error)
	assert.Equal(t, "settled", record.Status)
	assert.EqualValues(t, 500, record.ActualQuota, "one call costs frozen $5/1k × 100000 quota/USD")
	require.NoError(t, model.DB.First(&user, user.Id).Error)
	assert.EqualValues(t, 99_500, user.Quota)
	assert.EqualValues(t, 500, user.UsedQuota)
}
