package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func createKKAIPolicyIncident(tx *gorm.DB, incident *model.KKAIPolicyIncident) (bool, error) {
	create := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(incident)
	return create.RowsAffected > 0, create.Error
}

func loadKKAIRiskActionReplay(tx *gorm.DB, input *normalizedRiskAction, result *RiskActionResult) error {
	var stored model.KKAIPolicyIncident
	if err := tx.Where("event_id = ?", input.EventID).First(&stored).Error; err != nil {
		return err
	}
	if stored.InputSHA256 != input.InputSHA256 {
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

func persistKKAIRiskActionResult(
	tx *gorm.DB,
	incident *model.KKAIPolicyIncident,
	result *RiskActionResult,
	actions []string,
	actionResults []string,
	now int64,
) error {
	incident.ActionTaken = strings.Join(actions, ",")
	incident.ActionResult = strings.Join(actionResults, ",")
	incident.TokenDisabled = result.TokenDisabled
	incident.UserDisabled = result.UserDisabled
	incident.UserDisableSkipped = result.UserDisableSkipped
	incident.ChannelDisabled = result.ChannelDisabled
	incident.UpdatedAt = now
	return tx.Model(&model.KKAIPolicyIncident{}).
		Where("id = ?", incident.ID).
		Updates(map[string]any{
			"action_taken":         incident.ActionTaken,
			"action_result":        incident.ActionResult,
			"token_disabled":       incident.TokenDisabled,
			"user_disabled":        incident.UserDisabled,
			"user_disable_skipped": incident.UserDisableSkipped,
			"channel_disabled":     incident.ChannelDisabled,
			"updated_at":           incident.UpdatedAt,
		}).Error
}

func enqueueKKAIRiskActionOutbox(tx *gorm.DB, incident *model.KKAIPolicyIncident, now int64) error {
	payload, err := common.Marshal(riskActionOutboxPayload{
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
	return tx.Create(&model.KKAIOutboxEvent{
		EventKey:    "risk-action:" + incident.EventID,
		Topic:       KKAIOutboxTopicRiskActionCommitted,
		AggregateID: incident.EventID,
		Payload:     string(payload),
		Status:      model.KKAIOutboxStatusPending,
		AvailableAt: now,
		LastError:   "",
		CreatedAt:   now,
	}).Error
}
