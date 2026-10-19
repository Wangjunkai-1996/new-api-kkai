package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func CORS() gin.HandlerFunc {
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	config.AllowCredentials = true
	config.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"*"}
	platformCORS := cors.New(config)
	return func(c *gin.Context) {
		if tenantID := common.GetContextKeyInt64(c, constant.ContextKeyTenantID); tenantID > 0 {
			origin, ok := tenantBrowserOrigin(c.Request)
			if !ok {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			if origin != "" {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Access-Control-Allow-Credentials", "true")
				c.Writer.Header().Add("Vary", "Origin")
			}
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With")
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
			c.Next()
			return
		}
		platformCORS(c)
	}
}

// Tenant browser requests use only their own HTTPS origin. Server-to-server
// relay and verified payment callbacks normally have no Origin header.
func tenantBrowserOrigin(request *http.Request) (string, bool) {
	values := request.Header.Values("Origin")
	if len(values) == 0 {
		return "", true
	}
	if len(values) != 1 {
		return "", false
	}
	origin, err := common.NormalizeOrigin(values[0])
	if err != nil {
		return "", false
	}
	expected, err := common.NormalizeOrigin("https://" + request.Host)
	return origin, err == nil && origin == expected
}

func Version() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-New-Api-Version", common.Version)
		c.Next()
	}
}
