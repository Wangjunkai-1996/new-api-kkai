package common

import (
	"testing"

	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipOverridePreservesAuthorizedRequest(t *testing.T) {
	cases := []struct {
		name      string
		overrides map[string]any
		permitted bool
	}{
		{"transport settings", map[string]any{"stream": true}, true},
		{"unchanged input", map[string]any{"input": "original"}, true},
		{"sampling can affect expressions", map[string]any{"temperature": 0.2}, false},
		{"expanded input", map[string]any{"input": "original plus unbudgeted context"}, false},
		{"paid tool added", map[string]any{"tools": []any{map[string]any{"type": "web_search"}}}, false},
		{"output increased", map[string]any{"max_output_tokens": 200}, false},
		{"additional copies", map[string]any{"n": 2}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := &RelayInfo{MembershipBillingRequestID: "reserved", MembershipPricing: &hosttypes.MembershipPricing{MaxOutputTokens: 100, OutputCopies: 1}, ChannelMeta: &ChannelMeta{ParamOverride: tc.overrides}}
			output, err := ApplyParamOverrideWithRelayInfo([]byte(`{"input":"original","max_output_tokens":100,"n":1}`), info)
			if tc.permitted {
				require.NoError(t, err)
				assert.NotEmpty(t, output)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestMembershipOverrideUnchangedBodyStillValidatesJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"invalid JSON", `{"model":`},
		{"array root", `[{"model":"gpt-6"}]`},
		{"out-of-range number", `{"max_output_tokens":1e999}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &RelayInfo{
				MembershipBillingRequestID: "reserved",
				ChannelMeta: &ChannelMeta{ParamOverride: map[string]any{
					"operations": []any{map[string]any{"mode": "set_header", "path": "X-Test", "value": "enabled"}},
				}},
			}

			output, err := ApplyParamOverrideWithRelayInfo([]byte(tc.body), info)
			require.Error(t, err)
			assert.Nil(t, output)
		})
	}
}

func TestMembershipOverrideUnchangedBodyKeepsRuntimeSync(t *testing.T) {
	info := &RelayInfo{
		MembershipBillingRequestID: "reserved",
		RelayFormat:                relaytypes.RelayFormatOpenAIResponses,
		ReasoningEffort:            "medium",
		ChannelMeta: &ChannelMeta{ParamOverride: map[string]any{
			"operations": []any{
				map[string]any{"mode": "set_header", "path": "X-Test", "value": "enabled"},
				map[string]any{"mode": "set", "path": "reasoning.effort", "value": "high"},
			},
		}},
	}
	body := []byte(`{"reasoning":{"effort":"high"},"input":"hello"}`)

	output, err := ApplyParamOverrideWithRelayInfo(body, info)
	require.NoError(t, err)
	assert.Equal(t, body, output)
	assert.Equal(t, "high", info.ReasoningEffort)
	assert.True(t, info.UseRuntimeHeadersOverride)
	assert.Equal(t, "enabled", info.RuntimeHeadersOverride["x-test"])
	assert.Contains(t, info.ParamOverrideAudit, "set reasoning.effort = high")
}

func TestMembershipOverrideAllowsStreamOnlyChanges(t *testing.T) {
	info := &RelayInfo{
		MembershipBillingRequestID: "reserved",
		ChannelMeta: &ChannelMeta{ParamOverride: map[string]any{
			"stream":         true,
			"stream_options": map[string]any{"include_usage": true},
		}},
	}

	output, err := ApplyParamOverrideWithRelayInfo([]byte(`{"input":"hello","stream":false,"stream_options":{"include_usage":false}}`), info)
	require.NoError(t, err)
	assert.JSONEq(t, `{"input":"hello","stream":true,"stream_options":{"include_usage":true}}`, string(output))
}
