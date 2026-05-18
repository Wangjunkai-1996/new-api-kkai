package middleware

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func abortWithOpenAiMessage(c *gin.Context, statusCode int, message string, code ...types.ErrorCode) {
	codeStr := ""
	if len(code) > 0 {
		codeStr = string(code[0])
	}
	userId := c.GetInt("id")
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"message": common.MessageWithRequestId(message, c.GetString(common.RequestIdKey)),
			"type":    "new_api_error",
			"code":    codeStr,
		},
	})
	c.Abort()
	logger.LogError(c.Request.Context(), fmt.Sprintf("user %d | %s", userId, message))
}

func abortWithAffinityChannelDisabled(c *gin.Context) {
	logger.LogError(c.Request.Context(), i18n.T(c, i18n.MsgDistributorAffinityChannelDisabled))
	abortWithOpenAiMessage(c, http.StatusServiceUnavailable, types.PublicMessageUpstreamUnavailable, types.ErrorCodeUpstreamUnavailable)
}

func abortWithPublicAPIError(c *gin.Context, apiErr *types.NewAPIError) {
	statusCode, message, code := publicMiddlewareAPIError(apiErr)
	abortWithOpenAiMessage(c, statusCode, message, code)
}

func publicMiddlewareAPIError(apiErr *types.NewAPIError) (int, string, types.ErrorCode) {
	if apiErr == nil {
		return http.StatusInternalServerError, types.PublicMessageUpstreamError, types.ErrorCodeBadResponse
	}
	if types.IsUpstreamUnavailableError(apiErr) {
		return http.StatusServiceUnavailable, types.PublicMessageUpstreamUnavailable, types.ErrorCodeUpstreamUnavailable
	}
	statusCode := apiErr.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusInternalServerError
	}
	if publicMiddlewareMessageLooksUnsafe(apiErr.Error()) {
		return statusCode, types.PublicMessageUpstreamError, types.ErrorCodeBadResponse
	}
	return statusCode, apiErr.Error(), apiErr.GetErrorCode()
}

var (
	publicMiddlewareAuthorizationPattern = regexp.MustCompile(`(?i)\b(authorization|x-api-key|api-key|api_key|upstream[_ -]?key|client[_ -]?token)\s*[:=]\s*(bearer\s+)?[^\s,;)}\]]+`)
	publicMiddlewareBearerPattern        = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]{8,}`)
	publicMiddlewareSKLikePattern        = regexp.MustCompile(`(?i)\bsk-[a-z0-9][a-z0-9._-]{6,}`)
)

func publicMiddlewareMessageLooksUnsafe(message string) bool {
	message = strings.TrimSpace(message)
	if message == "" {
		return false
	}
	return types.LooksLikeNoisyUpstreamMessage(message) ||
		publicMiddlewareAuthorizationPattern.MatchString(message) ||
		publicMiddlewareBearerPattern.MatchString(message) ||
		publicMiddlewareSKLikePattern.MatchString(message)
}

func abortWithMidjourneyMessage(c *gin.Context, statusCode int, code int, description string) {
	c.JSON(statusCode, gin.H{
		"description": description,
		"type":        "new_api_error",
		"code":        code,
	})
	c.Abort()
	logger.LogError(c.Request.Context(), description)
}
