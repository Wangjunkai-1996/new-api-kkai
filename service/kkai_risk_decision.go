package service

import "fmt"

func DecideKKAIRiskStreamEvent(event RiskStreamEvent) (string, RiskDurableActions, error) {
	if event.Recommendation == RiskDecisionObserve || event.Recommendation == RiskDecisionReject {
		return event.Recommendation, RiskDurableActions{}, nil
	}
	if event.Recommendation != RiskDecisionDisable {
		return "", RiskDurableActions{}, fmt.Errorf("%w: unsupported recommendation", ErrRiskStreamDecisionRejected)
	}
	evidenceLevel, _ := event.Metadata["evidence_level"].(string)
	causality, _ := event.Metadata["causality"].(string)
	if evidenceLevel != "confirmed" {
		return "", RiskDurableActions{}, fmt.Errorf("%w: evidence is not confirmed", ErrRiskStreamDecisionRejected)
	}

	switch causality {
	case "client_token":
		allowed, _ := event.Metadata["client_token_action_allowed"].(bool)
		if !allowed || event.UserID <= 0 || event.TokenID <= 0 {
			return "", RiskDurableActions{}, fmt.Errorf("%w: client token action is not authorized", ErrRiskStreamDecisionRejected)
		}
		return event.Recommendation, RiskDurableActions{DisableToken: true, DisableUser: true}, nil
	case "upstream_key":
		if event.Source != RiskSourceUpstreamPolicy || event.ChannelID <= 0 {
			return "", RiskDurableActions{}, fmt.Errorf("%w: upstream channel action is not authorized", ErrRiskStreamDecisionRejected)
		}
		return event.Recommendation, RiskDurableActions{DisableChannel: true}, nil
	default:
		return "", RiskDurableActions{}, fmt.Errorf("%w: unsupported causality", ErrRiskStreamDecisionRejected)
	}
}
