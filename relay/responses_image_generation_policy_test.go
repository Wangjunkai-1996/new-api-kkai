package relay

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/relayconvert"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnforceResponsesImageGenerationGroupPolicy(t *testing.T) {
	tests := []struct {
		name        string
		group       string
		body        string
		wantBody    string
		wantChanged bool
		wantDenied  bool
		wantError   string
	}{
		{
			name:        "image group keeps forced image generation",
			group:       service.ImageGenerationTokenGroup,
			body:        `{"model":"gpt-5","tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`,
			wantBody:    `{"model":"gpt-5","tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`,
			wantChanged: false,
		},
		{
			name:        "image studio group keeps forced image generation",
			group:       service.ImageStudioTokenGroup,
			body:        `{"model":"gpt-5","tools":[{"type":"image_generation"}],"tool_choice":"required"}`,
			wantBody:    `{"model":"gpt-5","tools":[{"type":"image_generation"}],"tool_choice":"required"}`,
			wantChanged: false,
		},
		{
			name:        "ordinary group removes only image generation",
			group:       "default",
			body:        `{"model":"gpt-5","input":"hello","tools":[{"type":"image_generation","quality":"high"},{"type":"web_search_preview"}],"tool_choice":"auto"}`,
			wantBody:    `{"model":"gpt-5","input":"hello","tools":[{"type":"web_search_preview"}],"tool_choice":"auto"}`,
			wantChanged: true,
		},
		{
			name:        "ordinary group removes image-only tools and redundant auto choice",
			group:       "codex",
			body:        `{"model":"gpt-5","input":"write code","tools":[{"type":"image_generation"}],"tool_choice":"auto"}`,
			wantBody:    `{"model":"gpt-5","input":"write code"}`,
			wantChanged: true,
		},
		{
			name:        "function mentioning image generation is preserved",
			group:       "default",
			body:        `{"model":"gpt-5","tools":[{"type":"function","name":"image_generation","description":"call image_generation internally"}]}`,
			wantBody:    `{"model":"gpt-5","tools":[{"type":"function","name":"image_generation","description":"call image_generation internally"}]}`,
			wantChanged: true,
		},
		{
			name:        "request without policy fields is unchanged",
			group:       "default",
			body:        `{"model":"gpt-5","input":"hello"}`,
			wantBody:    `{"model":"gpt-5","input":"hello"}`,
			wantChanged: false,
		},
		{
			name:        "case insensitive policy fields cannot bypass filtering",
			group:       "default",
			body:        `{"model":"gpt-5","Tools":[{"Type":"image_generation"}]}`,
			wantBody:    `{"model":"gpt-5"}`,
			wantChanged: true,
		},
		{
			name:        "duplicate top level tools are normalized before forwarding",
			group:       "default",
			body:        `{"model":"gpt-5","tools":[{"type":"image_generation"}],"tools":[{"type":"web_search_preview"}]}`,
			wantBody:    `{"model":"gpt-5","tools":[{"type":"web_search_preview"}]}`,
			wantChanged: true,
		},
		{
			name:        "duplicate tool type is normalized before forwarding",
			group:       "default",
			body:        `{"model":"gpt-5","tools":[{"type":"image_generation","type":"web_search_preview"}]}`,
			wantBody:    `{"model":"gpt-5","tools":[{"type":"web_search_preview"}]}`,
			wantChanged: true,
		},
		{
			name:        "required choice remains when another tool is available",
			group:       "default",
			body:        `{"model":"gpt-5","tools":[{"type":"image_generation"},{"type":"file_search","vector_store_ids":["vs_1"]}],"tool_choice":"required"}`,
			wantBody:    `{"model":"gpt-5","tools":[{"type":"file_search","vector_store_ids":["vs_1"]}],"tool_choice":"required"}`,
			wantChanged: true,
		},
		{
			name:       "required choice with only image tool is denied",
			group:      "default",
			body:       `{"model":"gpt-5","tools":[{"type":"image_generation"}],"tool_choice":"required"}`,
			wantDenied: true,
		},
		{
			name:       "direct object choice is denied without tools",
			group:      "default",
			body:       `{"model":"gpt-5","tool_choice":{"type":"image_generation"}}`,
			wantDenied: true,
		},
		{
			name:       "direct string choice is denied without tools",
			group:      "default",
			body:       `{"model":"gpt-5","tool_choice":"image_generation"}`,
			wantDenied: true,
		},
		{
			name:  "allowed tools choice is filtered with the main tool list",
			group: "default",
			body: `{
				"model":"gpt-5",
				"tools":[{"type":"image_generation"},{"type":"web_search_preview"}],
				"tool_choice":{"type":"allowed_tools","mode":"auto","tools":[{"type":"image_generation"},{"type":"web_search_preview"}]}
			}`,
			wantBody: `{
				"model":"gpt-5",
				"tools":[{"type":"web_search_preview"}],
				"tool_choice":{"type":"allowed_tools","mode":"auto","tools":[{"type":"web_search_preview"}]}
			}`,
			wantChanged: true,
		},
		{
			name:       "required allowed tools choice with only image tool is denied",
			group:      "default",
			body:       `{"model":"gpt-5","tools":[{"type":"image_generation"}],"tool_choice":{"type":"allowed_tools","mode":"required","tools":[{"type":"image_generation"}]}}`,
			wantDenied: true,
		},
		{
			name:      "allowed tools choice rejects invalid mode",
			group:     "default",
			body:      `{"model":"gpt-5","tools":[{"type":"image_generation"}],"tool_choice":{"type":"allowed_tools","mode":"sometimes","tools":[{"type":"image_generation"}]}}`,
			wantError: "auto or required is required",
		},
		{
			name:      "ambiguous tool type casing fails closed",
			group:     "default",
			body:      `{"model":"gpt-5","tools":[{"type":"web_search_preview","Type":"image_generation"}]}`,
			wantError: "ambiguous type field",
		},
		{
			name:        "auto group fails closed",
			group:       "auto",
			body:        `{"model":"gpt-5","tools":[{"type":"image_generation"}]}`,
			wantBody:    `{"model":"gpt-5"}`,
			wantChanged: true,
		},
		{
			name:        "group matching is exact",
			group:       " image ",
			body:        `{"model":"gpt-5","tools":[{"type":"image_generation"}]}`,
			wantBody:    `{"model":"gpt-5"}`,
			wantChanged: true,
		},
		{
			name:      "malformed tools fail closed",
			group:     "default",
			body:      `{"model":"gpt-5","tools":{"type":"image_generation"}}`,
			wantError: "JSON array is required",
		},
		{
			name:      "malformed tool choice fails closed",
			group:     "default",
			body:      `{"model":"gpt-5","tool_choice":true}`,
			wantError: "string or JSON object is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, changed, err := enforceResponsesImageGenerationGroupPolicy([]byte(test.body), test.group)
			if test.wantDenied {
				require.ErrorIs(t, err, errResponsesImageGenerationGroupForbidden)
				assert.False(t, changed)
				assert.Nil(t, got)
				return
			}
			if test.wantError != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, test.wantError)
				assert.False(t, changed)
				assert.Nil(t, got)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, test.wantChanged, changed)
			assert.JSONEq(t, test.wantBody, string(got))
		})
	}
}

