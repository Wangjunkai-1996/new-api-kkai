package middleware

import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const (
	trustedRequestIDProxiesEnv = "TRUSTED_REQUEST_ID_PROXY_CIDRS"
)

func RequestId() func(c *gin.Context) {
	return requestIDMiddleware(splitCIDRs(os.Getenv(trustedRequestIDProxiesEnv)))
}

func requestIDMiddleware(trustedCIDRs []string) gin.HandlerFunc {
	trustedNetworks := parseTrustedNetworks(trustedCIDRs)
	return func(c *gin.Context) {
		id := requestIDFromTrustedProxy(c.Request, trustedNetworks)
		if id == "" {
			id = common.NewRequestId()
		}
		c.Set(common.RequestIdKey, id)
		ctx := context.WithValue(c.Request.Context(), common.RequestIdKey, id)
		c.Request = c.Request.WithContext(ctx)
		c.Header(common.RequestIdKey, id)
		c.Header(common.StandardRequestIdKey, id)
		c.Next()
	}
}

func splitCIDRs(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t'
	})
}

func parseTrustedNetworks(cidrs []string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(strings.TrimSpace(cidr))
		if err == nil {
			networks = append(networks, network)
		}
	}
	return networks
}

func requestIDFromTrustedProxy(request *http.Request, networks []*net.IPNet) string {
	if request == nil || !trustedRemoteAddress(request.RemoteAddr, networks) {
		return ""
	}
	requestID := strings.TrimSpace(request.Header.Get(common.StandardRequestIdKey))
	if !validRequestID(requestID) {
		return ""
	}
	return requestID
}

func trustedRemoteAddress(remoteAddress string, networks []*net.IPNet) bool {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = remoteAddress
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return false
	}
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func validRequestID(requestID string) bool {
	if requestID == "" || len(requestID) > 128 {
		return false
	}
	for _, char := range requestID {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			continue
		}
		switch char {
		case '-', '_', '.', ':':
			continue
		default:
			return false
		}
	}
	return true
}
