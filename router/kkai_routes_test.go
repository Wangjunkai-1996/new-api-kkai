package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoStudioOutboxRedriveRouteIsReachableOnlyThroughAdminAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
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
		"/api/image-studio/generations/:id/input":    {http.MethodGet},
		"/api/image-studio/assets/:id":               {http.MethodGet},
		"/api/image-studio/assets/:id/content":       {http.MethodGet},
		"/api/image-studio/assets/:id/download":      {http.MethodGet},
		"/api/image-studio/assets/:id/edit-source":   {http.MethodGet},
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

func TestImageAsyncRoutesRequireAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerImageStudioAPIRoutes(engine.Group("/api"))
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/submissions/lookup"},
		{http.MethodPost, "/uploads"},
		{http.MethodGet, "/uploads/1"},
		{http.MethodPost, "/uploads/1/parts/1"},
		{http.MethodGet, "/uploads/1/parts"},
		{http.MethodPost, "/uploads/1/complete"},
		{http.MethodDelete, "/uploads/1"},
		{http.MethodPost, "/generations/quote"},
		{http.MethodPost, "/generations"},
	} {
		t.Run(route.method+route.path, func(t *testing.T) {
			request := httptest.NewRequest(route.method, "/api/image-studio"+route.path, strings.NewReader(`{}`))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusUnauthorized, recorder.Code)
		})
	}
}

func TestImageOutputEditSourceRequiresLoginAndFeatureAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerImageStudioAPIRoutes(engine.Group("/api"))
	request := httptest.NewRequest(http.MethodGet, "/api/image-studio/assets/1/edit-source?signature=not-a-dashboard-login", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)

	fixture := newVideoStudioAccessFixture(t, "off")
	rawSetting := config.GlobalConfig.Get("image_studio")
	originalSetting, err := config.ConfigToMap(rawSetting)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, config.UpdateConfigFromMap(rawSetting, originalSetting)) })
	require.NoError(t, config.UpdateConfigFromMap(rawSetting, map[string]string{"access_mode": "off"}))
	registerImageStudioAPIRoutes(fixture.engine.Group("/api"))
	recorder = fixture.request(http.MethodGet, "/api/image-studio/assets/1/edit-source", "")
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "image_studio_access_denied")
}

func TestImageStudioPlaygroundRegistersGenerationAndEditRoutes(t *testing.T) {
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
	require.True(t, methodsByPath["/pg/images/edits/quote"][http.MethodPost])
	require.True(t, methodsByPath["/pg/images/edits"][http.MethodPost])
	require.False(t, methodsByPath["/pg/images/tasks"][http.MethodGet])
}
