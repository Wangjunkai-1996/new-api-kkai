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

func TestGroupStatusCacheUsageHonorsClientStreamFlag(t *testing.T) {
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

func TestGroupStatusCacheUsageMinimumInput(t *testing.T) {
	for _, test := range []struct {
		name, upstream, returned string
		prompt, cached           int
		stream, eligible         bool
	}{
		{name: "short GPT stream", upstream: "gpt-6.2-sol", prompt: 1023, stream: true},
		{name: "minimum GPT stream with zero read", upstream: "gpt-6.2-sol", prompt: 1024, stream: true, eligible: true},
		{name: "short non-stream with reported cache", upstream: "openai/gpt-5.4", prompt: 1023, cached: 512},
		{name: "minimum non-stream with cache", upstream: "openai/gpt-5.4", prompt: 1024, cached: 512, eligible: true},
		{name: "minimum non-stream without cache", upstream: "gpt-4o", prompt: 1024},
		{name: "Codex review", upstream: "codex-auto-review", prompt: 1023, stream: true},
		{name: "dated reasoning model", upstream: "o3-mini-2025-01-31", prompt: 1023, stream: true},
		{name: "upstream resolves deployment alias", upstream: "deployment", returned: "gpt-6.2-astra", prompt: 1023, stream: true},
		{name: "GPT alias served by another model", upstream: "gpt-6.2-sol", returned: "deepseek-chat", prompt: 1023, stream: true, eligible: true},
		{name: "mapping overrides client GPT name", upstream: "gemini-3-pro", prompt: 1023, stream: true, eligible: true},
		{name: "self-hosted model", upstream: "openai/gpt-oss-120b", prompt: 512, stream: true, eligible: true},
		{name: "unrecognized provider", upstream: "custom/gpt-6.2-sol", prompt: 512, stream: true, eligible: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				OriginModelName: "gpt-5",
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: test.upstream},
				RelayMode:       relayconstant.RelayModeResponses,
				IsStream:        test.stream,
			}
			info.ObserveResponseModel(test.returned)
			usage := &dto.Usage{PromptTokens: test.prompt}
			summary := textQuotaSummary{PromptTokens: test.prompt, CacheTokens: test.cached}
			got := groupStatusCacheUsage(info, false, false, usage, summary)
			assert.Equal(t, test.eligible, got != nil)
		})
	}
}
