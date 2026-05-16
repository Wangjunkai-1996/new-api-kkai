package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	policyIncidentFingerprintPrefix = "sha256:"
	PolicyIncidentMetadataRedacted  = "[redacted]"
)

var (
	ErrNilPolicyIncidentEvent        = errors.New("policy incident event is nil")
	ErrPolicyIncidentEventAppendOnly = errors.New("policy incident events are append-only")
)

// PolicyIncidentEvent is an append-only audit record for cyber policy actions.
// It stores only request metadata and upstream key fingerprints, never raw keys
// or request prompts.
type PolicyIncidentEvent struct {
	Id                     int       `json:"id" gorm:"primaryKey"`
	RequestId              string    `json:"request_id" gorm:"type:varchar(64);index;default:''"`
	UserId                 int       `json:"user_id" gorm:"index;default:0"`
	TokenId                int       `json:"token_id" gorm:"index;default:0"`
	TokenName              string    `json:"token_name" gorm:"type:varchar(128);default:''"`
	ModelName              string    `json:"model_name" gorm:"type:varchar(128);index;default:''"`
	ChannelId              int       `json:"channel_id" gorm:"index;default:0"`
	ChannelType            int       `json:"channel_type" gorm:"index;default:0"`
	UpstreamKeyFingerprint string    `json:"upstream_key_fingerprint" gorm:"type:varchar(80);index;default:''"`
	StatusCode             int       `json:"status_code" gorm:"default:0"`
	ErrorCode              string    `json:"error_code" gorm:"type:varchar(128);default:''"`
	ErrorMessage           string    `json:"error_message" gorm:"type:text"`
	EvidenceLevel          string    `json:"evidence_level" gorm:"type:varchar(32);default:''"`
	Causality              string    `json:"causality" gorm:"type:varchar(64);default:''"`
	ActionTaken            string    `json:"action_taken" gorm:"type:varchar(64);default:''"`
	ActionResult           string    `json:"action_result" gorm:"type:varchar(64);default:''"`
	Metadata               JSONValue `json:"metadata" gorm:"type:json"`
	CreatedAt              int64     `json:"created_at" gorm:"bigint;index"`
}

type PolicyIncidentNotificationPayload struct {
	IncidentId             int       `json:"incident_id"`
	RequestId              string    `json:"request_id"`
	UserId                 int       `json:"user_id"`
	TokenId                int       `json:"token_id"`
	TokenName              string    `json:"token_name"`
	ModelName              string    `json:"model_name"`
	ChannelId              int       `json:"channel_id"`
	ChannelType            int       `json:"channel_type"`
	UpstreamKeyFingerprint string    `json:"upstream_key_fingerprint"`
	StatusCode             int       `json:"status_code"`
	ErrorCode              string    `json:"error_code"`
	ErrorMessage           string    `json:"error_message"`
	EvidenceLevel          string    `json:"evidence_level"`
	Causality              string    `json:"causality"`
	ActionTaken            string    `json:"action_taken"`
	ActionResult           string    `json:"action_result"`
	Metadata               JSONValue `json:"metadata"`
	CreatedAt              int64     `json:"created_at"`
}

func (PolicyIncidentEvent) TableName() string {
	return "policy_incident_events"
}

func (e *PolicyIncidentEvent) BeforeCreate(tx *gorm.DB) error {
	if e.CreatedAt == 0 {
		e.CreatedAt = common.GetTimestamp()
	}
	e.ErrorMessage = redactPolicyIncidentText(e.ErrorMessage, e.UpstreamKeyFingerprint)
	e.UpstreamKeyFingerprint = NormalizePolicyIncidentKeyFingerprint(e.UpstreamKeyFingerprint)

	metadata, err := NormalizePolicyIncidentMetadata(e.Metadata)
	if err != nil {
		return err
	}
	e.Metadata = metadata
	return nil
}

func (e *PolicyIncidentEvent) BeforeUpdate(tx *gorm.DB) error {
	return ErrPolicyIncidentEventAppendOnly
}

func (e *PolicyIncidentEvent) BeforeDelete(tx *gorm.DB) error {
	return ErrPolicyIncidentEventAppendOnly
}

func (e *PolicyIncidentEvent) Insert() error {
	return InsertPolicyIncidentEvent(e)
}

func InsertPolicyIncidentEvent(event *PolicyIncidentEvent) error {
	if event == nil {
		return ErrNilPolicyIncidentEvent
	}
	return DB.Create(event).Error
}

func (e *PolicyIncidentEvent) SetMetadata(metadata any) error {
	normalized, err := NormalizePolicyIncidentMetadata(metadata)
	if err != nil {
		return err
	}
	e.Metadata = normalized
	return nil
}

