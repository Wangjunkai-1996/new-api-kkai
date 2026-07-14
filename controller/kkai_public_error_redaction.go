package controller

import (
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

var (
	kkaiPublicCredentialPattern = regexp.MustCompile(`(?i)\b(authorization|x-api-key|api[-_ ]?key|upstream[-_ ]?key|client[-_ ]?token)\s*[:=]?\s*(bearer\s+)?[^\s,;)}\]]+`)
	kkaiPublicBearerPattern     = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]{8,}`)
	kkaiPublicSKPattern         = regexp.MustCompile(`(?i)\bsk-[a-z0-9][a-z0-9._-]{6,}`)
	kkaiPublicLinkPattern       = regexp.MustCompile(`(?i)(https?://[^\s,;)}\]]+|t\.me/[^\s,;)}\]]+|discord\.gg/[^\s,;)}\]]+)`)
)

func kkaiPublicPayloadUnsafe(c *gin.Context, values ...string) bool {
	for _, value := range values {
		if kkaiPublicTextUnsafe(c, value) {
			return true
		}
	}
	return false
}

func kkaiPublicTaskDataUnsafe(c *gin.Context, data any) bool {
	if data == nil {
		return false
	}
	encoded, err := common.Marshal(data)
	return err != nil || kkaiPublicTextUnsafe(c, string(encoded))
}

func kkaiPublicTextUnsafe(c *gin.Context, text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	if kkaiPublicCredentialPattern.MatchString(text) || kkaiPublicBearerPattern.MatchString(text) ||
		kkaiPublicSKPattern.MatchString(text) || kkaiPublicLinkPattern.MatchString(text) {
		return true
	}
	for _, marker := range []string{"telegram", "wechat", "qq group", "buy key", "buy api", "recharge", "contact us", "广告", "购买", "充值", "加群", "联系我们", "qq群", "微信"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	for _, secret := range kkaiPublicContextSecrets(c) {
		if strings.Contains(text, secret) {
			return true
		}
	}
	return false
}

func kkaiScrubPublicText(c *gin.Context, text string) string {
	if text == "" {
		return ""
	}
	text = common.MaskSensitiveInfo(text)
	for _, secret := range kkaiPublicContextSecrets(c) {
		text = strings.ReplaceAll(text, secret, "[redacted]")
	}
	text = kkaiPublicCredentialPattern.ReplaceAllString(text, "$1: [redacted]")
	text = kkaiPublicBearerPattern.ReplaceAllString(text, "[redacted]")
	text = kkaiPublicSKPattern.ReplaceAllString(text, "[redacted]")
	text = kkaiPublicLinkPattern.ReplaceAllString(text, "[redacted]")
	return text
}

func kkaiPublicContextSecrets(c *gin.Context) []string {
	if c == nil {
		return nil
	}
	values := []string{
		c.GetString("token_key"),
		common.GetContextKeyString(c, constant.ContextKeyChannelKey),
	}
	if c.Request != nil {
		values = append(values, c.Request.Header.Get("Authorization"))
	}
	secrets := make([]string, 0, len(values)*3)
	for _, value := range values {
		appendKKAIPublicSecretVariants(&secrets, value)
	}
	return secrets
}

func appendKKAIPublicSecretVariants(secrets *[]string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	variants := []string{value}
	fields := strings.Fields(value)
	if len(fields) == 2 && strings.EqualFold(fields[0], "bearer") {
		variants = append(variants, fields[1])
	} else {
		variants = append(variants, "Bearer "+value, "sk-"+strings.TrimPrefix(value, "sk-"))
	}
	for _, variant := range variants {
		if variant != "" && !containsKKAIString(*secrets, variant) {
			*secrets = append(*secrets, variant)
		}
	}
}

func containsKKAIString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func kkaiIsPublicUpstreamError(apiErr *types.NewAPIError) bool {
	if apiErr == nil {
		return false
	}
	switch apiErr.GetErrorType() {
	case types.ErrorTypeOpenAIError, types.ErrorTypeClaudeError, types.ErrorTypeUpstreamError:
		return true
	}
	switch apiErr.GetErrorCode() {
	case types.ErrorCodeDoRequestFailed, types.ErrorCodeReadResponseBodyFailed,
		types.ErrorCodeBadResponseStatusCode, types.ErrorCodeBadResponse,
		types.ErrorCodeBadResponseBody, types.ErrorCodeEmptyResponse,
		types.ErrorCodeAwsInvokeError, types.ErrorCodePromptBlocked,
		types.ErrorCodeChannelAwsClientError, types.ErrorCodeChannelInvalidKey:
		return true
	}
	return false
}
