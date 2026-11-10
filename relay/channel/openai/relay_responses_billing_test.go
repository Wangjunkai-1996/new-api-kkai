package openai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
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

// The next upstream read stays blocked until the scanner closes its body.
// The channels let the downstream writer force cancellation during the final
// callback without depending on sleeps or goroutine scheduling.
type responsesLifecycleBody struct {
	data      *strings.Reader
	waiting   chan struct{}
	closed    chan struct{}
	readOnce  sync.Once
	closeOnce sync.Once
	readErr   error
	closeErr  error
}

func (b *responsesLifecycleBody) Read(p []byte) (int, error) {
	if b.data.Len() > 0 {
		return b.data.Read(p)
	}
	b.readOnce.Do(func() { close(b.waiting) })
	if b.readErr != nil {
		return 0, b.readErr
	}
	<-b.closed
	return 0, b.closeErr
}

func (b *responsesLifecycleBody) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

type responsesLifecycleWriter struct {
	*httptest.ResponseRecorder
	body              *responsesLifecycleBody
	cancel            context.CancelFunc
	cancelBeforeFlush bool
	waitForCleanup    bool
	failWrite         int
	writeErr          error
	flushErr          error
	writes            int
	needle            string
}

func (w *responsesLifecycleWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == w.failWrite {
		return 0, w.writeErr
	}
	n, err := w.ResponseRecorder.Write(p)
	if w.cancelBeforeFlush && strings.Contains(w.Body.String(), w.needle) {
		<-w.body.waiting
		w.cancel()
		<-w.body.closed
	}
	return n, err
}

func (w *responsesLifecycleWriter) Flush() {
	w.ResponseRecorder.Flush()
	if !strings.Contains(w.Body.String(), w.needle) || w.body == nil {
		return
	}
	<-w.body.waiting
	if w.cancel != nil {
		w.cancel()
	}
	if w.waitForCleanup {
		<-w.body.closed
	}
}

func (w *responsesLifecycleWriter) FlushError() error {
	return w.flushErr
}

