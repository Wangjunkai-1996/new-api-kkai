package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFrontendMode(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    frontendMode
		wantErr string
	}{
		{name: "empty defaults to embedded", raw: "", want: frontendModeEmbedded},
		{name: "whitespace defaults to embedded", raw: "  ", want: frontendModeEmbedded},
		{name: "embedded", raw: "embedded", want: frontendModeEmbedded},
		{name: "external", raw: "external", want: frontendModeExternal},
		{
			name:    "unknown value",
			raw:     "remote",
			wantErr: `invalid FRONTEND_MODE "remote": expected embedded or external`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mode, err := parseFrontendMode(test.raw)
			if test.wantErr != "" {
				require.Error(t, err)
				assert.EqualError(t, err, test.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, test.want, mode)
		})
	}
}

func TestSetRouterPanicsForInvalidFrontendMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("FRONTEND_MODE", "invalid")

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		SetRouter(gin.New(), ThemeAssets{})
	}()

	panicErr, ok := recovered.(error)
	require.True(t, ok, "invalid FRONTEND_MODE should panic with an error")
	assert.EqualError(t, panicErr, `invalid FRONTEND_MODE "invalid": expected embedded or external`)
}

func TestSetRouterRejectsEmbeddedModeWithoutAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("FRONTEND_MODE", "embedded")

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		SetRouter(gin.New(), ThemeAssets{
			DefaultIndexPage: []byte("default"),
			ClassicIndexPage: []byte("classic"),
		})
	}()

	panicErr, ok := recovered.(error)
	require.True(t, ok, "missing embedded assets should panic with a clear error")
	assert.EqualError(t, panicErr, "embedded frontend assets are unavailable; set FRONTEND_MODE=external or build with frontend assets")
}

func TestLegacyFrontendRedirectWorksWithoutEmbeddedAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("FRONTEND_MODE", "embedded")
	t.Setenv("FRONTEND_BASE_URL", "https://frontend.example.test/")
	originalIsMasterNode := common.IsMasterNode
	common.IsMasterNode = false
	t.Cleanup(func() { common.IsMasterNode = originalIsMasterNode })

	engine := gin.New()
	SetRouter(engine, ThemeAssets{})

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dashboard?tab=usage", nil))
	require.Equal(t, http.StatusMovedPermanently, recorder.Code)
	assert.Equal(t, "https://frontend.example.test/dashboard?tab=usage", recorder.Header().Get("Location"))
}

func TestExternalFrontendModeUsesAPI404WithoutRedirectOrSPA(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("FRONTEND_MODE", "external")
	t.Setenv("FRONTEND_BASE_URL", "https://frontend.example.test")

	engine := gin.New()
	SetRouter(engine, ThemeAssets{})

	apiRecorder := httptest.NewRecorder()
	engine.ServeHTTP(apiRecorder, httptest.NewRequest(http.MethodGet, "/api/does-not-exist?from=test", nil))
	require.Equal(t, http.StatusNotFound, apiRecorder.Code)
	assert.Contains(t, apiRecorder.Body.String(), `"error"`)
	assert.NotContains(t, apiRecorder.Header().Values("Location"), "https://frontend.example.test")

	webRecorder := httptest.NewRecorder()
	engine.ServeHTTP(webRecorder, httptest.NewRequest(http.MethodGet, "/dashboard/does-not-exist", nil))
	require.Equal(t, http.StatusNotFound, webRecorder.Code)
	assert.Empty(t, webRecorder.Body.String())
	assert.NotContains(t, webRecorder.Header().Values("Location"), "https://frontend.example.test")

	rootRecorder := httptest.NewRecorder()
	engine.ServeHTTP(rootRecorder, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusNotFound, rootRecorder.Code)
	assert.Empty(t, rootRecorder.Body.String())
}

func TestIsAPIStylePathUsesSegmentBoundaries(t *testing.T) {
	for _, path := range []string{
		"/api",
		"/api/missing",
		"/invitations/api/missing",
		"/assets/missing",
		"/v1/missing",
		"/v1beta/missing",
		"/pg/missing",
		"/mj/missing",
		"/suno/missing",
		"/kling/missing",
		"/jimeng/missing",
		"/proxy/mj/missing",
	} {
		assert.Truef(t, isAPIStylePath(path), "%s should use API-style 404", path)
	}

	for _, path := range []string{
		"/apiary/missing",
		"/invitations/apiary/missing",
		"/v10/missing",
		"/proxy/mjure/missing",
		"/dashboard/missing",
		"/unknown",
		"",
	} {
		assert.Falsef(t, isAPIStylePath(path), "%s should use ordinary 404", path)
	}

}
