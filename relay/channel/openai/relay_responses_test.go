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
		})
	}
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
