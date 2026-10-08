package openai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIAdaptorPlaygroundRequestURL(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	tests := []struct {
		name         string
		requestPath  string
		upstreamPath string
		playground   bool
	}{
		{"playground", "/pg/chat/completions", "/v1/chat/completions", true},
		{"playground query", "/pg/chat/completions?trace=one%2Ftwo", "/v1/chat/completions?trace=one%2Ftwo", true},
		{"API", "/v1/chat/completions", "/v1/chat/completions", false},
		{"API query", "/v1/chat/completions?trace=one%2Ftwo", "/v1/chat/completions?trace=one%2Ftwo", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, tt.requestPath, nil)
			info, err := relaycommon.GenRelayInfo(c, types.RelayFormatOpenAI, nil, nil)
			require.NoError(t, err)
			info.ChannelMeta = &relaycommon.ChannelMeta{
				ChannelType:    constant.ChannelTypeOpenAI,
				ChannelBaseUrl: "https://upstream.example",
			}

			adaptor := &Adaptor{}
			adaptor.Init(info)
			upstreamURL, err := adaptor.GetRequestURL(info)

			require.NoError(t, err)
			assert.Equal(t, "https://upstream.example"+tt.upstreamPath, upstreamURL)
			assert.Equal(t, tt.playground, info.IsPlayground)
		})
	}
}
