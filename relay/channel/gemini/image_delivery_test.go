package gemini

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type failingImageWriter struct {
	gin.ResponseWriter
}

func (w *failingImageWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestGeminiImageHandlerRecordsDeliveryFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Writer = &failingImageWriter{ResponseWriter: c.Writer}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"predictions":[{"mimeType":"image/png","bytesBase64Encoded":"image"}]}`)),
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	_, apiErr := GeminiImageHandler(c, info, response)

	require.Nil(t, apiErr)
	outcome, tracked := service.ResponseDeliveryOutcome(c)
	require.True(t, tracked)
	require.Equal(t, service.DeliveryOutcomeWriteFailed, outcome)
}
