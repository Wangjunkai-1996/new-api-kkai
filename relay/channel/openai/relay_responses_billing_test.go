package openai

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesHandlersBillActualToolCalls(t *testing.T) {
	operation_setting.SetToolPriceForTest("priced_fn", 5)
	t.Cleanup(func() { operation_setting.DeleteToolPriceForTest("priced_fn") })
	handlers := []struct {
		name    string
		stream  bool
		convert bool
		handle  func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{"responses_json", false, false, OaiResponsesHandler},
		{"responses_stream", true, false, OaiResponsesStreamHandler},
		{"chat_json", false, true, OaiResponsesToChatHandler},
		{"chat_buffered", true, true, OaiResponsesToChatBufferedStreamHandler},
		{"chat_stream", true, true, OaiResponsesToChatStreamHandler},
	}
	searchOutputs := []dto.ResponsesOutput{
		{ID: "ws_1", Type: dto.BuildInCallWebSearchCall, Status: "completed"},
		{ID: "ws_2", Type: dto.BuildInCallWebSearchCall, Status: "completed"},
		{ID: "fs_1", Type: dto.BuildInCallFileSearchCall, Status: "completed"},
		{ID: "fc_1", Type: "function_call", Name: "web_search", CallId: "call_1", Arguments: []byte(`"{}"`)},
		{ID: "fc_2", Type: "function_call", Name: "priced_fn", CallId: "call_2", Arguments: []byte(`"{}"`)},
		{ID: "fc_3", Type: "function_call", Name: "unpriced_fn", CallId: "call_3", Arguments: []byte(`"{}"`)},
	}
	cases := []struct {
		name            string
		outputs         []dto.ResponsesOutput
		webTool         string
		originalWebTool string
		omitFinalOutput bool
		finalOnly       bool
		wantWeb         int
		wantFile        int
		wantImages      []relaycommon.ResponsesImageGenerationCall
	}{
		{name: "declarations_only", webTool: dto.BuildInToolWebSearchPreview},
		{name: "actual_calls_deduplicated", outputs: searchOutputs, webTool: dto.BuildInToolWebSearchPreview, wantWeb: 2, wantFile: 1},
		{name: "standard_search_price_identity", outputs: searchOutputs, webTool: dto.BuildInToolWebSearch, wantWeb: 2, wantFile: 1},
		{name: "override_preview_to_standard", outputs: searchOutputs, webTool: dto.BuildInToolWebSearch, originalWebTool: dto.BuildInToolWebSearchPreview, wantWeb: 2, wantFile: 1},
		{name: "override_standard_to_preview", outputs: searchOutputs, webTool: dto.BuildInToolWebSearchPreview, originalWebTool: dto.BuildInToolWebSearch, wantWeb: 2, wantFile: 1},
		{name: "terminal_only", outputs: searchOutputs, webTool: dto.BuildInToolWebSearchPreview, finalOnly: true, wantWeb: 2, wantFile: 1},
		{name: "items_without_final_output", outputs: searchOutputs, webTool: dto.BuildInToolWebSearchPreview, omitFinalOutput: true, wantWeb: 2, wantFile: 1},
		{
			name: "only_completed_image_results", webTool: dto.BuildInToolWebSearchPreview,
			outputs: []dto.ResponsesOutput{
				{ID: "img_1", Type: dto.ResponsesOutputTypeImageGenerationCall, Status: "completed", Result: "image1", Quality: "low", Size: "1024x1024"},
				{ID: "img_2", Type: dto.ResponsesOutputTypeImageGenerationCall, Status: "completed", Result: "image2", Quality: "high", Size: "1536x1024"},
				{ID: "img_duplicate", Type: dto.ResponsesOutputTypeImageGenerationCall, Status: "completed", Result: "image1", Quality: "low", Size: "1024x1024"},
				{ID: "img_failed", Type: dto.ResponsesOutputTypeImageGenerationCall, Status: "failed", Result: "partial"},
				{ID: "img_empty", Type: dto.ResponsesOutputTypeImageGenerationCall, Status: "completed"},
			},
			wantImages: []relaycommon.ResponsesImageGenerationCall{{Quality: "low", Size: "1024x1024"}, {Quality: "high", Size: "1536x1024"}},
		},
	}
	for _, handler := range handlers {
		for _, tc := range cases {
			t.Run(handler.name+"/"+tc.name, func(t *testing.T) {
				response := dto.OpenAIResponsesResponse{
					ID: "resp_billing", Model: "gpt-4o", Status: []byte(`"completed"`), Output: tc.outputs,
					Tools: []map[string]any{{"type": tc.webTool}, {"type": dto.BuildInToolFileSearch}, {"type": "function", "name": "web_search"}, {"type": dto.BuildInToolImageGeneration}},
					Usage: &dto.Usage{InputTokens: 2, OutputTokens: 3, TotalTokens: 5},
				}
				var body strings.Builder
				if handler.stream {
					if !tc.finalOnly {
						for i := range tc.outputs {
							event := dto.ResponsesStreamResponse{Type: dto.ResponsesOutputTypeItemDone, OutputIndex: &i, Item: &tc.outputs[i]}
							data, err := common.Marshal(event)
							require.NoError(t, err)
							body.WriteString("data: " + string(data) + "\n\n")
							// A duplicate item event must not become another billed call.
							body.WriteString("data: " + string(data) + "\n\n")
						}
					}
					if tc.omitFinalOutput {
						response.Output = nil
					}
					data, err := common.Marshal(dto.ResponsesStreamResponse{Type: "response.completed", Response: &response})
					require.NoError(t, err)
					body.WriteString("data: " + string(data) + "\n\n")
				} else {
					data, err := common.Marshal(response)
					require.NoError(t, err)
					body.Write(data)
				}
				c, resp, info := newResponsesStreamHandlerTest(t, body.String())
				info.IsStream = handler.stream
				info.RelayFormat = types.RelayFormatOpenAIResponses
				if handler.convert {
					info.RelayFormat = types.RelayFormatOpenAI
				} else {
					originalWebTool := tc.originalWebTool
					if originalWebTool == "" {
						originalWebTool = tc.webTool
					}
					info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
						originalWebTool:           {ToolName: originalWebTool, CallCount: 7},
						dto.BuildInToolFileSearch: {ToolName: dto.BuildInToolFileSearch, CallCount: 7},
					}}
				}
				usage, apiErr := handler.handle(c, info, resp)
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				assert.Equal(t, 5, usage.TotalTokens)
				require.NotNil(t, info.ResponsesUsageInfo)
				tools := info.ResponsesUsageInfo.BuiltInTools
				require.NotNil(t, tools[dto.BuildInToolWebSearchPreview])
				require.NotNil(t, tools[dto.BuildInToolFileSearch])
				assert.Equal(t, tc.wantWeb, tools[dto.BuildInToolWebSearchPreview].CallCount)
				assert.Equal(t, tc.webTool, tools[dto.BuildInToolWebSearchPreview].ToolName)
				assert.Equal(t, tc.wantFile, tools[dto.BuildInToolFileSearch].CallCount)
				assert.NotContains(t, tools, "function")
				assert.NotContains(t, tools, "unpriced_fn")
				if tc.wantWeb > 0 {
					require.Contains(t, tools, "priced_fn")
					assert.Equal(t, 1, tools["priced_fn"].CallCount)
				} else {
					assert.NotContains(t, tools, "priced_fn")
				}
				if handler.convert {
					assert.Empty(t, info.ResponsesUsageInfo.ImageGenerationCalls, "Chat conversion must not bill images it does not deliver")
				} else {
					assert.ElementsMatch(t, tc.wantImages, info.ResponsesUsageInfo.ImageGenerationCalls)
				}
			})
		}
	}
}

