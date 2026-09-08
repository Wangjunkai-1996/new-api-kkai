package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newResponsesStreamHandlerTest(t *testing.T, body string) (*gin.Context, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()

	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(common.RequestIdKey, "responses-stream-test")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o"},
		IsStream:    true,
		DisablePing: true,
	}
	return c, resp, info
}

func TestOaiResponsesStreamHandlerAcceptsSuccessfulTerminalEvents(t *testing.T) {
	for _, eventType := range []string{"response.completed", "response.done"} {
		t.Run(eventType, func(t *testing.T) {
			body := `data: {"type":"` + eventType + `","response":{"status":"completed","usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}` + "\n\n"
			c, resp, info := newResponsesStreamHandlerTest(t, body)

			usage, apiErr := OaiResponsesStreamHandler(c, info, resp)

			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			require.Equal(t, 2, usage.PromptTokens)
			require.Equal(t, 3, usage.CompletionTokens)
			require.Equal(t, 5, usage.TotalTokens)
			require.NotNil(t, info.StreamStatus)
			require.True(t, info.StreamStatus.IsNormalEnd())
			require.False(t, info.StreamStatus.HasErrors())
		})
	}
}

func TestOaiResponsesHandlerMarksOnlyContentFilteredIncompleteResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		body       string
		wantReason string
	}{
		{
			name:       "content filter",
			body:       `{"id":"resp_1","status":"incomplete","incomplete_details":{"reason":"content_filter"},"usage":{"input_tokens":100,"output_tokens":0,"total_tokens":100}}`,
			wantReason: "openai_responses_incomplete_reason=content_filter",
		},
		{
			name: "token limit",
			body: `{"id":"resp_2","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":100,"output_tokens":0,"total_tokens":100}}`,
		},
		{
			name: "completed response ignores stale details",
			body: `{"id":"resp_3","status":"completed","incomplete_details":{"reason":"content_filter"},"usage":{"input_tokens":100,"output_tokens":0,"total_tokens":100}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			usage, apiErr := OaiResponsesHandler(c, &relaycommon.RelayInfo{}, &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(test.body)),
			})

			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			require.Equal(t, 100, usage.PromptTokens)
			require.Equal(t, test.wantReason, common.GetContextKeyString(c, constant.ContextKeyAdminRejectReason))
		})
	}
}

func TestOaiResponsesStreamHandlerMarksLocallyEstimatedUsage(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantCompletion int
	}{
		{
			name:           "missing prompt usage",
			body:           "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"output_tokens\":3,\"total_tokens\":3}}}\n\n",
			wantCompletion: 3,
		},
		{
			name: "missing all usage",
			body: "data: {\"type\":\"response.output_text.delta\",\"delta\":\"locally counted output\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, resp, info := newResponsesStreamHandlerTest(t, test.body)
			info.SetEstimatePromptTokens(17)

			usage, apiErr := OaiResponsesStreamHandler(c, info, resp)

			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			require.Equal(t, 17, usage.PromptTokens)
			if test.wantCompletion > 0 {
				require.Equal(t, test.wantCompletion, usage.CompletionTokens)
			} else {
				require.Positive(t, usage.CompletionTokens)
			}
			require.True(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
		})
	}
}

func TestOaiResponsesStreamHandlerRejectsFailedOrUnterminatedStreams(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantMessage string
	}{
		{
			name:        "failed event preserves upstream error",
			body:        "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"message\":\"upstream overloaded\",\"type\":\"server_error\",\"code\":\"overloaded\"}}}\n\n",
			wantMessage: "upstream overloaded",
		},
		{
			name:        "incomplete event includes reason",
			body:        "data: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\",\"incomplete_details\":{\"reason\":\"max_output_tokens\"}}}\n\n",
			wantMessage: "response.incomplete (reason=max_output_tokens)",
		},
		{
			name:        "eof before terminal event",
			body:        "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\n",
			wantMessage: "without a successful terminal event",
		},
		{
			name:        "done marker without terminal event",
			body:        "data: [DONE]\n\n",
			wantMessage: "without a successful terminal event",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, resp, info := newResponsesStreamHandlerTest(t, test.body)

			usage, apiErr := OaiResponsesStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			require.Contains(t, apiErr.Error(), test.wantMessage)
			require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
			require.True(t, types.IsSkipRetryError(apiErr))
			require.NotNil(t, info.StreamStatus)
			require.True(t, info.StreamStatus.HasErrors())
			if test.name == "failed event preserves upstream error" {
				require.Equal(t, "response.failed", common.GetContextKeyString(c, constant.ContextKeyResponsesStreamFailedEventType))
				require.Equal(t, "overloaded", common.GetContextKeyString(c, constant.ContextKeyResponsesStreamUpstreamErrorCode))
				require.Equal(t, "upstream overloaded", common.GetContextKeyString(c, constant.ContextKeyResponsesStreamUpstreamErrorMessage))
				require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamOutputStarted))
				require.Equal(t, 1, common.GetContextKeyInt(c, constant.ContextKeyResponsesStreamEventCount))
				require.Equal(t, http.StatusOK, common.GetContextKeyInt(c, constant.ContextKeyResponsesStreamUpstreamStatusCode))
				require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamTerminalError))
				require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamRetryAllowed))
			}
		})
	}
}

func TestOaiResponsesStreamHandlerRecordsWhitespaceSemanticOutputBeforeFailure(t *testing.T) {
	body := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\" \"}\n\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"message\":\"overloaded\",\"code\":\"server_is_overloaded\"}}}\n\n"
	c, resp, info := newResponsesStreamHandlerTest(t, body)

	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	require.True(t, types.IsSkipRetryError(apiErr))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamOutputStarted))
	require.Equal(t, "server_is_overloaded", common.GetContextKeyString(c, constant.ContextKeyResponsesStreamUpstreamErrorCode))
	require.Equal(t, 3, common.GetContextKeyInt(c, constant.ContextKeyResponsesStreamEventCount))
	require.Equal(t, http.StatusOK, common.GetContextKeyInt(c, constant.ContextKeyResponsesStreamUpstreamStatusCode))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamTerminalError))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamRetryAllowed))
}

func TestResponsesStreamSemanticOutputDetectorUsesRawEventFields(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		data      string
		want      bool
	}{
		{
			name:      "function arguments done",
			eventType: "response.function_call_arguments.done",
			data:      `{"type":"response.function_call_arguments.done","arguments":"{\"x\":1}"}`,
			want:      true,
		},
		{
			name:      "whitespace function arguments done is output",
			eventType: "response.function_call_arguments.done",
			data:      `{"type":"response.function_call_arguments.done","arguments":" "}`,
			want:      true,
		},
		{
			name:      "empty function arguments done",
			eventType: "response.function_call_arguments.done",
			data:      `{"type":"response.function_call_arguments.done","arguments":""}`,
		},
		{
			name:      "custom tool input done",
			eventType: "response.custom_tool_call_input.done",
			data:      `{"type":"response.custom_tool_call_input.done","input":"patch"}`,
			want:      true,
		},
		{
			name:      "reasoning encrypted item",
			eventType: "response.output_item.added",
			data:      `{"type":"response.output_item.added","item":{"type":"reasoning","encrypted_content":"opaque"}}`,
			want:      true,
		},
		{
			name:      "empty reasoning skeleton",
			eventType: "response.output_item.added",
			data:      `{"type":"response.output_item.added","item":{"type":"reasoning","summary":[]}}`,
		},
		{
			name:      "whitespace reasoning summary part is output",
			eventType: "response.reasoning_summary_part.added",
			data:      `{"type":"response.reasoning_summary_part.added","part":{"type":"summary_text","text":" "}}`,
			want:      true,
		},
		{
			name:      "audio delta",
			eventType: "response.audio_transcript.delta",
			data:      `{"type":"response.audio_transcript.delta","delta":"spoken"}`,
			want:      true,
		},
		{
			name:      "image partial",
			eventType: "response.image_generation_call.partial_image",
			data:      `{"type":"response.image_generation_call.partial_image","partial_image_b64":"abc"}`,
			want:      true,
		},
		{
			name:      "done item is a replay boundary",
			eventType: "response.output_item.done",
			data:      `{"type":"response.output_item.done","item":{"type":"reasoning"}}`,
			want:      true,
		},
		{
			name:      "empty completed response",
			eventType: "response.completed",
			data:      `{"type":"response.completed","response":{"output":[]}}`,
		},
		{
			name:      "completed function call output",
			eventType: "response.completed",
			data:      `{"type":"response.completed","response":{"output":[{"type":"function_call","arguments":"{\"x\":1}"}]}}`,
			want:      true,
		},
		{
			name:      "error is not semantic output",
			eventType: "error",
			data:      `{"type":"error","error":{"code":"server_is_overloaded"}}`,
		},
		{
			name:      "future event is conservatively guarded",
			eventType: "response.future_event",
			data:      `{"type":"response.future_event"}`,
			want:      true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := &dto.ResponsesStreamResponse{Type: test.eventType}
			require.Equal(t, test.want, responsesStreamEventStartsSemanticOutput(response, test.data))
		})
	}
}

func TestResponsesStreamFirstResponseDetectorIgnoresControlAndUnknownEvents(t *testing.T) {
	for _, test := range []struct {
		name string
		typ  string
		data string
		want bool
	}{
		{name: "created", typ: "response.created", data: `{"type":"response.created"}`},
		{name: "future control", typ: "response.future_event", data: `{"type":"response.future_event"}`},
		{name: "text delta", typ: "response.output_text.delta", data: `{"type":"response.output_text.delta","delta":"x"}`, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, responsesStreamEventStartsFirstResponse(&dto.ResponsesStreamResponse{Type: test.typ}, test.data))
		})
	}
}

func TestOaiResponsesStreamHandlerBoundsAndRedactsFailureDiagnostics(t *testing.T) {
	message := `upstream rejected Authorization: "Bearer sk-client-secret" and "api_key":"provider-secret"; ` + strings.Repeat("x", responsesStreamDiagnosticMessageLimit)
	payload, err := common.Marshal(gin.H{
		"type": "response.failed",
		"response": gin.H{
			"status": "failed",
			"error":  gin.H{"message": message, "code": "overloaded"},
		},
	})
	require.NoError(t, err)
	c, resp, info := newResponsesStreamHandlerTest(t, "data: "+string(payload)+"\n\n")

	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	got := common.GetContextKeyString(c, constant.ContextKeyResponsesStreamUpstreamErrorMessage)

	require.LessOrEqual(t, len(got), responsesStreamDiagnosticMessageLimit)
	require.Contains(t, got, "upstream rejected")
	require.Equal(t, "response.failed", common.GetContextKeyString(c, constant.ContextKeyResponsesStreamFailedEventType))
	require.NotContains(t, got, "Bearer sk-client-secret")
	require.NotContains(t, got, "Bearer")
	require.NotContains(t, got, "sk-client-secret")
	require.NotContains(t, got, "provider-secret")
	require.Contains(t, got, "[redacted]")
}

func TestOaiResponsesStreamHandlerClassifiesTopLevelPolicyErrorsBeforeForwarding(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		wantCode      types.ErrorCode
		wantStatus    int
		wantCausality string
	}{
		{
			name:          "confirmed cyber",
			body:          "data: {\"type\":\"error\",\"error\":{\"message\":\"request rejected\",\"type\":\"policy_error\",\"code\":\"cyber_policy\"}}\n\n",
			wantCode:      types.ErrorCode("cyber_policy"),
			wantStatus:    http.StatusForbidden,
			wantCausality: service.KKAIPolicyCausalityClientToken,
		},
		{
			name:       "local audit unavailable",
			body:       "data: {\"type\":\"error\",\"error\":{\"message\":\"audit unavailable\",\"type\":\"new_api_error\",\"code\":\"policy_audit_unavailable\"}}\n\n",
			wantCode:   types.ErrorCodePolicyAuditUnavailable,
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, resp, info := newResponsesStreamHandlerTest(t, test.body)

			usage, apiErr := OaiResponsesStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			require.Equal(t, test.wantCode, apiErr.GetErrorCode())
			require.Equal(t, test.wantStatus, apiErr.StatusCode)
			require.True(t, types.IsSkipRetryError(apiErr))
			classification := service.ClassifyKKAIUpstreamPolicyError(apiErr)
			require.Equal(t, test.wantCausality != "", classification.Detected)
			require.Equal(t, test.wantCausality, classification.Causality)
			require.False(t, c.Writer.Written())
		})
	}
}

func TestOaiResponsesStreamHandlerDetectsTerminalPolicyAfterClientDisconnect(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
	c.Set(common.RequestIdKey, "responses-client-disconnect-test")
	c.Writer = &cancelAfterWriter{
		ResponseWriter: c.Writer,
		needle:         "response.created",
		cancel:         cancel,
	}

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\n" +
				"data: {\"type\":\"error\",\"error\":{\"message\":\"request rejected\",\"type\":\"policy_error\",\"code\":\"cyber_policy\"}}\n\n",
		)),
		Header: http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
		IsStream:    true,
		DisablePing: true,
	}

	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCode("cyber_policy"), apiErr.GetErrorCode())
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	require.True(t, types.IsSkipRetryError(apiErr))
	require.Equal(t, service.KKAIPolicyCausalityClientToken, service.ClassifyKKAIUpstreamPolicyError(apiErr).Causality)
	require.NotNil(t, info.StreamStatus)
	require.True(t, info.StreamStatus.HasErrors())
}
