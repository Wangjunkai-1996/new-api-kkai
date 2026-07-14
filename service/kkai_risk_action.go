package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	RiskSourceEdgeGuard      = "edge_guard"
	RiskSourceUpstreamPolicy = "upstream_policy"
	RiskSourceManualReview   = "manual_review"

	RiskDecisionObserve = "observe"
	RiskDecisionReject  = "reject"
	RiskDecisionDisable = "disable"

	KKAIOutboxTopicRiskActionCommitted = "kkai.risk.action.committed"
)

var (
	ErrRiskActionInvalidInput        = errors.New("invalid risk action input")
	ErrRiskActionIdempotencyConflict = errors.New("risk action idempotency conflict")
	ErrRiskActionTokenNotFound       = errors.New("risk action token not found")
	ErrRiskActionUserNotFound        = errors.New("risk action user not found")
	ErrRiskActionChannelNotFound     = errors.New("risk action channel not found")

	riskEventIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	riskHexPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	riskSecretPattern  = regexp.MustCompile(`(?i)(\bbearer\s+|\bsk-)[a-z0-9._~+/=-]{6,}`)
)

type RiskDurableActions struct {
	DisableToken   bool `json:"disable_token"`
	DisableUser    bool `json:"disable_user"`
	DisableChannel bool `json:"disable_channel"`
}

type RiskActionInput struct {
	EventID                string
	Source                 string
	OccurredAt             int64
	RequestID              string
	UserID                 int
	TokenID                int
	ChannelID              int
	ModelName              string
	RuleVersion            string
	EvidenceSHA256         string
	TokenFingerprint       string
	UpstreamKeyFingerprint string
	Decision               string
	Metadata               map[string]any
	Actions                RiskDurableActions
}

type RiskActionResult struct {
	IncidentID         int64
	Replayed           bool
	TokenDisabled      bool
	UserDisabled       bool
	UserDisableSkipped bool
	ChannelDisabled    bool
}

type RiskActionService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewRiskActionService(db *gorm.DB) *RiskActionService {
	return &RiskActionService{db: db, now: time.Now}
}

type normalizedRiskAction struct {
	RiskActionInput
	MetadataJSON string
	InputSHA256  string
}

type riskActionOutboxPayload struct {
	IncidentID         int64  `json:"incident_id"`
	EventID            string `json:"event_id"`
	RequestID          string `json:"request_id"`
	UserID             int    `json:"user_id"`
	TokenID            int    `json:"token_id"`
	ChannelID          int    `json:"channel_id"`
	TokenDisabled      bool   `json:"token_disabled"`
	UserDisabled       bool   `json:"user_disabled"`
	UserDisableSkipped bool   `json:"user_disable_skipped"`
	ChannelDisabled    bool   `json:"channel_disabled"`
}

