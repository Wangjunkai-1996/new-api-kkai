package channel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonpkg "github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNewUpstreamRequestUsesClientContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(ctx)

	request, err := newUpstreamRequest(c, "https://upstream.example/v1/images/generations", http.NoBody)
	require.NoError(t, err)
	cancel()
	require.ErrorIs(t, request.Context().Err(), context.Canceled)
}

func TestDoRequestCancelsUpstreamWhenClientDisconnects(t *testing.T) {
	requestStarted := make(chan struct{})
	upstreamCanceled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		<-request.Context().Done()
		close(upstreamCanceled)
	}))
	t.Cleanup(upstream.Close)
	service.InitHttpClient()

	ctx, cancel := context.WithCancel(context.Background())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(ctx)
	c.Request.Body = http.NoBody
	request, err := newUpstreamRequest(c, upstream.URL, http.NoBody)
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	requestDone := make(chan error, 1)
	go func() {
		_, requestErr := doRequest(c, request, info)
		requestDone <- requestErr
	}()

	waitForSignal(t, requestStarted, "upstream request did not start")
	cancel()
	select {
	case requestErr := <-requestDone:
		require.Error(t, requestErr)
		require.ErrorIs(t, c.Request.Context().Err(), context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("relay request did not stop after client cancellation")
	}
	waitForSignal(t, upstreamCanceled, "upstream context was not canceled")
}

func TestApplyRelayRequestIDUsesLocalTrustedID(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(commonpkg.RequestIdKey, "edge-request-1")
	request := httptest.NewRequest(http.MethodPost, "https://upstream.example/v1/images/generations", nil)

	applyRelayRequestID(request, c)

	require.Equal(t, "edge-request-1", request.Header.Get(commonpkg.StandardRequestIdKey))
}

func TestUpstreamRequestIDPrefersStandardHeader(t *testing.T) {
	header := http.Header{}
	header.Set(commonpkg.StandardRequestIdKey, "upstream-standard")
	header.Set(commonpkg.RequestIdKey, "upstream-legacy")
	require.Equal(t, "upstream-standard", upstreamRequestID(header))
}

func waitForSignal(t *testing.T, signal <-chan struct{}, timeoutMessage string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(timeoutMessage)
	}
}
