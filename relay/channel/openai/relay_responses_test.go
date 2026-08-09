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
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
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

func TestOaiResponsesStreamHandlerIgnoresClientDisconnectWithoutTerminalEvent(t *testing.T) {
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
		Body: &blockingBody{
			chunk:  []byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\n"),
			closed: make(chan struct{}),
		},
		Header: http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
		IsStream:    true,
		DisablePing: true,
	}

	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Zero(t, usage.TotalTokens)
	require.NotNil(t, info.StreamStatus)
	require.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
	require.False(t, info.StreamStatus.HasErrors())
}
