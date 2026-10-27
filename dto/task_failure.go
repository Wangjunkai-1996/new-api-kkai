package dto

// TaskFailure contains only the safe, user-visible part of a failed task.
// Code drives localized explanations; Message is the sanitized upstream text.
type TaskFailure struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
	Stage   string `json:"stage,omitempty"`
}
