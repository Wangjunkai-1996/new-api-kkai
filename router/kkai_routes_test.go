package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoStudioOutboxRedriveRouteIsReachableOnlyThroughAdminAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("video-outbox-redrive-test"))))
	apiRouter := engine.Group("/api")
	registerKKAIRoutes(apiRouter, func(c *gin.Context) { c.Next() })

	found := false
	for _, route := range engine.Routes() {
		if route.Method == http.MethodPost && route.Path == "/api/admin/video-studio/outbox/:id/redrive" {
			found = true
			break
		}
	}
	require.True(t, found)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/admin/video-studio/outbox/1/redrive",
		strings.NewReader(`{"redrive_key":"unauthorized-retry"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}
