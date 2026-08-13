package service

import (
	"fmt"
)

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
	case KKAIPolicyCausalityClientToken:
		// Upstream Cyber policy incidents are warning-only: reject and audit the
		// request, then let the local guard apply a short per-key cooldown.
		if event.Source == RiskSourceUpstreamPolicy {
			return RiskDecisionReject, RiskDurableActions{}, nil
		}
		allowed, _ := event.Metadata["client_token_action_allowed"].(bool)
		if !allowed || event.UserID <= 0 || event.TokenID < 0 {
			return "", RiskDurableActions{}, fmt.Errorf("%w: client token action is not authorized", ErrRiskStreamDecisionRejected)
		}
		actions := RiskDurableActions{DisableUser: true}
		if event.TokenID > 0 {
			actions.DisableToken = true
		}
		return event.Recommendation, actions, nil
	case KKAIPolicyCausalityUpstreamKey:
		return RiskDecisionReject, RiskDurableActions{}, nil
	case KKAIPolicyCausalityAmbiguous:
		return RiskDecisionReject, RiskDurableActions{}, nil
	default:
		return "", RiskDurableActions{}, fmt.Errorf("%w: unsupported causality", ErrRiskStreamDecisionRejected)
	}
}

func validUpstreamClientPolicyAuthorization(
	source string,
	decision string,
	userID int,
	tokenID int,
	channelID int,
	ruleVersion string,
	tokenFingerprint string,
	upstreamKeyFingerprint string,
	metadata map[string]any,
) bool {
	if source != RiskSourceUpstreamPolicy || decision != RiskDecisionDisable || userID <= 0 || tokenID < 0 || channelID <= 0 {
		return false
	}
	if ruleVersion == "" || upstreamKeyFingerprint == "" {
		return false
	}
	if (tokenID > 0) != (tokenFingerprint != "") {
		return false
	}
	evidenceLevel, _ := metadata["evidence_level"].(string)
	causality, _ := metadata["causality"].(string)
	actionAllowed, _ := metadata["client_token_action_allowed"].(bool)
	markerConfirmed, _ := metadata["client_policy_marker_confirmed"].(bool)
	clientAuthMode, _ := metadata["client_auth_mode"].(string)
	ruleID, _ := metadata["rule_id"].(string)
	originalStatus, ok := riskMetadataInteger(metadata["original_status_code"])
	validOriginalStatus := ok && originalStatus >= 100 && originalStatus <= 599
	validClientIdentity := clientAuthMode == kkaiPolicyClientAuthBearer && tokenID > 0 && tokenFingerprint != "" ||
		clientAuthMode == kkaiPolicyClientAuthPlayground && tokenID == 0 && tokenFingerprint == ""
	return evidenceLevel == "confirmed" &&
		causality == KKAIPolicyCausalityClientToken &&
		actionAllowed && markerConfirmed && validOriginalStatus &&
		validClientIdentity && ruleID == ruleVersion
}

func validUpstreamClientCooldownAuthorization(
	source string,
	decision string,
	userID int,
	tokenID int,
	channelID int,
	ruleVersion string,
	tokenFingerprint string,
	upstreamKeyFingerprint string,
	metadata map[string]any,
) bool {
	if source != RiskSourceUpstreamPolicy || decision != RiskDecisionReject ||
		userID <= 0 || tokenID <= 0 || channelID <= 0 {
		return false
	}
	if ruleVersion == "" || tokenFingerprint == "" || upstreamKeyFingerprint == "" {
		return false
	}
	evidenceLevel, _ := metadata["evidence_level"].(string)
	causality, _ := metadata["causality"].(string)
	cooldownAllowed, _ := metadata["client_token_cooldown_allowed"].(bool)
	markerConfirmed, _ := metadata["client_policy_marker_confirmed"].(bool)
	clientAuthMode, _ := metadata["client_auth_mode"].(string)
	ruleID, _ := metadata["rule_id"].(string)
	originalStatus, ok := riskMetadataInteger(metadata["original_status_code"])
	return evidenceLevel == "confirmed" &&
		causality == KKAIPolicyCausalityClientToken && cooldownAllowed && markerConfirmed &&
		clientAuthMode == kkaiPolicyClientAuthBearer && ok &&
		originalStatus >= 100 && originalStatus <= 599 && ruleID == ruleVersion
}
