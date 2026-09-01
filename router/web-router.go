package router

import (
	"embed"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// ThemeAssets holds the embedded frontend assets for both themes.
type ThemeAssets struct {
	DefaultBuildFS   embed.FS
	DefaultIndexPage []byte
	ClassicBuildFS   embed.FS
	ClassicIndexPage []byte
}

func (assets ThemeAssets) hasEmbeddedFrontend() bool {
	if len(assets.DefaultIndexPage) == 0 || len(assets.ClassicIndexPage) == 0 {
		return false
	}

	_, defaultErr := assets.DefaultBuildFS.ReadFile("web/default/dist/index.html")
	_, classicErr := assets.ClassicBuildFS.ReadFile("web/classic/dist/index.html")
	return defaultErr == nil && classicErr == nil
}

func SetWebRouter(router *gin.Engine, assets ThemeAssets) {
	defaultFS := common.EmbedFolder(assets.DefaultBuildFS, "web/default/dist")
	classicFS := common.EmbedFolder(assets.ClassicBuildFS, "web/classic/dist")
	themeFS := common.NewThemeAwareFS(defaultFS, classicFS)

	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.Use(middleware.GlobalWebRateLimit())
	router.Use(middleware.Cache())
	router.Use(static.Serve("/", themeFS))
	router.NoRoute(func(c *gin.Context) {
		c.Set(middleware.RouteTagKey, "web")
		if isAPIStylePath(c.Request.URL.Path) {
			controller.RelayNotFound(c)
			return
		}
		c.Header("Cache-Control", "no-cache")
		if common.GetTheme() == "classic" {
			c.Data(http.StatusOK, "text/html; charset=utf-8", assets.ClassicIndexPage)
		} else {
			c.Data(http.StatusOK, "text/html; charset=utf-8", assets.DefaultIndexPage)
		}
	})
}

func setExternalFrontendNoRoute(router *gin.Engine) {
	router.NoRoute(func(c *gin.Context) {
		if isAPIStylePath(c.Request.URL.Path) {
			c.Set(middleware.RouteTagKey, "api")
			controller.RelayNotFound(c)
			return
		}

		c.Set(middleware.RouteTagKey, "web")
		c.AbortWithStatus(http.StatusNotFound)
	})
}

func isAPIStylePath(path string) bool {
	for _, prefix := range []string{
		"/api",
		"/invitations/api",
		"/dashboard/billing",
		"/assets",
		"/v1",
		"/v1beta",
		"/pg",
		"/mj",
		"/suno",
		"/kling",
		"/jimeng",
	} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}

	// The legacy Midjourney API accepts a dynamic first path segment, e.g.
	// /proxy/mj/submit/imagine. Keep unmatched requests under that route in
	// the API error format instead of returning an HTML/SPA response.
	segments := strings.Split(strings.Trim(path, "/"), "/")
	return len(segments) >= 2 && segments[1] == "mj"
}
