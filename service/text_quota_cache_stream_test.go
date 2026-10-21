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

func TestGroupStatusCacheUsageExcludesOnlyNonStreamZeroCache(t *testing.T) {
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
		cachedTokens int
		wantEligible bool
	}{
		{name: "non-stream zero cache"},
		{name: "non-stream low positive cache", cachedTokens: 1, wantEligible: true},
		{name: "non-stream high cache", cachedTokens: 930, wantEligible: true},
		{name: "stream zero cache", isStream: true, wantEligible: true},
		{name: "stream high cache", isStream: true, cachedTokens: 930, wantEligible: true},
		{name: "non-stream client internally streamed zero cache", isStream: true, clientStream: &nonStream},
		{name: "non-stream client internally streamed positive cache", isStream: true, clientStream: &nonStream, cachedTokens: 930, wantEligible: true},
		{name: "stream client internally buffered zero cache", clientStream: &stream, wantEligible: true},
		{name: "stream client internally buffered positive cache", clientStream: &stream, cachedTokens: 930, wantEligible: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				IsStream:       test.isStream,
				ClientIsStream: test.clientStream,
				RelayMode:      relayconstant.RelayModeChatCompletions,
				RelayFormat:    types.RelayFormatOpenAI,
			}
			requestSummary := summary
			requestSummary.CacheTokens = test.cachedTokens
			cacheUsage := groupStatusCacheUsage(info, false, false, originUsage, requestSummary)
			if !test.wantEligible {
				assert.Nil(t, cacheUsage)
				return
			}
			require.NotNil(t, cacheUsage)
			assert.Equal(t, int64(1_000), cacheUsage.PromptTokens)
			assert.Equal(t, int64(test.cachedTokens), cacheUsage.CachedTokens)
		})
	}
}
