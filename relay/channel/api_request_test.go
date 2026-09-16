package channel

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"testing"
	"time"

	commonpkg "github.com/QuantumNous/new-api/common"
	systemconstant "github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cancellationObservingResponseBody struct {
	ctx                 context.Context
	canceledBeforeClose bool
}

func (b *cancellationObservingResponseBody) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (b *cancellationObservingResponseBody) Close() error {
	select {
	case <-b.ctx.Done():
		b.canceledBeforeClose = true
	default:
	}
	return nil
}

func TestUpstreamPolicyResponseBodyCancelsBeforeClosingTransport(t *testing.T) {
	requestContext, cancel := context.WithCancel(context.Background())
	innerBody := &cancellationObservingResponseBody{ctx: requestContext}
	body := &upstreamPolicyResponseBody{ReadCloser: innerBody, cancel: cancel}

	require.NoError(t, body.Close())

	assert.True(t, innerBody.canceledBeforeClose, "a blocking transport close must not delay upstream cancellation")
	assert.ErrorIs(t, requestContext.Err(), context.Canceled)
}

func TestUpstreamPolicyHTTPClientExtendsImageTimeout(t *testing.T) {
	baseClient := &http.Client{Timeout: time.Minute}
	imageInfo := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}

	imageClient := upstreamPolicyHTTPClient(baseClient, imageInfo)

	require.NotSame(t, baseClient, imageClient)
	assert.Equal(t, time.Minute, baseClient.Timeout)
	assert.Equal(t, imageUpstreamRequestTimeout, imageClient.Timeout)

	chatClient := upstreamPolicyHTTPClient(baseClient, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeChatCompletions})
	assert.Same(t, baseClient, chatClient)

	unlimitedClient := &http.Client{}
	assert.Same(t, unlimitedClient, upstreamPolicyHTTPClient(unlimitedClient, imageInfo))
}

func TestExecuteTaskRequestClassifiesRequestWritePhase(t *testing.T) {
	tests := []struct {
		name               string
		writeAttempted     bool
		submissionPossible bool
	}{
		{name: "pre-write failure", submissionPossible: false},
		{name: "post-write failure", writeAttempted: true, submissionPossible: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "https://example.invalid/tasks", nil)
			_, err := executeTaskRequest(request, func(tracedRequest *http.Request) (*http.Response, error) {
				if test.writeAttempted {
					trace := httptrace.ContextClientTrace(tracedRequest.Context())
					require.NotNil(t, trace)
					require.NotNil(t, trace.WroteRequest)
					trace.WroteRequest(httptrace.WroteRequestInfo{})
				}
				return nil, errors.New("transport failure")
			})
			require.Error(t, err)

			var requestErr *TaskRequestError
			require.ErrorAs(t, err, &requestErr)
			require.Equal(t, test.submissionPossible, requestErr.SubmissionPossible())
		})
	}
}

func TestDoRequestRecordsUpstreamHeaderTime(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)
	service.InitHttpClient()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Body = http.NoBody
	request, err := http.NewRequest(http.MethodPost, upstream.URL, http.NoBody)
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{
		StartTime:   time.Now(),
		ChannelMeta: &relaycommon.ChannelMeta{},
	}

	response, err := doRequest(ctx, request, info)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.True(t, info.UpstreamHeaderTime.After(info.StartTime))
}

func TestDoRequestRecordsGatewayRequestID(t *testing.T) {
	service.InitHttpClient()
	for _, tt := range []struct {
		name, oneAPI, client, generic, want string
	}{
		{"Sub2 ID", "", "sub2-request-123", "provider-request", "sub2-request-123"},
		{"existing priority", "oneapi-request", "sub2-request", "provider-request", "oneapi-request"},
		{"provider ID only", "", "", "provider-request", ""},
		{"absent", "", "", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set(commonpkg.RequestIdKey, tt.oneAPI)
				w.Header().Set("X-Client-Request-ID", tt.client)
				w.Header().Set("X-Request-Id", tt.generic)
				w.WriteHeader(http.StatusBadGateway)
			}))
			t.Cleanup(upstream.Close)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", http.NoBody)
			request, err := http.NewRequest(http.MethodPost, upstream.URL, http.NoBody)
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
			response, err := doRequest(ctx, request, info)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			assert.Equal(t, tt.want, ctx.GetString(commonpkg.UpstreamRequestIdKey))
		})
	}
}

