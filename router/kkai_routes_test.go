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

func TestVideoStudioTokenRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerVideoStudioAPIRoutes(engine.Group("/api"))

	routes := engine.Routes()
	methodsByPath := map[string]map[string]bool{}
	for _, route := range routes {
		if methodsByPath[route.Path] == nil {
			methodsByPath[route.Path] = map[string]bool{}
		}
		methodsByPath[route.Path][route.Method] = true
	}
	require.True(t, methodsByPath["/api/video-studio/token"][http.MethodGet])
	require.True(t, methodsByPath["/api/video-studio/token"][http.MethodPost])
	require.True(t, methodsByPath["/api/admin/video-studio/model-candidates"][http.MethodGet])
	require.True(t, methodsByPath["/api/admin/video-studio/assets/:id"][http.MethodGet])
}
