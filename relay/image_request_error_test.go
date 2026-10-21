package relay

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageHelperClassifiesTransportTimeoutWithoutExposingDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	client := service.GetHttpClient()
	originalTransport := client.Transport
	t.Cleanup(func() { client.Transport = originalTransport })

	for _, test := range []struct {
		name           string
		beforeDispatch error
		transportError error
		wantCode       string
		wantOriginal   types.ErrorCode
		wantDispatches int
	}{
		{name: "deadline before dispatch", beforeDispatch: context.DeadlineExceeded, wantCode: "image_request_timeout", wantOriginal: "request_timeout"},
		{name: "cancellation before dispatch", beforeDispatch: context.Canceled, wantCode: "image_service_unavailable", wantOriginal: types.ErrorCodeDoRequestFailed},
		{name: "transport deadline", transportError: context.DeadlineExceeded, wantCode: "image_request_timeout", wantOriginal: "request_timeout", wantDispatches: 1},
		{name: "typed network timeout", transportError: &net.DNSError{Err: "https://private.example?key=provider-secret", Name: "private.example", IsTimeout: true}, wantCode: "image_request_timeout", wantOriginal: "request_timeout", wantDispatches: 1},
		{name: "plain message mentioning timeout", transportError: errors.New("timeout: visit https://private.example?key=provider-secret"), wantCode: "image_service_unavailable", wantOriginal: types.ErrorCodeDoRequestFailed, wantDispatches: 1},
		{name: "transport cancellation", transportError: context.Canceled, wantCode: "image_service_unavailable", wantOriginal: types.ErrorCodeDoRequestFailed, wantDispatches: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			requestContext := context.Background()
			if test.beforeDispatch == context.DeadlineExceeded {
				var cancel context.CancelFunc
				requestContext, cancel = context.WithDeadline(requestContext, time.Unix(1, 0))
				t.Cleanup(cancel)
			} else if test.beforeDispatch == context.Canceled {
				var cancel context.CancelFunc
				requestContext, cancel = context.WithCancel(requestContext)
				cancel()
			}
			dispatches := 0
			client.Transport = taskVisibilityRoundTripper(func(*http.Request) (*http.Response, error) {
				dispatches++
				return nil, test.transportError
			})

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(requestContext)
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(string(constant.ContextKeyChannelType), constant.ChannelTypeOpenAI)
			c.Set(string(constant.ContextKeyChannelBaseUrl), "https://private.example")
			c.Set(string(constant.ContextKeyChannelKey), "provider-secret")
			c.Set(string(constant.ContextKeyOriginalModel), "gpt-image-1")
			info := &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeImagesGenerations, RelayFormat: types.RelayFormatOpenAIImage,
				OriginModelName: "gpt-image-1", RequestURLPath: "/v1/images/generations",
				Request: &dto.ImageRequest{Model: "gpt-image-1", Prompt: "draw a tree"},
			}

			apiErr := ImageHelper(c, info)
			require.NotNil(t, apiErr)
			assert.Equal(t, test.wantDispatches, dispatches)
			assert.Equal(t, types.ErrorCodeDoRequestFailed, apiErr.GetErrorCode())
			assert.Equal(t, test.wantOriginal, apiErr.GetOriginalErrorCode())
			assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
			assert.False(t, types.IsSkipRetryError(apiErr))
			assert.Equal(t, test.wantCode, service.ClassifyImageGenerationError(apiErr))
			assert.NotSame(t, apiErr, apiErr.Err)
			assert.False(t, errors.Is(apiErr, context.DeadlineExceeded), "the transport cause remains redacted")
			message := "upstream error: do request failed"
			if test.beforeDispatch != nil {
				message = "upstream error: do request canceled before dispatch"
			}
			assert.Equal(t, message, apiErr.Error())
			assert.Equal(t, message, apiErr.ToOpenAIError().Message)
		})
	}
}
