package dto

import (
	"encoding/json"
	"testing"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesRequestTokenCountMeta(t *testing.T) {
	for _, tc := range []struct {
		name      string
		input     string
		wantText  string
		wantFiles []types.FileType
		wantData  []string
	}{
		{
			name:     "plain input",
			input:    `"hello"`,
			wantText: "hello",
		},
		{
			name: "message history",
			input: `[
				{"role":"user","content":"question"},
				{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer\n继续"}]},
				{"role":"user","content":[{"type":"input_text","text":"follow up"}]}
			]`,
			wantText: "question\nanswer\n继续\nfollow up",
		},
		{
			name: "function history",
			input: `[
				{"type":"function_call","call_id":"opaque-call-id","name":"lookup","arguments":"{\"query\":\"你好\"}"},
				{"type":"function_call_output","call_id":"opaque-call-id","output":"result\nwith details"}
			]`,
			wantText: "lookup\n{\"query\":\"你好\"}\nresult\nwith details",
		},
		{
			name: "custom tool history",
			input: `[
				{"type":"custom_tool_call","call_id":"opaque-call-id","name":"shell","input":"pwd\nls"},
				{"type":"custom_tool_call_output","call_id":"opaque-call-id","output":[{"type":"input_text","text":"directory listing"}]},
				{"type":"custom_tool_call_output","call_id":"another-call-id","output":"exit status 0"}
			]`,
			wantText: "shell\npwd\nls\ndirectory listing\nexit status 0",
		},
		{
			name: "multimodal function output uses media estimation",
			input: `[
				{"type":"function_call_output","call_id":"opaque-call-id","output":[
					{"type":"input_text","text":"tool result"},
					{"type":"input_image","image_url":"https://example.com/result.png"},
					{"type":"input_file","file_url":"https://example.com/result.pdf"}
				]}
			]`,
			wantText:  "tool result",
			wantFiles: []types.FileType{types.FileTypeImage, types.FileTypeFile},
			wantData:  []string{"https://example.com/result.png", "https://example.com/result.pdf"},
		},
		{
			name: "opaque context and unknown shapes remain tolerant",
			input: `[
				{"type":"reasoning","encrypted_content":"ciphertext-not-text","summary":[{"type":"summary_text","text":"opaque-summary"}]},
				{"type":"future_item","payload":"unknown-payload"},
				{"type":"function_call","name":42,"arguments":{"unknown":"object arguments"}},
				{"type":"function_call_output","output":{"unknown":"object output"}},
				{"role":"user","content":[
					{"type":"input_image","file_id":"opaque-file-id"},
					{"type":"input_file","file_data":"base64-not-text","file_id":"opaque-file-id"},
					{"type":"future_part","text":"unknown-part"},
					{"type":"input_text","text":"visible text"}
				]}
			]`,
			wantText: "visible text",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := OpenAIResponsesRequest{Model: "gpt-4.1", Input: json.RawMessage(tc.input)}
			before, err := kitutil.Marshal(req)
			require.NoError(t, err)

			meta := req.GetTokenCountMeta()
			assert.Equal(t, tc.wantText, meta.CombineText)
			require.Len(t, meta.Files, len(tc.wantFiles))
			for i, file := range meta.Files {
				assert.Equal(t, tc.wantFiles[i], file.FileType)
				assert.Equal(t, tc.wantData[i], file.Source.GetRawData())
			}

			after, err := kitutil.Marshal(req)
			require.NoError(t, err)
			assert.Equal(t, before, after, "estimation must not change the upstream request")
		})
	}
}
