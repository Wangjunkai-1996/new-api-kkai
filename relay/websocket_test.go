package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWssHelperMapsPublicStatusWithoutLosingUpstreamStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("cyber_policy"))
	}))
	t.Cleanup(upstream.Close)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	ctx.Set("status_code_mapping", `{"403":401}`)
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "test-upstream-key")
	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeRealtime,
		RequestURLPath: "/v1/realtime",
	}

	apiErr := WssHelper(ctx, info)

	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
	require.Equal(t, http.StatusForbidden, apiErr.GetOriginalStatusCode())
	require.True(t, service.ClassifyKKAIUpstreamPolicyError(apiErr).Detected)
}