func TestResponsesToolBillingDoesNotCommitFailedAttempt(t *testing.T) {
	body := "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"web_search_call\",\"id\":\"ws_1\"}}\n\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"message\":\"failed\",\"type\":\"server_error\"}}}\n\n"
	c, resp, info := newResponsesStreamHandlerTest(t, body)
	_, apiErr := OaiResponsesStreamHandler(c, info, resp)
	require.NotNil(t, apiErr)
	assert.Nil(t, info.ResponsesUsageInfo)

	resp.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":2,\"output_tokens\":3,\"total_tokens\":5}}}\n\n"))
	_, apiErr = OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	assert.Zero(t, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
}

func TestResponsesToolBillingRequiresSuccessfulImageTerminal(t *testing.T) {
	for _, terminal := range []string{"", "response.incomplete", "response.completed"} {
		t.Run("terminal_"+terminal, func(t *testing.T) {
			body := "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"img_1\",\"type\":\"image_generation_call\",\"status\":\"completed\",\"result\":\"image\",\"quality\":\"low\",\"size\":\"1024x1024\"}}\n\n"
			if terminal != "" {
				status, err := common.Marshal(strings.TrimPrefix(terminal, "response."))
				require.NoError(t, err)
				data, err := common.Marshal(dto.ResponsesStreamResponse{Type: terminal, Response: &dto.OpenAIResponsesResponse{
					Status: status, Usage: &dto.Usage{InputTokens: 2, OutputTokens: 3, TotalTokens: 5},
				}})
				require.NoError(t, err)
				body += "data: " + string(data) + "\n\n"
			}
			c, resp, info := newResponsesStreamHandlerTest(t, body)
			info.RelayFormat = types.RelayFormatOpenAIResponses
			_, apiErr := OaiResponsesStreamHandler(c, info, resp)
			if terminal != "response.completed" {
				require.NotNil(t, apiErr)
				assert.Nil(t, info.ResponsesUsageInfo)
				return
			}
			require.Nil(t, apiErr)
			require.NotNil(t, info.ResponsesUsageInfo)
			assert.Len(t, info.ResponsesUsageInfo.ImageGenerationCalls, 1)
		})
	}
}

