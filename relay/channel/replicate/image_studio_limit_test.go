package replicate

import (
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

func TestReplicateImageStudioResponseLimitRejectsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := httptest.NewRequest(http.MethodPost, "/pg/images", nil)
	const maximum = int64(32)
	c.Request = request.WithContext(service.WithImageStudioResponseLimit(request.Context(), maximum))
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", int(maximum)+1))),
	}

	_, apiErr := (&Adaptor{}).DoResponse(c, response, &relaycommon.RelayInfo{})
	require.NotNil(t, apiErr)
	require.ErrorIs(t, apiErr, service.ErrImageStudioResponseTooLarge)
}
