package service

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestDecideKKAIRiskStreamEventRequiresConfirmedCausality(t *testing.T) {
	tests := []struct {
		name        string
		event       RiskStreamEvent
		wantActions RiskDurableActions
		wantErr     error
	}{
		{
			name: "observe records without durable action",
			event: RiskStreamEvent{
				Recommendation: RiskDecisionObserve,
			},
		},
		{
			name: "confirmed client token disables token and user",
			event: RiskStreamEvent{
				Source:         RiskSourceEdgeGuard,
				Recommendation: RiskDecisionDisable,
				UserID:         10,
				TokenID:        11,
				Metadata: map[string]any{
					"evidence_level":              "confirmed",
					"causality":                   "client_token",
					"client_token_action_allowed": true,
				},
			},
			wantActions: RiskDurableActions{DisableToken: true, DisableUser: true},
		},
		{
			name: "confirmed upstream key disables channel",
			event: RiskStreamEvent{
				Source:         RiskSourceUpstreamPolicy,
				Recommendation: RiskDecisionDisable,
				ChannelID:      12,
				Metadata: map[string]any{
					"evidence_level": "confirmed",
					"causality":      "upstream_key",
				},
			},
			wantActions: RiskDurableActions{DisableChannel: true},
		},
		{
			name: "unconfirmed evidence is rejected",
			event: RiskStreamEvent{
				Source:         RiskSourceEdgeGuard,
				Recommendation: RiskDecisionDisable,
				Metadata: map[string]any{
					"evidence_level": "suspected",
					"causality":      "client_token",
				},
			},
			wantErr: ErrRiskStreamDecisionRejected,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, actions, err := DecideKKAIRiskStreamEvent(test.event)
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.event.Recommendation, decision)
			require.Equal(t, test.wantActions, actions)
		})
	}
}

func TestRiskActionOutboxHandlerDeliversCacheInvalidationAndNotification(t *testing.T) {
	var invalidatedUser int
	var invalidatedTokens int
	var refreshedChannels int
	var notified int
	handler := RiskActionOutboxHandler{
		InvalidateUser: func(userID int) error {
			invalidatedUser = userID
			return nil
		},
		InvalidateUserTokens: func(userID int) error {
			invalidatedTokens = userID
			return nil
		},
		RefreshChannels: func() { refreshedChannels++ },
		Notify: func(riskActionOutboxPayload) error {
			notified++
			return nil
		},
	}
	payload, err := common.Marshal(riskActionOutboxPayload{
		IncidentID:      1,
		EventID:         "event-1",
		UserID:          10,
		TokenDisabled:   true,
		UserDisabled:    true,
		ChannelDisabled: true,
	})
	require.NoError(t, err)

	err = handler.Handle(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)})
	require.NoError(t, err)
	require.Equal(t, 10, invalidatedUser)
	require.Equal(t, 10, invalidatedTokens)
	require.Equal(t, 1, refreshedChannels)
	require.Equal(t, 1, notified)
}

func TestRiskActionOutboxHandlerRetriesFailedDelivery(t *testing.T) {
	expected := errors.New("redis unavailable")
	handler := RiskActionOutboxHandler{
		InvalidateUser:       func(int) error { return expected },
		InvalidateUserTokens: func(int) error { return nil },
		RefreshChannels:      func() {},
		Notify:               func(riskActionOutboxPayload) error { return nil },
	}
	payload, err := common.Marshal(riskActionOutboxPayload{EventID: "event-1", UserID: 10, UserDisabled: true})
	require.NoError(t, err)

	err = handler.Handle(context.Background(), model.KKAIOutboxEvent{Payload: string(payload)})
	require.ErrorIs(t, err, expected)
}