func TestChatHandlersBillPricedFunctionCalls(t *testing.T) {
	operation_setting.SetToolPriceForTest("priced_chat_fn", 5)
	t.Cleanup(func() { operation_setting.DeleteToolPriceForTest("priced_chat_fn") })
	tests := []struct {
		name   string
		stream bool
		handle func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{"chat_json", false, OpenaiHandler},
		{"chat_stream", true, OaiStreamHandler},
		{"responses_json", false, OaiChatToResponsesHandler},
		{"responses_stream", true, OaiChatToResponsesStreamHandler},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := `[{"index":0,"id":"call_1","type":"function","function":{"name":"priced_chat_fn","arguments":"{}"}},{"index":1,"id":"call_2","type":"function","function":{"name":"unpriced_chat_fn","arguments":"{}"}},{"index":2,"id":"call_3","type":"function","function":{"name":"web_search","arguments":"{}"}}]`
			body := `{"id":"chat_1","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","tool_calls":` + calls + `},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`
			if test.stream {
				first := `data: {"id":"chat_1","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":` + calls + `}}]}` + "\n\n"
				body = first + `data: {"id":"chat_1","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}` + "\n\ndata: [DONE]\n\n"
			}
			c, resp, info := newResponsesStreamHandlerTest(t, body)
			info.IsStream = test.stream
			info.RelayFormat = types.RelayFormatOpenAI
			info.RelayMode = relayconstant.RelayModeChatCompletions
			if strings.HasPrefix(test.name, "responses_") {
				info.RelayFormat = types.RelayFormatOpenAIResponses
			}
			usage, apiErr := test.handle(c, info, resp)
			require.Nil(t, apiErr)
			require.Equal(t, 5, usage.TotalTokens)
			require.NotNil(t, info.ResponsesUsageInfo)
			require.Contains(t, info.ResponsesUsageInfo.BuiltInTools, "priced_chat_fn")
			assert.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools["priced_chat_fn"].CallCount)
			assert.NotContains(t, info.ResponsesUsageInfo.BuiltInTools, "unpriced_chat_fn")
			assert.NotContains(t, info.ResponsesUsageInfo.BuiltInTools, "web_search")
		})
	}
}
