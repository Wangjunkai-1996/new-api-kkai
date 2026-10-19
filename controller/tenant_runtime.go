package controller

import (
	"net"
	"net/http"

	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// TenantRuntime is deliberately inaccessible through a reverse proxy. Never
// use ClientIP here: forwarded headers cannot authorize controller evidence.
func TenantRuntime(snapshot service.TenantRuntimeSnapshot) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
		if err != nil || !net.ParseIP(host).IsLoopback() {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"tenant_runtime": snapshot}})
	}
}
