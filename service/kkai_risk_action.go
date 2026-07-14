package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

type RiskActionService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewRiskActionService(db *gorm.DB) *RiskActionService {
	return &RiskActionService{db: db, now: time.Now}
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
		if err := enqueueKKAIRiskActionOutbox(tx, incident, now); err != nil {
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
