package relay

import (
	"fmt"
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
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPrepareResponsesPassthroughPreservesRequestAcrossChannelRetries(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previousPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() { settings.PassThroughRequestEnabled = previousPassThrough })

	for _, tc := range []struct {
		name        string
		channelType int
		global      bool
		route       bool
	}{
		{name: "global openai", channelType: constant.ChannelTypeOpenAI, global: true},
		{name: "channel codex", channelType: constant.ChannelTypeCodex},
		{name: "channel newapi", channelType: constant.ChannelTypeNewAPI},
		{name: "channel sub2api", channelType: constant.ChannelTypeSub2API},
		{name: "route advanced custom", channelType: constant.ChannelTypeAdvancedCustom, route: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings.PassThroughRequestEnabled = tc.global
			const modelName = "client-model@unknown:opaque"
			const input = `{"model":"client-model@unknown:opaque","input":[{"role":"user","content":"keep"}],"tools":[{"type":"function","name":"keep","parameters":{"const":9007199254740993}}],"reasoning":{"effort":"low"},"temperature":0,"top_p":0,"stream":true,"client_extension":{"enabled":true}}`
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(input))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			common.SetContextKey(c, constant.ContextKeyOriginalModel, modelName)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://provider.invalid")
			request := &dto.OpenAIResponsesRequest{}
			require.NoError(t, common.UnmarshalBodyReusable(c, request))
			original, err := common.Marshal(request)
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{
				Request: request, RelayMode: relayconstant.RelayModeResponses,
				RelayFormat: types.RelayFormatOpenAIResponses, OriginModelName: modelName,
				RequestURLPath: "/v1/responses", UsingGroup: service.ImageGenerationTokenGroup,
			}

			for attempt, mappedModel := range []string{"first@temperature:0.2", "retry@effort:high"} {
				common.SetContextKey(c, constant.ContextKeyChannelType, tc.channelType)
				common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: !tc.global && !tc.route})
				if tc.route {
					common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
						AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
							IncomingPath: "/v1/responses", UpstreamPath: "/v1/responses", Converter: "none", PassThroughBodyEnabled: true,
						}}},
					})
				}
				if attempt == 1 {
					// The retry changes channel as well as its model mapping.
					common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeSub2API)
					common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: true})
				}
				c.Set("model_mapping", fmt.Sprintf(`{"%s":"%s"}`, modelName, mappedModel))
				adaptor, body, closer, apiErr := PrepareResponsesRequest(c, info, request)
				require.Nil(t, apiErr)
				require.NotNil(t, adaptor)
				payload, err := io.ReadAll(body)
				require.NoError(t, err)
				require.NoError(t, closer.Close())
				assert.Equal(t, input, string(payload))
				assert.True(t, info.IsModelMapped)
				assert.Equal(t, mappedModel, info.UpstreamModelName)
				assert.Nil(t, info.ReasoningConversion)
				after, err := common.Marshal(request)
				require.NoError(t, err)
				assert.Equal(t, original, after)
			}
		})
	}
}

func TestPrepareResponsesRequestRejectsNil(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previousPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() { settings.PassThroughRequestEnabled = previousPassThrough })
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%t", passthrough), func(t *testing.T) {
			settings.PassThroughRequestEnabled = passthrough
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
			_, body, closer, apiErr := PrepareResponsesRequest(c, &relaycommon.RelayInfo{}, nil)
			require.NotNil(t, apiErr)
			assert.EqualError(t, apiErr, "failed to copy responses request: copy source cannot be nil")
			assert.Equal(t, types.ErrorCodeInvalidRequest, apiErr.GetErrorCode())
			assert.True(t, types.IsSkipRetryError(apiErr))
			assert.Nil(t, body)
			assert.Nil(t, closer)
		})
	}
}