func TestOaiResponsesStreamHandlerCompletionLifecycle(t *testing.T) {
	oldMode, oldTimeout := gin.Mode(), constant.StreamingTimeout
	gin.SetMode(gin.TestMode)
	constant.StreamingTimeout = 1
	t.Cleanup(func() {
		gin.SetMode(oldMode)
		constant.StreamingTimeout = oldTimeout
	})

	for _, tc := range []struct {
		name              string
		event             string
		responseStatus    string
		cancel            bool
		cancelBeforeFlush bool
		closeEOF          bool
		eof               bool
		scannerError      bool
		priorError        bool
		failWrite         int
		failFlush         bool
		wantEnd           relaycommon.StreamEndReason
		wantError         bool
	}{
		{name: "completed without upstream EOF", event: "response.completed", responseStatus: "completed", wantEnd: relaycommon.StreamEndReasonDone},
		{name: "done alias without upstream EOF", event: "response.done", responseStatus: "completed", wantEnd: relaycommon.StreamEndReasonDone},
		{name: "disconnect during terminal flush", event: "response.completed", responseStatus: "completed", cancel: true, wantEnd: relaycommon.StreamEndReasonDone},
		{name: "completion then cancellation and cleanup EOF", event: "response.completed", responseStatus: "completed", cancel: true, closeEOF: true, wantEnd: relaycommon.StreamEndReasonDone},
		{name: "midstream disconnect", event: "response.output_text.delta", cancel: true, wantEnd: relaycommon.StreamEndReasonClientGone, wantError: true},
		{name: "midstream disconnect followed by cleanup EOF", event: "response.output_text.delta", cancel: true, closeEOF: true, wantEnd: relaycommon.StreamEndReasonClientGone, wantError: true},
		{name: "cancellation before flush", event: "response.completed", responseStatus: "completed", cancel: true, cancelBeforeFlush: true, wantEnd: relaycommon.StreamEndReasonClientGone, wantError: true},
		{name: "event write failure", event: "response.completed", responseStatus: "completed", failWrite: 1, wantEnd: relaycommon.StreamEndReasonHandlerStop, wantError: true},
		{name: "data write failure", event: "response.completed", responseStatus: "completed", failWrite: 2, wantEnd: relaycommon.StreamEndReasonHandlerStop, wantError: true},
		{name: "delimiter write failure", event: "response.completed", responseStatus: "completed", failWrite: 3, wantEnd: relaycommon.StreamEndReasonHandlerStop, wantError: true},
		{name: "flush failure", event: "response.completed", responseStatus: "completed", failFlush: true, wantEnd: relaycommon.StreamEndReasonHandlerStop, wantError: true},
		{name: "protocol error before completion", event: "response.completed", responseStatus: "completed", priorError: true, cancel: true, wantEnd: relaycommon.StreamEndReasonClientGone, wantError: true},
		{name: "upstream read failure before handler returns", event: "response.completed", responseStatus: "completed", scannerError: true, wantEnd: relaycommon.StreamEndReasonScannerErr, wantError: true},
		{name: "done with failed response", event: "response.done", responseStatus: "failed", eof: true, wantEnd: relaycommon.StreamEndReasonEOF, wantError: true},
		{name: "done with incomplete response", event: "response.done", responseStatus: "incomplete", eof: true, wantEnd: relaycommon.StreamEndReasonEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := fmt.Sprintf(`{"type":%q,"response":{"status":%q,"usage":{"input_tokens":7,"output_tokens":5,"total_tokens":12},"output":[{"id":"img_1","type":"image_generation_call","status":"completed","result":"image-data"}]}}`, tc.event, tc.responseStatus)
			if tc.responseStatus == "" {
				payload = `{"type":"response.output_text.delta","delta":"hello"}`
			}
			sse := "data: " + payload + "\n\n"
			if tc.priorError {
				sse = "data: invalid JSON\n\n" + sse
			}
			body := &responsesLifecycleBody{data: strings.NewReader(sse), waiting: make(chan struct{}), closed: make(chan struct{}), closeErr: io.ErrClosedPipe}
			if tc.closeEOF {
				body.closeErr = io.EOF
			}
			if tc.scannerError {
				body.readErr = io.ErrUnexpectedEOF
			}
			t.Cleanup(func() { require.NoError(t, body.Close()) })
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			writer := &responsesLifecycleWriter{
				ResponseRecorder: httptest.NewRecorder(), body: body, needle: payload + "\n\n",
				cancelBeforeFlush: tc.cancelBeforeFlush, waitForCleanup: tc.cancel || tc.scannerError,
				failWrite: tc.failWrite, writeErr: errors.New("downstream write failed"),
			}
			if tc.cancel {
				writer.cancel = cancel
			}
			if tc.failFlush {
				writer.flushErr = writer.writeErr
			}
			var upstream io.ReadCloser = body
			if tc.eof {
				upstream = io.NopCloser(strings.NewReader(sse))
				writer.body = nil
			}
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			info := &relaycommon.RelayInfo{
				OriginModelName: "gpt-4o", IsStream: true, DisablePing: true, StartTime: time.Now(),
				RelayFormat: types.RelayFormatOpenAIResponses,
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o"},
			}
			info.SetEstimatePromptTokens(7)
			resp := &http.Response{StatusCode: http.StatusOK, Body: upstream, Header: http.Header{"Content-Type": {"text/event-stream"}}}
			usage, apiErr := OaiResponsesStreamHandler(c, info, resp)
			if tc.responseStatus == "failed" || tc.responseStatus == "incomplete" {
				require.NotNil(t, apiErr)
				assert.Empty(t, info.ResponsesUsageInfo, "KKAI unsuccessful terminals do not commit image charges")
				return
			}
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			require.NotNil(t, info.StreamStatus)
			assert.Equal(t, tc.wantEnd, info.StreamStatus.EndReason)
			assert.Equal(t, tc.responseStatus, info.StreamStatus.ResponseOutcome())
			log := service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 1, 0, 1).Snapshot()["admin_info"].(map[string]any)["stream_status"].(map[string]any)
			assert.Equal(t, tc.wantError, log["status"] == "error")
			if tc.responseStatus != "" {
				assert.Equal(t, 7, usage.PromptTokens)
				assert.Equal(t, 5, usage.CompletionTokens)
				assert.Equal(t, 12, usage.TotalTokens)
				require.NotNil(t, info.ResponsesUsageInfo)
				require.Contains(t, info.ResponsesUsageInfo.BuiltInTools, dto.BuildInToolImageGeneration)
				assert.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].CallCount)
			}
			if tc.failWrite == 0 {
				assert.Contains(t, writer.Body.String(), payload+"\n\n")
			} else {
				assert.ErrorIs(t, info.StreamStatus.EndError, writer.writeErr)
			}
			if tc.failFlush {
				assert.ErrorIs(t, info.StreamStatus.EndError, writer.flushErr)
			}
			if !tc.eof {
				select {
				case <-body.closed:
				default:
					t.Fatal("upstream body was not closed")
				}
			}
		})
	}
}

