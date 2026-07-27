package channel

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskSubmitResponseBuffersJSONUntilExplicitWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	response, err := NewJSONTaskSubmitResponse(
		"upstream-task",
		[]byte(`{"upstream":"payload"}`),
		map[string]string{"id": "task_public"},
	)
	require.NoError(t, err)

	assert.Empty(t, recorder.Body.String(), "adaptor response parsing must not write to the client")
	require.NoError(t, response.WriteTo(ctx))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"id":"task_public"}`, recorder.Body.String())
	assert.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"))
}
