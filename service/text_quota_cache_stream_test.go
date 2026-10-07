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

func TestGroupStatusCacheUsageIncludesNonStreamRequests(t *testing.T) {
	originUsage := &dto.Usage{PromptTokens: 1_000}
	summary := textQuotaSummary{
		PromptTokens:  1_000,
		CacheTokens:   930,
		UsageSemantic: dto.BillingUsageSemanticOpenAI,
	}

	nonStream := &relaycommon.RelayInfo{
		IsStream:    false,
		RelayMode:   relayconstant.RelayModeChatCompletions,
		RelayFormat: types.RelayFormatOpenAI,
	}
	nonStreamUsage := groupStatusCacheUsage(nonStream, false, false, originUsage, summary)
	require.NotNil(t, nonStreamUsage)

	stream := *nonStream
	stream.IsStream = true
	cacheUsage := groupStatusCacheUsage(&stream, false, false, originUsage, summary)
	require.NotNil(t, cacheUsage)
	assert.Equal(t, nonStreamUsage, cacheUsage)
	assert.Equal(t, int64(1_000), cacheUsage.PromptTokens)
	assert.Equal(t, int64(930), cacheUsage.CachedTokens)
}

func TestGroupStatusCacheUsageDoesNotDependOnClientStreamFlag(t *testing.T) {
	clientStream := false
	info := &relaycommon.RelayInfo{
		IsStream:       true,
		ClientIsStream: &clientStream,
		RelayMode:      relayconstant.RelayModeChatCompletions,
		RelayFormat:    types.RelayFormatOpenAI,
	}
	originUsage := &dto.Usage{PromptTokens: 1_000}
	summary := textQuotaSummary{
		PromptTokens:  1_000,
		CacheTokens:   930,
		UsageSemantic: dto.BillingUsageSemanticOpenAI,
	}

	cacheUsage := groupStatusCacheUsage(info, false, false, originUsage, summary)
	require.NotNil(t, cacheUsage)
	assert.Equal(t, int64(1_000), cacheUsage.PromptTokens)
	assert.Equal(t, int64(930), cacheUsage.CachedTokens)
}
