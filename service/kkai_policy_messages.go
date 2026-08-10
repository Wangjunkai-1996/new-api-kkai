package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/types"
)

func KKAIPolicyMessageForKeyword() string {
	return "提示：本次请求包含暂不支持的内容，已停止处理。请修改相关内容后重试。本次拦截仅针对当前请求，不会停用你的账号或 API Key。"
}

func KKAIPolicyMessageForCyber() string {
	return "安全警告：本次请求触发了上游安全策略，已停止处理。Token/账号已停用，等待人工复核。"
}

func KKAIPolicyMessageForKeyCooldown(retryAfter int) string {
	if retryAfter < 1 {
		retryAfter = 1
	}
	return fmt.Sprintf("安全提示：当前 API Key 已被临时隔离，还需等待 %d 秒。请停止相关请求并联系管理员复核。", retryAfter)
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
		return "当前会话已因安全策略停止处理，请联系管理员复核。"
	default:
		return "本次请求已被安全策略停止处理。"
	}
}
