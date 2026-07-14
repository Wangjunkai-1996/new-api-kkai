package riskguard

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
)

const maxInspectionTextBytes = 256 * 1024

type Inspection struct {
	BodyBytes  int64
	BodySHA256 string
	Model      string
	Text       string
}

type replayReadCloser struct {
	io.Reader
	io.Closer
}

func InspectRequest(request *http.Request, maxBodyBytes int64) (Inspection, error) {
	if request == nil || request.Body == nil || request.Method != http.MethodPost || maxBodyBytes <= 0 {
		return Inspection{}, nil
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return Inspection{}, nil
	}
	originalBody := request.Body
	captured, err := io.ReadAll(io.LimitReader(originalBody, maxBodyBytes+1))
	if err != nil {
		return Inspection{}, err
	}
	request.Body = &replayReadCloser{
		Reader: io.MultiReader(bytes.NewReader(captured), originalBody),
		Closer: originalBody,
	}
	if int64(len(captured)) > maxBodyBytes {
		return Inspection{}, nil
	}

	var document map[string]any
	decoder := json.NewDecoder(bytes.NewReader(captured))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return Inspection{}, nil
	}
	digest := sha256.Sum256(captured)
	return Inspection{
		BodyBytes:  int64(len(captured)),
		BodySHA256: hex.EncodeToString(digest[:]),
		Model:      stringField(document, "model", 128),
		Text:       inspectionText(request.URL.Path, document),
	}, nil
}

func inspectionText(path string, document map[string]any) string {
	switch {
	case strings.HasSuffix(path, "/chat/completions"), path == "/v1/messages":
		return lastRoleContent(document["messages"], "user")
	case strings.HasSuffix(path, "/responses"):
		if text, ok := document["input"].(string); ok {
			return boundedText(text)
		}
		return lastRoleContent(document["input"], "user")
	case strings.HasSuffix(path, "/completions"):
		return collectText(document["prompt"])
	case strings.Contains(path, ":generateContent"):
		return lastRoleContent(document["contents"], "user")
	default:
		return ""
	}
}

func lastRoleContent(raw any, role string) string {
	items, ok := raw.([]any)
	if !ok {
		return ""
	}
	for index := len(items) - 1; index >= 0; index-- {
		item, ok := items[index].(map[string]any)
		if !ok || !strings.EqualFold(stringField(item, "role", 32), role) {
			continue
		}
		if content, exists := item["content"]; exists {
			return collectText(content)
		}
		return collectText(item["parts"])
	}
	return ""
}

func collectText(value any) string {
	var builder strings.Builder
	appendText(&builder, value)
	return boundedText(builder.String())
}

func appendText(builder *strings.Builder, value any) {
	if builder.Len() >= maxInspectionTextBytes {
		return
	}
	switch typed := value.(type) {
	case string:
		builder.WriteString(typed)
		builder.WriteByte('\n')
	case []any:
		for _, item := range typed {
			appendText(builder, item)
		}
	case map[string]any:
		for _, key := range []string{"text", "content", "input_text"} {
			if item, exists := typed[key]; exists {
				appendText(builder, item)
			}
		}
	}
}

func boundedText(value string) string {
	if len(value) > maxInspectionTextBytes {
		return value[:maxInspectionTextBytes]
	}
	return value
}

func stringField(document map[string]any, key string, maxLength int) string {
	value, _ := document[key].(string)
	value = strings.TrimSpace(value)
	if len(value) > maxLength {
		return value[:maxLength]
	}
	return value
}

func TokenFingerprint(authorization string) string {
	prefix, token, found := strings.Cut(strings.TrimSpace(authorization), " ")
	if !found || !strings.EqualFold(prefix, "bearer") || len(token) < 8 {
		return ""
	}
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
