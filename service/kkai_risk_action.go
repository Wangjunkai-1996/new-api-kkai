package service

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

type RiskActionService struct {
	db                   *gorm.DB
	now                  func() time.Time
	invalidateUser       func(int) error
	invalidateUserTokens func(int) error
}

func NewRiskActionService(db *gorm.DB) *RiskActionService {
	return &RiskActionService{
		db:                   db,
		now:                  time.Now,
		invalidateUser:       model.InvalidateUserCache,
		invalidateUserTokens: model.InvalidateUserTokensCache,
	}
}

func (s *RiskActionService) Apply(ctx context.Context, input RiskActionInput) (*RiskActionResult, error) {
	if s == nil || s.db == nil || s.now == nil {
		return nil, ErrRiskActionInvalidInput
	}
	normalized, err := normalizeRiskActionInput(input)
	if err != nil {
		return nil, err
	}

	result := &RiskActionResult{}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := s.now().Unix()
		incident := newKKAIPolicyIncident(normalized, now)
		created, err := createKKAIPolicyIncident(tx, incident)
		if err != nil {
			return err
		}
		if !created {
			return loadKKAIRiskActionReplay(tx, normalized, result)
		}

		actions, actionResults, err := applyKKAIRiskMutations(tx, normalized, result)
		if err != nil {
			return err
		}
		if err := persistKKAIRiskActionResult(tx, incident, result, actions, actionResults, now); err != nil {
			return err
		}
		if err := enqueueKKAIRiskActionOutbox(tx, incident, normalized.Actions, now); err != nil {
			return err
		}
		result.IncidentID = incident.ID
		return nil
	})
	if err != nil {
		return &RiskActionResult{CooldownIdentityValidated: result.CooldownIdentityValidated}, err
	}
	s.invalidateCommittedRiskCaches(ctx, normalized.UserID, normalized.Actions, result)
	return result, nil
}

func (s *RiskActionService) invalidateCommittedRiskCaches(ctx context.Context, userID int, actions RiskDurableActions, result *RiskActionResult) {
	if s == nil || result == nil || userID <= 0 {
		return
	}
	invalidateUser, invalidateTokens := riskActionCacheInvalidationTargets(actions, result.UserDisableSkipped)
	if invalidateUser && s.invalidateUser != nil {
		if err := s.invalidateUser(userID); err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("KKAI committed user cache invalidation failed: %v", err))
		}
	}
	if invalidateTokens && s.invalidateUserTokens != nil {
		if err := s.invalidateUserTokens(userID); err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("KKAI committed token cache invalidation failed: %v", err))
		}
	}
}

func riskActionCacheInvalidationTargets(actions RiskDurableActions, userDisableSkipped bool) (bool, bool) {
	invalidateUser := actions.DisableUser && !userDisableSkipped
	return invalidateUser, actions.DisableToken || invalidateUser
}

func newKKAIPolicyIncident(input *normalizedRiskAction, now int64) *model.KKAIPolicyIncident {
	return &model.KKAIPolicyIncident{
		EventID:                input.EventID,
		InputSHA256:            input.InputSHA256,
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
		Metadata:               input.MetadataJSON,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
}
