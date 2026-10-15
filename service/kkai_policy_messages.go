package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/relaykit/types"
)

func KKAIPolicyMessageForKeyword() string {
	return "提示：本次请求包含暂不支持的内容，已停止处理。请修改相关内容后重试。本次拦截仅针对当前请求，不会停用你的账号或 API Key。"
}

func KKAIPolicyMessageForCyber() string {
	return "本次请求未通过上游安全检查，已停止处理。当前会话将暂停使用 60 秒，请稍后调整请求内容再试。"
}

func KKAIPolicyMessageForCyberWithoutKey() string {
	return "本次请求未通过上游安全检查，已停止处理。请调整请求内容后再试。"
}

func KKAIPolicyMessageForKeyCooldown(retryAfter int) string {
	if retryAfter < 1 {
		retryAfter = 1
	}
	return fmt.Sprintf("当前会话暂时无法继续，请在 %d 秒后重试。", retryAfter)
}

func KKAIPolicyMessageForKeyCooldownUnavailable() string {
	return "安全策略状态暂时不可用，本次请求未发送至模型服务。请稍后重试。"
}

func KKAIPolicyMessageForLocalCode(code types.ErrorCode) string {
	switch code {
	case types.ErrorCodeRequestPolicyBlocked:
		return "本次请求未通过安全审查，已停止处理。请修改请求内容后重试。"
	case types.ErrorCodePolicyContextIncomplete:
		return "请求上下文无法被完整审查，已停止处理。请重新提交完整上下文。"
	case types.ErrorCodePolicyAuditUnavailable:
		return "安全审查服务暂时不可用，本次请求未发送至模型服务。请稍后重试。"
	case types.ErrorCodeSessionBlockedByCyberPolicy:
		return "当前会话暂时无法继续，请新建会话后重试。"
	default:
		return "本次请求已被安全策略停止处理。"
	}
}
