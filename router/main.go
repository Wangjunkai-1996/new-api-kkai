package router

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"

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
	platformHosts := make([]string, 0)
	for _, rawHost := range strings.Split(os.Getenv("TENANT_PLATFORM_HOSTS"), ",") {
		if host := strings.TrimSpace(rawHost); host != "" {
			canonical, err := model.NormalizeTenantHost(host)
			if err != nil {
				panic(fmt.Errorf("invalid TENANT_PLATFORM_HOSTS entry %q: %w", host, err))
			}
			platformHosts = append(platformHosts, canonical)
		}
	}
	if len(platformHosts) == 0 {
		address, err := url.Parse(system_setting.ServerAddress)
		if err == nil && (address.Scheme == "https" || address.Scheme == "http") {
			if host, err := model.NormalizeTenantHost(address.Host); err == nil {
				platformHosts = append(platformHosts, host)
			}
		}
	}
	routingEnabled := strings.EqualFold(strings.TrimSpace(os.Getenv("TENANT_ROUTING_ENABLED")), "true")
	router.Use(middleware.TenantRouting(model.DB, routingEnabled, platformHosts...), middleware.TenantFeatureGate())

	SetApiRouter(router)
	SetDashboardRouter(router)
	SetRelayRouter(router)
	SetTaskPluginProtocolRouter(router)
	SetTaskRouter(router)
	SetVideoRouter(router)
	pluginDispatcher := SetPluginRouter(router)
	if mode == frontendModeExternal {
		// FRONTEND_MODE=external takes precedence over the legacy redirect
		// setting; the edge server owns all frontend requests in this mode.
		if frontendBaseURL := strings.TrimSpace(os.Getenv("FRONTEND_BASE_URL")); frontendBaseURL != "" {
			common.SysLog("FRONTEND_BASE_URL is ignored when FRONTEND_MODE=external")
		}
		setExternalFrontendNoRoute(router, pluginDispatcher)
		return
	}

	if frontendBaseURL == "" {
		SetWebRouter(router, assets, pluginDispatcher)
	} else {
		frontendBaseURL = strings.TrimSuffix(frontendBaseURL, "/")
		router.NoRoute(pluginDispatcher, middleware.RouteTag("web"), middleware.AccessTokenAudit(), func(c *gin.Context) {
			c.Redirect(http.StatusMovedPermanently, fmt.Sprintf("%s%s", frontendBaseURL, c.Request.RequestURI))
		})
	}
}
