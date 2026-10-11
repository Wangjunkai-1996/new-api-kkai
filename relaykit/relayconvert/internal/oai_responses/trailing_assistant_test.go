package oairesponses

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/convdiag"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesToClaudeDropsTrailingAssistantText(t *testing.T) {
	maxTokens := uint(128)
	ctx, collector := convdiag.WithCollector(context.Background())
	request := &dto.OpenAIResponsesRequest{
		Model:          "claude-test",
		MaxOutputTokens: &maxTokens,
		Input: json.RawMessage(`[
			{"role":"user","content":"question"},
			{"role":"assistant","content":"stale answer"}
		]`),
	}

	converted, err := OpenAIResponsesRequestToClaudeMessages(ctx, &convmeta.Values{}, request)
	require.NoError(t, err)
	require.Len(t, converted.Messages, 1)
	assert.Equal(t, "user", converted.Messages[0].Role)
	assert.Contains(t, diagnosticCodes(collector.Diagnostics()), "trailing_assistant_omitted")
}

func TestResponsesToGeminiKeepsTrailingFunctionCall(t *testing.T) {
	ctx, collector := convdiag.WithCollector(context.Background())
	request := &dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		Input: json.RawMessage(`[
			{"role":"user","content":"question"},
			{"type":"function_call","call_id":"call_1","name":"lookup","arguments":{"q":"x"}}
		]`),
	}

	converted, err := OpenAIResponsesRequestToGeminiChat(ctx, request, &convmeta.Values{})
	require.NoError(t, err)
	require.Len(t, converted.Contents, 2)
	assert.Equal(t, "model", converted.Contents[1].Role)
	require.Len(t, converted.Contents[1].Parts, 1)
	assert.NotNil(t, converted.Contents[1].Parts[0].FunctionCall)
	assert.Empty(t, collector.Diagnostics())
}

func diagnosticCodes(diagnostics []types.ConversionDiagnostic) []string {
	codes := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		codes = append(codes, diagnostic.Code)
	}
	return codes
}
