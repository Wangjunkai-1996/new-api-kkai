package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKKAIRiskDecisionNeverMutatesForUpstreamPolicy(t *testing.T) {
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
	assert.Equal(t, RiskDecisionReject, decision)
	assert.Equal(t, RiskDurableActions{}, actions)
}

func TestKKAIRiskDecisionNeverDisablesConfirmedUpstreamClientIdentity(t *testing.T) {
	validEvent := func() RiskStreamEvent {
		return RiskStreamEvent{
			Source:                 RiskSourceUpstreamPolicy,
			Recommendation:         RiskDecisionReject,
			UserID:                 10,
			TokenID:                11,
			ChannelID:              12,
			RuleVersion:            kkaiPolicyRuleVersion,
			TokenFingerprint:       RiskFingerprint("client-key"),
			UpstreamKeyFingerprint: RiskFingerprint("upstream-key"),
			Metadata: map[string]any{
				"causality":                      KKAIPolicyCausalityClientToken,
				"client_auth_mode":               kkaiPolicyClientAuthBearer,
				"client_policy_marker_confirmed": true,
				"client_token_cooldown_allowed":  true,
				"evidence_level":                 "confirmed",
				"original_status_code":           http.StatusBadRequest,
				"rule_id":                        kkaiPolicyRuleVersion,
			},
		}
	}

	t.Run("bearer records without durable action", func(t *testing.T) {
		decision, actions, err := DecideKKAIRiskStreamEvent(validEvent())
		require.NoError(t, err)
		assert.Equal(t, RiskDecisionReject, decision)
		assert.Equal(t, RiskDurableActions{}, actions)
	})

	t.Run("playground records without durable action", func(t *testing.T) {
		event := validEvent()
		event.TokenID = 0
		event.TokenFingerprint = ""
		event.Metadata["client_auth_mode"] = kkaiPolicyClientAuthPlayground
		event.Metadata["client_token_cooldown_allowed"] = false
		decision, actions, err := DecideKKAIRiskStreamEvent(event)
		require.NoError(t, err)
		assert.Equal(t, RiskDecisionReject, decision)
		assert.Equal(t, RiskDurableActions{}, actions)
	})
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
