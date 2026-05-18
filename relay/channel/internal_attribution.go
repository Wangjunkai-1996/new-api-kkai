package channel

import (
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	commonpkg "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

const (
	internalAttributionRequestID     = "X-NewAPI-Request-Id"
	internalAttributionUserID        = "X-NewAPI-User-Id"
	internalAttributionTokenID       = "X-NewAPI-Token-Id"
	internalAttributionTokenName     = "X-NewAPI-Token-Name"
	internalAttributionChannelID     = "X-NewAPI-Channel-Id"
	internalAttributionMultiKeyIndex = "X-NewAPI-Multi-Key-Index"
	internalAttributionModel         = "X-NewAPI-Model"
	internalAttributionSource        = "X-NewAPI-Source"
)

var internalAttributionHeaderNames = []string{
	internalAttributionRequestID,
	internalAttributionUserID,
	internalAttributionTokenID,
	internalAttributionTokenName,
	internalAttributionChannelID,
	internalAttributionMultiKeyIndex,
	internalAttributionModel,
	internalAttributionSource,
}

func applyInternalAttributionHeaders(req *http.Request, c *gin.Context, info *relaycommon.RelayInfo) {
	if req == nil || req.URL == nil {
		return
	}
	applyInternalAttributionHeadersForURL(req.Header, req.URL.String(), c, info)
}

func applyInternalAttributionHeadersForURL(headers http.Header, upstreamURL string, c *gin.Context, info *relaycommon.RelayInfo) {
	if headers == nil {
		return
	}
	if !isInternalAttributionUpstreamURL(upstreamURL) {
		removeInternalAttributionHeaders(headers)
		return
	}

	headers.Set(internalAttributionRequestID, attributionRequestID(c, info))
	headers.Set(internalAttributionUserID, strconv.Itoa(attributionUserID(c, info)))
	headers.Set(internalAttributionTokenID, strconv.Itoa(attributionTokenID(c, info)))
	headers.Set(internalAttributionTokenName, attributionTokenName(c))
	headers.Set(internalAttributionChannelID, strconv.Itoa(attributionChannelID(c, info)))
	headers.Set(internalAttributionMultiKeyIndex, strconv.Itoa(attributionMultiKeyIndex(c, info)))
	headers.Set(internalAttributionModel, attributionModel(c, info))
	headers.Set(internalAttributionSource, "new-api")
}

func removeInternalAttributionHeaders(headers http.Header) {
	for _, name := range internalAttributionHeaderNames {
		headers.Del(name)
	}
}

func isInternalAttributionUpstreamURL(upstreamURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(upstreamURL))
	if err != nil {
		return false
	}
	return isInternalAttributionHost(parsed.Hostname())
}

func isInternalAttributionHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return false
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}

	if zoneIndex := strings.LastIndex(host, "%"); zoneIndex >= 0 {
		host = host[:zoneIndex]
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast()
}

func attributionRequestID(c *gin.Context, info *relaycommon.RelayInfo) string {
	if c != nil {
		if requestID := c.GetString(commonpkg.RequestIdKey); requestID != "" {
			return requestID
		}
	}
	if info != nil {
		return info.RequestId
	}
	return ""
}

func attributionUserID(c *gin.Context, info *relaycommon.RelayInfo) int {
	return contextIntWithFallback(c, constant.ContextKeyUserId, func() int {
		if info == nil {
			return 0
		}
		return info.UserId
	})
}

func attributionTokenID(c *gin.Context, info *relaycommon.RelayInfo) int {
	return contextIntWithFallback(c, constant.ContextKeyTokenId, func() int {
		if info == nil {
			return 0
		}
		return info.TokenId
	})
}

func attributionTokenName(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return c.GetString("token_name")
}

func attributionChannelID(c *gin.Context, info *relaycommon.RelayInfo) int {
	return contextIntWithFallback(c, constant.ContextKeyChannelId, func() int {
		if info == nil || info.ChannelMeta == nil {
			return 0
		}
		return info.ChannelId
	})
}

func attributionMultiKeyIndex(c *gin.Context, info *relaycommon.RelayInfo) int {
	return contextIntWithFallback(c, constant.ContextKeyChannelMultiKeyIndex, func() int {
		if info == nil || info.ChannelMeta == nil {
			return 0
		}
		return info.ChannelMultiKeyIndex
	})
}

func attributionModel(c *gin.Context, info *relaycommon.RelayInfo) string {
	if c != nil {
		if model := commonpkg.GetContextKeyString(c, constant.ContextKeyOriginalModel); model != "" {
			return model
		}
	}
	if info == nil {
		return ""
	}
	if info.OriginModelName != "" {
		return info.OriginModelName
	}
	if info.ChannelMeta != nil {
		return info.UpstreamModelName
	}
	return ""
}

func contextIntWithFallback(c *gin.Context, key constant.ContextKey, fallback func() int) int {
	if c != nil {
		if _, ok := commonpkg.GetContextKey(c, key); ok {
			return commonpkg.GetContextKeyInt(c, key)
		}
	}
	return fallback()
}
