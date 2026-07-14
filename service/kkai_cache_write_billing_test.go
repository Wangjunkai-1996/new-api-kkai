package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKKAIOpenAICacheWriteFeedsTieredCreationVariable(t *testing.T) {
	expr := `tier("base", p * 2 + cr * 0.2 + cc * 2.5)`
	usage := &dto.Usage{
		PromptTokens: 3619,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         2921,
			CachedCreationTokens: 3000,
			CacheWriteTokens:     3616,
		},
	}

	params := BuildTieredTokenParams(usage, false, billingexpr.UsedVars(expr))
	assert.Zero(t, params.P)
	assert.Equal(t, float64(usage.PromptTokens), params.Len)
	assert.Equal(t, 2921.0, params.CR)
	assert.Equal(t, 3616.0, params.CC)

	cost, _, err := billingexpr.RunExpr(expr, params)
	require.NoError(t, err)
	assert.InDelta(t, 9624.2, cost, 1e-9)
}
