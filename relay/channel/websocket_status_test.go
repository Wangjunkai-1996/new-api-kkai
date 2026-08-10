package channel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type websocketStatusTestAdaptor struct {
	Adaptor
	requestURL string
}

func (a websocketStatusTestAdaptor) GetRequestURL(*relaycommon.RelayInfo) (string, error) {
	return a.requestURL, nil
}

func (websocketStatusTestAdaptor) SetupRequestHeader(*gin.Context, *http.Header, *relaycommon.RelayInfo) error {
	return nil
}

func TestDoWssRequestPreservesHandshakeHTTPStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("cyber_policy"))
	}))
	t.Cleanup(upstream.Close)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	adaptor := websocketStatusTestAdaptor{requestURL: strings.Replace(upstream.URL, "http://", "ws://", 1)}

	_, err := DoWssRequest(adaptor, ctx, info, nil)

	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.GetOriginalStatusCode())
	require.True(t, service.ClassifyKKAIUpstreamPolicyError(apiErr).Detected)
}

func TestDoWssRequestPreservesLocalSessionBlockWithoutCyberClassification(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"session_blocked_by_cyber_policy"}}`))
	}))
	t.Cleanup(upstream.Close)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	adaptor := websocketStatusTestAdaptor{requestURL: strings.Replace(upstream.URL, "http://", "ws://", 1)}

	_, err := DoWssRequest(adaptor, ctx, info, nil)

	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, types.ErrorCodeSessionBlockedByCyberPolicy, apiErr.GetErrorCode())
	require.True(t, types.IsSkipRetryError(apiErr))
	require.False(t, service.ClassifyKKAIUpstreamPolicyError(apiErr).Detected)
}
