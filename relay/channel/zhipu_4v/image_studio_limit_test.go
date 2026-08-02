package zhipu_4v

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

func TestZhipuImageStudioResponseLimitRejectsOversizedBody(t *testing.T) {
	c := limitedZhipuImageContext(32)
	response := &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 33))),
	}
	_, apiErr := zhipu4vImageHandler(c, response, &relaycommon.RelayInfo{})
	require.NotNil(t, apiErr)
	require.Equal(t, service.ErrImageStudioResponseTooLarge.Error(), apiErr.Error())
}

func limitedZhipuImageContext(maximum int64) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Request = request.WithContext(service.WithImageStudioResponseLimit(request.Context(), maximum))
	return c
}
