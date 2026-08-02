package channel_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDoApiRequestPropagatesCallerDeadlineToUpstream(t *testing.T) {
	releaseUpstream := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		select {
		case <-releaseUpstream:
		case <-time.After(time.Second):
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)
	service.InitHttpClient()

	requestContext, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(requestContext)
	info := &relaycommon.RelayInfo{
		RequestURLPath: "/v1/images/generations", RelayMode: relayconstant.RelayModeImagesGenerations,
		StartTime: time.Now(), ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: upstream.URL, ChannelType: constant.ChannelTypeOpenAI,
		},
	}

	_, err := channel.DoApiRequest(&openai.Adaptor{}, c, info, strings.NewReader(`{"model":"gpt-image-1"}`))
	close(releaseUpstream)
	require.Error(t, err)
}
