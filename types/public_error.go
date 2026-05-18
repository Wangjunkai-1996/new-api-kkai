package types

import "strings"

const (
	PublicMessageRequestBlockedByPolicy        = "request blocked by policy"
	PublicMessageRequestTemporarilyUnavailable = "request temporarily unavailable"
	PublicMessageUpstreamUnavailable           = "upstream unavailable"
	PublicMessageUpstreamError                 = "upstream error"
)

func IsUpstreamUnavailableError(err *NewAPIError) bool {
	if err == nil {
		return false
	}
	switch err.GetErrorCode() {
	case ErrorCodeChannelNoAvailableKey, ErrorCodePolicyUpstreamKeyIsolated, ErrorCodeGetChannelFailed:
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "no available keys") ||
		strings.Contains(text, "no enabled keys") ||
		strings.Contains(text, "no keys available") ||
		strings.Contains(text, "policy breaker") ||
		strings.Contains(text, "upstream key is temporarily isolated")
}

func LooksLikeNoisyUpstreamMessage(message string) bool {
	text := strings.ToLower(strings.TrimSpace(message))
	if text == "" {
		return false
	}
	markers := []string{
		"http://",
		"https://",
		"t.me/",
		"telegram",
		"discord.gg",
		"wechat",
		"微信",
		"qq群",
		"qq group",
		"广告",
		"购买",
		"buy key",
		"buy api",
		"recharge",
		"充值",
		"加群",
		"contact us",
		"联系我们",
	}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
