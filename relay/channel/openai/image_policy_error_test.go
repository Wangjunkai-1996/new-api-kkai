package openai

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type imagePolicyHandler func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)

func TestOpenaiImageStreamHandlerInterceptsPolicyErrorsBeforeForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	for _, test := range imagePolicyErrorCases() {
		t.Run(test.name, func(t *testing.T) {
			body := `data: {"type":"error","error":{"message":"request rejected","type":"policy_error","code":"` + test.code + `"}}` + "\n\n"
			c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", true)

			usage, apiErr := OpenaiImageStreamHandler(c, info, resp)

			assertImagePolicyError(t, usage, apiErr, test.wantStatus, test.wantCausality)
			require.Empty(t, recorder.Body.String())
		})
	}
}

func TestOpenaiImageJSONHandlersInterceptPolicyErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handlers := []struct {
		name    string
		stream  bool
		handler imagePolicyHandler
	}{
		{name: "non stream", handler: OpenaiImageHandler},
		{name: "stream JSON fallback", stream: true, handler: OpenaiImageStreamHandler},
	}
	for _, handler := range handlers {
		for _, test := range imagePolicyErrorCases() {
			t.Run(handler.name+"/"+test.name, func(t *testing.T) {
				body := `{"error":{"message":"request rejected","type":"policy_error","code":"` + test.code + `"}}`
				c, recorder, resp, info := newImageTestContext(t, body, "application/json", handler.stream)

				usage, apiErr := handler.handler(c, info, resp)

				assertImagePolicyError(t, usage, apiErr, test.wantStatus, test.wantCausality)
				require.Empty(t, recorder.Body.String())
			})
		}
	}
}

func imagePolicyErrorCases() []struct {
	name          string
	code          string
	wantStatus    int
	wantCausality string
} {
	return []struct {
		name          string
		code          string
		wantStatus    int
		wantCausality string
	}{
		{name: "local audit unavailable", code: string(types.ErrorCodePolicyAuditUnavailable), wantStatus: http.StatusServiceUnavailable},
		{name: "confirmed cyber", code: "cyber_policy", wantStatus: http.StatusForbidden, wantCausality: service.KKAIPolicyCausalityClientToken},
	}
}

func assertImagePolicyError(t *testing.T, usage *dto.Usage, apiErr *types.NewAPIError, wantStatus int, wantCausality string) {
	t.Helper()
	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, wantStatus, apiErr.StatusCode)
	require.True(t, types.IsSkipRetryError(apiErr))
	classification := service.ClassifyKKAIUpstreamPolicyError(apiErr)
	require.Equal(t, wantCausality != "", classification.Detected)
	require.Equal(t, wantCausality, classification.Causality)
}