// The 12h cache-write split reaches billing from JSON, stream and compaction
// responses. The usage is the OpenAI prompt caching guide example.
func TestOaiResponsesHandlersKeepCacheWriteTTLSplit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})

	const usage = `"usage":{"input_tokens":12000,"output_tokens":0,"total_tokens":12000,"input_tokens_details":{"cached_tokens":4000,"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_30m_tokens":3000,"ttl_12h_tokens":4000}}}`
	const response = `{"id":"resp_1","object":"response","model":"gpt-6-astra","status":"completed","output":[],` + usage + `}`
	for _, tt := range []struct {
		name, contentType, body string
	}{
		{"json", "application/json", response},
		{"stream", "text/event-stream", `data: {"type":"response.completed","response":` + response + "}\n\ndata: [DONE]\n\n"},
		{"compaction", "application/json", `{"id":"cmp_1","object":"response.compaction","output":[],` + usage + `}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{
				RelayFormat:     types.RelayFormatOpenAIResponses,
				OriginModelName: "gpt-6-astra",
				IsStream:        tt.name == "stream",
				DisablePing:     true,
				ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: "gpt-6-astra"},
			}
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(tt.body)),
				Header:     http.Header{"Content-Type": []string{tt.contentType}},
			}
			var usage *dto.Usage
			var apiErr *types.NewAPIError
			switch tt.name {
			case "json":
				usage, apiErr = OaiResponsesHandler(c, info, resp)
			case "stream":
				usage, apiErr = OaiResponsesStreamHandler(c, info, resp)
			default:
				usage, apiErr = OaiResponsesCompactionHandler(c, resp)
			}
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, 12000, usage.PromptTokens)
			assert.Equal(t, 4000, usage.PromptTokensDetails.CachedTokens)
			assert.Equal(t, 7000, usage.PromptTokensDetails.CacheCreationTokensTotal())
			assert.Equal(t, 4000, usage.PromptTokensDetails.CacheWrite12hTokens())
		})
	}
}

func TestOaiResponsesHandlerRewritesSGLangCreatedAtToInt(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeSGLang},
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_1","object":"response","created_at":1786588600.0,"status":"completed","output":[]}`)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}

	_, apiErr := OaiResponsesHandler(c, info, resp)
	require.Nil(t, apiErr)
	assert.Contains(t, w.Body.String(), `"created_at":1786588600`)
	assert.NotContains(t, w.Body.String(), `1786588600.0`)
}

func TestOaiResponsesStreamHandlerRewritesSGLangCreatedAtToInt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(common.RequestIdKey, "sglang-created-at-test")

	info := &relaycommon.RelayInfo{
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeSGLang,
			UpstreamModelName: "served-model",
		},
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"created_at\":1.7865886E9,\"status\":\"completed\",\"output\":[]}}\n\n" +
				"data: [DONE]\n\n",
		)),
		Header: http.Header{"Content-Type": []string{"text/event-stream"}},
	}

	_, apiErr := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	assert.Contains(t, w.Body.String(), `"created_at":1786588600`)
	assert.NotContains(t, w.Body.String(), `1.7865886E9`)
	assert.NotContains(t, w.Body.String(), `1786588600.0`)
}