func TestDoRequestClearsPreviousAttemptRequestID(t *testing.T) {
	service.InitHttpClient()
	for _, tt := range []struct {
		name, generic  string
		networkFailure bool
	}{
		{name: "no correlation headers"},
		{name: "provider ID only", generic: "provider-request"},
		{name: "network failure", networkFailure: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/first" {
					w.Header().Set("X-Client-Request-ID", "first-sub2-request")
				} else if tt.generic != "" {
					w.Header().Set("X-Request-Id", tt.generic)
				}
				w.WriteHeader(http.StatusBadGateway)
			}))
			t.Cleanup(upstream.Close)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", http.NoBody)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
			request, err := http.NewRequest(http.MethodPost, upstream.URL+"/first", http.NoBody)
			require.NoError(t, err)
			response, err := doRequest(ctx, request, info)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.Equal(t, "first-sub2-request", ctx.GetString(commonpkg.UpstreamRequestIdKey))

			if tt.networkFailure {
				upstream.Close()
			}
			request, err = http.NewRequest(http.MethodPost, upstream.URL+"/second", http.NoBody)
			require.NoError(t, err)
			response, err = doRequest(ctx, request, info)
			if tt.networkFailure {
				require.Error(t, err)
				assert.Nil(t, response)
			} else {
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
			}
			assert.Empty(t, ctx.GetString(commonpkg.UpstreamRequestIdKey))
		})
	}
}

func TestDoRequestNegotiatesSub2TTFTForOpenAIStreams(t *testing.T) {
	service.InitHttpClient()
	tests := []struct {
		name       string
		stream     bool
		apiType    int
		wantHeader string
	}{
		{name: "OpenAI stream", stream: true, apiType: systemconstant.APITypeOpenAI, wantHeader: "1"},
		{name: "OpenAI nonstream", apiType: systemconstant.APITypeOpenAI},
		{name: "other stream", stream: true, apiType: systemconstant.APITypeAnthropic},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := make(chan string, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				header <- r.Header.Get("X-Sub2-TTFT")
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(upstream.Close)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", http.NoBody)
			req, err := http.NewRequest(http.MethodPost, upstream.URL, http.NoBody)
			require.NoError(t, err)
			req.Header.Set("X-Sub2-TTFT", "client-value")
			info := &relaycommon.RelayInfo{
				IsStream:    tt.stream,
				DisablePing: true,
				ChannelMeta: &relaycommon.ChannelMeta{ApiType: tt.apiType},
			}

			resp, err := doRequest(ctx, req, info)

			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, tt.wantHeader, <-header)
		})
	}
}

func TestSetupApiRequestHeaderUsesUpstreamStreamWithoutChangingClientMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		IsStream:         false,
		UpstreamIsStream: true,
	}
	header := http.Header{}

	SetupApiRequestHeader(info, c, &header)

	require.Equal(t, "text/event-stream", header.Get("Accept"))
	require.False(t, info.IsStream)
}

func TestProcessHeaderOverride_ChannelTestSkipsPassthroughRules(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Empty(t, headers)
}

func TestProcessHeaderOverride_ChannelTestSkipsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	_, ok := headers["x-upstream-trace"]
	require.False(t, ok)
}

func TestProcessHeaderOverride_NonTestKeepsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-upstream-trace"])
}

func TestProcessHeaderOverride_RuntimeOverrideIsFinalHeaderMap(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		IsChannelTest:             false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"x-static":  "runtime-value",
			"x-runtime": "runtime-only",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
				"X-Legacy": "legacy-only",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "runtime-value", headers["x-static"])
	require.Equal(t, "runtime-only", headers["x-runtime"])
	_, exists := headers["x-legacy"]
	require.False(t, exists)
}

func TestProcessHeaderOverride_PassthroughSkipsAcceptEncoding(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")
	ctx.Request.Header.Set("Accept-Encoding", "gzip")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-trace-id"])

	_, hasAcceptEncoding := headers["accept-encoding"]
	require.False(t, hasAcceptEncoding)
}

func TestProcessHeaderOverride_PassHeadersTemplateSetsRuntimeHeaders(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set("Originator", "Codex CLI")
	ctx.Request.Header.Set("Session_id", "sess-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		RequestHeaders: map[string]string{
			"Originator": "Codex CLI",
			"Session_id": "sess-123",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]any{
				"operations": []any{
					map[string]any{
						"mode":  "pass_headers",
						"value": []any{"Originator", "Session_id", "X-Codex-Beta-Features"},
					},
				},
			},
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
			},
		},
	}

	_, err := relaycommon.ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)
	require.NoError(t, err)
	require.True(t, info.UseRuntimeHeadersOverride)
	require.Equal(t, "Codex CLI", info.RuntimeHeadersOverride["originator"])
	require.Equal(t, "sess-123", info.RuntimeHeadersOverride["session_id"])
	_, exists := info.RuntimeHeadersOverride["x-codex-beta-features"]
	require.False(t, exists)
	require.Equal(t, "legacy-value", info.RuntimeHeadersOverride["x-static"])

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "Codex CLI", headers["originator"])
	require.Equal(t, "sess-123", headers["session_id"])
	_, exists = headers["x-codex-beta-features"]
	require.False(t, exists)

	upstreamReq := httptest.NewRequest(http.MethodPost, "https://example.com/v1/responses", nil)
	applyHeaderOverrideToRequest(upstreamReq, headers)
	require.Equal(t, "Codex CLI", upstreamReq.Header.Get("Originator"))
	require.Equal(t, "sess-123", upstreamReq.Header.Get("Session_id"))
	require.Empty(t, upstreamReq.Header.Get("X-Codex-Beta-Features"))
}
