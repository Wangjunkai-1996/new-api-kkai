package claude

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type claudePolicyHandler func(*gin.Context, *http.Response, *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError)

func TestClaudeHandlersInterceptStructuredPolicyErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	handlers := []struct {
		name    string
		stream  bool
		handler claudePolicyHandler
	}{
		{name: "json", handler: ClaudeHandler},
		{name: "stream", stream: true, handler: ClaudeStreamHandler},
	}
	tests := []struct {
		name          string
		code          string
		wantStatus    int
		wantCausality string
	}{
		{name: "local audit unavailable", code: string(types.ErrorCodePolicyAuditUnavailable), wantStatus: http.StatusServiceUnavailable},
		{name: "confirmed cyber", code: "cyber_policy", wantStatus: http.StatusForbidden, wantCausality: service.KKAIPolicyCausalityClientToken},
	}

	for _, handler := range handlers {
		for _, test := range tests {
			t.Run(handler.name+"/"+test.name, func(t *testing.T) {
				payload := `{"type":"error","error":{"type":"policy_error","code":"` + test.code + `","message":"request rejected"}}`
				if handler.stream {
					payload = "data: " + payload + "\n\n"
				}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				info := &relaycommon.RelayInfo{
					RelayFormat: types.RelayFormatClaude,
					ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"},
				}
				resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload))}

				usage, apiErr := handler.handler(c, resp, info)

				require.Nil(t, usage)
				require.NotNil(t, apiErr)
				require.Equal(t, test.wantStatus, apiErr.StatusCode)
				require.True(t, types.IsSkipRetryError(apiErr))
				classification := service.ClassifyKKAIUpstreamPolicyError(apiErr)
				require.Equal(t, test.wantCausality != "", classification.Detected)
				require.Equal(t, test.wantCausality, classification.Causality)
				require.Empty(t, recorder.Body.String())
			})
		}
	}
}
