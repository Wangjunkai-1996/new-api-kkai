package claudemessages

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeMessagesRequestToOpenAIChatHoistsToolResultImages(t *testing.T) {
	got, err := ClaudeMessagesRequestToOpenAIChat(dto.ClaudeRequest{
		Model: "claude-test",
		Messages: []dto.ClaudeMessage{
			{
				Role: "assistant",
				Content: []dto.ClaudeMediaMessage{{
					Type: "tool_use",
					Id:   "call_1",
					Name: "lookup",
				}},
			},
			{
				Role: "user",
				Content: []dto.ClaudeMediaMessage{{
					Type:      "tool_result",
					ToolUseId: "call_1",
					Content: []dto.ClaudeMediaMessage{
						{Type: "text", Text: stringPointer("lookup result")},
						{Type: "image", Source: &dto.ClaudeMessageSource{
							Type:      "base64",
							MediaType: "image/png",
							Data:      "aW1hZ2U=",
						}},
					},
				}},
			},
		},
	}, nil)
	require.NoError(t, err)

	require.Len(t, got.Messages, 3)
	assert.Equal(t, "assistant", got.Messages[0].Role)
	assert.Equal(t, "tool", got.Messages[1].Role)
	assert.Equal(t, "lookup result", got.Messages[1].StringContent())
	assert.Equal(t, "user", got.Messages[2].Role)

	media := got.Messages[2].ParseContent()
	require.Len(t, media, 1)
	assert.Equal(t, "data:image/png;base64,aW1hZ2U=", media[0].GetImageMedia().Url)
}

func stringPointer(value string) *string {
	return &value
}
