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

func TestKKAIRiskDecisionAuthorizesOnlyConfirmedUpstreamClientIdentity(t *testing.T) {
	validEvent := func() RiskStreamEvent {
		return RiskStreamEvent{
			Source:                 RiskSourceUpstreamPolicy,
			Recommendation:         RiskDecisionDisable,
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
				"client_token_action_allowed":    true,
				"evidence_level":                 "confirmed",
				"original_status_code":           http.StatusBadRequest,
				"rule_id":                        kkaiPolicyRuleVersion,
			},
		}
	}

	t.Run("bearer disables exact token and user", func(t *testing.T) {
		decision, actions, err := DecideKKAIRiskStreamEvent(validEvent())
		require.NoError(t, err)
		assert.Equal(t, RiskDecisionDisable, decision)
		assert.Equal(t, RiskDurableActions{DisableToken: true, DisableUser: true}, actions)
	})

	t.Run("playground disables only user", func(t *testing.T) {
		event := validEvent()
		event.TokenID = 0
		event.TokenFingerprint = ""
		event.Metadata["client_auth_mode"] = kkaiPolicyClientAuthPlayground
		decision, actions, err := DecideKKAIRiskStreamEvent(event)
		require.NoError(t, err)
		assert.Equal(t, RiskDecisionDisable, decision)
		assert.Equal(t, RiskDurableActions{DisableUser: true}, actions)
	})

	tests := []struct {
		name   string
		mutate func(*RiskStreamEvent)
	}{
		{name: "missing original status", mutate: func(event *RiskStreamEvent) { delete(event.Metadata, "original_status_code") }},
		{name: "status below HTTP range", mutate: func(event *RiskStreamEvent) { event.Metadata["original_status_code"] = 99 }},
		{name: "status above HTTP range", mutate: func(event *RiskStreamEvent) { event.Metadata["original_status_code"] = 600 }},
		{name: "status has invalid type", mutate: func(event *RiskStreamEvent) { event.Metadata["original_status_code"] = "not-a-status" }},
		{name: "missing client marker", mutate: func(event *RiskStreamEvent) { delete(event.Metadata, "client_policy_marker_confirmed") }},
		{name: "missing auth mode", mutate: func(event *RiskStreamEvent) { delete(event.Metadata, "client_auth_mode") }},
		{name: "missing channel", mutate: func(event *RiskStreamEvent) { event.ChannelID = 0 }},
		{name: "missing upstream fingerprint", mutate: func(event *RiskStreamEvent) { event.UpstreamKeyFingerprint = "" }},
		{name: "missing bearer fingerprint", mutate: func(event *RiskStreamEvent) { event.TokenFingerprint = "" }},
		{name: "rule mismatch", mutate: func(event *RiskStreamEvent) { event.Metadata["rule_id"] = "different-rule" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := validEvent()
			test.mutate(&event)
			_, _, err := DecideKKAIRiskStreamEvent(event)
			require.ErrorIs(t, err, ErrRiskStreamDecisionRejected)
		})
	}
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
