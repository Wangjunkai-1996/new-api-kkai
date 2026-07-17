package relay

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type imageFailingWriter struct {
	gin.ResponseWriter
}

func (w *imageFailingWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestImageDeliveryErrorAllowsDeliveredResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	service.IOCopyBytesGracefully(c, nil, []byte("image"))

	require.Nil(t, imageDeliveryError(c))
}

func TestStartImageSyncRequestRejectsSaturationWithoutQueueing(t *testing.T) {
	gate := newImageSyncAdmissionGate(1, 1)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 7}}
	held, ok := gate.TryAcquire(imageSyncAccountID(info))
	require.True(t, ok)
	defer held.Release()

	finish, apiErr := startImageSyncRequest(c, info, gate)

	require.Nil(t, finish)
	require.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeImageSyncConcurrencyExceeded, apiErr.GetErrorCode())
	require.Equal(t, "1", recorder.Header().Get("Retry-After"))
}

func TestImageDeliveryErrorRejectsWriteFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Writer = &imageFailingWriter{ResponseWriter: c.Writer}
	service.IOCopyBytesGracefully(c, nil, []byte("image"))

	apiErr := imageDeliveryError(c)

	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeImageDeliveryFailed, apiErr.GetErrorCode())
	require.True(t, types.IsSkipRetryError(apiErr))
}

func TestImageDeliveryErrorRejectsClientGone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	service.IOCopyBytesGracefully(c, nil, []byte("image"))

	apiErr := imageDeliveryError(c)

	require.Equal(t, statusClientClosedRequest, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeImageClientGone, apiErr.GetErrorCode())
}

func TestImageUpstreamErrorMapsBusinessDeadline(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	cancel := startImageSyncDeadline(c, time.Nanosecond)
	defer cancel()
	<-c.Request.Context().Done()

	apiErr := imageUpstreamRequestError(c, errors.New("upstream canceled"))

	require.Equal(t, http.StatusGatewayTimeout, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeImageSyncDeadlineExceeded, apiErr.GetErrorCode())
	require.NotContains(t, apiErr.Error(), "<!DOCTYPE html>")
}

func TestImageHTTPStatusErrorMapsCloudflareTimeout(t *testing.T) {
	apiErr := imageHTTPStatusError(524)

	require.Equal(t, http.StatusGatewayTimeout, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeImageSyncDeadlineExceeded, apiErr.GetErrorCode())
	require.Nil(t, imageHTTPStatusError(http.StatusBadGateway))
}