func (s *RiskActionService) Apply(ctx context.Context, input RiskActionInput) (*RiskActionResult, error) {
	if s == nil || s.db == nil {
		return nil, ErrRiskActionInvalidInput
	}
	normalized, err := normalizeRiskActionInput(input)
	if err != nil {
		return nil, err
	}

	result := &RiskActionResult{}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := s.now().Unix()
		incident := &model.KKAIPolicyIncident{
			EventID:                normalized.EventID,
			InputSHA256:            normalized.InputSHA256,
			Source:                 normalized.Source,
			OccurredAt:             normalized.OccurredAt,
			RequestID:              normalized.RequestID,
			UserID:                 normalized.UserID,
			TokenID:                normalized.TokenID,
			ChannelID:              normalized.ChannelID,
			ModelName:              normalized.ModelName,
			RuleVersion:            normalized.RuleVersion,
			EvidenceSHA256:         normalized.EvidenceSHA256,
			TokenFingerprint:       normalized.TokenFingerprint,
			UpstreamKeyFingerprint: normalized.UpstreamKeyFingerprint,
			Decision:               normalized.Decision,
			Metadata:               normalized.MetadataJSON,
			CreatedAt:              now,
			UpdatedAt:              now,
		}

		create := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(incident)
		if create.Error != nil {
			return create.Error
		}
		if create.RowsAffected == 0 {
			var stored model.KKAIPolicyIncident
			if err := tx.Where("event_id = ?", normalized.EventID).First(&stored).Error; err != nil {
				return err
			}
			if stored.InputSHA256 != normalized.InputSHA256 {
				return ErrRiskActionIdempotencyConflict
			}
			result.IncidentID = stored.ID
			result.Replayed = true
			result.TokenDisabled = stored.TokenDisabled
			result.UserDisabled = stored.UserDisabled
			result.UserDisableSkipped = stored.UserDisableSkipped
			result.ChannelDisabled = stored.ChannelDisabled
			return nil
		}

		actions := make([]string, 0, 3)
		actionResults := make([]string, 0, 3)
		if normalized.Actions.DisableToken {
			changed, err := disableRiskToken(tx, normalized.TokenID, normalized.UserID)
			if err != nil {
				return err
			}
			result.TokenDisabled = changed
			actions = append(actions, "disable_token")
			actionResults = append(actionResults, changedResult(changed))
		}
		if normalized.Actions.DisableUser {
			changed, skipped, err := disableRiskUser(tx, normalized.UserID)
			if err != nil {
				return err
			}
			result.UserDisabled = changed
			result.UserDisableSkipped = skipped
			actions = append(actions, "disable_user")
			if skipped {
				actionResults = append(actionResults, "skipped_privileged")
			} else {
				actionResults = append(actionResults, changedResult(changed))
			}
		}
		if normalized.Actions.DisableChannel {
			changed, err := disableRiskChannel(tx, normalized.ChannelID)
			if err != nil {
				return err
			}
			result.ChannelDisabled = changed
			actions = append(actions, "disable_channel")
			actionResults = append(actionResults, changedResult(changed))
		}
		if len(actions) == 0 {
			actions = append(actions, "record_incident")
			actionResults = append(actionResults, "recorded")
		}

		incident.ActionTaken = strings.Join(actions, ",")
		incident.ActionResult = strings.Join(actionResults, ",")
		incident.TokenDisabled = result.TokenDisabled
		incident.UserDisabled = result.UserDisabled
		incident.UserDisableSkipped = result.UserDisableSkipped
		incident.ChannelDisabled = result.ChannelDisabled
		incident.UpdatedAt = now
		if err := tx.Model(&model.KKAIPolicyIncident{}).
			Where("id = ?", incident.ID).
			Updates(map[string]any{
				"action_taken":         incident.ActionTaken,
				"action_result":        incident.ActionResult,
				"token_disabled":       incident.TokenDisabled,
				"user_disabled":        incident.UserDisabled,
				"user_disable_skipped": incident.UserDisableSkipped,
				"channel_disabled":     incident.ChannelDisabled,
				"updated_at":           incident.UpdatedAt,
			}).Error; err != nil {
			return err
		}

		outboxPayload, err := common.Marshal(riskActionOutboxPayload{
			IncidentID:         incident.ID,
			EventID:            incident.EventID,
			RequestID:          incident.RequestID,
			UserID:             incident.UserID,
			TokenID:            incident.TokenID,
			ChannelID:          incident.ChannelID,
			TokenDisabled:      incident.TokenDisabled,
			UserDisabled:       incident.UserDisabled,
			UserDisableSkipped: incident.UserDisableSkipped,
			ChannelDisabled:    incident.ChannelDisabled,
		})
		if err != nil {
			return err
		}
		outbox := &model.KKAIOutboxEvent{
			EventKey:    "risk-action:" + incident.EventID,
			Topic:       KKAIOutboxTopicRiskActionCommitted,
			AggregateID: incident.EventID,
			Payload:     string(outboxPayload),
			Status:      model.KKAIOutboxStatusPending,
			AvailableAt: now,
			LastError:   "",
			CreatedAt:   now,
		}
		if err := tx.Create(outbox).Error; err != nil {
			return err
		}

		result.IncidentID = incident.ID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

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
	switch input.Source {
	case RiskSourceEdgeGuard, RiskSourceUpstreamPolicy, RiskSourceManualReview:
	default:
		return nil, ErrRiskActionInvalidInput
	}
	switch input.Decision {
	case RiskDecisionObserve, RiskDecisionReject, RiskDecisionDisable:
	default:
		return nil, ErrRiskActionInvalidInput
	}
	if input.Actions.DisableToken && (input.TokenID <= 0 || input.UserID <= 0) {
		return nil, ErrRiskActionInvalidInput
	}
	if input.Actions.DisableUser && input.UserID <= 0 {
		return nil, ErrRiskActionInvalidInput
	}
	if input.Actions.DisableChannel && input.ChannelID <= 0 {
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
	if tokenFingerprint != "" {
		input.TokenFingerprint = "sha256:" + tokenFingerprint
	}
	if upstreamFingerprint != "" {
		input.UpstreamKeyFingerprint = "sha256:" + upstreamFingerprint
	}

	metadataJSON, err := normalizeRiskMetadata(input.Metadata)
	if err != nil {
		return nil, err
	}
	canonical, err := common.Marshal(struct {
		EventID                string             `json:"event_id"`
		Source                 string             `json:"source"`
		OccurredAt             int64              `json:"occurred_at"`
		RequestID              string             `json:"request_id"`
		UserID                 int                `json:"user_id"`
		TokenID                int                `json:"token_id"`
		ChannelID              int                `json:"channel_id"`
		ModelName              string             `json:"model_name"`
		RuleVersion            string             `json:"rule_version"`
		EvidenceSHA256         string             `json:"evidence_sha256"`
		TokenFingerprint       string             `json:"token_fingerprint"`
		UpstreamKeyFingerprint string             `json:"upstream_key_fingerprint"`
		Decision               string             `json:"decision"`
		Metadata               json.RawMessage    `json:"metadata"`
		Actions                RiskDurableActions `json:"actions"`
	}{
		EventID:                input.EventID,
		Source:                 input.Source,
		OccurredAt:             input.OccurredAt,
		RequestID:              input.RequestID,
		UserID:                 input.UserID,
		TokenID:                input.TokenID,
		ChannelID:              input.ChannelID,
		ModelName:              input.ModelName,
		RuleVersion:            input.RuleVersion,
		EvidenceSHA256:         input.EvidenceSHA256,
		TokenFingerprint:       input.TokenFingerprint,
		UpstreamKeyFingerprint: input.UpstreamKeyFingerprint,
		Decision:               input.Decision,
		Metadata:               json.RawMessage(metadataJSON),
		Actions:                input.Actions,
	})
	if err != nil {
		return nil, ErrRiskActionInvalidInput
	}
	sum := sha256.Sum256(canonical)
	return &normalizedRiskAction{
		RiskActionInput: input,
		MetadataJSON:    metadataJSON,
		InputSHA256:     hex.EncodeToString(sum[:]),
	}, nil
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

func normalizeRiskMetadata(metadata map[string]any) (string, error) {
	if metadata == nil {
		return "{}", nil
	}
	allowed := map[string]struct{}{
		"case_id":                     {},
		"causality":                   {},
		"client_token_action_allowed": {},
		"evidence_level":              {},
		"request_body_bytes":          {},
		"request_body_sha256":         {},
		"rule_id":                     {},
	}
	normalized := make(map[string]any, len(metadata))
	for key, value := range metadata {
		if _, ok := allowed[key]; !ok {
			return "", ErrRiskActionInvalidInput
		}
		switch key {
		case "case_id", "causality", "evidence_level", "rule_id":
			text, ok := value.(string)
			text = strings.TrimSpace(text)
			if !ok || !riskEventIDPattern.MatchString(text) || riskSecretPattern.MatchString(text) {
				return "", ErrRiskActionInvalidInput
			}
			normalized[key] = text
		case "client_token_action_allowed":
			flag, ok := value.(bool)
			if !ok {
				return "", ErrRiskActionInvalidInput
			}
			normalized[key] = flag
		case "request_body_bytes":
			count, ok := riskMetadataInteger(value)
			if !ok || count < 0 {
				return "", ErrRiskActionInvalidInput
			}
			normalized[key] = count
		case "request_body_sha256":
			text, ok := value.(string)
			if !ok {
				return "", ErrRiskActionInvalidInput
			}
			digest, err := normalizeRiskDigest(text, true)
			if err != nil {
				return "", err
			}
			normalized[key] = digest
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

func riskMetadataInteger(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int32:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		converted := int64(number)
		return converted, float64(converted) == number
	default:
		return 0, false
	}
}

func disableRiskToken(tx *gorm.DB, tokenID int, userID int) (bool, error) {
	var token model.Token
	query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", tokenID)
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	if err := query.First(&token).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, ErrRiskActionTokenNotFound
		}
		return false, err
	}
	if token.Status == common.TokenStatusDisabled {
		return false, nil
	}
	if err := tx.Model(&model.Token{}).Where("id = ?", token.Id).
		Update("status", common.TokenStatusDisabled).Error; err != nil {
		return false, err
	}
	return true, nil
}

func disableRiskUser(tx *gorm.DB, userID int) (bool, bool, error) {
	var user model.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, false, ErrRiskActionUserNotFound
		}
		return false, false, err
	}
	if user.Role >= common.RoleAdminUser {
		return false, true, nil
	}
	if user.Status == common.UserStatusDisabled {
		return false, false, nil
	}
	if err := tx.Model(&model.User{}).Where("id = ?", user.Id).
		Update("status", common.UserStatusDisabled).Error; err != nil {
		return false, false, err
	}
	return true, false, nil
}

func disableRiskChannel(tx *gorm.DB, channelID int) (bool, error) {
	var channel model.Channel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", channelID).First(&channel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, ErrRiskActionChannelNotFound
		}
		return false, err
	}
	if channel.Status == common.ChannelStatusAutoDisabled {
		return false, nil
	}
	if err := tx.Model(&model.Channel{}).Where("id = ?", channel.Id).
		Update("status", common.ChannelStatusAutoDisabled).Error; err != nil {
		return false, err
	}
	return true, nil
}

func changedResult(changed bool) string {
	if changed {
		return "disabled"
	}
	return "already_disabled"
}

func RiskFingerprint(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(secret))
	return fmt.Sprintf("sha256:%x", sum[:])
}
