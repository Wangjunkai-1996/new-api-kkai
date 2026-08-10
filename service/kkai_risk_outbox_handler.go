package service

import (
	"context"
	"fmt"
	"strings"

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
			notify := notifyRootUser
			if payload.UserDisableSkipped {
				notify = notifyRootUserHighPriority
			}
			return notify(
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
		if payload.UserCacheInvalidationRequired || payload.TokenCacheInvalidationRequired {
			invalidateUser, invalidateTokens := riskActionCacheInvalidationTargets(
				riskActionsFromIncident(incident.ActionTaken),
				incident.UserDisableSkipped,
			)
			if payload.UserCacheInvalidationRequired != invalidateUser ||
				payload.TokenCacheInvalidationRequired != invalidateTokens {
				return ErrRiskActionInvalidInput
			}
		}
		switch incident.Source {
		case RiskSourceUpstreamPolicy:
			var metadata map[string]any
			if err := common.UnmarshalJsonStr(incident.Metadata, &metadata); err != nil ||
				payload.ChannelDisabled ||
				!validUpstreamClientPolicyAuthorization(
					incident.Source,
					incident.Decision,
					incident.UserID,
					incident.TokenID,
					incident.ChannelID,
					incident.RuleVersion,
					incident.TokenFingerprint,
					incident.UpstreamKeyFingerprint,
					metadata,
				) ||
				(incident.TokenID == 0 && payload.TokenDisabled) {
				return ErrRiskActionInvalidInput
			}
		case RiskSourceEdgeGuard, RiskSourceManualReview:
		default:
			return ErrRiskActionInvalidInput
		}
	}
	if payload.UserCacheInvalidationRequired && payload.UserDisableSkipped {
		return ErrRiskActionInvalidInput
	}
	if payload.UserCacheInvalidationRequired || payload.UserDisabled {
		if payload.UserID <= 0 {
			return ErrRiskActionInvalidInput
		}
		if err := h.InvalidateUser(payload.UserID); err != nil {
			return err
		}
	}
	if payload.TokenCacheInvalidationRequired || payload.TokenDisabled || payload.UserDisabled {
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
	return payload.TokenDisabled || payload.UserDisabled || payload.UserDisableSkipped || payload.ChannelDisabled ||
		payload.UserCacheInvalidationRequired || payload.TokenCacheInvalidationRequired
}

func riskActionsFromIncident(actionTaken string) RiskDurableActions {
	var actions RiskDurableActions
	for _, action := range strings.Split(actionTaken, ",") {
		switch strings.TrimSpace(action) {
		case "disable_token":
			actions.DisableToken = true
		case "disable_user":
			actions.DisableUser = true
		case "disable_channel":
			actions.DisableChannel = true
		}
	}
	return actions
}
