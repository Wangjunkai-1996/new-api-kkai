package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const maxRiskStreamPayloadBytes = 16 * 1024

func SignRiskStreamEvent(event RiskStreamEvent, secret string) (string, string, error) {
	if len(secret) < 32 {
		return "", "", ErrRiskStreamNoSecret
	}
	if err := validateRiskStreamEvent(event); err != nil {
		return "", "", err
	}
	payload, err := common.Marshal(event)
	if err != nil || len(payload) > maxRiskStreamPayloadBytes {
		return "", "", ErrRiskStreamInvalidEvent
	}
	signature := hex.EncodeToString(riskStreamMAC(payload, []byte(secret)))
	return string(payload), signature, nil
}

func VerifyRiskStreamMessage(
	payload string,
	signature string,
	secret string,
	now time.Time,
	maxAge time.Duration,
	maxFuture time.Duration,
) (*RiskStreamEvent, error) {
	if len(secret) < 32 {
		return nil, ErrRiskStreamNoSecret
	}
	if len(payload) == 0 || len(payload) > maxRiskStreamPayloadBytes || maxAge <= 0 || maxFuture < 0 {
		return nil, ErrRiskStreamInvalidEvent
	}
	providedHex := strings.TrimSpace(signature)
	if len(providedHex) != sha256.Size*2 {
		return nil, ErrRiskStreamInvalidSignature
	}
	provided, err := hex.DecodeString(providedHex)
	if err != nil || !hmac.Equal(riskStreamMAC([]byte(payload), []byte(secret)), provided) {
		return nil, ErrRiskStreamInvalidSignature
	}

	var fields map[string]json.RawMessage
	if err := common.UnmarshalJsonStr(payload, &fields); err != nil {
		return nil, ErrRiskStreamInvalidEvent
	}
	for field := range fields {
		switch field {
		case "event_id", "source", "occurred_at", "nonce", "request_id", "user_id", "token_id", "channel_id",
			"model", "rule_version", "evidence_sha256", "token_fingerprint", "upstream_key_fingerprint",
			"recommendation", "metadata":
		default:
			return nil, ErrRiskStreamInvalidEvent
		}
	}

	var event RiskStreamEvent
	if err := common.UnmarshalJsonStr(payload, &event); err != nil {
		return nil, ErrRiskStreamInvalidEvent
	}
	if err := validateRiskStreamEvent(event); err != nil {
		return nil, err
	}
	occurredAt := time.Unix(event.OccurredAt, 0)
	if occurredAt.Before(now.Add(-maxAge)) || occurredAt.After(now.Add(maxFuture)) {
		return nil, ErrRiskStreamStaleEvent
	}
	return &event, nil
}

func validateRiskStreamEvent(event RiskStreamEvent) error {
	if event.EventID != strings.TrimSpace(event.EventID) || !riskEventIDPattern.MatchString(event.EventID) || event.OccurredAt <= 0 {
		return ErrRiskStreamInvalidEvent
	}
	if event.Nonce != strings.TrimSpace(event.Nonce) || !riskEventIDPattern.MatchString(event.Nonce) || len(event.Nonce) < 16 {
		return ErrRiskStreamInvalidEvent
	}
	if event.Source != strings.TrimSpace(event.Source) || event.Recommendation != strings.TrimSpace(event.Recommendation) {
		return ErrRiskStreamInvalidEvent
	}
	if event.UserID < 0 || event.TokenID < 0 || event.ChannelID < 0 {
		return ErrRiskStreamInvalidEvent
	}
	if len(event.RequestID) > 64 || len(event.ModelName) > 128 || len(event.RuleVersion) > 64 {
		return ErrRiskStreamInvalidEvent
	}
	switch event.Source {
	case RiskSourceEdgeGuard, RiskSourceUpstreamPolicy:
	default:
		return ErrRiskStreamInvalidEvent
	}
	if _, err := normalizeRiskDigest(event.EvidenceSHA256, true); err != nil {
		return ErrRiskStreamInvalidEvent
	}
	if _, err := normalizeRiskDigest(event.TokenFingerprint, false); err != nil {
		return ErrRiskStreamInvalidEvent
	}
	if _, err := normalizeRiskDigest(event.UpstreamKeyFingerprint, false); err != nil {
		return ErrRiskStreamInvalidEvent
	}
	if _, err := normalizeRiskMetadata(event.Metadata); err != nil {
		return ErrRiskStreamInvalidEvent
	}
	switch event.Recommendation {
	case RiskDecisionObserve, RiskDecisionReject, RiskDecisionDisable:
	default:
		return ErrRiskStreamInvalidEvent
	}
	return nil
}

func riskStreamMAC(payload []byte, secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}
