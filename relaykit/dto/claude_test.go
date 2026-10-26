package dto

import (
	"testing"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeRequestPreservesSafeguardsAndPerMessageOutputConfig(t *testing.T) {
	request := ClaudeRequest{
		Model:      "claude-test",
		Safeguards: []byte(`{"custom":"enabled"}`),
		Messages: []ClaudeMessage{{
			Role:         "system",
			Content:      "policy",
			OutputConfig: []byte(`{"effort":"high"}`),
		}},
	}

	raw, err := kitutil.Marshal(request)
	require.NoError(t, err)

	var decoded ClaudeRequest
	require.NoError(t, kitutil.Unmarshal(raw, &decoded))
	assert.JSONEq(t, `{"custom":"enabled"}`, string(decoded.Safeguards))
	require.Len(t, decoded.Messages, 1)
	assert.JSONEq(t, `{"effort":"high"}`, string(decoded.Messages[0].OutputConfig))
}

func TestClaudeRequestCountsEitherExplicitOutputLimit(t *testing.T) {
	small, medium, large := uint(512), uint(2048), uint(4096)
	for _, tc := range []struct {
		name          string
		maxTokens     *uint
		maxToSample   *uint
		wantMaxTokens int
	}{
		{name: "legacy limit only", maxToSample: &medium, wantMaxTokens: 2048},
		{name: "larger legacy limit", maxTokens: &small, maxToSample: &medium, wantMaxTokens: 2048},
		{name: "larger standard limit", maxTokens: &large, maxToSample: &medium, wantMaxTokens: 4096},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := ClaudeRequest{MaxTokens: tc.maxTokens, MaxTokensToSample: tc.maxToSample}
			assert.Equal(t, tc.wantMaxTokens, request.GetTokenCountMeta().MaxTokens)
		})
	}
}
