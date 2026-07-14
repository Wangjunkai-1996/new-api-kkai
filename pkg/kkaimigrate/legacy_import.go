package kkaimigrate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type legacyPolicyIncident struct {
	ID                     int64  `gorm:"column:id"`
	RequestID              string `gorm:"column:request_id"`
	UserID                 int    `gorm:"column:user_id"`
	TokenID                int    `gorm:"column:token_id"`
	ModelName              string `gorm:"column:model_name"`
	ChannelID              int    `gorm:"column:channel_id"`
	UpstreamKeyFingerprint string `gorm:"column:upstream_key_fingerprint"`
	EvidenceLevel          string `gorm:"column:evidence_level"`
	Causality              string `gorm:"column:causality"`
	ActionTaken            string `gorm:"column:action_taken"`
	ActionResult           string `gorm:"column:action_result"`
	Metadata               string `gorm:"column:metadata"`
	CreatedAt              int64  `gorm:"column:created_at"`
}

func importLegacyPolicyIncidents(db *gorm.DB) error {
	if !db.Migrator().HasTable("policy_incident_events") {
		return nil
	}
	var rows []legacyPolicyIncident
	if err := db.Table("policy_incident_events").Order("id ASC").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		metadata := normalizedLegacyJSON(row.Metadata)
		canonical, _ := json.Marshal(row)
		evidence := sha256Hex([]byte(fmt.Sprintf("legacy-policy:%d:%s", row.ID, row.RequestID)))
		incident := model.KKAIPolicyIncident{
			EventID:                fmt.Sprintf("legacy-policy-incident:%d", row.ID),
			InputSHA256:            sha256Hex(canonical),
			Source:                 "legacy_import",
			OccurredAt:             row.CreatedAt,
			RequestID:              row.RequestID,
			UserID:                 row.UserID,
			TokenID:                row.TokenID,
			ChannelID:              row.ChannelID,
			ModelName:              row.ModelName,
			RuleVersion:            "legacy",
			EvidenceSHA256:         evidence,
			UpstreamKeyFingerprint: row.UpstreamKeyFingerprint,
			Decision:               "legacy_record",
			Metadata:               metadata,
			ActionTaken:            row.ActionTaken,
			ActionResult:           row.ActionResult,
			TokenDisabled:          strings.Contains(row.ActionTaken, "token_disabled"),
			UserDisabled:           strings.Contains(row.ActionTaken, "user_disabled"),
			UserDisableSkipped:     strings.Contains(row.ActionTaken, "user_disable_skipped"),
			ChannelDisabled:        strings.Contains(row.ActionTaken, "upstream_isolated"),
			CreatedAt:              row.CreatedAt,
			UpdatedAt:              row.CreatedAt,
		}
		if incident.OccurredAt <= 0 {
			incident.OccurredAt = 1
			incident.CreatedAt = 1
			incident.UpdatedAt = 1
		}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&incident).Error; err != nil {
			return err
		}
	}
	return nil
}

type legacyBalanceAdjustment struct {
	OperationID         string  `gorm:"column:operation_id"`
	UserID              int     `gorm:"column:user_id"`
	Delta               int64   `gorm:"column:delta"`
	Reason              string  `gorm:"column:reason"`
	Metadata            string  `gorm:"column:metadata"`
	PayloadSHA256       string  `gorm:"column:payload_sha256"`
	OriginalOperationID *string `gorm:"column:original_operation_id"`
	BalanceBefore       int64   `gorm:"column:balance_before"`
	BalanceAfter        int64   `gorm:"column:balance_after"`
	CreatedAt           int64   `gorm:"column:created_at"`
}

func importLegacyBalanceAdjustments(db *gorm.DB) error {
	if !db.Migrator().HasTable("internal_balance_adjustments") {
		return nil
	}
	var rows []legacyBalanceAdjustment
	if err := db.Table("internal_balance_adjustments").Order("id ASC").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		adjustment := model.KKAIInternalBalanceAdjustment{
			OperationID:         row.OperationID,
			UserID:              row.UserID,
			Delta:               row.Delta,
			Reason:              row.Reason,
			Metadata:            normalizedLegacyJSON(row.Metadata),
			PayloadSHA256:       row.PayloadSHA256,
			OriginalOperationID: row.OriginalOperationID,
			BalanceBefore:       row.BalanceBefore,
			BalanceAfter:        row.BalanceAfter,
			CreatedAt:           row.CreatedAt,
		}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&adjustment).Error; err != nil {
			return err
		}
	}
	return nil
}

func normalizedLegacyJSON(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 2048 {
		return "{}"
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return "{}"
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
