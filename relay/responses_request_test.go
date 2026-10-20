package relay

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPrepareResponsesCompactUsesCapableAdaptors(t *testing.T) {
	for _, test := range []struct {
		name        string
		channelType int
		allowed     bool
	}{
		{"openai", constant.ChannelTypeOpenAI, true},
		{"codex", constant.ChannelTypeCodex, true},
		{"newapi", constant.ChannelTypeNewAPI, true},
		{"sub2api", constant.ChannelTypeSub2API, true},
		{"advanced_custom", constant.ChannelTypeAdvancedCustom, true},
		{"gemini", constant.ChannelTypeGemini, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(`{"model":"gpt-5","input":[]}`))
			common.SetContextKey(c, constant.ContextKeyChannelType, test.channelType)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://provider.example")
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-5")
			if test.channelType == constant.ChannelTypeAdvancedCustom {
				common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
					AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
						IncomingPath: "/v1/responses/compact", UpstreamPath: "/v1/responses/compact", Converter: "none",
					}}},
				})
			}
			request := &dto.OpenAIResponsesRequest{Model: "gpt-5", Input: []byte(`[]`)}
			info := &relaycommon.RelayInfo{Request: request, RelayMode: relayconstant.RelayModeResponsesCompact,
				RelayFormat: types.RelayFormatOpenAIResponsesCompaction, OriginModelName: "gpt-5",
				RequestURLPath: "/v1/responses/compact", UsingGroup: "default"}
			adaptor, body, closer, apiErr := PrepareResponsesRequest(c, info, request)
			if !test.allowed {
				require.NotNil(t, apiErr)
				assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
				assert.True(t, types.IsSkipRetryError(apiErr))
				assert.Nil(t, body)
				return
			}
			require.Nil(t, apiErr)
			t.Cleanup(func() { require.NoError(t, closer.Close()) })
			payload, err := io.ReadAll(body)
			require.NoError(t, err)
			assert.Contains(t, string(payload), `"model":"gpt-5"`)
			requestURL, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			assert.True(t, strings.HasSuffix(requestURL, "/responses/compact"), requestURL)
		})
	}
}

func TestPrepareTenantResponsesRequestForwardsAuthorizedDefault(t *testing.T) {
	setupTenantResponsesLimitCatalog(t)
	settings := model_setting.GetGlobalSettings()
	previousPassThrough := settings.PassThroughRequestEnabled
	settings.PassThroughRequestEnabled = false
	t.Cleanup(func() { settings.PassThroughRequestEnabled = previousPassThrough })
	for _, tc := range []struct {
		name, body  string
		passThrough bool
		override    map[string]any
		wantInvalid bool
	}{
		{name: "converted default", body: `{"model":"tenant-text","input":"hello"}`},
		{name: "passthrough default", body: `{"model":"tenant-text","input":"hello","client_extension":{"enabled":true}}`, passThrough: true},
		{name: "passthrough null", body: `{"model":"tenant-text","input":"hello","max_output_tokens":null,"client_extension":{"enabled":true}}`, passThrough: true},
		{name: "passthrough explicit cap", body: `{"model":"tenant-text","input":"hello","max_output_tokens":10,"client_extension":{"enabled":true}}`, passThrough: true},
		{name: "override deletes cap", body: `{"model":"tenant-text","input":"hello"}`, override: map[string]any{"operations": []any{map[string]any{"path": "max_output_tokens", "mode": "delete"}}}, wantInvalid: true},
		{name: "override raises cap", body: `{"model":"tenant-text","input":"hello"}`, override: map[string]any{"max_output_tokens": 41}, wantInvalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://provider.invalid")
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "tenant-text")
			common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: tc.passThrough})
			common.SetContextKey(c, constant.ContextKeyChannelParamOverride, tc.override)
			request := &dto.OpenAIResponsesRequest{}
			require.NoError(t, common.UnmarshalBodyReusable(c, request))
			info := &relaycommon.RelayInfo{TenantID: 7, Request: request, RelayMode: relayconstant.RelayModeResponses, RelayFormat: types.RelayFormatOpenAIResponses, OriginModelName: "tenant-text", RequestURLPath: "/v1/responses", UsingGroup: "default"}
			price := hosttypes.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}}
			meta := request.GetTokenCountMeta()
			require.NoError(t, prepareTenantResponsesOutputLimit(info, &price, meta))
			require.NoError(t, service.PrepareTenantBudget(info, &price, 1, meta))
			_, body, closer, apiErr := PrepareResponsesRequest(c, info, request)
			require.Nil(t, apiErr)
			t.Cleanup(func() { require.NoError(t, closer.Close()) })
			payload, err := io.ReadAll(body)
			require.NoError(t, err)
			outbound, err := http.NewRequest(http.MethodPost, "https://provider.invalid/v1/responses", bytes.NewReader(payload))
			require.NoError(t, err)
			outbound.Header.Set("Content-Type", "application/json")
			err = service.ValidateTenantOutbound(outbound, info)
			if tc.wantInvalid {
				require.Error(t, err, "parameter overrides cannot remove or raise the authorized cap")
				return
			}
			require.NoError(t, err)
			assert.EqualValues(t, *request.MaxOutputTokens, gjson.GetBytes(payload, "max_output_tokens").Uint())
			if tc.passThrough {
				assert.True(t, gjson.GetBytes(payload, "client_extension.enabled").Bool(), "unknown raw fields survive default injection")
				storage, err := common.GetBodyStorage(c)
				require.NoError(t, err)
				original, err := storage.Bytes()
				require.NoError(t, err)
				assert.Equal(t, tc.body, string(original), "the original client body remains unchanged")
			}
		})
	}
}
