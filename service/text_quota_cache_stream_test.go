package service

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupStatusCacheUsageUsesClientStreamFlag(t *testing.T) {
	originUsage := &dto.Usage{PromptTokens: 1_000}
	summary := textQuotaSummary{
		PromptTokens:  1_000,
		CacheTokens:   930,
		UsageSemantic: dto.BillingUsageSemanticOpenAI,
	}

	stream, nonStream := true, false
	for _, test := range []struct {
		name         string
		isStream     bool
		clientStream *bool
		wantEligible bool
	}{
		{name: "non-stream request"},
		{name: "stream request", isStream: true, wantEligible: true},
		{name: "non-stream client with internal streaming", isStream: true, clientStream: &nonStream},
		{name: "stream client with internal buffering", clientStream: &stream, wantEligible: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				IsStream:       test.isStream,
				ClientIsStream: test.clientStream,
				RelayMode:      relayconstant.RelayModeChatCompletions,
				RelayFormat:    types.RelayFormatOpenAI,
			}
			cacheUsage := groupStatusCacheUsage(info, false, false, originUsage, summary)
			if !test.wantEligible {
				assert.Nil(t, cacheUsage)
				return
			}
			require.NotNil(t, cacheUsage)
			assert.Equal(t, int64(1_000), cacheUsage.PromptTokens)
			assert.Equal(t, int64(930), cacheUsage.CachedTokens)
		})
	}
}
