package openai

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIImageStudioResponseLimitRejectsOversizedSuccessBody(t *testing.T) {
	c := newLimitedImageResponseContext(t, 64)
	n := uint(1)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 65))),
		Header:     make(http.Header),
	}

	_, apiErr := OpenaiImageHandler(c, &relaycommon.RelayInfo{Request: &dto.ImageRequest{N: &n}}, response)
	require.NotNil(t, apiErr)
	require.Equal(t, service.ErrImageStudioResponseTooLarge.Error(), apiErr.Error())
}

func TestOpenAIImageStudioResponseValidationRejectsEmptyAndAmbiguousData(t *testing.T) {
	tests := []string{
		`{"data":[],"usage":{"total_tokens":1}}`,
		`{"data":[{"url":"https://example.test/a","b64_json":"YQ=="}],"usage":{"total_tokens":1}}`,
		`{"data":[{}],"usage":{"total_tokens":1}}`,
		`{"data":[{"url":"https://example.test/a"},{"url":"https://example.test/b"}],"usage":{"total_tokens":1}}`,
	}
	for _, body := range tests {
		c := newLimitedImageResponseContext(t, 1<<20)
		n := uint(1)
		response := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(body)),
			Header:     make(http.Header),
		}
		_, apiErr := OpenaiImageHandler(c, &relaycommon.RelayInfo{Request: &dto.ImageRequest{N: &n}}, response)
		require.NotNil(t, apiErr, body)
		require.Equal(t, service.ErrInvalidImageRelayResponse.Error(), apiErr.Error(), body)
	}
}

func newLimitedImageResponseContext(t *testing.T, maximum int64) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	request = request.WithContext(service.WithImageStudioResponseLimit(request.Context(), maximum))
	c.Request = request
	return c
}
