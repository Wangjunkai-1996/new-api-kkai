package common

import (
	"testing"

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
