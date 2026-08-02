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

func TestImageStudioPhaseOneRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerImageStudioAPIRoutes(engine.Group("/api"))

	methodsByPath := map[string]map[string]bool{}
	for _, route := range engine.Routes() {
		if methodsByPath[route.Path] == nil {
			methodsByPath[route.Path] = map[string]bool{}
		}
		methodsByPath[route.Path][route.Method] = true
	}
	expected := map[string][]string{
		"/api/image-studio/models":                   {http.MethodGet},
		"/api/image-studio/token":                    {http.MethodGet, http.MethodPost},
		"/api/image-studio/samples":                  {http.MethodGet},
		"/api/image-studio/samples/:id":              {http.MethodGet},
		"/api/image-studio/generations":              {http.MethodGet},
		"/api/image-studio/generations/:id":          {http.MethodGet, http.MethodDelete},
		"/api/image-studio/assets/:id":               {http.MethodGet},
		"/api/image-studio/assets/:id/content":       {http.MethodGet},
		"/api/image-studio/assets/:id/download":      {http.MethodGet},
		"/api/admin/image-studio/model-candidates":   {http.MethodGet},
		"/api/admin/image-studio/model-profiles":     {http.MethodGet, http.MethodPost},
		"/api/admin/image-studio/model-profiles/:id": {http.MethodGet, http.MethodPut, http.MethodDelete},
		"/api/admin/image-studio/samples":            {http.MethodGet, http.MethodPost},
		"/api/admin/image-studio/samples/:id":        {http.MethodGet, http.MethodPut, http.MethodDelete},
		"/api/admin/image-studio/sample-assets":      {http.MethodPost},
		"/api/admin/image-studio/outbox/:id/redrive": {http.MethodPost},
	}
	for path, methods := range expected {
		for _, method := range methods {
			require.Truef(t, methodsByPath[path][method], "%s %s must be registered", method, path)
		}
	}
	require.False(t, methodsByPath["/api/image-studio/tasks"][http.MethodGet])
}

func TestImageStudioPhaseOnePlaygroundRegistersTextToImageOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetRelayRouter(engine)

	methodsByPath := map[string]map[string]bool{}
	for _, route := range engine.Routes() {
		if methodsByPath[route.Path] == nil {
			methodsByPath[route.Path] = map[string]bool{}
		}
		methodsByPath[route.Path][route.Method] = true
	}
	require.True(t, methodsByPath["/pg/images/quote"][http.MethodPost])
	require.True(t, methodsByPath["/pg/images"][http.MethodPost])
	require.False(t, methodsByPath["/pg/images/edits"][http.MethodPost])
	require.False(t, methodsByPath["/pg/images/tasks"][http.MethodGet])
}