func BenchmarkPrepareResponsesPassthrough(b *testing.B) {
	settings := model_setting.GetGlobalSettings()
	previousPassThrough := settings.PassThroughRequestEnabled
	settings.PassThroughRequestEnabled = false
	b.Cleanup(func() { settings.PassThroughRequestEnabled = previousPassThrough })
	previousDiskCache := common.GetDiskCacheConfig()
	common.SetDiskCacheConfig(common.DiskCacheConfig{Enabled: false})
	b.Cleanup(func() { common.SetDiskCacheConfig(previousDiskCache) })

	for _, sizeMB := range []int{1, 8} {
		for _, group := range []string{"default", service.ImageGenerationTokenGroup} {
			b.Run(fmt.Sprintf("%dMiB/%s", sizeMB, group), func(b *testing.B) {
				input := `{"model":"gpt-6.1-sol","input":[{"role":"user","content":"` + strings.Repeat("x", sizeMB<<20) + `"}],"stream":true}`
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(input))
				c.Request.Header.Set("Content-Type", "application/json")
				b.Cleanup(func() { common.CleanupBodyStorage(c) })
				common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeSub2API)
				common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-6.1-sol")
				common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: true})
				request := &dto.OpenAIResponsesRequest{}
				require.NoError(b, common.UnmarshalBodyReusable(c, request))
				info := &relaycommon.RelayInfo{
					Request: request, RelayMode: relayconstant.RelayModeResponses,
					RelayFormat: types.RelayFormatOpenAIResponses, OriginModelName: request.Model,
					RequestURLPath: "/v1/responses", UsingGroup: group,
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_, _, closer, apiErr := PrepareResponsesRequest(c, info, request)
					if apiErr != nil {
						b.Fatal(apiErr)
					}
					if err := closer.Close(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

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

func TestPrepareSub2ResponsesToolFilteringKeepsRetryInput(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previousPassThrough := settings.PassThroughRequestEnabled
	settings.PassThroughRequestEnabled = false
	t.Cleanup(func() { settings.PassThroughRequestEnabled = previousPassThrough })

	const input = `{"model":"gpt-6.1-sol","input":[{"role":"user","content":"keep"},{"role":"assistant","tools":[{"type":"web_search"},{"type":"function","name":"keep","parameters":{"const":9007199254740993}}]}],"tools":[{"type":"web_search"},{"type":"computer"},{"type":"function","name":"keep","parameters":{"const":9007199254740993}}],"stream":true,"temperature":0,"parallel_tool_calls":false}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(input))
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeSub2API)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://provider.invalid")
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-6.1-sol")
	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.Unmarshal([]byte(input), request))
	original, err := common.Marshal(request)
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{
		Request: request, RelayMode: relayconstant.RelayModeResponses,
		RelayFormat: types.RelayFormatOpenAIResponses, OriginModelName: request.Model,
		RequestURLPath: "/v1/responses", UsingGroup: "default", IsStream: true,
	}
	common.SetContextKey(c, constant.ContextKeyChannelParamOverride, map[string]any{
		"operations": []any{
			map[string]any{"mode": "prune_objects", "path": "tools", "value": "web_search"},
			map[string]any{"mode": "prune_objects", "path": "tools", "value": "computer"},
			map[string]any{"mode": "prune_objects", "path": "input.*.tools", "value": "web_search"},
		},
	})

	for attempt := 0; attempt < 2; attempt++ {
		if attempt == 1 {
			// A different channel's retry must start with the original tools.
			common.SetContextKey(c, constant.ContextKeyChannelParamOverride, map[string]any{})
		}
		_, body, closer, apiErr := PrepareResponsesRequest(c, info, request)
		require.Nil(t, apiErr)
		payload, readErr := io.ReadAll(body)
		require.NoError(t, closer.Close())
		require.NoError(t, readErr)
		wantTools, wantHistoryTools := int64(1), int64(1)
		if attempt == 1 {
			wantTools, wantHistoryTools = 3, 2
		}
		assert.Equal(t, wantTools, gjson.GetBytes(payload, "tools.#").Int())
		assert.Equal(t, wantHistoryTools, gjson.GetBytes(payload, "input.1.tools.#").Int())
		assert.Equal(t, "9007199254740993", gjson.GetBytes(payload, "tools.#(type==function).parameters.const").Raw)
		assert.Equal(t, "9007199254740993", gjson.GetBytes(payload, "input.1.tools.#(type==function).parameters.const").Raw)
		assert.False(t, gjson.GetBytes(payload, "input.0.tools").Exists())
		assert.Equal(t, "keep", gjson.GetBytes(payload, "input.0.content").String())
		assert.Equal(t, "0", gjson.GetBytes(payload, "temperature").Raw)
		assert.Equal(t, "false", gjson.GetBytes(payload, "parallel_tool_calls").Raw)
		after, marshalErr := common.Marshal(request)
		require.NoError(t, marshalErr)
		assert.Equal(t, original, after)
	}
}
