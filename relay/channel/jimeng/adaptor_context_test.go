package jimeng

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	commonpkg "github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDoRequestUsesClientContext(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)
	service.InitHttpClient()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(ctx)
	info := jimengTestRelayInfo(upstream.URL)

	_, err := (&Adaptor{}).DoRequest(c, info, strings.NewReader(`{"prompt":"test"}`))

	require.Error(t, err)
	require.Zero(t, upstreamCalls.Load())
}

func TestDoRequestPropagatesLocalRequestID(t *testing.T) {
	requestID := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requestID <- request.Header.Get(commonpkg.StandardRequestIdKey)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)
	service.InitHttpClient()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Set(commonpkg.RequestIdKey, "edge-request-1")
	response, err := (&Adaptor{}).DoRequest(c, jimengTestRelayInfo(upstream.URL), strings.NewReader(`{"prompt":"test"}`))
	require.NoError(t, err)
	require.NoError(t, response.(*http.Response).Body.Close())
	require.Equal(t, "edge-request-1", <-requestID)
}

func jimengTestRelayInfo(baseURL string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelBaseUrl: baseURL,
		ApiKey:         "access-key|secret-key",
	}}
}