func (e *PolicyIncidentEvent) AdminNotificationPayload() PolicyIncidentNotificationPayload {
	if e == nil {
		return PolicyIncidentNotificationPayload{}
	}
	return PolicyIncidentNotificationPayload{
		IncidentId:             e.Id,
		RequestId:              e.RequestId,
		UserId:                 e.UserId,
		TokenId:                e.TokenId,
		TokenName:              e.TokenName,
		ModelName:              e.ModelName,
		ChannelId:              e.ChannelId,
		ChannelType:            e.ChannelType,
		UpstreamKeyFingerprint: e.UpstreamKeyFingerprint,
		StatusCode:             e.StatusCode,
		ErrorCode:              e.ErrorCode,
		ErrorMessage:           e.ErrorMessage,
		EvidenceLevel:          e.EvidenceLevel,
		Causality:              e.Causality,
		ActionTaken:            e.ActionTaken,
		ActionResult:           e.ActionResult,
		Metadata:               cloneJSONValue(e.Metadata),
		CreatedAt:              e.CreatedAt,
	}
}

func FingerprintPolicyIncidentUpstreamKey(upstreamKey string) string {
	upstreamKey = strings.TrimSpace(upstreamKey)
	if upstreamKey == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(upstreamKey))
	return policyIncidentFingerprintPrefix + hex.EncodeToString(sum[:])
}

func NormalizePolicyIncidentKeyFingerprint(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	lowerValue := strings.ToLower(value)
	if strings.HasPrefix(lowerValue, policyIncidentFingerprintPrefix) {
		hexPart := strings.TrimPrefix(lowerValue, policyIncidentFingerprintPrefix)
		if isSHA256Hex(hexPart) {
			return policyIncidentFingerprintPrefix + hexPart
		}
	}
	if isSHA256Hex(lowerValue) {
		return policyIncidentFingerprintPrefix + lowerValue
	}
	return FingerprintPolicyIncidentUpstreamKey(value)
}

func NormalizePolicyIncidentMetadata(metadata any) (JSONValue, error) {
	if metadata == nil {
		return nil, nil
	}

	switch v := metadata.(type) {
	case JSONValue:
		return normalizePolicyIncidentMetadataBytes([]byte(v))
	case json.RawMessage:
		return normalizePolicyIncidentMetadataBytes([]byte(v))
	case []byte:
		return normalizePolicyIncidentMetadataBytes(v)
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, nil
		}
		return normalizePolicyIncidentMetadataBytes([]byte(v))
	default:
		b, err := json.Marshal(metadata)
		if err != nil {
			return nil, err
		}
		return normalizePolicyIncidentMetadataBytes(b)
	}
}

func normalizePolicyIncidentMetadataBytes(raw []byte) (JSONValue, error) {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return nil, nil
	}

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		encoded, marshalErr := json.Marshal(string(raw))
		if marshalErr != nil {
			return nil, marshalErr
		}
		return JSONValue(encoded), nil
	}

	sanitized := sanitizePolicyIncidentMetadataValue(decoded)
	encoded, err := json.Marshal(sanitized)
	if err != nil {
		return nil, err
	}
	return JSONValue(encoded), nil
}

func sanitizePolicyIncidentMetadataValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		sanitized := make(map[string]any, len(v))
		for key, item := range v {
			switch {
			case isPolicyIncidentPromptMetadataKey(key):
				sanitized[key] = PolicyIncidentMetadataRedacted
			case isPolicyIncidentUpstreamKeyMetadataKey(key):
				sanitized[key] = fingerprintMetadataValue(item)
			default:
				sanitized[key] = sanitizePolicyIncidentMetadataValue(item)
			}
		}
		return sanitized
	case []any:
		sanitized := make([]any, len(v))
		for i, item := range v {
			sanitized[i] = sanitizePolicyIncidentMetadataValue(item)
		}
		return sanitized
	default:
		return v
	}
}

func fingerprintMetadataValue(value any) string {
	switch v := value.(type) {
	case string:
		return NormalizePolicyIncidentKeyFingerprint(v)
	default:
		return FingerprintPolicyIncidentUpstreamKey(fmt.Sprint(v))
	}
}

func isPolicyIncidentPromptMetadataKey(key string) bool {
	switch normalizePolicyIncidentMetadataKey(key) {
	case "prompt", "messages", "message", "input", "inputs":
		return true
	default:
		return false
	}
}

func isPolicyIncidentUpstreamKeyMetadataKey(key string) bool {
	switch normalizePolicyIncidentMetadataKey(key) {
	case "upstreamkey", "upstreamapikey", "apikey", "authorization":
		return true
	default:
		return false
	}
}

func normalizePolicyIncidentMetadataKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, "_", "")
	key = strings.ReplaceAll(key, "-", "")
	key = strings.ReplaceAll(key, " ", "")
	return key
}

func isSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func cloneJSONValue(value JSONValue) JSONValue {
	if value == nil {
		return nil
	}
	cloned := make([]byte, len(value))
	copy(cloned, value)
	return JSONValue(cloned)
}

func redactPolicyIncidentText(text string, upstreamKey string) string {
	text = common.MaskSensitiveInfo(text)
	upstreamKey = strings.TrimSpace(upstreamKey)
	if upstreamKey == "" {
		return text
	}
	return strings.ReplaceAll(text, upstreamKey, PolicyIncidentMetadataRedacted)
}
