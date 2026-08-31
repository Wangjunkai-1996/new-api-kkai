package router

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

type frontendMode string

const (
	frontendModeEmbedded frontendMode = "embedded"
	frontendModeExternal frontendMode = "external"
)

func parseFrontendMode(raw string) (frontendMode, error) {
	value := strings.TrimSpace(raw)
	switch value {
	case "":
		return frontendModeEmbedded, nil
	case string(frontendModeEmbedded):
		return frontendModeEmbedded, nil
	case string(frontendModeExternal):
		return frontendModeExternal, nil
	default:
		return "", fmt.Errorf("invalid FRONTEND_MODE %q: expected embedded or external", value)
	}
}

func SetRouter(router *gin.Engine, assets ThemeAssets) {
	mode, err := parseFrontendMode(os.Getenv("FRONTEND_MODE"))
	if err != nil {
		// A bad startup configuration must fail closed instead of silently
		// exposing a partially configured frontend.
		panic(err)
	}
	frontendBaseURL := os.Getenv("FRONTEND_BASE_URL")
	if common.IsMasterNode && frontendBaseURL != "" {
		frontendBaseURL = ""
		common.SysLog("FRONTEND_BASE_URL is ignored on master node")
	}
	if mode == frontendModeEmbedded && frontendBaseURL == "" && !assets.hasEmbeddedFrontend() {
		panic(fmt.Errorf("embedded frontend assets are unavailable; set FRONTEND_MODE=external or build with frontend assets"))
	}

	SetApiRouter(router)
	SetDashboardRouter(router)
	SetRelayRouter(router)
	SetVideoRouter(router)
	if mode == frontendModeExternal {
		// FRONTEND_MODE=external takes precedence over the legacy redirect
		// setting; the edge server owns all frontend requests in this mode.
		if frontendBaseURL := strings.TrimSpace(os.Getenv("FRONTEND_BASE_URL")); frontendBaseURL != "" {
			common.SysLog("FRONTEND_BASE_URL is ignored when FRONTEND_MODE=external")
		}
		setExternalFrontendNoRoute(router)
		return
	}

	if frontendBaseURL == "" {
		SetWebRouter(router, assets)
	} else {
		frontendBaseURL = strings.TrimSuffix(frontendBaseURL, "/")
		router.NoRoute(func(c *gin.Context) {
			c.Set(middleware.RouteTagKey, "web")
			c.Redirect(http.StatusMovedPermanently, fmt.Sprintf("%s%s", frontendBaseURL, c.Request.RequestURI))
		})
	}
}
