package controller

import (
	"net"
	"net/http"

	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// Socket identity, never forwarded headers, authorizes operator observation.
// This endpoint cannot change durable control; only the host controller can.
func MembershipBillingRecoveryRuntime(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	status, err := service.MembershipBillingRecoveryRuntimeStatus(c.Request.Context())
	code := http.StatusOK
	if err != nil {
		code = http.StatusServiceUnavailable
	}
	c.JSON(code, gin.H{"success": err == nil, "data": gin.H{"membership_recovery": status}})
}
