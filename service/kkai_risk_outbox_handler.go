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
