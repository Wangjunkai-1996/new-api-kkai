package relay

import (
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
	"github.com/QuantumNous/new-api/setting/model_setting"
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

func TestPrepareTenantResponsesRequestPreservesPlatformSemantics(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previousPassThrough := settings.PassThroughRequestEnabled
	settings.PassThroughRequestEnabled = false
	t.Cleanup(func() { settings.PassThroughRequestEnabled = previousPassThrough })
	for _, tc := range []struct {
		name, body  string
		passThrough bool
		override    map[string]any
	}{
		{name: "converted omitted limit", body: `{"model":"tenant-text","input":"hello"}`},
		{name: "passthrough omitted limit", body: `{"model":"tenant-text","input":"hello","client_extension":{"enabled":true}}`, passThrough: true},
		{name: "passthrough null", body: `{"model":"tenant-text","input":"hello","max_output_tokens":null,"client_extension":{"enabled":true}}`, passThrough: true},
		{name: "passthrough explicit cap", body: `{"model":"tenant-text","input":"hello","max_output_tokens":10,"client_extension":{"enabled":true}}`, passThrough: true},
		{name: "normal override deletes cap", body: `{"model":"tenant-text","input":"hello","max_output_tokens":10}`, override: map[string]any{"operations": []any{map[string]any{"path": "max_output_tokens", "mode": "delete"}}}},
		{name: "normal override sets cap", body: `{"model":"tenant-text","input":"hello"}`, override: map[string]any{"max_output_tokens": 41}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var platformPayload []byte
			for _, tenant := range []int64{0, 7} {
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
				info := &relaycommon.RelayInfo{TenantID: tenant, Request: request, RelayMode: relayconstant.RelayModeResponses, RelayFormat: types.RelayFormatOpenAIResponses, OriginModelName: "tenant-text", RequestURLPath: "/v1/responses", UsingGroup: "default"}
				_, body, closer, apiErr := PrepareResponsesRequest(c, info, request)
				require.Nil(t, apiErr)
				payload, err := io.ReadAll(body)
				require.NoError(t, err)
				require.NoError(t, closer.Close())
				if tenant == 0 {
					platformPayload = payload
					continue
				}
				assert.JSONEq(t, string(platformPayload), string(payload), "tenant does not invent output limits or change channel override semantics")
				if tc.passThrough {
					assert.Equal(t, tc.body, string(payload))
					assert.True(t, gjson.GetBytes(payload, "client_extension.enabled").Bool())
				}
			}
		})
	}
}
