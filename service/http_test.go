package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type failingResponseWriter struct {
	gin.ResponseWriter
	err error
}

func (w *failingResponseWriter) Write(_ []byte) (int, error) {
	return 0, w.err
}

func TestIOCopyBytesGracefullyReportsDelivered(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	outcome := IOCopyBytesGracefully(c, nil, []byte("image"))

	require.Equal(t, DeliveryOutcomeDelivered, outcome)
	require.Equal(t, "image", recorder.Body.String())
}

func TestIOCopyBytesGracefullyReportsClientGone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)

	outcome := IOCopyBytesGracefully(c, nil, []byte("image"))

	require.Equal(t, DeliveryOutcomeClientGone, outcome)
}

func TestIOCopyBytesGracefullyReportsWriteFailure(t *testing.T) {
	wantErr := errors.New("write failed")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Writer = &failingResponseWriter{ResponseWriter: c.Writer, err: wantErr}

	outcome := IOCopyBytesGracefully(c, nil, []byte("image"))

	require.Equal(t, DeliveryOutcomeWriteFailed, outcome)
	recorded, ok := ResponseDeliveryOutcome(c)
	require.True(t, ok)
	require.Equal(t, DeliveryOutcomeWriteFailed, recorded)
}

func TestIOCopyBytesGracefullyRecordsMissingWriter(t *testing.T) {
	c := &gin.Context{Request: httptest.NewRequest(http.MethodGet, "/", nil)}

	outcome := IOCopyBytesGracefully(c, nil, []byte("image"))

	require.Equal(t, DeliveryOutcomeWriteFailed, outcome)
	recorded, ok := ResponseDeliveryOutcome(c)
	require.True(t, ok)
	require.Equal(t, DeliveryOutcomeWriteFailed, recorded)
}

func TestShouldCopyUpstreamHeaderPreservesLocalRequestID(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	copyHeader := ShouldCopyUpstreamHeader(c, common.StandardRequestIdKey, []string{"upstream-request"})

	require.False(t, copyHeader)
	require.Equal(t, "upstream-request", c.GetString(common.UpstreamRequestIdKey))
}