func TestNewResponsesImageGenerationPolicyError(t *testing.T) {
	denied := newResponsesImageGenerationPolicyError(errResponsesImageGenerationGroupForbidden)
	require.NotNil(t, denied)
	assert.Equal(t, http.StatusForbidden, denied.StatusCode)
	assert.Equal(t, types.ErrorCodeAccessDenied, denied.GetErrorCode())
	assert.True(t, types.IsSkipRetryError(denied))

	invalid := newResponsesImageGenerationPolicyError(errors.New("invalid tools"))
	require.NotNil(t, invalid)
	assert.Equal(t, http.StatusBadRequest, invalid.StatusCode)
	assert.Equal(t, types.ErrorCodeInvalidRequest, invalid.GetErrorCode())
	assert.True(t, types.IsSkipRetryError(invalid))
}

func TestResponsesImageGenerationPolicyAppliesOnlyToFinalResponsesFormat(t *testing.T) {
	tests := []struct {
		name  string
		info  *relaycommon.RelayInfo
		apply bool
	}{
		{
			name:  "nil relay info",
			info:  nil,
			apply: false,
		},
		{
			name:  "direct responses request",
			info:  &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIResponses},
			apply: true,
		},
		{
			name: "responses converted to Gemini",
			info: &relaycommon.RelayInfo{
				RelayFormat:            types.RelayFormatOpenAIResponses,
				RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatGemini},
			},
			apply: false,
		},
		{
			name: "chat converted to responses",
			info: &relaycommon.RelayInfo{
				RelayFormat:            types.RelayFormatOpenAI,
				RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses},
			},
			apply: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.apply, responsesImageGenerationPolicyApplies(test.info))
		})
	}
}

