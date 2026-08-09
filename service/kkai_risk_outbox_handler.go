package service

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type RiskActionOutboxHandler struct {
	InvalidateUser       func(int) error
	InvalidateUserTokens func(int) error
	RefreshChannels      func()
	LookupIncident       func(context.Context, int64, string) (model.KKAIPolicyIncident, error)
	Notify               func(riskActionOutboxPayload) error
}

func NewRiskActionOutboxHandler() RiskActionOutboxHandler {
	return RiskActionOutboxHandler{
		InvalidateUser:       model.InvalidateUserCache,
		InvalidateUserTokens: model.InvalidateUserTokensCache,
		RefreshChannels: func() {
			model.InitChannelCache()
			ResetProxyClientCache()
		},
		LookupIncident: func(ctx context.Context, incidentID int64, eventID string) (model.KKAIPolicyIncident, error) {
			var incident model.KKAIPolicyIncident
			if model.DB == nil {
				return incident, ErrKKAIOutboxInvalidConfiguration
			}
			err := model.DB.WithContext(ctx).
				Where("id = ? AND event_id = ?", incidentID, eventID).
				First(&incident).Error
			return incident, err
		},
		Notify: func(payload riskActionOutboxPayload) error {
			return notifyRootUser(
				"policy_incident",
				"Policy incident action committed",
				fmt.Sprintf(
					"event_id=%s incident_id=%d user_id=%d token_id=%d channel_id=%d token_disabled=%t user_disabled=%t channel_disabled=%t",
					payload.EventID,
					payload.IncidentID,
					payload.UserID,
					payload.TokenID,
					payload.ChannelID,
					payload.TokenDisabled,
					payload.UserDisabled,
					payload.ChannelDisabled,
				),
			)
		},
	}
}

func (h RiskActionOutboxHandler) Handle(ctx context.Context, event model.KKAIOutboxEvent) error {
	if h.InvalidateUser == nil || h.InvalidateUserTokens == nil || h.RefreshChannels == nil || h.Notify == nil {
		return ErrKKAIOutboxInvalidConfiguration
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var payload riskActionOutboxPayload
	if err := common.UnmarshalJsonStr(event.Payload, &payload); err != nil || !riskEventIDPattern.MatchString(payload.EventID) {
		return ErrRiskActionInvalidInput
	}
	if riskActionOutboxHasDurableFlags(payload) {
		if h.LookupIncident == nil {
			return ErrKKAIOutboxInvalidConfiguration
		}
		incident, err := h.LookupIncident(ctx, payload.IncidentID, payload.EventID)
		if err != nil {
			return err
		}
		if incident.ID != payload.IncidentID || incident.EventID != payload.EventID {
			return ErrRiskActionInvalidInput
		}
		if payload.RequestID != incident.RequestID ||
			payload.UserID != incident.UserID ||
			payload.TokenID != incident.TokenID ||
			payload.ChannelID != incident.ChannelID {
			return ErrRiskActionInvalidInput
		}
		if payload.TokenDisabled != incident.TokenDisabled ||
			payload.UserDisabled != incident.UserDisabled ||
			payload.UserDisableSkipped != incident.UserDisableSkipped ||
			payload.ChannelDisabled != incident.ChannelDisabled {
			return ErrRiskActionInvalidInput
		}
		switch incident.Source {
		case RiskSourceUpstreamPolicy:
			payload.TokenDisabled = false
			payload.UserDisabled = false
			payload.UserDisableSkipped = false
			payload.ChannelDisabled = false
		case RiskSourceEdgeGuard, RiskSourceManualReview:
		default:
			return ErrRiskActionInvalidInput
		}
	}
	if payload.UserDisabled {
		if payload.UserID <= 0 {
			return ErrRiskActionInvalidInput
		}
		if err := h.InvalidateUser(payload.UserID); err != nil {
			return err
		}
	}
	if payload.TokenDisabled || payload.UserDisabled {
		if payload.UserID <= 0 {
			return ErrRiskActionInvalidInput
		}
		if err := h.InvalidateUserTokens(payload.UserID); err != nil {
			return err
		}
	}
	if payload.ChannelDisabled {
		h.RefreshChannels()
	}
	return h.Notify(payload)
}

func riskActionOutboxHasDurableFlags(payload riskActionOutboxPayload) bool {
	return payload.TokenDisabled || payload.UserDisabled || payload.UserDisableSkipped || payload.ChannelDisabled
}
