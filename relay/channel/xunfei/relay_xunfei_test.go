package xunfei

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestXunfeiMakeRequestPreservesHandshakeHTTPStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("cyber_policy"))
	}))
	t.Cleanup(upstream.Close)
	authURL := strings.Replace(upstream.URL, "http://", "ws://", 1)

	_, _, err := xunfeiMakeRequest(context.Background(), dto.GeneralOpenAIRequest{}, "", authURL, "")

	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.GetOriginalStatusCode())
	require.True(t, service.ClassifyKKAIUpstreamPolicyError(apiErr).Detected)
}
