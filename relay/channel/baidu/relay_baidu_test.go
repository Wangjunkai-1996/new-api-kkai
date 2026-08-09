package baidu

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

type baiduRoundTripFunc func(*http.Request) (*http.Response, error)

func (f baiduRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestBaiduAccessTokenPreservesCyberHTTPStatus(t *testing.T) {
	if service.GetHttpClient() == nil {
		service.InitHttpClient()
	}
	client := service.GetHttpClient()
	oldTransport := client.Transport
	client.Transport = baiduRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Body:       io.NopCloser(strings.NewReader("API key has been disabled")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	t.Cleanup(func() { client.Transport = oldTransport })

	_, err := getBaiduAccessTokenHelper("client-id|client-secret")

	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.GetOriginalStatusCode())
	classification := service.ClassifyKKAIUpstreamPolicyError(apiErr)
	require.True(t, classification.Detected)
	require.Equal(t, service.KKAIPolicyCausalityUpstreamKey, classification.Causality)
}
