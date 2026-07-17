package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRequestIDAcceptsValidIDFromTrustedProxy(t *testing.T) {
	engine := gin.New()
	engine.Use(requestIDMiddleware([]string{"10.0.0.0/8"}))
	engine.GET("/", func(c *gin.Context) { c.String(http.StatusOK, c.GetString(common.RequestIdKey)) })
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.20.30.40:1234"
	request.Header.Set(common.StandardRequestIdKey, "edge-01:request_42")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, "edge-01:request_42", recorder.Body.String())
	require.Equal(t, "edge-01:request_42", recorder.Header().Get(common.StandardRequestIdKey))
	require.Equal(t, "edge-01:request_42", recorder.Header().Get(common.RequestIdKey))
}

func TestRequestIDRejectsHeaderFromUntrustedClient(t *testing.T) {
	engine := gin.New()
	engine.Use(requestIDMiddleware([]string{"10.0.0.0/8"}))
	engine.GET("/", func(c *gin.Context) { c.String(http.StatusOK, c.GetString(common.RequestIdKey)) })
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "203.0.113.9:1234"
	request.Header.Set(common.StandardRequestIdKey, "forged-id")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.NotEqual(t, "forged-id", recorder.Body.String())
	require.NotEmpty(t, recorder.Body.String())
}

func TestRequestIDRejectsInvalidTrustedHeader(t *testing.T) {
	engine := gin.New()
	engine.Use(requestIDMiddleware([]string{"10.0.0.0/8"}))
	engine.GET("/", func(c *gin.Context) { c.String(http.StatusOK, c.GetString(common.RequestIdKey)) })
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.20.30.40:1234"
	request.Header.Set(common.StandardRequestIdKey, "invalid id with spaces")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.NotEqual(t, "invalid id with spaces", recorder.Body.String())
}
