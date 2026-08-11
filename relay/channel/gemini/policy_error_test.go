package gemini

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

type geminiPolicyHandler func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)

func TestGeminiHandlersInterceptStructuredPolicyErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	handlers := []struct {
		name    string
		stream  bool
		handler geminiPolicyHandler
	}{
		{name: "chat json", handler: GeminiChatHandler},
		{name: "native json", handler: GeminiTextGenerationHandler},
		{name: "responses json", handler: GeminiResponsesHandler},
		{name: "chat stream", stream: true, handler: GeminiChatStreamHandler},
	}
	tests := []struct {
		name          string
		payload       string
		wantStatus    int
		wantCausality string
	}{
		{
			name:       "local audit unavailable in Gemini detail reason",
			payload:    `{"error":{"code":503,"message":"audit unavailable","status":"UNAVAILABLE","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"policy_audit_unavailable","domain":"sub2api"}]}}`,
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:          "confirmed cyber in Gemini detail reason",
			payload:       `{"error":{"code":403,"message":"request rejected","status":"PERMISSION_DENIED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"cyber_policy","domain":"sub2api"}]}}`,
			wantStatus:    http.StatusForbidden,
			wantCausality: service.KKAIPolicyCausalityClientToken,
		},
		{
			name:       "context incomplete in Gemini detail reason",
			payload:    `{"error":{"code":422,"message":"context incomplete","status":"FAILED_PRECONDITION","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"policy_context_incomplete","domain":"sub2api"}]}}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
	}

	for _, handler := range handlers {
		for _, test := range tests {
			t.Run(handler.name+"/"+test.name, func(t *testing.T) {
				payload := test.payload
				if handler.stream {
					payload = "data: " + payload + "\n\n"
				}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-test:generateContent", nil)
				info := &relaycommon.RelayInfo{
					RelayFormat: types.RelayFormatGemini,
					ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-test"},
				}
				resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload))}

				usage, apiErr := handler.handler(c, info, resp)

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
