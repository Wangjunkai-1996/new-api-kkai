package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestTenantTextContentCodexToolDeclarations(t *testing.T) {
	operation_setting.SetToolPriceForTest("tenant_content_priced", 5)
	t.Cleanup(func() { operation_setting.DeleteToolPriceForTest("tenant_content_priced") })

	for _, test := range []struct {
		name      string
		body      string
		wantError string
	}{
		{
			name: "Codex first text turn",
			body: `{"input":[
				{"type":"additional_tools","id":"tools_fixture","role":"developer","tools":[
					{"type":"namespace","name":"functions","description":"Caller tools","tools":[
						{"type":"function","name":"local_lookup","strict":true,"parameters":{"type":"object","properties":{"image":{"type":"string"},"source":{"type":"object"},"tools":{"type":"array","items":{"type":"string"}}}}},
						{"type":"custom","name":"local_patch","format":{"type":"grammar","syntax":"lark","definition":"start: WORD"}}
					]}
				]},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
			]}`,
		},
		{
			name: "top level nested namespaces",
			body: `{"tools":[{"type":"namespace","name":"outer","tools":[{"type":"namespace","name":"inner","tools":[{"type":"function","name":"local_lookup"},{"type":"custom","name":"local_patch"}]}]}]}`,
		},
		{
			name:      "additional hosted tool",
			body:      `{"input":[{"type":"additional_tools","tools":[{"type":"web_search"}]}]}`,
			wantError: "provider-hosted tools are unavailable",
		},
		{
			name:      "hosted tool inside additional namespace",
			body:      `{"input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"functions","tools":[{"type":"image_generation"}]}]}]}`,
			wantError: "provider-hosted tools are unavailable",
		},
		{
			name:      "hosted tool inside top level namespaces",
			body:      `{"tools":[{"type":"namespace","name":"outer","tools":[{"type":"namespace","name":"inner","tools":[{"type":"web_search"}]}]}]}`,
			wantError: "provider-hosted tools are unavailable",
		},
		{
			name:      "paid function inside additional namespace",
			body:      `{"input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"tenant_content_priced"}]}]}]}`,
			wantError: "paid tools are unavailable",
		},
		{
			name:      "paid custom tool inside additional namespace",
			body:      `{"input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"tenant_content_priced"}]}]}]}`,
			wantError: "paid tools are unavailable",
		},
		{
			name:      "paid function inside top level namespace",
			body:      `{"tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"tenant_content_priced"}]}]}`,
			wantError: "paid tools are unavailable",
		},
		{
			name:      "additional tools in message content",
			body:      `{"input":[{"type":"message","content":[{"type":"additional_tools","tools":[{"type":"function","name":"local_lookup"}]}]}]}`,
			wantError: "only text and caller function content is supported",
		},
		{
			name:      "additional tools in chat messages",
			body:      `{"messages":[{"type":"additional_tools","tools":[{"type":"function","name":"local_lookup"}]}]}`,
			wantError: "only text and caller function content is supported",
		},
		{
			name:      "additional tools in nested input array",
			body:      `{"input":[[{"type":"additional_tools","tools":[{"type":"function","name":"local_lookup"}]}]]}`,
			wantError: "only text and caller function content is supported",
		},
		{
			name:      "invalid namespace tools",
			body:      `{"tools":[{"type":"namespace","name":"functions","tools":{"type":"web_search"}}]}`,
			wantError: "invalid reseller function tools",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateTenantTextPayload([]byte(test.body), "tenant-text")
			if test.wantError == "" {
				require.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, test.wantError)
		})
	}
}