func TestOaiResponsesStreamHandlerKeepsNonSGLangCreatedAt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(common.RequestIdKey, "openai-created-at-test")

	info := &relaycommon.RelayInfo{
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: "gpt-test"},
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"created_at\":1786588600.0,\"status\":\"completed\",\"output\":[]}}\n\n" +
				"data: [DONE]\n\n",
		)),
		Header: http.Header{"Content-Type": []string{"text/event-stream"}},
	}

	_, apiErr := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	assert.Contains(t, w.Body.String(), `1786588600.0`)
}

// A converted hosted web search reaches Azure and Xinference Chat Completions
// as web_search_options, like OpenAI; the upstream decides whether it applies.
// Azure Responses bills web search by tool_usage.web_search.num_requests (Bing
// transactions) under bing_web_search, and the explicit zeros silence the
// web_search_call items, which leaves one surcharge row.
func TestAzureHostedWebSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})

	t.Run("Claude to Chat", func(t *testing.T) {
		const body = `{"model":"gpt-5.1","max_tokens":64,"messages":[{"role":"user","content":"Weather?"}],"tools":[{"type":"web_search_20250305","name":"web_search"}]}`
		for _, channelType := range []int{constant.ChannelTypeAzure, constant.ChannelTypeXinference, constant.ChannelTypeOpenAI} {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{
				RelayFormat: types.RelayFormatClaude,
				ChannelMeta: &relaycommon.ChannelMeta{ChannelType: channelType, UpstreamModelName: "gpt-5.1"},
			}
			adaptor := &Adaptor{}
			adaptor.Init(info)
			var request dto.ClaudeRequest
			require.NoError(t, common.UnmarshalJsonStr(body, &request))
			converted, err := adaptor.ConvertClaudeRequest(c, info, &request)
			require.NoError(t, err)
			wire, err := common.Marshal(converted)
			require.NoError(t, err)
			assert.True(t, strings.Contains(string(wire), `"web_search_options"`), channelType)
			var codes []string
			for _, diagnostic := range info.ConversionDiagnostics() {
				codes = append(codes, diagnostic.Code)
			}
			assert.False(t, slices.Contains(codes, "hosted_web_search_omitted"), channelType)
		}
	})

	const searchCall = `{"type":"web_search_call","status":"completed","action":{"type":"search","query":"go"}}`
	const usage = `"usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13},"tool_usage":{"web_search":{"num_requests":5}}`
	output := strings.Repeat(searchCall+",", 3) + `{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Go 1.25"}]}`
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{
			name:        "json",
			contentType: "application/json",
			body:        `{"id":"resp_1","object":"response","model":"gpt-5.1","status":"completed","output":[` + output + `],` + usage + `}`,
		},
		{
			name:        "stream",
			contentType: "text/event-stream",
			body: strings.Repeat(`data: {"type":"response.output_item.done","output_index":0,"item":`+searchCall+"}\n\n", 3) +
				`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","model":"gpt-5.1","status":"completed","output":[` + output + `],` + usage + "}}\n\ndata: [DONE]\n\n",
		},
	}
	for _, tt := range tests {
		t.Run("Responses "+tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{
				RelayMode:       relayconstant.RelayModeResponses,
				RelayFormat:     types.RelayFormatOpenAIResponses,
				OriginModelName: "gpt-5.1",
				IsStream:        tt.name == "stream",
				DisablePing:     true,
				ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeAzure, UpstreamModelName: "gpt-5.1"},
			}
			adaptor := &Adaptor{}
			adaptor.Init(info)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(tt.body)),
				Header:     http.Header{"Content-Type": []string{tt.contentType}},
			}
			var apiErr *types.NewAPIError
			if info.IsStream {
				_, apiErr = OaiResponsesStreamHandler(c, info, resp)
			} else {
				_, apiErr = OaiResponsesHandler(c, info, resp)
			}
			require.Nil(t, apiErr)
			billed := map[string]int{}
			for name, tool := range info.ResponsesUsageInfo.BuiltInTools {
				if tool.CallCount > 0 {
					billed[name] = tool.CallCount
				}
			}
			assert.Equal(t, map[string]int{"bing_web_search": 5}, billed)
		})
	}
}
