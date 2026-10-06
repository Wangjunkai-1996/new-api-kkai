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
