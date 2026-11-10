package middleware

import (
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTokenAuthReadOnlyTranslatesErrors(t *testing.T) {
	require.NoError(t, i18n.Init())
	for _, tc := range []struct {
		acceptLanguage string
		want           string
	}{
		{"zh-CN", "未提供令牌"},
		{"en-US", "Token not provided"},
	} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/log/token", nil)
		c.Request.Header.Set("Accept-Language", tc.acceptLanguage)
		TokenAuthReadOnly()(c)
		assert.True(t, c.IsAborted(), tc.acceptLanguage)
		assert.Equal(t, http.StatusUnauthorized, recorder.Code, tc.acceptLanguage)
		assert.JSONEq(t, `{"success":false,"message":"`+tc.want+`"}`, recorder.Body.String(), tc.acceptLanguage)
	}
}

func TestAPIKeyFromWebSocketSubprotocol(t *testing.T) {
	tests := []struct {
		name      string
		protocols string
		wantKey   string
		wantOK    bool
	}{
		{
			name:      "responses protocol only",
			protocols: "responses",
			wantOK:    false,
		},
		{
			name:      "realtime protocol only",
			protocols: "realtime",
			wantOK:    false,
		},
		{
			name:      "responses with insecure key",
			protocols: "responses, openai-insecure-api-key.sk-test",
			wantKey:   "sk-test",
			wantOK:    true,
		},
		{
			name:      "realtime with beta and insecure key",
			protocols: "realtime, openai-insecure-api-key.sk-realtime, openai-beta.realtime-v1",
			wantKey:   "sk-realtime",
			wantOK:    true,
		},
		{
			name:      "empty insecure key",
			protocols: "responses, openai-insecure-api-key.",
			wantOK:    false,
		},
		{
			name:      "bare insecure marker is not a key",
			protocols: "openai-insecure-api-key",
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKey, gotOK := apiKeyFromWebSocketSubprotocol(tt.protocols)
			assert.Equal(t, tt.wantOK, gotOK)
			assert.Equal(t, tt.wantKey, gotKey)
		})
	}
}

func TestApplyWebSocketSubprotocolAuthorizationDoesNotOverrideProtocolOnly(t *testing.T) {
	header := http.Header{}
	header.Set("Authorization", "Bearer sk-original")
	header.Set("Sec-WebSocket-Protocol", "responses")
	header.Add("Sec-WebSocket-Protocol", "openai-beta.realtime-v1")

	assert.False(t, applyWebSocketSubprotocolAuthorization(header))
	assert.Equal(t, "Bearer sk-original", header.Get("Authorization"))
}

func TestApplyWebSocketSubprotocolAuthorizationOverridesWithInsecureKey(t *testing.T) {
	header := http.Header{}
	header.Set("Authorization", "Bearer sk-original")
	header.Set("Sec-WebSocket-Protocol", "responses, openai-insecure-api-key.sk-from-protocol")

	assert.True(t, applyWebSocketSubprotocolAuthorization(header))
	assert.Equal(t, "Bearer sk-from-protocol", header.Get("Authorization"))
}

func TestApplyWebSocketSubprotocolAuthorizationReadsRepeatedHeaders(t *testing.T) {
	header := http.Header{}
	header.Set("Authorization", "Bearer sk-original")
	header.Add("Sec-WebSocket-Protocol", "responses")
	header.Add("Sec-WebSocket-Protocol", "openai-insecure-api-key.sk-later-field")

	assert.True(t, applyWebSocketSubprotocolAuthorization(header))
	assert.Equal(t, "Bearer sk-later-field", header.Get("Authorization"))
}
