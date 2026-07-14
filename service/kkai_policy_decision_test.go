package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKKAIRiskDecisionRequiresExplicitUpstreamActionPermission(t *testing.T) {
	event := RiskStreamEvent{
		Source:         RiskSourceUpstreamPolicy,
		Recommendation: RiskDecisionDisable,
		ChannelID:      12,
		Metadata: map[string]any{
			"evidence_level": "confirmed",
			"causality":      KKAIPolicyCausalityUpstreamKey,
		},
	}

	decision, actions, err := DecideKKAIRiskStreamEvent(event)
	require.NoError(t, err)
	assert.Equal(t, RiskDecisionReject, decision)
	assert.Equal(t, RiskDurableActions{}, actions)

	event.Metadata["upstream_action_allowed"] = true
	decision, actions, err = DecideKKAIRiskStreamEvent(event)
	require.NoError(t, err)
	assert.Equal(t, RiskDecisionDisable, decision)
	assert.Equal(t, RiskDurableActions{DisableChannel: true}, actions)
}

func TestKKAIRiskDecisionRecordsAmbiguousAttributionWithoutAction(t *testing.T) {
	decision, actions, err := DecideKKAIRiskStreamEvent(RiskStreamEvent{
		Recommendation: RiskDecisionDisable,
		Metadata: map[string]any{
			"evidence_level": "confirmed",
			"causality":      KKAIPolicyCausalityAmbiguous,
		},
	})

	require.NoError(t, err)
	assert.Equal(t, RiskDecisionReject, decision)
	assert.Equal(t, RiskDurableActions{}, actions)
}
