package model

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"gorm.io/gorm"
)

// TaskFailureSnapshot never stores a raw response, credentials or source URL.
// It lives in private_data so diagnostics do not alter the accounting schema.
type TaskFailureSnapshot struct {
	Code       string `json:"code"`
	Message    string `json:"message,omitempty"`
	Stage      string `json:"stage,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	RecordedAt int64  `json:"recorded_at,omitempty"`
}

var (
	taskFailureURL        = regexp.MustCompile(`(?i)(?:https?|s3|gs|file)://[^\s<>"']+|data:[^\s<>"']+`)
	taskFailureCredential = regexp.MustCompile(`(?is)\b(?:authorization|proxy-authorization|cookie|set-cookie|api[-_ ]?key|access[-_ ]?key|private[-_ ]?key|client[-_ ]?secret|secret[-_ ]?key|access[-_ ]?token|refresh[-_ ]?token|session[-_ ]?token|secret|password|signature|credential|key)["']?\s*[=:].*`)
	taskFailureBearer     = regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+[^\s,;"'}]+|\b(?:sk-[a-z0-9_-]{8,}|AIza[a-z0-9_-]{20,}|eyJ[a-z0-9_-]+\.[a-z0-9_-]+\.[a-z0-9_-]+)`)
	taskFailureLogStatus  = regexp.MustCompile(`^status_code=\d{3},\s*`)
)

// SafeTaskFailureMessage retains business wording, while excluding transport
// diagnostics and redacting credentials even when no URL is present.
func SafeTaskFailureMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" || len(message) > 32*1024 {
		return ""
	}
	lower := strings.ToLower(message)
	for _, internal := range []string{"<html", "<!doctype", "goroutine ", "panic:", "stack trace", "sqlstate", "dial tcp", "x509:", "connection refused", "tls handshake"} {
		if strings.Contains(lower, internal) {
			return ""
		}
	}
	// JSON is not a business message. Extract allowlisted fields before calling.
	if strings.HasPrefix(message, "{") || strings.HasPrefix(message, "[") {
		return ""
	}
	message = taskFailureURL.ReplaceAllString(message, "[redacted]")
	message = taskFailureCredential.ReplaceAllString(message, "[redacted]")
	message = taskFailureBearer.ReplaceAllString(message, "[redacted]")
	message = common.MaskSensitiveInfo(message)
	message = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return ' '
		}
		return r
	}, message)
	message = strings.TrimSpace(message)
	runes := []rune(message)
	if len(runes) > 2048 {
		message = string(runes[:2048]) + "…"
	}
	return message
}

// taskFailureEvidence reads only known error fields; arbitrary provider data
// and echoed request/input fields can never become public diagnostic text.
func taskFailureEvidence(raw string) (string, string, bool) {
	raw = taskFailureLogStatus.ReplaceAllString(strings.TrimSpace(raw), "")
	for depth := 0; depth < 3 && len(raw) <= 64*1024; depth++ {
		var payload map[string]any
		if common.UnmarshalJsonStr(raw, &payload) != nil || payload == nil {
			return "", raw, false
		}
		if nested, ok := payload["error"].(map[string]any); ok {
			payload = nested
		} else if message, ok := payload["error"].(string); ok {
			raw = message
			continue
		}
		code, _ := payload["code"].(string)
		nestedMessage := ""
		for _, field := range []string{"message", "fail_reason"} {
			if message, ok := payload[field].(string); ok && strings.TrimSpace(message) != "" {
				if strings.HasPrefix(strings.TrimSpace(message), "{") {
					nestedMessage = message
					break
				}
				return code, message, true
			}
		}
		if nestedMessage != "" {
			raw = nestedMessage
			continue
		}
		if nested, ok := payload["error"].(map[string]any); ok {
			encoded, err := common.Marshal(nested)
			if err == nil {
				raw = string(encoded)
				continue
			}
		}
		return code, "", true
	}
	return "", "", false
}

