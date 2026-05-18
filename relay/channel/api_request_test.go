package channel

import (
	"net/http"
	"net/http/httptest"
	"testing"

	commonpkg "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

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

func TestApplyInternalAttributionHeaders_InternalUpstreamGetsContextHeaders(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set(commonpkg.RequestIdKey, "req-context")
	ctx.Set("token_name", "prod-token")
	commonpkg.SetContextKey(ctx, constant.ContextKeyUserId, 42)
	commonpkg.SetContextKey(ctx, constant.ContextKeyTokenId, 77)
	commonpkg.SetContextKey(ctx, constant.ContextKeyChannelId, 13)
	commonpkg.SetContextKey(ctx, constant.ContextKeyChannelMultiKeyIndex, 2)
	commonpkg.SetContextKey(ctx, constant.ContextKeyOriginalModel, "gpt-4.1")

	info := &relaycommon.RelayInfo{
		RequestId:       "req-fallback",
		UserId:          1000,
		TokenId:         2000,
		OriginModelName: "fallback-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:            3000,
			ChannelMultiKeyIndex: 9,
		},
	}
	upstreamReq := httptest.NewRequest(http.MethodPost, "http://localhost:8080/v1/chat/completions", nil)
	upstreamReq.Header.Set(internalAttributionUserID, "spoofed-user")

	applyInternalAttributionHeaders(upstreamReq, ctx, info)

	require.Equal(t, "req-context", upstreamReq.Header.Get(internalAttributionRequestID))
	require.Equal(t, "42", upstreamReq.Header.Get(internalAttributionUserID))
	require.Equal(t, "77", upstreamReq.Header.Get(internalAttributionTokenID))
	require.Equal(t, "prod-token", upstreamReq.Header.Get(internalAttributionTokenName))
	require.Equal(t, "13", upstreamReq.Header.Get(internalAttributionChannelID))
	require.Equal(t, "2", upstreamReq.Header.Get(internalAttributionMultiKeyIndex))
	require.Equal(t, "gpt-4.1", upstreamReq.Header.Get(internalAttributionModel))
	require.Equal(t, "new-api", upstreamReq.Header.Get(internalAttributionSource))
}

func TestApplyInternalAttributionHeaders_PrivateAndLinkLocalUpstreamsGetHeaders(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set(commonpkg.RequestIdKey, "req-private")

	for _, upstreamURL := range []string{
		"http://127.0.0.1:8080/v1/chat/completions",
		"http://10.0.0.5:8080/v1/chat/completions",
		"http://172.16.0.5:8080/v1/chat/completions",
		"http://192.168.1.5:8080/v1/chat/completions",
		"http://169.254.10.20:8080/v1/chat/completions",
		"http://[::1]:8080/v1/chat/completions",
		"http://[fc00::1]:8080/v1/chat/completions",
		"http://[fe80::1]:8080/v1/chat/completions",
	} {
		upstreamReq := httptest.NewRequest(http.MethodPost, upstreamURL, nil)

		applyInternalAttributionHeaders(upstreamReq, ctx, nil)

		require.Equal(t, "new-api", upstreamReq.Header.Get(internalAttributionSource), upstreamURL)
		require.Equal(t, "req-private", upstreamReq.Header.Get(internalAttributionRequestID), upstreamURL)
	}
}

func TestApplyInternalAttributionHeaders_PublicUpstreamDoesNotGetHeaders(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set(commonpkg.RequestIdKey, "req-public")
	commonpkg.SetContextKey(ctx, constant.ContextKeyUserId, 42)

	upstreamReq := httptest.NewRequest(http.MethodPost, "https://api.openai.com/v1/chat/completions", nil)
	for _, name := range internalAttributionHeaderNames {
		upstreamReq.Header.Set(name, "should-be-removed")
	}

	applyInternalAttributionHeaders(upstreamReq, ctx, nil)

	for _, name := range internalAttributionHeaderNames {
		require.Empty(t, upstreamReq.Header.Values(name), name)
	}
}
