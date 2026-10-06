package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareResponsesCompactUsesCapableAdaptors(t *testing.T) {
	for _, test := range []struct {
		name        string
		channelType int
		allowed     bool
	}{
		{"openai", constant.ChannelTypeOpenAI, true},
		{"codex", constant.ChannelTypeCodex, true},
		{"newapi", constant.ChannelTypeNewAPI, true},
		{"sub2api", constant.ChannelTypeSub2API, true},
		{"advanced_custom", constant.ChannelTypeAdvancedCustom, true},
		{"gemini", constant.ChannelTypeGemini, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(`{"model":"gpt-5","input":[]}`))
			common.SetContextKey(c, constant.ContextKeyChannelType, test.channelType)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://provider.example")
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-5")
			if test.channelType == constant.ChannelTypeAdvancedCustom {
				common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
					AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
						IncomingPath: "/v1/responses/compact", UpstreamPath: "/v1/responses/compact", Converter: "none",
					}}},
				})
			}
			request := &dto.OpenAIResponsesRequest{Model: "gpt-5", Input: []byte(`[]`)}
			info := &relaycommon.RelayInfo{Request: request, RelayMode: relayconstant.RelayModeResponsesCompact,
				RelayFormat: types.RelayFormatOpenAIResponsesCompaction, OriginModelName: "gpt-5",
				RequestURLPath: "/v1/responses/compact", UsingGroup: "default"}
			adaptor, body, closer, apiErr := PrepareResponsesRequest(c, info, request)
			if !test.allowed {
				require.NotNil(t, apiErr)
				assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
				assert.True(t, types.IsSkipRetryError(apiErr))
				assert.Nil(t, body)
				return
			}
			require.Nil(t, apiErr)
			t.Cleanup(func() { require.NoError(t, closer.Close()) })
			payload, err := io.ReadAll(body)
			require.NoError(t, err)
			assert.Contains(t, string(payload), `"model":"gpt-5"`)
			requestURL, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			assert.True(t, strings.HasSuffix(requestURL, "/responses/compact"), requestURL)
		})
	}
}
