package service

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
)

var (
	imageDiagnosticIdentifier = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
	imageDiagnosticAuth       = regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+[^\s,;"'<>]+`)
	imageDiagnosticSecret     = regexp.MustCompile(`(?i)\b(?:authorization|api[_-]?key|access[_-]?token|refresh[_-]?token|token|secret|password|credential)["']?\s*[:=]\s*(?:"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|[^\s,;"'<>]+)`)
	imageDiagnosticUnclean    = regexp.MustCompile(`(?i)\b(?:authorization|api[_-]?key|access[_-]?token|refresh[_-]?token|token|secret|password|credential)["']?\s*[:=]`)
	imageDiagnosticURL        = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s"'<>]*`)
	imageDiagnosticDocument   = regexp.MustCompile(`(?i)^(?:\{\s*"|\[\s*(?:\{|\[|"|\])|<(?:!doctype|html|head|body|error)(?:\s|>))`)
)

type imageUpstreamErrorDiagnostic struct {
	Message    string `json:"message,omitempty"`
	Type       string `json:"type,omitempty"`
	Code       string `json:"code,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	Truncated  bool   `json:"truncated"`
}

// imageGenerationDiagnostic is an admin-only projection, never evidence for
// completion, retry, policy enforcement or billing. It leaves apiErr untouched.
func imageGenerationDiagnostic(apiErr *types.NewAPIError, usingKey string, upstreamURLs []string, policyDetected bool) imageUpstreamErrorDiagnostic {
	diagnostic := imageUpstreamErrorDiagnostic{}
	if apiErr == nil {
		return diagnostic
	}
	if status := apiErr.GetOriginalStatusCode(); status >= 100 && status <= 599 {
		diagnostic.StatusCode = status
	}
	if upstream, ok := apiErr.RelayError.(types.OpenAIError); ok {
		diagnostic.Type = imageDiagnosticTag(upstream.Type, usingKey)
		if code, ok := upstream.Code.(string); ok {
			diagnostic.Code = imageDiagnosticTag(code, usingKey)
		}
	}
	if policyDetected || apiErr.GetPolicyEvidence() != "" ||
		IsKKAILocalPolicyCode(string(apiErr.GetErrorCode()), string(apiErr.GetOriginalErrorCode())) ||
		imageGenerationErrorCode(string(apiErr.GetOriginalErrorCode())) == ImageExecutionFailureContentPolicy {
		diagnostic.Message = "image request was rejected by a policy check"
		return diagnostic
	}
	message := apiErr.Error()
	// Avoid interpreting large response documents or malformed encodings as a
	// human error message. The structured coordinates remain useful on their own.
	if len(message) > 64*1024 || !utf8.ValidString(message) {
		return diagnostic
	}
	message = strings.TrimSpace(message)
	if imageDiagnosticDocument.MatchString(message) {
		return diagnostic
	}
	if usingKey != "" {
		message = strings.ReplaceAll(message, usingKey, "[redacted]")
		if encoded, err := common.Marshal(usingKey); err == nil && len(encoded) > 2 {
			message = strings.ReplaceAll(message, string(encoded[1:len(encoded)-1]), "[redacted]")
		}
	}
	message = common.MaskSensitiveInfo(message, upstreamURLs...)
	message = imageDiagnosticAuth.ReplaceAllString(message, "[redacted]")
	message = imageDiagnosticSecret.ReplaceAllString(message, "[redacted]")
	message = outboxSKPattern.ReplaceAllString(message, "[redacted]")
	message = imageDiagnosticURL.ReplaceAllString(message, "[redacted URL]")
	// An unterminated quoted credential is not safe to shorten or guess at.
	if imageDiagnosticUnclean.MatchString(message) {
		return diagnostic
	}
	message = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, message)
	message = strings.TrimSpace(message)
	if imageDiagnosticUnclean.MatchString(message) || imageDiagnosticURL.MatchString(message) ||
		imageDiagnosticAuth.MatchString(message) || outboxSKPattern.MatchString(message) || usingKey != "" && strings.Contains(message, usingKey) {
		return diagnostic
	}
	if len(message) > 2048 {
		end := 2048
		for !utf8.RuneStart(message[end]) {
			end--
		}
		message = message[:end]
		diagnostic.Truncated = true
	}
	diagnostic.Message = message
	return diagnostic
}

func imageDiagnosticTag(value, usingKey string) string {
	if !imageDiagnosticIdentifier.MatchString(value) || imageDiagnosticUnclean.MatchString(value) || outboxSKPattern.MatchString(value) || usingKey != "" && strings.Contains(value, usingKey) {
		return ""
	}
	return value
}