func TestResponsesHelperEnforcesImageGenerationPolicyOnFinalOutboundBody(t *testing.T) {
	if service.GetHttpClient() == nil {
		service.InitHttpClient()
	}
	originalPassThrough := model_setting.GetGlobalSettings().PassThroughRequestEnabled
	t.Cleanup(func() {
		model_setting.GetGlobalSettings().PassThroughRequestEnabled = originalPassThrough
	})

	tests := []struct {
		name            string
		group           string
		body            string
		passThrough     bool
		paramOverride   map[string]interface{}
		conversionChain []types.RelayFormat
		wantBody        string
		wantDenied      bool
	}{
		{
			name:        "passthrough body is filtered",
			group:       "default",
			passThrough: true,
			body:        `{"model":"gpt-5","input":"hello","tools":[{"type":"image_generation"},{"type":"web_search_preview"}],"tool_choice":"auto"}`,
			wantBody:    `{"model":"gpt-5","input":"hello","tools":[{"type":"web_search_preview"}],"tool_choice":"auto"}`,
		},
		{
			name:        "passthrough case variants cannot bypass filtering",
			group:       "default",
			passThrough: true,
			body:        `{"model":"gpt-5","Tools":[{"Type":"image_generation"}]}`,
			wantBody:    `{"model":"gpt-5"}`,
		},
		{
			name:            "passthrough ignores conversion history from a previous retry",
			group:           "default",
			passThrough:     true,
			conversionChain: []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatGemini},
			body:            `{"model":"gpt-5","input":"hello","tools":[{"type":"image_generation"},{"type":"web_search_preview"}],"tool_choice":"auto"}`,
			wantBody:        `{"model":"gpt-5","input":"hello","tools":[{"type":"web_search_preview"}],"tool_choice":"auto"}`,
		},
		{
			name:        "channel param override cannot inject image generation",
			group:       "default",
			body:        `{"model":"gpt-5","input":"hello"}`,
			passThrough: false,
			paramOverride: map[string]interface{}{
				"tools": []interface{}{
					map[string]interface{}{"type": "image_generation"},
					map[string]interface{}{"type": "file_search", "vector_store_ids": []interface{}{"vs_1"}},
				},
				"tool_choice": "auto",
			},
			wantBody: `{"model":"gpt-5","input":"hello","tools":[{"type":"file_search","vector_store_ids":["vs_1"]}],"tool_choice":"auto"}`,
		},
		{
			name:        "forced passthrough call is denied before upstream",
			group:       "default",
			passThrough: true,
			body:        `{"model":"gpt-5","input":"draw","tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`,
			wantDenied:  true,
		},
		{
			name:        "image group is passed through unchanged",
			group:       service.ImageGenerationTokenGroup,
			passThrough: true,
			body:        `{"model":"gpt-5","input":"draw","tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`,
			wantBody:    `{"model":"gpt-5","input":"draw","tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model_setting.GetGlobalSettings().PassThroughRequestEnabled = test.passThrough

			var calls atomic.Int32
			receivedBodies := make(chan []byte, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				body, _ := io.ReadAll(r.Body)
				receivedBodies <- body
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"stop","type":"invalid_request_error","code":"test_stop"}}`))
			}))
			defer upstream.Close()

			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(test.body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
			common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(ctx, constant.ContextKeyChannelKey, "test-upstream-key")
			common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "gpt-5")
			if test.paramOverride != nil {
				common.SetContextKey(ctx, constant.ContextKeyChannelParamOverride, test.paramOverride)
			}
			defer common.CleanupBodyStorage(ctx)

			var request dto.OpenAIResponsesRequest
			require.NoError(t, common.Unmarshal([]byte(test.body), &request))
			info := &relaycommon.RelayInfo{
				Request:                &request,
				RelayMode:              relayconstant.RelayModeResponses,
				RelayFormat:            types.RelayFormatOpenAIResponses,
				RequestConversionChain: test.conversionChain,
				RequestURLPath:         "/v1/responses",
				OriginModelName:        "gpt-5",
				UsingGroup:             test.group,
			}

			apiErr := ResponsesHelper(ctx, info)
			if test.wantDenied {
				require.NotNil(t, apiErr)
				assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
				assert.Equal(t, types.ErrorCodeAccessDenied, apiErr.GetErrorCode())
				assert.True(t, types.IsSkipRetryError(apiErr))
				assert.Zero(t, calls.Load())
				return
			}

			require.NotNil(t, apiErr, "the fake upstream deliberately returns 400")
			require.Equal(t, int32(1), calls.Load())
			gotBody := <-receivedBodies
			assert.JSONEq(t, test.wantBody, string(gotBody))
		})
	}
}

