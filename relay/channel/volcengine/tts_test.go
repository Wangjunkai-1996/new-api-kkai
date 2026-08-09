package volcengine

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHandleTTSWebSocketResponsePreservesHandshakeHTTPStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("cyber_policy"))
	}))
	t.Cleanup(upstream.Close)
	requestURL := strings.Replace(upstream.URL, "http://", "ws://", 1)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "app|token"}}

	_, apiErr := handleTTSWebSocketResponse(ctx, requestURL, VolcengineTTSRequest{}, info, "mp3")

	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	require.Equal(t, http.StatusForbidden, apiErr.GetOriginalStatusCode())
	require.True(t, service.ClassifyKKAIUpstreamPolicyError(apiErr).Detected)
}
