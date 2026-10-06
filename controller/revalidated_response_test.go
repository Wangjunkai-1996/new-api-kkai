package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicContentConditionalRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	content := "published notice"
	router := gin.New()
	router.GET("/notice", func(c *gin.Context) { serveRevalidatedJSON(c, content) })

	initial := httptest.NewRecorder()
	router.ServeHTTP(initial, httptest.NewRequest(http.MethodGet, "/notice", nil))
	require.Equal(t, http.StatusOK, initial.Code)
	var body publicContentResponse
	require.NoError(t, common.Unmarshal(initial.Body.Bytes(), &body))
	assert.True(t, body.Success)
	assert.Equal(t, content, body.Data)
	etag := initial.Header().Get("ETag")
	require.NotEmpty(t, etag)
	assert.Equal(t, "no-cache", initial.Header().Get("Cache-Control"))
	assert.Equal(t, "Accept-Encoding", initial.Header().Get("Vary"))

	for _, validator := range []string{etag, etag[2:], `"unrelated", ` + etag, "*"} {
		t.Run(validator, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/notice", nil)
			request.Header.Set("If-None-Match", validator)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			assert.Equal(t, http.StatusNotModified, response.Code)
			assert.Empty(t, response.Body.String())
			assert.Equal(t, etag, response.Header().Get("ETag"))
		})
	}

	content = "edited notice"
	request := httptest.NewRequest(http.MethodGet, "/notice", nil)
	request.Header.Set("If-None-Match", etag)
	updated := httptest.NewRecorder()
	router.ServeHTTP(updated, request)
	require.Equal(t, http.StatusOK, updated.Code)
	require.NoError(t, common.Unmarshal(updated.Body.Bytes(), &body))
	assert.Equal(t, content, body.Data)
	assert.NotEqual(t, etag, updated.Header().Get("ETag"))
}
