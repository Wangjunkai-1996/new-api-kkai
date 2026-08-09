package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var (
	riskEventIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	riskHexPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	riskSecretPattern  = regexp.MustCompile(`(?i)(\bbearer\s+|\bsk-)[a-z0-9._~+/=-]{6,}`)
)

func normalizeRiskActionInput(input RiskActionInput) (*normalizedRiskAction, error) {
	input.EventID = strings.TrimSpace(input.EventID)
	input.Source = strings.TrimSpace(input.Source)
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.ModelName = strings.TrimSpace(input.ModelName)
	input.RuleVersion = strings.TrimSpace(input.RuleVersion)
	input.Decision = strings.TrimSpace(input.Decision)
	if !riskEventIDPattern.MatchString(input.EventID) || input.OccurredAt <= 0 {
		return nil, ErrRiskActionInvalidInput
	}
	if len(input.RequestID) > 64 || len(input.ModelName) > 128 || len(input.RuleVersion) > 64 {
		return nil, ErrRiskActionInvalidInput
	}
	if !validRiskSource(input.Source) || !validRiskDecision(input.Decision) || !validRiskActions(input) {
		return nil, ErrRiskActionInvalidInput
	}

	evidence, err := normalizeRiskDigest(input.EvidenceSHA256, true)
	if err != nil {
		return nil, err
	}
	tokenFingerprint, err := normalizeRiskDigest(input.TokenFingerprint, false)
	if err != nil {
		return nil, err
	}
	upstreamFingerprint, err := normalizeRiskDigest(input.UpstreamKeyFingerprint, false)
	if err != nil {
		return nil, err
	}
	input.EvidenceSHA256 = evidence
	input.TokenFingerprint = prefixedRiskDigest(tokenFingerprint)
	input.UpstreamKeyFingerprint = prefixedRiskDigest(upstreamFingerprint)

	metadataJSON, err := normalizeRiskMetadata(input.Metadata)
	if err != nil {
		return nil, err
	}
	inputSHA256, err := riskActionInputSHA256(input, metadataJSON)
	if err != nil {
		return nil, err
	}
	return &normalizedRiskAction{
		RiskActionInput: input,
		MetadataJSON:    metadataJSON,
		InputSHA256:     inputSHA256,
	}, nil
}

func validRiskSource(source string) bool {
	return source == RiskSourceEdgeGuard || source == RiskSourceUpstreamPolicy || source == RiskSourceManualReview
}

func validRiskDecision(decision string) bool {
	return decision == RiskDecisionObserve || decision == RiskDecisionReject || decision == RiskDecisionDisable
}

func validRiskActions(input RiskActionInput) bool {
	if input.Actions.DisableToken && (input.TokenID <= 0 || input.UserID <= 0 || input.TokenFingerprint == "") {
		return false
	}
	if input.Actions.DisableUser && input.UserID <= 0 {
		return false
	}
	return !input.Actions.DisableChannel || (input.ChannelID > 0 && input.UpstreamKeyFingerprint != "")
}

func riskActionInputSHA256(input RiskActionInput, metadataJSON string) (string, error) {
	canonical, err := common.Marshal(struct {
		RiskActionInput
		Metadata json.RawMessage `json:"metadata"`
	}{
		RiskActionInput: input,
		Metadata:        json.RawMessage(metadataJSON),
	})
	if err != nil {
		return "", ErrRiskActionInvalidInput
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeRiskDigest(value string, required bool) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "sha256:")
	if value == "" && !required {
		return "", nil
	}
	if !riskHexPattern.MatchString(value) {
		return "", ErrRiskActionInvalidInput
	}
	return value, nil
}

func prefixedRiskDigest(value string) string {
	if value == "" {
		return ""
	}
	return "sha256:" + value
}

func normalizeRiskMetadata(metadata map[string]any) (string, error) {
	if metadata == nil {
		return "{}", nil
	}
	allowed := map[string]struct{}{
		"case_id": {}, "causality": {}, "client_token_action_allowed": {},
		"evidence_level": {}, "request_body_bytes": {}, "request_body_sha256": {},
		"rule_id": {}, "upstream_action_allowed": {}, "conversation_scope": {},
		"conversation_scope_source": {}, "conversation_scope_fingerprint": {},
	}
	normalized := make(map[string]any, len(metadata))
	for key, value := range metadata {
		if _, ok := allowed[key]; !ok {
			return "", ErrRiskActionInvalidInput
		}
		if err := normalizeRiskMetadataValue(normalized, key, value); err != nil {
			return "", err
		}
	}
	_, hasDigest := normalized["request_body_sha256"]
	_, hasBytes := normalized["request_body_bytes"]
	if hasDigest != hasBytes {
		return "", ErrRiskActionInvalidInput
	}
	encoded, err := common.Marshal(normalized)
	if err != nil || len(encoded) > 2048 {
		return "", ErrRiskActionInvalidInput
	}
	return string(encoded), nil
}

func normalizeRiskMetadataValue(normalized map[string]any, key string, value any) error {
	switch key {
	case "case_id", "causality", "evidence_level", "rule_id", "conversation_scope", "conversation_scope_source":
		text, ok := value.(string)
		text = strings.TrimSpace(text)
		if !ok || !riskEventIDPattern.MatchString(text) || riskSecretPattern.MatchString(text) {
			return ErrRiskActionInvalidInput
		}
		normalized[key] = text
	case "client_token_action_allowed", "upstream_action_allowed":
		flag, ok := value.(bool)
		if !ok {
			return ErrRiskActionInvalidInput
		}
		normalized[key] = flag
	case "request_body_bytes":
		count, ok := riskMetadataInteger(value)
		if !ok || count < 0 {
			return ErrRiskActionInvalidInput
		}
		normalized[key] = count
	case "request_body_sha256":
		text, ok := value.(string)
		if !ok {
			return ErrRiskActionInvalidInput
		}
		digest, err := normalizeRiskDigest(text, true)
		if err != nil {
			return err
		}
		normalized[key] = digest
	case "conversation_scope_fingerprint":
		text, ok := value.(string)
		if !ok {
			return ErrRiskActionInvalidInput
		}
		digest, err := normalizeRiskDigest(text, true)
		if err != nil {
			return err
		}
		normalized[key] = digest
	}
	return nil
}

func riskMetadataInteger(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int32:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number < math.MinInt64 || number > math.MaxInt64 {
			return 0, false
		}
		return int64(number), true
	default:
		return 0, false
	}
}

func RiskFingerprint(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(secret))
	return fmt.Sprintf("sha256:%x", sum[:])
}