// ClassifyTaskFailure is shared by Studio and video history. It never returns
// provider-controlled codes, so the UI can use a fixed translation dictionary.
func ClassifyTaskFailure(diagnostic string) string {
	diagnostic = strings.ToLower(diagnostic)
	for _, rule := range []struct {
		code  string
		terms []string
	}{
		{"privacy_restriction", []string{"privacy_restriction", "sensitivecontentdetected.privacyinformation", "may contain real person", "sensitive information"}},
		{"copyright_restriction", []string{"copyright_restriction", "copyright restrictions", "copyright infringement"}},
		{"content_policy_violation", []string{"content_policy_violation", "sensitivecontentdetected.policyviolation", "content policy", "content_policy", "safety policy", "内容审核", "内容违规"}},
		{"upstream_authentication", []string{"upstream_authentication", "invalid api key", "invalid_api_key", "authentication failed", "unauthorized", "鉴权失败"}},
		{"billing_failure", []string{"billing_failure", "扣费失败"}},
		{"quota_exceeded", []string{"quota_exceeded", "insufficient_user_quota", "insufficient_quota", "insufficient balance", "余额不足", "credit limit is insufficient"}},
		{"rate_limited", []string{"rate_limited", "rate limit", "too many requests", "限流", "请求过于频繁"}},
		{"unsupported_media", []string{"unsupported_media", "unsupportedimageformat", "format is not supported", "file type is not supported", "文件类型不受支持", "格式不受支持"}},
		{"media_unavailable", []string{"media_unavailable", "resource not found", "resource download failed", "invalidparameter.downloadfailed", "failed to download media from the provided url", "invalid video_url", "asset_ref_invalid", "资源同步失败"}},
		{"input_invalid", []string{"input_invalid", "invalidparameter", "invalid_parameter", "invalid parameter", "duration (seconds)", "pixel count", "width must be between", "video height"}},
		{"task_expired", []string{"task_expired", "任务不存在或已过期", "task expired", "task not found"}},
		{"temporary_failure", []string{"temporary_failure", "timed out", "timeout", "超时", "upstream request failed", "当前用户未分配该模型可用的厂商", "upstream returned error", "service unavailable", "internal server error", "overloaded"}},
		{"submission_failure", []string{"submission_failure", "task submission failed"}},
	} {
		for _, term := range rule.terms {
			if strings.Contains(diagnostic, term) {
				return rule.code
			}
		}
	}
	return ""
}

// NewTaskFailureSnapshot accepts provider error fields, never the whole input
// request. Unknown unstructured internal errors deliberately have no message.
func NewTaskFailureSnapshot(stage, code, reason string, payload []byte) *TaskFailureSnapshot {
	parsedCode, message, structured := taskFailureEvidence(reason)
	if code == "" {
		code = parsedCode
	}
	if len(payload) > 0 {
		payloadCode, payloadMessage, ok := taskFailureEvidence(string(payload))
		if ok && payloadMessage != "" {
			if payloadCode != "" {
				code = payloadCode
			}
			message, structured = payloadMessage, true
		}
	}
	classification := ClassifyTaskFailure(code + " " + message)
	if classification == "" {
		classification = "generation_failure"
		if !structured {
			message = ""
		}
	}
	if classification == "submission_failure" {
		message = ""
	}
	if stage != "submission" && stage != "generation" {
		stage = ""
	}
	return &TaskFailureSnapshot{Code: classification, Message: SafeTaskFailureMessage(message), Stage: stage, RecordedAt: time.Now().Unix()}
}

// FailureForDisplay returns only safe diagnostics for terminal failures. A
// missing legacy detail remains distinguishable from a new unknown failure.
func (task *Task) FailureForDisplay() *dto.TaskFailure {
	if task == nil || task.Status != TaskStatusFailure {
		return nil
	}
	failure := task.PrivateData.Failure
	if failure == nil {
		failure = NewTaskFailureSnapshot("", "", task.FailReason, task.Data)
		if failure.Message == "" && (failure.Code == "generation_failure" || failure.Code == "submission_failure") {
			failure.Code = "historical_detail_unavailable"
		}
	}
	code := ClassifyTaskFailure(failure.Code)
	if code == "" {
		code = "generation_failure"
		if failure.Code == "historical_detail_unavailable" {
			code = failure.Code
		}
	}
	stage := failure.Stage
	if stage != "submission" && stage != "generation" {
		stage = ""
	}
	return &dto.TaskFailure{Code: code, Message: SafeTaskFailureMessage(failure.Message), Stage: stage}
}

// RecordTaskFailure only attaches diagnostics after a definitive failure. The
// locked merge preserves refunds and other concurrent private_data changes.
func RecordTaskFailure(ctx context.Context, taskID int64, failure *TaskFailureSnapshot) error {
	if taskID <= 0 || failure == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task Task
		if err := lockForUpdate(tx).First(&task, taskID).Error; err != nil {
			return err
		}
		if task.Status != TaskStatusFailure || task.PrivateData.Failure != nil {
			return nil
		}
		if execution := task.PrivateData.Execution; execution != nil {
			failure.RequestID = execution.RequestID
		}
		task.PrivateData.Failure = failure
		return tx.Model(&Task{}).Where("id = ?", taskID).Update("private_data", task.PrivateData).Error
	})
}
