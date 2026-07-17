package replicate

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
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

func TestDoResponseRecordsDeliveryFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Writer = &failingImageWriter{ResponseWriter: c.Writer}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"status":"succeeded","output":"https://example.com/image.png"}`)),
	}
	info := &relaycommon.RelayInfo{
		Request:     &dto.ImageRequest{ResponseFormat: "url"},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}

	_, apiErr := (&Adaptor{}).DoResponse(c, response, info)

	require.Nil(t, apiErr)
	outcome, tracked := service.ResponseDeliveryOutcome(c)
	require.True(t, tracked)
	require.Equal(t, service.DeliveryOutcomeWriteFailed, outcome)
}
