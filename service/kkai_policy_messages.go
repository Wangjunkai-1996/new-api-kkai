package service

import "fmt"

func KKAIPolicyMessageForKeyword() string {
	return "提示：本次请求包含暂不支持的内容，已停止处理。请修改相关内容后重试。本次拦截仅针对当前请求，不会停用你的账号或 API Key。"
}

func KKAIPolicyMessageForCyber() string {
	return "安全警告：本次请求触发了安全策略，已停止处理。请检查并修改请求内容后再试。你的账号和 API Key 不会因此被停用。"
}

func KKAIPolicyMessageForKeyCooldown(retryAfter int) string {
	if retryAfter < 1 {
		retryAfter = 1
	}
	return fmt.Sprintf("安全提示：当前 API Key 暂时冷却中，还需等待 %d 秒。请修改触发安全策略的请求后再试。你的账号和 API Key 不会被停用。", retryAfter)
}