func TestTextHelperEnforcesImageGenerationPolicyAfterChatToResponsesConversion(t *testing.T) {
	if service.GetHttpClient() == nil {
		service.InitHttpClient()
	}

	var calls atomic.Int32
	receivedBodies := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		receivedBodies <- body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"stop","type":"invalid_request_error","code":"test_stop"}}`))
	}))
	defer upstream.Close()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeAdvancedCustom)
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "test-upstream-key")
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "gpt-5")
	common.SetContextKey(ctx, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{
			Routes: []dto.AdvancedCustomRoute{
				{
					IncomingPath: "/v1/chat/completions",
					UpstreamPath: "/v1/responses",
					Converter:    relayconvert.ConverterOpenAIChatToOpenAIResponses,
				},
			},
		},
	})
	defer common.CleanupBodyStorage(ctx)

	request := &dto.GeneralOpenAIRequest{
		Model:    "gpt-5",
		Messages: []dto.Message{{Role: "user", Content: "draw an image"}},
		Tools: []dto.ToolCallRequest{
			{Type: dto.BuildInToolImageGeneration},
			{Type: "function", Function: dto.FunctionRequest{Name: "lookup"}},
		},
	}
	info := &relaycommon.RelayInfo{
		Request:                request,
		RelayMode:              relayconstant.RelayModeChatCompletions,
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI},
		RequestURLPath:         "/v1/chat/completions",
		OriginModelName:        "gpt-5",
		UsingGroup:             "default",
	}

	apiErr := TextHelper(ctx, info)
	require.NotNil(t, apiErr, "the fake upstream deliberately returns 400")
	require.Equal(t, int32(1), calls.Load())
	gotBody := <-receivedBodies
	assert.NotContains(t, string(gotBody), dto.BuildInToolImageGeneration)
	assert.Contains(t, string(gotBody), `"name":"lookup"`)
}
