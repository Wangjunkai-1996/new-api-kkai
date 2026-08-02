package controller

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageRelayCaptureWriterDelaysBodyAndEnforcesBound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	outer := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(outer)
	capture, err := newImageRelayCaptureWriter(ctx.Writer, t.TempDir(), 5)
	require.NoError(t, err)
	t.Cleanup(capture.Remove)

	capture.Header().Set("Content-Type", "application/json")
	capture.WriteHeader(http.StatusCreated)
	written, err := capture.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, written)
	assert.Equal(t, http.StatusCreated, capture.Status())
	assert.Empty(t, outer.Body.String())
	assert.Empty(t, outer.Header().Get("Content-Type"))

	_, err = capture.Write([]byte("!"))
	require.ErrorIs(t, err, errImageRelayCaptureTooLarge)
	require.NoError(t, capture.Close())
	body, err := os.ReadFile(capture.Path())
	require.NoError(t, err)
	assert.Equal(t, "hello", string(body))
}
