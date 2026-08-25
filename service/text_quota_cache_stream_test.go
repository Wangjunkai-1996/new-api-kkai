package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupStatusCacheUsageExcludesNonStreamRequests(t *testing.T) {
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
	assert.Nil(t, groupStatusCacheUsage(nonStream, false, false, originUsage, summary))

	stream := *nonStream
	stream.IsStream = true
	cacheUsage := groupStatusCacheUsage(&stream, false, false, originUsage, summary)
	require.NotNil(t, cacheUsage)
	assert.Equal(t, int64(1_000), cacheUsage.PromptTokens)
	assert.Equal(t, int64(930), cacheUsage.CachedTokens)
}

func TestGroupStatusCacheUsageUsesClientStreamFlag(t *testing.T) {
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

	assert.Nil(t, groupStatusCacheUsage(info, false, false, originUsage, summary))
}
