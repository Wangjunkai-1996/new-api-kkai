package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTenantTextContentBoundary(t *testing.T) {
	for _, test := range []struct {
		name    string
		body    string
		allowed bool
	}{
		{"plain text", `{"messages":[{"role":"user","content":"hello"}]}`, true},
		{"caller function", `{"tools":[{"type":"function","function":{"name":"local_weather"}}]}`, true},
		{"responses text", `{"input":[{"role":"user","content":[{"type":"input_text","text":"hi"}]}]}`, true},
		{"responses image tool", `{"tools":[{"type":"image_generation"}]}`, false},
		{"server search", `{"tools":[{"type":"web_search"}]}`, false},
		{"nested image", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]}]}`, false},
		{"tool result image", `{"messages":[{"content":[{"type":"tool_result","content":[{"type":"image","source":{"type":"base64","data":"a"}}]}]}]}`, false},
		{"audio output", `{"modalities":["text","audio"]}`, false},
		{"extra body override", `{"extra_body":{"tools":[{"type":"web_search"}]}}`, false},
		{"message tools", `{"messages":[{"content":"hi","tools":[{"type":"web_search"}]}]}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateTenantTextPayload([]byte(test.body), "tenant-text")
			if test.allowed {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
