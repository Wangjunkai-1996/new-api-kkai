package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	policyIncidentFingerprintPrefix = "sha256:"
	PolicyIncidentErrorDetected     = "policy_incident_detected"
	policyIncidentMetadataMaxBytes  = 512
)

var (
	ErrNilPolicyIncidentEvent        = errors.New("policy incident event is nil")
	ErrPolicyIncidentEventAppendOnly = errors.New("policy incident events are append-only")
	ErrInvalidPolicyIncidentMetadata = errors.New("invalid policy incident metadata")
	policyIncidentCaseIDPattern      = regexp.MustCompile(`^policy-[0-9]{1,19}-[0-9a-f]{16}$`)
	policyIncidentMetadataKeys       = map[string]struct{}{
		"case_id":                     {},
		"request_body_sha256":         {},
		"request_body_bytes":          {},
		"client_token_action_allowed": {},
	}
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
	ActionTaken            string    `json:"action_taken" gorm:"type:varchar(255);default:''"`
	ActionResult           string    `json:"action_result" gorm:"type:varchar(255);default:''"`
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
	e.ErrorCode = PolicyIncidentErrorDetected
	e.ErrorMessage = PolicyIncidentErrorDetected
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
	if len(raw) > policyIncidentMetadataMaxBytes {
		return nil, ErrInvalidPolicyIncidentMetadata
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, ErrInvalidPolicyIncidentMetadata
	}
	sanitized, err := sanitizePolicyIncidentMetadata(decoded)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(sanitized)
	if err != nil || len(encoded) > policyIncidentMetadataMaxBytes {
		return nil, ErrInvalidPolicyIncidentMetadata
	}
	return JSONValue(encoded), nil
}

func sanitizePolicyIncidentMetadata(decoded map[string]any) (map[string]any, error) {
	if decoded == nil {
		return nil, ErrInvalidPolicyIncidentMetadata
	}
	sanitized := make(map[string]any, len(decoded))
	for key, value := range decoded {
		if _, allowed := policyIncidentMetadataKeys[key]; !allowed {
			return nil, ErrInvalidPolicyIncidentMetadata
		}
		switch key {
		case "case_id":
			caseID, ok := value.(string)
			if !ok || !policyIncidentCaseIDPattern.MatchString(caseID) {
				return nil, ErrInvalidPolicyIncidentMetadata
			}
			sanitized[key] = caseID
		case "request_body_sha256":
			digest, ok := value.(string)
			if !ok || !isSHA256Hex(digest) {
				return nil, ErrInvalidPolicyIncidentMetadata
			}
			sanitized[key] = digest
		case "request_body_bytes":
			bytes, ok := policyIncidentMetadataInteger(value)
			if !ok || bytes < 0 {
				return nil, ErrInvalidPolicyIncidentMetadata
			}
			sanitized[key] = bytes
		case "client_token_action_allowed":
			allowed, ok := value.(bool)
			if !ok {
				return nil, ErrInvalidPolicyIncidentMetadata
			}
			sanitized[key] = allowed
		}
	}
	_, hasDigest := sanitized["request_body_sha256"]
	_, hasBytes := sanitized["request_body_bytes"]
	if hasDigest != hasBytes {
		return nil, ErrInvalidPolicyIncidentMetadata
	}
	return sanitized, nil
}

func policyIncidentMetadataInteger(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		if number > math.MaxInt64 || number < math.MinInt64 || math.Trunc(number) != number {
			return 0, false
		}
		return int64(number), true
	default:
		return 0, false
	}
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
