package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func authorizedUpstreamPolicyMetadata(t *testing.T, authMode string) string {
	t.Helper()
	metadata, err := common.Marshal(map[string]any{
		"causality":                      KKAIPolicyCausalityClientToken,
		"client_auth_mode":               authMode,
		"client_policy_marker_confirmed": true,
		"client_token_action_allowed":    true,
		"evidence_level":                 "confirmed",
		"original_status_code":           http.StatusForbidden,
		"rule_id":                        kkaiPolicyRuleVersion,
	})
	require.NoError(t, err)
	return string(metadata)
}

func TestRiskActionOutboxHandlerDeliversAuthorizedUpstreamPolicyInvalidation(t *testing.T) {
	invalidatedUser := 0
	invalidatedTokens := 0
	refreshedChannels := 0
	var notified riskActionOutboxPayload
	handler := RiskActionOutboxHandler{
		InvalidateUser:       func(userID int) error { invalidatedUser = userID; return nil },
		InvalidateUserTokens: func(userID int) error { invalidatedTokens = userID; return nil },
		RefreshChannels:      func() { refreshedChannels++ },
		LookupIncident: func(_ context.Context, incidentID int64, eventID string) (model.KKAIPolicyIncident, error) {
			return model.KKAIPolicyIncident{
				ID: incidentID, EventID: eventID, Source: RiskSourceUpstreamPolicy,
				UserID: 10, TokenID: 11, ChannelID: 12,
				RuleVersion: kkaiPolicyRuleVersion, Decision: RiskDecisionDisable,
				TokenFingerprint: RiskFingerprint("client-key"), UpstreamKeyFingerprint: RiskFingerprint("upstream-key"),
				Metadata: authorizedUpstreamPolicyMetadata(t, kkaiPolicyClientAuthBearer), TokenDisabled: true, UserDisabled: true,
			}, nil
		},
		Notify: func(payload riskActionOutboxPayload) error { notified = payload; return nil },
	}
	payload, err := common.Marshal(riskActionOutboxPayload{
		IncidentID: 2, EventID: "upstream-event-1", UserID: 10, TokenID: 11, ChannelID: 12,
		TokenDisabled: true, UserDisabled: true,
	})
	require.NoError(t, err)

	require.NoError(t, handler.Handle(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)}))
	require.Equal(t, 10, invalidatedUser)
	require.Equal(t, 10, invalidatedTokens)
	require.Zero(t, refreshedChannels)
	require.Equal(t, int64(2), notified.IncidentID)
	require.True(t, notified.TokenDisabled)
	require.True(t, notified.UserDisabled)
	require.False(t, notified.ChannelDisabled)
}

func TestRiskActionOutboxHandlerDeliversPrivilegedTokenIncidentNotification(t *testing.T) {
	invalidatedUser := false
	invalidatedTokens := 0
	refreshedChannels := false
	var notified riskActionOutboxPayload
	handler := RiskActionOutboxHandler{
		InvalidateUser:       func(int) error { invalidatedUser = true; return nil },
		InvalidateUserTokens: func(userID int) error { invalidatedTokens = userID; return nil },
		RefreshChannels:      func() { refreshedChannels = true },
		LookupIncident: func(_ context.Context, incidentID int64, eventID string) (model.KKAIPolicyIncident, error) {
			return model.KKAIPolicyIncident{
				ID: incidentID, EventID: eventID, Source: RiskSourceUpstreamPolicy,
				UserID: 20, TokenID: 21, ChannelID: 22,
				RuleVersion: kkaiPolicyRuleVersion, Decision: RiskDecisionDisable,
				TokenFingerprint: RiskFingerprint("privileged-client-key"), UpstreamKeyFingerprint: RiskFingerprint("upstream-key"),
				Metadata: authorizedUpstreamPolicyMetadata(t, kkaiPolicyClientAuthBearer), TokenDisabled: true, UserDisableSkipped: true,
			}, nil
		},
		Notify: func(payload riskActionOutboxPayload) error { notified = payload; return nil },
	}
	payload, err := common.Marshal(riskActionOutboxPayload{
		IncidentID: 4, EventID: "upstream-privileged-token-event", UserID: 20, TokenID: 21, ChannelID: 22,
		TokenDisabled: true, UserDisableSkipped: true,
	})
	require.NoError(t, err)

	require.NoError(t, handler.Handle(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)}))
	require.False(t, invalidatedUser)
	require.Equal(t, 20, invalidatedTokens)
	require.False(t, refreshedChannels)
	require.True(t, notified.TokenDisabled)
	require.True(t, notified.UserDisableSkipped)
	require.False(t, notified.ChannelDisabled)
}

func TestRiskActionOutboxHandlerInvalidatesAlreadyDisabledTargets(t *testing.T) {
	invalidatedUser := 0
	invalidatedTokens := 0
	handler := RiskActionOutboxHandler{
		InvalidateUser:       func(userID int) error { invalidatedUser = userID; return nil },
		InvalidateUserTokens: func(userID int) error { invalidatedTokens = userID; return nil },
		RefreshChannels:      func() {},
		LookupIncident: func(_ context.Context, incidentID int64, eventID string) (model.KKAIPolicyIncident, error) {
			return model.KKAIPolicyIncident{
				ID: incidentID, EventID: eventID, Source: RiskSourceUpstreamPolicy,
				UserID: 30, TokenID: 31, ChannelID: 32,
				RuleVersion: kkaiPolicyRuleVersion, Decision: RiskDecisionDisable,
				TokenFingerprint: RiskFingerprint("already-disabled-client-key"), UpstreamKeyFingerprint: RiskFingerprint("upstream-key"),
				Metadata:    authorizedUpstreamPolicyMetadata(t, kkaiPolicyClientAuthBearer),
				ActionTaken: "disable_token,disable_user", ActionResult: "already_disabled,already_disabled",
			}, nil
		},
		Notify: func(riskActionOutboxPayload) error { return nil },
	}
	payload, err := common.Marshal(riskActionOutboxPayload{
		IncidentID: 5, EventID: "upstream-already-disabled-event", UserID: 30, TokenID: 31, ChannelID: 32,
		UserCacheInvalidationRequired: true, TokenCacheInvalidationRequired: true,
	})
	require.NoError(t, err)

	require.NoError(t, handler.Handle(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)}))
	require.Equal(t, 30, invalidatedUser)
	require.Equal(t, 30, invalidatedTokens)
}

func TestRiskActionOutboxHandlerRejectsUpstreamPolicyChannelMutation(t *testing.T) {
	mutated := false
	handler := RiskActionOutboxHandler{
		InvalidateUser:       func(int) error { mutated = true; return nil },
		InvalidateUserTokens: func(int) error { mutated = true; return nil },
		RefreshChannels:      func() { mutated = true },
		LookupIncident: func(_ context.Context, incidentID int64, eventID string) (model.KKAIPolicyIncident, error) {
			return model.KKAIPolicyIncident{
				ID: incidentID, EventID: eventID, Source: RiskSourceUpstreamPolicy,
				UserID: 10, TokenID: 11, ChannelID: 12, ChannelDisabled: true,
			}, nil
		},
		Notify: func(riskActionOutboxPayload) error { mutated = true; return nil },
	}
	payload, err := common.Marshal(riskActionOutboxPayload{
		IncidentID: 3, EventID: "upstream-channel-mutation", UserID: 10, TokenID: 11, ChannelID: 12, ChannelDisabled: true,
	})
	require.NoError(t, err)

	err = handler.Handle(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)})
	require.ErrorIs(t, err, ErrRiskActionInvalidInput)
	require.False(t, mutated)
}
