package channel

import (
	"net/http"

	commonpkg "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/kkaiattribution"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

var internalAttributionSigner *kkaiattribution.Signer

func InitInternalAttribution() error {
	signer, err := kkaiattribution.NewSignerFromEnvironment()
	if err != nil {
		internalAttributionSigner = nil
		return err
	}
	internalAttributionSigner = signer
	return nil
}

func applyInternalAttributionHeaders(req *http.Request, c *gin.Context, info *relaycommon.RelayInfo) error {
	if req == nil {
		return kkaiattribution.ErrInvalidEnvelope
	}
	_, err := internalAttributionSigner.ApplyRequest(req, internalAttributionClaims(c, info))
	return err
}

func applyInternalAttributionHeadersForURL(
	headers http.Header,
	method string,
	upstreamURL string,
	authority string,
	c *gin.Context,
	info *relaycommon.RelayInfo,
) error {
	_, err := internalAttributionSigner.Apply(headers, method, upstreamURL, authority, internalAttributionClaims(c, info))
	return err
}

func internalAttributionClaims(c *gin.Context, info *relaycommon.RelayInfo) kkaiattribution.Claims {
	claims := kkaiattribution.Claims{
		RequestID: attributionRequestID(c, info),
		Model:     attributionModel(c, info),
		Source:    "new-api",
	}
	if info != nil {
		claims.UserID = info.UserId
		claims.TokenID = info.TokenId
		if info.ChannelMeta != nil {
			claims.ChannelID = info.ChannelId
			claims.MultiKeyIndex = info.ChannelMultiKeyIndex
		}
	}

	claims.UserID = attributionContextInt(c, constant.ContextKeyUserId, claims.UserID)
	claims.TokenID = attributionContextInt(c, constant.ContextKeyTokenId, claims.TokenID)
	claims.ChannelID = attributionContextInt(c, constant.ContextKeyChannelId, claims.ChannelID)
	claims.MultiKeyIndex = attributionContextInt(c, constant.ContextKeyChannelMultiKeyIndex, claims.MultiKeyIndex)
	return claims
}

func attributionRequestID(c *gin.Context, info *relaycommon.RelayInfo) string {
	if c != nil {
		if requestID := c.GetString(commonpkg.RequestIdKey); requestID != "" {
			return requestID
		}
	}
	if info == nil {
		return ""
	}
	return info.RequestId
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

func attributionContextInt(c *gin.Context, key constant.ContextKey, fallback int) int {
	if c != nil {
		if _, exists := commonpkg.GetContextKey(c, key); exists {
			return commonpkg.GetContextKeyInt(c, key)
		}
	}
	return fallback
}
