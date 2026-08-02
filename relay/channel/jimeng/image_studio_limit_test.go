package jimeng

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestJimengImageStudioResponseLimitRejectsOversizedBody(t *testing.T) {
	c := limitedJimengImageContext(32)
	response := &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 33))),
	}
	_, apiErr := jimengImageHandler(c, response, &relaycommon.RelayInfo{})
	require.NotNil(t, apiErr)
	require.Equal(t, service.ErrImageStudioResponseTooLarge.Error(), apiErr.Error())
}

func limitedJimengImageContext(maximum int64) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Request = request.WithContext(service.WithImageStudioResponseLimit(request.Context(), maximum))
	return c
}
